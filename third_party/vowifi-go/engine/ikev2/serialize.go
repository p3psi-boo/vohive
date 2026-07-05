package ikev2

import (
	"crypto/sha1"
	"encoding/binary"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/eap"
)

// serializeIKE 将 IKE 消息序列化为 RFC 7296 §3.1 标准格式。
func serializeIKE(msg *IKEMessage) []byte {
	payloads := encodePayloads(msg)
	totalLen := ikeHeaderLen
	for i := range payloads {
		totalLen += len(payloads[i])
	}

	buf := make([]byte, totalLen)
	binary.BigEndian.PutUint64(buf[0:8], msg.InitiatorSPI)
	binary.BigEndian.PutUint64(buf[8:16], msg.ResponderSPI)
	buf[16] = byte(msg.NextPayload)
	buf[17] = msg.Version
	buf[18] = byte(msg.ExchangeType)
	buf[19] = msg.Flags
	binary.BigEndian.PutUint32(buf[20:24], msg.MessageID)
	binary.BigEndian.PutUint32(buf[24:28], uint32(totalLen))
	off := 28
	for _, p := range payloads {
		copy(buf[off:], p)
		off += len(p)
	}
	return buf
}

// encodePayloads 按 next_payload 链编码所有负载。
func encodePayloads(msg *IKEMessage) [][]byte {
	var out [][]byte
	for i, p := range msg.Payloads {
		next := byte(0)
		if i < len(msg.Payloads)-1 {
			next = byte(msg.Payloads[i+1].Type)
		}
		payloadLen := 4 + len(p.Data)
		buf := make([]byte, payloadLen)
		buf[0] = next
		buf[1] = 0
		binary.BigEndian.PutUint16(buf[2:4], uint16(payloadLen))
		copy(buf[4:], p.Data)
		out = append(out, buf)
	}
	return out
}

// parseIKE 解析 RFC 7296 标准 IKE 消息。
func parseIKE(raw []byte) (*IKEMessage, error) {
	if len(raw) < ikeHeaderLen {
		return nil, fmt.Errorf("ike message too short: %d bytes", len(raw))
	}
	if raw[17] != 0x20 {
		return nil, fmt.Errorf("unsupported IKE version: 0x%02x", raw[17])
	}
	declaredLen := int(binary.BigEndian.Uint32(raw[24:28]))
	if declaredLen != len(raw) {
		return nil, fmt.Errorf("declared length %d != actual %d", declaredLen, len(raw))
	}
	msg := &IKEMessage{
		InitiatorSPI: binary.BigEndian.Uint64(raw[0:8]),
		ResponderSPI: binary.BigEndian.Uint64(raw[8:16]),
		NextPayload:  NextPayload(raw[16]),
		Version:      raw[17],
		ExchangeType: ExchangeType(raw[18]),
		Flags:        raw[19],
		MessageID:    binary.BigEndian.Uint32(raw[20:24]),
	}
	off := ikeHeaderLen
	next := msg.NextPayload
	for off < len(raw) && next != 0 {
		if off+4 > len(raw) {
			return nil, fmt.Errorf("truncated payload header at %d", off)
		}
		plen := int(binary.BigEndian.Uint16(raw[off+2 : off+4]))
		if plen < 4 || off+plen > len(raw) {
			return nil, fmt.Errorf("invalid payload length %d at %d", plen, off)
		}
		msg.Payloads = append(msg.Payloads, Payload{
			Type: next,
			Data: append([]byte{}, raw[off+4:off+plen]...),
		})
		next = NextPayload(raw[off])
		off += plen
	}
	return msg, nil
}

