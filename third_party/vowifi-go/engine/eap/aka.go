// Package eap 实现 EAP-AKA 认证协议 (RFC 4187)。
// 支持 ISIM AKA (f1-f5 MILENAGE) 和密钥派生 (CK'/IK')。
package eap

import (
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/crypto"
)

const (
	SubtypeIdentity     = 5  // EAP-Request/AKA-Identity  (RFC 4187 §3)
	SubtypeChallenge    = 1  // EAP-Request/AKA-Challenge (RFC 4187 §3)
	SubtypeNotification = 12 // EAP-Request/AKA-Notification
	SubtypeSyncFailure  = 4  // EAP-Response/AKA-Synchronization-Failure

	AttributeAT_RAND       = 0x01
	AttributeAT_AUTN       = 0x02
	AttributeAT_RES        = 0x03
	AttributeAT_AUTS       = 0x04
	AttributeAT_MAC        = 0x0b
	AttributeAT_IDENTITY   = 0x0e
	AttributeAT_NOTIFICATION = 0x0c

	AKAAlgMILENAGE  = "milenage"
	AKAPrfHMACSHA1  = "hmac-sha1"
	AKAPrfHMACSHA256 = "hmac-sha256"
)

type IdentityRequest struct{}

type ChallengeRequest struct {
	RAND []byte
	AUTN []byte
	MAC  []byte
}

type AKAProvider interface {
	GetAKA(rand, autn []byte) (*AKAResult, error)
	GetIMSI() string
}

type AKAResult struct {
	RES   []byte
	CK    []byte
	IK    []byte
	AUTS  []byte
	AK    []byte
	MAC_A []byte
}

type EAPAKAResult struct {
	MSK  []byte
	RES  []byte
	KAUT []byte
}

// RunEAPAKA 完成 EAP-AKA 完整握手，返回 MSK、RES 和 K_AUT。
func RunEAPAKA(provider AKAProvider, challenge *ChallengeRequest) (*EAPAKAResult, error) {
	if provider == nil || challenge == nil {
		return nil, fmt.Errorf("eap: provider and challenge required")
	}
	if len(challenge.RAND) != 16 || len(challenge.AUTN) != 16 {
		return nil, fmt.Errorf("eap: RAND(%d) and AUTN(%d) must be 16 bytes", len(challenge.RAND), len(challenge.AUTN))
	}
	aka, err := provider.GetAKA(challenge.RAND, challenge.AUTN)
	if err != nil {
		return nil, fmt.Errorf("eap: AKA failed: %w", err)
	}
	if aka == nil || len(aka.RES) == 0 {
		return nil, fmt.Errorf("eap: AKA returned nil or empty RES")
	}
	imsi := provider.GetIMSI()
	identity := []byte(imsi)
	mk := deriveMK(identity, aka.IK, aka.CK)
	kaut := mk[16:32]
	msk := deriveMSKFromMK(mk, identity)
	return &EAPAKAResult{MSK: msk, RES: aka.RES, KAUT: kaut}, nil
}

// ParseChallenge 从 EAP-AKA Challenge 报文中提取 RAND 和 AUTN (RFC 4187 §5.3)。
func ParseChallenge(data []byte) (*ChallengeRequest, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("eap: challenge packet too short: %d bytes", len(data))
	}
	var req ChallengeRequest
	off := 0
	for off+4 <= len(data) {
		attrType := data[off]
		attrLen := int(data[off+2])<<8 | int(data[off+3])
		if attrLen < 4 || off+attrLen > len(data) {
			break
		}
		switch attrType {
		case AttributeAT_RAND:
			if attrLen >= 20 {
				req.RAND = makeCopy(data[off+4 : off+20])
			}
		case AttributeAT_AUTN:
			if attrLen >= 20 {
				req.AUTN = makeCopy(data[off+4 : off+20])
			}
		case AttributeAT_MAC:
			if attrLen >= 20 {
				req.MAC = makeCopy(data[off+4 : off+20])
			}
		}
		off += attrLen
	}
	if req.RAND == nil || req.AUTN == nil {
		return nil, fmt.Errorf("eap: challenge missing RAND or AUTN")
	}
	return &req, nil
}

