// Package ipsec 实现 IPsec ESP 封装/解封装 (RFC 4303)。
// 支持 AES-CBC-256/HMAC-SHA2-256-128 加密套件。
package ipsec

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/bufferpool"
	"github.com/iniwex5/vowifi-go/engine/crypto"
)

const (
	ESPHeaderBytes          = 8
	AESCBCIVBytes           = 16
	ESPTrailerBudgetBytes   = 17
	DefaultIntegrityCheckBytes = 16
	DefaultAntiReplayWindow    = 64
	IPProtoESP                  = 50
)

type SA struct {
	SPI      uint32
	EncKey   []byte
	IntegKey []byte
	NextSeq  uint64
	Replay   *AntiReplayWindow
	Packets  uint64
	Bytes    uint64
}

func NewSA(spi uint32, encKey, integKey []byte) *SA {
	return &SA{
		SPI:      spi,
		EncKey:   encKey,
		IntegKey: integKey,
		NextSeq:  1,
		Replay:   NewAntiReplayWindow(DefaultAntiReplayWindow),
	}
}

func (sa *SA) AllocateSequence() (uint64, error) {
	seq := sa.NextSeq
	if seq > uint64(^uint32(0)) {
		return 0, fmt.Errorf("esp: sequence exhausted")
	}
	sa.NextSeq++
	return seq, nil
}

func (sa *SA) Record(bytes int) {
	sa.Packets++
	sa.Bytes += uint64(bytes)
}

type AntiReplayWindow struct {
	Size       uint16
	highestSeq uint64
	bitmap     uint64
}

func NewAntiReplayWindow(size uint16) *AntiReplayWindow {
	if size > 64 {
		size = 64
	}
	if size < 1 {
		size = 1
	}
	return &AntiReplayWindow{Size: size}
}

func (w *AntiReplayWindow) Accept(seq uint64) (accepted bool, reason string) {
	if seq == 0 {
		return false, "rejected_invalid_sequence"
	}

	if w.highestSeq == 0 || seq > w.highestSeq {
		delta := seq - w.highestSeq
		if delta >= uint64(w.Size) {
			w.bitmap = 1
		} else {
			w.bitmap = (w.bitmap << delta) | 1
		}
		w.bitmap &= w.windowMask()
		w.highestSeq = seq
		return true, "accepted_new_highest"
	}

	offset := w.highestSeq - seq
	if offset >= uint64(w.Size) {
		return false, "rejected_too_old"
	}

	mask := uint64(1) << offset
	if w.bitmap&mask != 0 {
		return false, "rejected_duplicate"
	}

	w.bitmap |= mask
	return true, "accepted_within_window"
}

func (w *AntiReplayWindow) HighestSequence() uint64 { return w.highestSeq }

func (w *AntiReplayWindow) windowMask() uint64 {
	if w.Size >= 64 {
		return ^uint64(0)
	}
	return (uint64(1) << w.Size) - 1
}

type EspPacketMetadata struct {
	SPI              uint32
	SequenceNumber   uint64
	ProtectedBytes   int
	OuterFrameBytes  int
	HeaderBytes      int
}

func ParseEspFrameMetadata(frame []byte) (*EspPacketMetadata, error) {
	if len(frame) < ESPHeaderBytes {
		return nil, fmt.Errorf("esp: frame too short: %d bytes", len(frame))
	}
	return &EspPacketMetadata{
		SPI:             binary.BigEndian.Uint32(frame[0:4]),
		SequenceNumber:  uint64(binary.BigEndian.Uint32(frame[4:8])),
		ProtectedBytes:  len(frame) - ESPHeaderBytes,
		OuterFrameBytes: len(frame),
		HeaderBytes:     ESPHeaderBytes,
	}, nil
}

func Encrypt(saIdentifier uint32, seqNum uint64, innerPacket []byte, nextHeader uint8, encKey, integKey []byte) ([]byte, error) {
	if saIdentifier == 0 {
		return nil, fmt.Errorf("esp: invalid SA identifier")
	}
	if seqNum == 0 || seqNum > uint64(^uint32(0)) {
		return nil, fmt.Errorf("esp: sequence exhausted")
	}

	iv, err := crypto.GenerateRandom(AESCBCIVBytes)
	if err != nil {
		return nil, fmt.Errorf("esp: random IV: %w", err)
	}

	padded := buildEspPlaintext(innerPacket, nextHeader)
	ciphertext, err := aesCbcEncrypt(encKey, iv, padded)
	if err != nil {
		return nil, fmt.Errorf("esp: encrypt: %w", err)
	}

	frameSize := ESPHeaderBytes + AESCBCIVBytes + len(ciphertext) + DefaultIntegrityCheckBytes
	buf := bufferpool.Get(frameSize)
	frame := buf[:frameSize]

	off := 0
	binary.BigEndian.PutUint32(frame[off:off+4], saIdentifier)
	off += 4
	binary.BigEndian.PutUint32(frame[off:off+4], uint32(seqNum))
	off += 4
	copy(frame[off:off+AESCBCIVBytes], iv)
	off += AESCBCIVBytes
	copy(frame[off:], ciphertext)
	off += len(ciphertext)

	integrityData := frame[:off]
	icv := espIntegrityTag(integKey, integrityData)
	copy(frame[off:off+len(icv)], icv)

	result := make([]byte, frameSize)
	copy(result, frame)
	bufferpool.Put(buf)
	return result, nil
}