// serializeEncryptedIKE 构造标准 IKEv2 SK 加密 payload (RFC 7296 §3.14)。
func serializeEncryptedIKE(msg *IKEMessage, encKey, integKey []byte, initSPI, respSPI uint64, msgID uint32) []byte {
	plain := serializeIKE(msg)
	blockSize := 16
	padLen := blockSize - (len(plain)%blockSize) + blockSize
	paddedLen := 1 + len(plain) + padLen
	plaintext := make([]byte, paddedLen)
	copy(plaintext[1:], plain)
	for i := 0; i < padLen; i++ {
		plaintext[1+len(plain)+i] = byte(i)
	}
	plaintext[0] = byte(msg.NextPayload)

	iv := mustGenerateRandom(16)
	block, _ := crypto.NewAESCipher(encKey)
	mode := crypto.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(plaintext))
	mode.CryptBlocks(ciphertext, plaintext)

	encPayload := append(iv, ciphertext...)
	payloadLen := 4 + len(encPayload)
	skBuf := make([]byte, payloadLen)
	skBuf[0] = byte(msg.NextPayload)
	skBuf[1] = 0
	binary.BigEndian.PutUint16(skBuf[2:4], uint16(payloadLen))
	copy(skBuf[4:], encPayload)

	hdr := make([]byte, ikeHeaderLen)
	binary.BigEndian.PutUint64(hdr[0:8], initSPI)
	binary.BigEndian.PutUint64(hdr[8:16], respSPI)
	binary.BigEndian.PutUint32(hdr[20:24], msgID)
	hdr[16] = byte(PayloadSK)
	hdr[17] = 0x20
	hdr[18] = byte(msg.ExchangeType)
	hdr[19] = msg.Flags
	totalLen := ikeHeaderLen + len(skBuf)
	binary.BigEndian.PutUint32(hdr[24:28], uint32(totalLen))

	integData := append(hdr, skBuf...)
	icv := crypto.HmacSha256(integKey, integData)[:12]

	result := make([]byte, len(hdr)+len(skBuf)+len(icv))
	copy(result[:len(hdr)], hdr)
	copy(result[len(hdr):len(hdr)+len(skBuf)], skBuf)
	copy(result[len(hdr)+len(skBuf):], icv)
	return result
}

// parseEncryptedIKE 解析标准 IKEv2 SK 加密 payload。
func parseEncryptedIKE(raw []byte, encKey, integKey []byte, initSPI, respSPI uint64) (*IKEMessage, error) {
	if len(raw) < ikeHeaderLen+12 {
		return nil, fmt.Errorf("encrypted ike too short: %d", len(raw))
	}
	if _, err := parseIKE(raw); err != nil {
		return nil, fmt.Errorf("header: %w", err)
	}
	integLen := 12
	signedLen := len(raw) - integLen
	expectedICV := crypto.HmacSha256(integKey, raw[:signedLen])[:12]
	receivedICV := raw[signedLen:]
	if !crypto.ConstantTimeEqual(expectedICV, receivedICV) {
		return nil, fmt.Errorf("integrity check failed")
	}

	skOffset := ikeHeaderLen
	skLen := int(binary.BigEndian.Uint16(raw[skOffset+2 : skOffset+4]))
	if skOffset+skLen > signedLen {
		return nil, fmt.Errorf("SK payload extends beyond signed data")
	}
	encData := raw[skOffset+4 : skOffset+skLen]
	iv := encData[:16]
	ciphertext := encData[16:]

	block, _ := crypto.NewAESCipher(encKey)
	plaintext := make([]byte, len(ciphertext))
	mode := crypto.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)

	if len(plaintext) < 2 {
		return nil, fmt.Errorf("decrypted plaintext too short")
	}
	padLen := int(plaintext[len(plaintext)-1])
	innerLen := len(plaintext) - 1 - padLen
	if innerLen < 0 {
		return nil, fmt.Errorf("invalid padding length: %d", padLen)
	}
	for i := 0; i < padLen; i++ {
		if plaintext[innerLen+1+i] != byte(i) {
			return nil, fmt.Errorf("invalid padding at offset %d", i)
		}
	}
	return parseIKE(plaintext[1 : innerLen+1])
}

// deriveSKEYSEED 计算 SKEYSEED (RFC 7296 §2.14)。
func deriveSKEYSEED(ni, nr, sharedSecret []byte) []byte {
	seed := append(ni, nr...)
	return crypto.HmacSha256(seed, sharedSecret)
}