// BuildChallengeResponse 构造 EAP-AKA Challenge 响应报文。
// 包含 AT_RES(8字节) + AT_MAC(16字节HMAC), 共 32 字节 payload。
func BuildChallengeResponse(res []byte, kaut []byte, msgData []byte) []byte {
	buf := make([]byte, 0, 64)
	buf = appendEapAKAAttr(buf, AttributeAT_RES, res)
	mac := computeEapAKAMAC(kaut, append(buf, msgData...))
	buf = appendEapAKAAttr(buf, AttributeAT_MAC, mac[:16])
	return buf
}

// BuildSyncFailureResponse 构造 EAP-AKA 同步失败响应。
func BuildSyncFailureResponse(auts []byte) []byte {
	return appendEapAKAAttr(nil, AttributeAT_AUTS, auts)
}

// BuildIdentityResponse 构造 EAP-AKA Identity 响应。
func BuildIdentityResponse(identity string) []byte {
	return appendEapAKAAttr(nil, AttributeAT_IDENTITY, []byte(identity))
}

// appendEapAKAAttr 添加 EAP-AKA 属性 TLV。
func appendEapAKAAttr(buf []byte, typ byte, value []byte) []byte {
	totalLen := 4 + len(value)
	buf = append(buf, typ)
	buf = append(buf, 0)
	buf = append(buf, byte(totalLen>>8))
	buf = append(buf, byte(totalLen&0xFF))
	buf = append(buf, value...)
	return buf
}

// computeEapAKAMAC 计算 AT_MAC 值 (RFC 4187 §5.8)。
// 使用 HmacSha1(K_AUT, 报文数据)。
func computeEapAKAMAC(kaut, data []byte) [20]byte {
	mac := hmac.New(sha1.New, kaut)
	mac.Write(data)
	var result [20]byte
	mac.Sum(result[:0])
	return result
}

// deriveMK 派生 Master Key (RFC 4187 §7)。
// MK = SHA1(Identity || IK || CK)
func deriveMK(identity, ik, ck []byte) []byte {
	h := sha1.New()
	h.Write(identity)
	h.Write(ik)
	h.Write(ck)
	return h.Sum(nil)
}

// deriveMSKFromMK 从 MK 派生 MSK (RFC 4187 §7, FIPS 186-2 PRF)。
// K_encr = MK[0:16], K_aut = MK[16:32]
// MSK 通过 PRF+ (K_encr, IK'||CK'||RES) 派生 64 字节
func deriveMSKFromMK(mk, identity []byte) []byte {
	kencr := mk[0:16]
	seed := identity
	msk, _ := crypto.PrfPlus(kencr, seed, 64)
	return msk
}

// deriveCKPrime 从 CK 和 IMSI 派生 CK' (3GPP TS 33.402 Annex A.2)。
func deriveCKPrime(ck []byte, imsi string) []byte {
	return prfDerive(ck, [][]byte{[]byte("3GPP CK' Derivation"), []byte(imsi)}, 16)
}

// deriveIKPrime 从 IK 和 IMSI 派生 IK'。
func deriveIKPrime(ik []byte, imsi string) []byte {
	return prfDerive(ik, [][]byte{[]byte("3GPP IK' Derivation"), []byte(imsi)}, 16)
}

// deriveMSK 从 RES, CK', IK' 派生 MSK (RFC 5448)。
func deriveMSK(res, ckPrime, ikPrime []byte) []byte {
	seed := append(res, ckPrime...)
	seed = append(seed, ikPrime...)
	key := append(ckPrime, ikPrime...)
	prfK, _ := crypto.PrfPlus(key, seed, 64)
	return prfK
}

// MILENAGE AKA 算法 (3GPP TS 35.206)。
// f1 计算 MAC-A (网络认证码), f2 计算 RES, f3 计算 CK, f4 计算 IK, f5 计算 AK。
func Milenage(key, rand, autn []byte) (*AKAResult, error) {
	if len(key) != 16 || len(rand) != 16 || len(autn) != 16 {
		return nil, fmt.Errorf("milenage: key(%d) rand(%d) autn(%d) must be 16 bytes", len(key), len(rand), len(autn))
	}
	const (
		c1 = byte(0x00)
		c2 = byte(0x01)
		c3 = byte(0x02)
		c4 = byte(0x03)
		c5 = byte(0x04)
		r1 = byte(0x40)
		r2 = byte(0x00)
		r3 = byte(0x20)
		r4 = byte(0x40)
		r5 = byte(0x60)
	)

	sqnak := milenageF5(key, rand, c5, r5)
	sqn := make([]byte, 6)
	for i := 0; i < 6; i++ {
		sqn[i] = autn[i] ^ sqnak[i]
	}

	macA := milenageF1(key, rand, sqn, autn[6:8], c1, r1)
	expectedMAC := autn[8:]
	for i := 0; i < 8; i++ {
		if macA[i] != expectedMAC[i] {
			return &AKAResult{AUTS: buildAUTS(key, rand, sqn)}, fmt.Errorf("milenage: MAC-A mismatch")
		}
	}

	res := milenageF2F3F4F5(key, rand, c2, c3, c4, c5, r2, r3, r4, r5)
	res.MAC_A = macA
	return res, nil
}

