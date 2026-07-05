// Package crypto 提供 IKEv2/IPsec 密码学原语。
// 实现 RFC 7296 (IKEv2) 和 RFC 5996 要求的 DH 交换、加密、完整性、PRF 系列。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"hash"
	"net"
)

const (
	DHGroup1  = 1  // 768-bit MODP
	DHGroup2  = 2  // 1024-bit MODP
	DHGroup5  = 5  // 1536-bit MODP
	DHGroup14 = 14 // 2048-bit MODP

	AESKeyLen128 = 16
	AESKeyLen256 = 32
	AESBlockSize = 16
)

type DHKeyPair struct {
	Group      int
	PrivateKey []byte
	PublicKey  []byte
}

// AesCbcEncrypt PKCS7-padded AES-CBC 加密。
func AesCbcEncrypt(key, iv, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)
	return ciphertext, nil
}

// AesCbcDecrypt PKCS7 去填充 AES-CBC 解密。
func AesCbcDecrypt(key, iv, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext not block-aligned: %d bytes", len(ciphertext))
	}
	plaintext := make([]byte, len(ciphertext))
	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plaintext, ciphertext)
	return pkcs7Unpad(plaintext)
}

// HmacSha1 HMAC-SHA1。
func HmacSha1(key, data []byte) []byte {
	return hmacHash(hmac.New(sha1.New, key), data)
}

// HmacSha256 HMAC-SHA256。
func HmacSha256(key, data []byte) []byte {
	return hmacHash(hmac.New(sha256.New, key), data)
}

// AesCmac AES-CMAC (RFC 4493)。
func AesCmac(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	return cmacCompute(block, data), nil
}

// PrfPlus IKEv2 PRF+ (RFC 7296 §2.13)。
// 基于 HMAC-SHA256 迭代扩展 key 到指定 length。
// 计数器从 1 开始递增,每次迭代追加到输出。
func PrfPlus(key, seed []byte, length int) ([]byte, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("prf+ key is empty")
	}
	out := make([]byte, 0, length)
	var prev []byte
	counter := byte(1)
	for len(out) < length {
		h := hmac.New(sha256.New, key)
		if prev != nil {
			h.Write(prev)
		}
		h.Write(seed)
		h.Write([]byte{counter})
		prev = h.Sum(nil)
		out = append(out, prev...)
		counter++
	}
	return out[:length], nil
}

// GenerateRandom 生成指定长度安全随机字节。
func GenerateRandom(length int) ([]byte, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("rand.Read: %w", err)
	}
	return b, nil
}

// ----- internal helpers -----

func hmacHash(mac hash.Hash, data []byte) []byte {
	mac.Write(data)
	return mac.Sum(nil)
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padLen := blockSize - len(data)%blockSize
	pad := make([]byte, padLen)
	for i := range pad {
		pad[i] = byte(padLen)
	}
	return append(data, pad...)
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}
	padLen := int(data[len(data)-1])
	if padLen == 0 || padLen > len(data) || padLen > aes.BlockSize {
		return nil, fmt.Errorf("invalid pkcs7 padding: %d", padLen)
	}
	for i := len(data) - padLen; i < len(data); i++ {
		if data[i] != byte(padLen) {
			return nil, fmt.Errorf("invalid pkcs7 padding byte at %d", i)
		}
	}
	return data[:len(data)-padLen], nil
}

// Uint64ToBytes uint64 转大端字节数组。
func Uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// NewAESCipher 创建 AES cipher.Block。
func NewAESCipher(key []byte) (cipher.Block, error) {
	return aes.NewCipher(key)
}

// NewCBCEncrypter 创建 CBC 加密器。
func NewCBCEncrypter(block cipher.Block, iv []byte) cipher.BlockMode {
	return cipher.NewCBCEncrypter(block, iv)
}

// NewCBCDecrypter 创建 CBC 解密器。
func NewCBCDecrypter(block cipher.Block, iv []byte) cipher.BlockMode {
	return cipher.NewCBCDecrypter(block, iv)
}

// ConstantTimeEqual 常量时间比较。
func ConstantTimeEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// HashNATAddress 计算 NAT Detection 地址 hash (RFC 3947)。
func HashNATAddress(addr string, port int) []byte {
	ip := net.ParseIP(addr)
	if ip == nil {
		return []byte{}
	}
	ip4 := ip.To4()
	if ip4 != nil {
		buf := make([]byte, 6)
		copy(buf[0:4], ip4)
		buf[4] = byte(port >> 8)
		buf[5] = byte(port & 0xFF)
		return buf
	}
	// IPv6
	buf := make([]byte, 18)
	copy(buf[0:16], ip.To16())
	buf[16] = byte(port >> 8)
	buf[17] = byte(port & 0xFF)
	return buf
}