// deriveIKEKeyMaterial 从 SKEYSEED 派生 IKE SA 密钥材料。
func deriveIKEKeyMaterial(ni, nr, sharedSecret []byte, initSPI, respSPI uint64) *KeyMaterial {
	skeyseed := deriveSKEYSEED(ni, nr, sharedSecret)
	seed := append(ni, nr...)
	seed = append(seed, crypto.Uint64ToBytes(initSPI)...)
	seed = append(seed, crypto.Uint64ToBytes(respSPI)...)
	prfBytes := 32
	integBytes := 20
	encBytes := 16
	total := prfBytes*3 + integBytes*2 + encBytes*2
	keymat, _ := crypto.PrfPlus(skeyseed, seed, total)
	off := 0
	return &KeyMaterial{
		SK_d:  keymat[off : off+prfBytes],
		SK_ai: keymat[off+prfBytes : off+prfBytes+integBytes],
		SK_ar: keymat[off+prfBytes+integBytes : off+prfBytes+2*integBytes],
		SK_ei: keymat[off+prfBytes+2*integBytes : off+prfBytes+2*integBytes+encBytes],
		SK_er: keymat[off+prfBytes+2*integBytes+encBytes : off+prfBytes+2*integBytes+2*encBytes],
		SK_pi: keymat[off+prfBytes+2*integBytes+2*encBytes : off+2*prfBytes+2*integBytes+2*encBytes],
		SK_pr: keymat[off+2*prfBytes+2*integBytes+2*encBytes : off+3*prfBytes+2*integBytes+2*encBytes],
	}
}

// buildSAProposal 按 RFC 7296 §3.3.1 标准格式编码 SA Proposal（支持多个）。
func buildSAProposal(proposal Proposal) []byte {
	return buildSAProposals([]Proposal{proposal})
}

func buildSAProposals(proposals []Proposal) []byte {
	var allBuf []byte
	for i := range proposals {
		hasMore := i < len(proposals)-1
		allBuf = append(allBuf, buildSingleProposal(proposals[i], hasMore, i+1)...)
	}
	return allBuf
}

func buildSingleProposal(proposal Proposal, hasMore bool, propNum int) []byte {
	var tbuf []byte
	for i, t := range proposal.Transforms {
		tbuf = append(tbuf, encodeTransform(t, i < len(proposal.Transforms)-1)...)
	}
	propLen := 8 + len(proposal.SPI) + len(tbuf)
	buf := make([]byte, propLen)
	next := byte(0)
	if hasMore {
		next = 2
	}
	buf[0] = next
	buf[1] = 0
	binary.BigEndian.PutUint16(buf[2:4], uint16(propLen))
	buf[4] = byte(propNum)
	buf[5] = byte(proposal.ProtocolID)
	buf[6] = byte(len(proposal.SPI))
	buf[7] = byte(len(proposal.Transforms))
	copy(buf[8:], proposal.SPI)
	copy(buf[8+len(proposal.SPI):], tbuf)
	return buf
}

// encodeTransform 按 RFC 7296 §3.3.2 标准格式编码单个 Transform。
func encodeTransform(t Transform, hasMore bool) []byte {
	attrBytes := encodeTransformAttributes(t.Attributes)
	xformLen := 8 + len(attrBytes)
	buf := make([]byte, xformLen)
	next := byte(0)
	if hasMore {
		next = 3
	}
	buf[0] = next
	buf[1] = 0
	binary.BigEndian.PutUint16(buf[2:4], uint16(xformLen))
	buf[4] = byte(t.Type)
	buf[5] = 0
	binary.BigEndian.PutUint16(buf[6:8], uint16(t.ID))
	copy(buf[8:], attrBytes)
	return buf
}

func encodeTransformAttributes(attrs map[uint16][]byte) []byte {
	var buf []byte
	for k, v := range attrs {
		if len(v) <= 2 {
			attrLen := 4
			b := make([]byte, attrLen)
			binary.BigEndian.PutUint16(b[0:2], k|0x8000)
			copy(b[2:4], v)
			buf = append(buf, b...)
		}
	}
	return buf
}