func milenageF1(key, rand, sqn, amf []byte, c, r byte) []byte {
	input := buildMilenageInput(rand, amf, sqn, c, r)
	return milenageCore(key, input)
}

func milenageF2F3F4F5(key, rand []byte, c2, c3, c4, c5, r2, r3, r4, r5 byte) *AKAResult {
	in2 := buildMilenageInput(rand, nil, nil, c2, r2)
	out2 := milenageCore(key, in2)
	in3 := buildMilenageInput(rand, nil, nil, c3, r3)
	out3 := milenageCore(key, in3)
	in4 := buildMilenageInput(rand, nil, nil, c4, r4)
	out4 := milenageCore(key, in4)
	in5 := buildMilenageInput(rand, nil, nil, c5, r5)
	out5 := milenageCore(key, in5)
	return &AKAResult{
		RES:  makeCopy(out2[8:16]),
		CK:   makeCopy(out3[:16]),
		IK:   makeCopy(out4[:16]),
		AK:   makeCopy(out5[:6]),
	}
}

func milenageF5(key, rand []byte, c, r byte) []byte {
	input := buildMilenageInput(rand, nil, nil, c, r)
	return milenageCore(key, input)[:6]
}

func milenageCore(key, input []byte) []byte {
	block, _ := aes.NewCipher(key)
	out := make([]byte, 16)
	block.Encrypt(out, input)
	for i := 0; i < 16; i++ {
		out[i] ^= input[i]
	}
	tmp := make([]byte, 16)
	block.Encrypt(tmp, out)
	return tmp
}

func buildMilenageInput(rand, amf, sqn []byte, c, r byte) []byte {
	in := make([]byte, 16)
	copy(in[0:], rand[0:15])
	in[15] = rand[15] ^ keyMod(rand, amf)
	for i := 0; i < 16; i++ {
		in[i] ^= c
	}
	in[15] ^= r
	return in
}

func keyMod(rand, amf []byte) byte {
	var v byte
	for _, b := range rand {
		v ^= b
	}
	for _, b := range amf {
		v ^= b
	}
	return v
}

func buildAUTS(key, rand, sqn []byte) []byte {
	const (
		c1 = byte(0x00)
		r1 = byte(0x40)
	)
	in := buildMilenageInput(rand, nil, nil, c1, r1)
	ak := milenageCore(key, in)[:6]
	auts := make([]byte, 14)
	for i := 0; i < 6; i++ {
		auts[i] = sqn[i] ^ ak[i]
	}
	macS := milenageF1Star(key, rand, sqn, nil, c1, r1)
	copy(auts[6:], macS[:8])
	return auts
}

func milenageF1Star(key, rand, sqn, amf []byte, c, r byte) []byte {
	return milenageF1(key, rand, sqn, amf, c, r)
}

func prfDerive(key []byte, inputs [][]byte, length int) []byte {
	h := hmac.New(sha256.New, key)
	for _, input := range inputs {
		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[:], uint16(len(input)))
		h.Write(lenBuf[:])
		h.Write(input)
	}
	return h.Sum(nil)[:length]
}

// prfDeriveSHA1 使用 HMAC-SHA1 派生密钥 (3GPP TS 33.220 Annex B.2)。
func prfDeriveSHA1(key []byte, inputs [][]byte, length int) []byte {
	h := hmac.New(sha1.New, key)
	for _, input := range inputs {
		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[:], uint16(len(input)))
		h.Write(lenBuf[:])
		h.Write(input)
	}
	return h.Sum(nil)[:length]
}

func makeCopy(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