func Decrypt(frame []byte, encKey, integKey []byte) ([]byte, uint8, error) {
	if _, err := ParseEspFrameMetadata(frame); err != nil {
		return nil, 0, err
	}

	icvLen := DefaultIntegrityCheckBytes
	minLen := ESPHeaderBytes + AESCBCIVBytes + icvLen
	if len(frame) < minLen {
		return nil, 0, fmt.Errorf("esp: frame too short: %d < %d", len(frame), minLen)
	}

	signedLen := len(frame) - icvLen
	signed := frame[:signedLen]
	receivedICV := frame[signedLen:]

	expectedICV := espIntegrityTag(integKey, signed)
	if subtle.ConstantTimeCompare(expectedICV, receivedICV) != 1 {
		return nil, 0, fmt.Errorf("esp: integrity check failed")
	}

	ivStart := ESPHeaderBytes
	ivEnd := ivStart + AESCBCIVBytes
	iv := frame[ivStart:ivEnd]
	ciphertext := frame[ivEnd:signedLen]

	plaintext, err := aesCbcDecrypt(encKey, iv, ciphertext)
	if err != nil {
		return nil, 0, fmt.Errorf("esp: decrypt: %w", err)
	}

	innerPacket, nextHeader, err := stripEspPadding(plaintext)
	if err != nil {
		return nil, 0, fmt.Errorf("esp: strip padding: %w", err)
	}

	return innerPacket, nextHeader, nil
}

func EspPadLen(payloadLen int) int {
	base := payloadLen + 2
	return (aes.BlockSize - base%aes.BlockSize) % aes.BlockSize
}

func EspEncryptedLen(payloadLen int) int {
	padLen := EspPadLen(payloadLen)
	plainLen := payloadLen + padLen + 2
	return ESPHeaderBytes + AESCBCIVBytes + plainLen + DefaultIntegrityCheckBytes
}

func buildEspPlaintext(innerPacket []byte, nextHeader uint8) []byte {
	padLen := EspPadLen(len(innerPacket))
	total := len(innerPacket) + padLen + 2
	out := make([]byte, total)
	copy(out, innerPacket)
	off := len(innerPacket)
	for i := 0; i < padLen; i++ {
		out[off+i] = byte(i + 1)
	}
	off += padLen
	out[off] = byte(padLen)
	out[off+1] = nextHeader
	return out
}

func stripEspPadding(plaintext []byte) ([]byte, uint8, error) {
	if len(plaintext) < 2 {
		return nil, 0, fmt.Errorf("esp: plaintext too short for ESP trailer")
	}
	nextHeader := plaintext[len(plaintext)-1]
	padLen := int(plaintext[len(plaintext)-2])
	payloadEnd := len(plaintext) - 2
	if padLen > payloadEnd {
		return nil, 0, fmt.Errorf("esp: pad_len %d exceeds payload", padLen)
	}
	payloadEnd -= padLen
	padding := plaintext[payloadEnd : payloadEnd+padLen]
	for i := 0; i < padLen; i++ {
		if padding[i] != byte(i+1) {
			return nil, 0, fmt.Errorf("esp: invalid padding at offset %d: got %d want %d", i, padding[i], i+1)
		}
	}
	innerPacket := make([]byte, payloadEnd)
	copy(innerPacket, plaintext[:payloadEnd])
	return innerPacket, nextHeader, nil
}

func espIntegrityTag(key, message []byte) []byte {
	hmac := crypto.HmacSha256(key, message)
	return hmac[:DefaultIntegrityCheckBytes]
}

func aesCbcEncrypt(key, iv, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(plaintext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("aes-cbc: plaintext not block-aligned: %d", len(plaintext))
	}
	ciphertext := make([]byte, len(plaintext))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, plaintext)
	return ciphertext, nil
}

func aesCbcDecrypt(key, iv, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("aes-cbc: ciphertext not block-aligned: %d", len(ciphertext))
	}
	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)
	return plaintext, nil
}