// parseProposals 解析 RFC 7296 标准 SA payload。
func parseProposals(data []byte) []Proposal {
	var proposals []Proposal
	off := 0
	for off+8 <= len(data) {
		propLen := int(binary.BigEndian.Uint16(data[off+2 : off+4]))
		if propLen < 8 || off+propLen > len(data) {
			break
		}
		protoId := ProtocolID(data[off+5])
		spiLen := int(data[off+6])
		numTransforms := int(data[off+7])
		off += 8
		if off+spiLen > len(data) {
			break
		}
		spi := make([]byte, spiLen)
		copy(spi, data[off:off+spiLen])
		off += spiLen

		var transforms []Transform
		tEnd := off - 8 + propLen - spiLen
		for t := 0; t < numTransforms && off+8 <= len(data) && off < tEnd; t++ {
			xformLen := int(binary.BigEndian.Uint16(data[off+2 : off+4]))
			if xformLen < 8 || off+xformLen > len(data) {
				break
			}
			transforms = append(transforms, Transform{
				Type:       TransformType(data[off+4]),
				ID:         TransformID(binary.BigEndian.Uint16(data[off+6 : off+8])),
				Attributes: parseTransformAttributes(data[off+8 : off+xformLen]),
			})
			off += xformLen
		}
		proposals = append(proposals, Proposal{
			ProtocolID: protoId,
			SPI:        spi,
			Transforms: transforms,
		})
		if hasMore := data[off-8]&0x02 != 0; !hasMore {
			break
		}
	}
	return proposals
}

func parseTransformAttributes(data []byte) map[uint16][]byte {
	attrs := make(map[uint16][]byte)
	for i := 0; i+4 <= len(data); {
		k := binary.BigEndian.Uint16(data[i : i+2])
		if k&0x8000 != 0 {
			attrs[k&0x7FFF] = append([]byte{}, data[i+2:i+4]...)
			i += 4
			continue
		}
		vlen := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		i += 4
		if i+vlen <= len(data) {
			attrs[k] = append([]byte{}, data[i:i+vlen]...)
			i += vlen
		} else {
			break
		}
	}
	return attrs
}

func buildKEPayload(group int, pub []byte) []byte {
	buf := make([]byte, 4+len(pub))
	binary.BigEndian.PutUint16(buf[0:2], uint16(group))
	binary.BigEndian.PutUint16(buf[2:4], 0)
	copy(buf[4:], pub)
	return buf
}

func parseKE(data []byte) (int, []byte) {
	if len(data) < 4 {
		return 0, nil
	}
	group := int(binary.BigEndian.Uint16(data[0:2]))
	return group, append([]byte{}, data[4:]...)
}

func buildChildSAProposal() []byte {
	spi := make([]byte, 4)
	binary.BigEndian.PutUint32(spi, 0xc0000000)
	encAtr := map[uint16][]byte{0x800e: {0x00, 0x10}}
	enc := Transform{Type: TransformENCR, ID: EncryptionENCR_AES_CBC_256, Attributes: encAtr}
	integ := Transform{Type: TransformINTEG, ID: IntegrityAUTH_HMAC_SHA2_256_128}
	esn := Transform{Type: TransformESN, ID: 0}
	return buildSAProposal(Proposal{
		ProtocolID: ProtocolESP,
		SPI:        spi,
		Transforms: []Transform{enc, integ, esn},
	})
}

func buildTS(selectors []TrafficSelector) []byte {
	if len(selectors) == 0 {
		buf := make([]byte, 8+16+16)
		buf[0] = 1
		buf[4] = 7
		buf[5] = 0
		return buf
	}
	sel := selectors[0]
	buf := make([]byte, 8+len(sel.StartAddr)*2+4)
	buf[0] = 1
	buf[4] = sel.TSType
	buf[5] = sel.IPProto
	binary.BigEndian.PutUint16(buf[6:8], sel.StartPort)
	copy(buf[8:8+len(sel.StartAddr)], sel.StartAddr)
	off := 8 + len(sel.StartAddr)
	binary.BigEndian.PutUint16(buf[off:off+2], sel.EndPort)
	copy(buf[off+2:off+2+len(sel.EndAddr)], sel.EndAddr)
	return buf
}

func buildIdentity(id string) []byte {
	buf := make([]byte, 4+len(id))
	buf[0] = 3
	copy(buf[4:], []byte(id))
	return buf
}

func buildEAPIdentityPayload() []byte {
	return []byte{2, 1, 0, 6, 0x30, 0x30, 0x30, 0x30}
}

func buildEAPResponse(result *eap.EAPAKAResult, challenge *eap.ChallengeRequest) []byte {
	return eap.BuildChallengeResponse(result.RES, result.KAUT, nil)
}

func extractEAPChallenge(payloads []Payload) (*eap.ChallengeRequest, error) {
	for _, p := range payloads {
		if p.Type == PayloadEAP && len(p.Data) >= 40 {
			return eap.ParseChallenge(p.Data)
		}
	}
	return nil, fmt.Errorf("no EAP challenge found")
}

func isEAPSuccess(data []byte) bool {
	return len(data) >= 4 && data[0] == 3
}

// buildNotify 构造 RFC 7296 §3.10 标准 Notify payload。
func buildNotify(msgType NotifyMessageType) []byte {
	body := make([]byte, 4)
	body[0] = 0
	body[1] = 0
	binary.BigEndian.PutUint16(body[2:4], uint16(msgType))
	return body
}

// buildNotifyData 构造带数据的标准 Notify payload。
func buildNotifyData(msgType NotifyMessageType, data []byte) []byte {
	body := make([]byte, 4+len(data))
	body[0] = 0
	body[1] = 0
	binary.BigEndian.PutUint16(body[2:4], uint16(msgType))
	copy(body[4:], data)
	return body
}

// buildAUTHPayload 构造 IKEv2 AUTH payload (RFC 7296 §2.16)。
func buildAUTHPayload(msk []byte, ikesa *IKESA, saInitReq []byte, idiPayload []byte, responderNonce []byte) []byte {
	idiHash := crypto.HmacSha256(ikesa.Keys.SK_pi, idiPayload)
	authInput := make([]byte, 0, len(saInitReq)+len(responderNonce)+len(idiHash))
	authInput = append(authInput, saInitReq...)
	authInput = append(authInput, responderNonce...)
	authInput = append(authInput, idiHash...)
	sharedKey := crypto.HmacSha256(msk, []byte("Key Pad for IKEv2"))
	mac := crypto.HmacSha256(sharedKey, authInput)[:16]
	buf := make([]byte, 4+len(mac))
	buf[0] = 2
	copy(buf[4:], mac)
	return buf
}

// buildNATHash 计算 NAT Detection hash (RFC 3947)。
// 使用 SHA1 哈希而非 HMAC：SHA1(SPI_i || SPI_r || IP || port)
func buildNATHash(spiI, spiR uint64, addr string, port int) []byte {
	spiData := make([]byte, 16)
	binary.BigEndian.PutUint64(spiData[0:8], spiI)
	binary.BigEndian.PutUint64(spiData[8:16], spiR)
	addrData := crypto.HashNATAddress(addr, port)
	hashInput := append(spiData, addrData...)
	h := sha1.Sum(hashInput)
	return h[:]
}

func detectNAT(payloads []Payload, localAddr string) bool {
	for _, p := range payloads {
		if p.Type == PayloadNotify && len(p.Data) >= 8 {
			msgType := NotifyMessageType(binary.BigEndian.Uint16(p.Data[0:2]))
			if msgType == NotifyNAT_DETECTION_SOURCE_IP {
				return true
			}
		}
	}
	return false
}

func bytesEqual(a, b []byte) bool { return crypto.ConstantTimeEqual(a, b) }
func appendUint16(buf []byte, v uint16) []byte {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return append(buf, b[:]...)
}
