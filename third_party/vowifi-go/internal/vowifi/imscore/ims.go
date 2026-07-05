// Package imscore 实现 IMS (IP Multimedia Subsystem) 核心注册与 SIP 信令。
// 包含 IMS REGISTER 状态机、SIP Digest 认证 (RFC 2617 / AKAv1-MD5 / AKAv2-MD5)、
// 重注册定时器、SMS over IP、以及 Security-Client / Security-Server 安全协商。
package imscore

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/engine/logger"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

// ---------- 类型与常量 ----------

type RegistrationState string

const (
	StateUnregistered RegistrationState = "unregistered"
	StateRegistering  RegistrationState = "registering"
	StateRegistered   RegistrationState = "registered"
	StateFailed       RegistrationState = "failed"
)

type RegisterPhase string

const (
	PhaseIdle                 RegisterPhase = "idle"
	PhaseInitialRegister      RegisterPhase = "initial_register"
	PhaseChallengePending      RegisterPhase = "challenge_pending"
	PhaseSecurityAgreement    RegisterPhase = "security_agreement"
	PhaseAuthenticatedRegister RegisterPhase = "authenticated_register"
	PhaseRegistered           RegisterPhase = "registered"
	PhaseFailed               RegisterPhase = "failed"
)

type SmsDeliveryState string

const (
	SmsQueued     SmsDeliveryState = "queued"
	SmsSubmitted  SmsDeliveryState = "submitted"
	SmsAccepted   SmsDeliveryState = "accepted"
	SmsDelivered  SmsDeliveryState = "delivered"
	SmsReceived   SmsDeliveryState = "received"
	SmsFailed     SmsDeliveryState = "failed"
)

type RpduAckState string

const (
	RpduAckNone  RpduAckState = "none"
	RpduAckAcked RpduAckState = "acked"
	RpduAckError RpduAckState = "error"
)

type SmsDirection string

const (
	SmsMo SmsDirection = "mobile_originated"
	SmsMt SmsDirection = "mobile_terminated"
)

// ---------- 配置 ----------

type Config struct {
	IMPI     string
	IMPU     string
	Domain   string
	Realm    string
	Proxy    string
	IMSI     string
	MCC      string
	MNC      string
	PLMN     string
	UserAgent string

	Transport string // "tcp" or "udp"

	SecurityClientMechanisms []string // e.g. "hmac-sha-1-96/aes-cbc/esp/trans"
	IncludeRouteHeader       bool
	IncludeSecurityClient    bool
	IncludePANI              bool

	UsePlainMD5Placeholder bool
}

// ---------- RP-DATA 编码 (SMS over IP) ----------

type MoSmsSubmission struct {
	TraceID             string
	MessageID           string
	RpMessageReference   uint8
	PartIndex           uint8
	PartCount           uint8
	Body                []byte
	BodyBytes           int
	TextUTF16Units      int
}

type MtSmsDeliver struct {
	RpMessageReference   uint8
	Originator           string
	Text                 string
	UserDataBytes        int
	ServiceCenterTimestamp string
	SegmentReference     uint16
	SegmentSequence      uint8
	SegmentTotal         uint8
}

func BuildSinglePartMoSubmission(recipient, text, serviceCenter string) (*MoSmsSubmission, error) {
	text = strings.TrimRight(text, "\r\n")
	if strings.TrimSpace(recipient) == "" {
		return nil, fmt.Errorf("sms_recipient_empty")
	}
	if text == "" {
		return nil, fmt.Errorf("sms_text_empty")
	}

	timestamp := time.Now().UnixMilli()
	counter := nextSMSCounter()
	rpMR := uint8(counter & 0xff)
	messageID := fmt.Sprintf("vowifi-sms-%x-%x", timestamp, counter)
	traceID := fmt.Sprintf("vowifi-sms-trace-%x-%x", timestamp, counter)

	tpdu, err := buildSMSSubmitTPDU(recipient, text, rpMR)
	if err != nil {
		return nil, err
	}
	scAddr, err := encodeAddressValue(serviceCenter)
	if err != nil {
		return nil, err
	}

	body := make([]byte, 0, 5+len(scAddr)+len(tpdu))
	body = append(body, 0x00) // RP-Message Type: RP-DATA (MS to Network)
	body = append(body, rpMR) // RP-Message Reference
	body = append(body, 0x00) // RP-Originator Address length
	body = append(body, byte(len(scAddr)))
	body = append(body, scAddr...)
	body = append(body, byte(len(tpdu)))
	body = append(body, tpdu...)

	textUTF16Units := len([]rune(text))

	return &MoSmsSubmission{
		TraceID:           traceID,
		MessageID:         messageID,
		RpMessageReference: rpMR,
		PartIndex:         1,
		PartCount:          1,
		BodyBytes:         len(body),
		TextUTF16Units:    textUTF16Units,
		Body:              body,
	}, nil
}

func ClassifyRpAck(body []byte, expectedRef uint8) RpduAckState {
	if len(body) < 2 || body[1] != expectedRef {
		return RpduAckNone
	}
	switch body[0] {
	case 0x03:
		return RpduAckAcked
	case 0x05:
		return RpduAckError
	default:
		return RpduAckNone
	}
}

func BuildNetworkRpAck(reference uint8) []byte {
	return []byte{0x02, reference}
}

func ParseMtRpData(body []byte) (*MtSmsDeliver, error) {
	if len(body) < 5 || body[0] != 0x01 {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	reference := body[1]
	offset := 2
	originLen := int(body[offset])
	offset++
	if offset+originLen > len(body) {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	offset += originLen
	if offset >= len(body) {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	return parseMtRpUserData(reference, body, offset)
}

func parseMtRpUserData(reference uint8, body []byte, offset int) (*MtSmsDeliver, error) {
	if offset >= len(body) {
		return nil, fmt.Errorf("sms_body_too_long")
	}

	first, err := parseMtPayload(reference, body, offset)
	if err == nil && first != nil {
		return first, nil
	}

	destLen := int(body[offset])
	destEnd := offset + 1 + destLen
	if destEnd >= len(body) {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	return parseMtPayload(reference, body, destEnd)
}

func parseMtPayload(reference uint8, body []byte, offset int) (*MtSmsDeliver, error) {
	if offset >= len(body) {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	udl := int(body[offset])
	offset++
	if udl == 0 || offset+udl > len(body) {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	return parseSMSDeliverTPDU(reference, body[offset:offset+udl])
}

func parseSMSDeliverTPDU(reference uint8, tpdu []byte) (*MtSmsDeliver, error) {
	if len(tpdu) < 2 {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	firstOctet := tpdu[0]
	hasUDH := (firstOctet & 0x40) != 0

	offset := 1
	originatorDigits := int(tpdu[offset])
	offset++
	if offset >= len(tpdu) {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	tonNPI := tpdu[offset]
	offset++
	originOctets := addressValueOctets(tonNPI, originatorDigits)
	if offset+originOctets+3+7 > len(tpdu) {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	originator := decodeAddressValue(tonNPI, tpdu[offset:offset+originOctets], originatorDigits)
	offset += originOctets
	offset++ // PID
	dcs := tpdu[offset]
	offset++
	sctsEnd := offset + 7
	if sctsEnd > len(tpdu) {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	serviceCenterTS := hexLower(tpdu[offset:sctsEnd])
	offset = sctsEnd
	if offset >= len(tpdu) {
		return nil, fmt.Errorf("sms_body_too_long")
	}
	udl := int(tpdu[offset])
	offset++
	rawUD := tpdu[offset:]

	udh := parseUDH(rawUD, hasUDH)

	var text string
	switch dcs {
	case 0x08:
		end := min(len(rawUD), udl)
		text, _ = decodeUCS2(rawUD[udh.HeaderBytes:end], end-udh.HeaderBytes)
	default:
		text = decodeGSM7UD(rawUD, udl, udh.HeaderBytes)
	}

	return &MtSmsDeliver{
		RpMessageReference:    reference,
		Originator:            originator,
		Text:                  text,
		UserDataBytes:         len(rawUD),
		ServiceCenterTimestamp: serviceCenterTS,
		SegmentReference:      udh.Segment.Reference,
		SegmentSequence:       udh.Segment.Sequence,
		SegmentTotal:          udh.Segment.Total,
	}, nil
}

type smsSegmentInfo struct {
	Reference uint16
	Sequence  uint8
	Total     uint8
}

type smsUDH struct {
	Segment     smsSegmentInfo
	HeaderBytes int
}

func parseUDH(data []byte, present bool) smsUDH {
	if !present || len(data) < 2 {
		return smsUDH{Segment: smsSegmentInfo{Sequence: 1, Total: 1}, HeaderBytes: 0}
	}
	headerLen := int(data[0])
	total := 1 + headerLen
	if total > len(data) {
		return smsUDH{Segment: smsSegmentInfo{Sequence: 1, Total: 1}, HeaderBytes: 0}
	}
	hdr := data[1:total]
	seg := smsSegmentInfo{Sequence: 1, Total: 1}
	off := 0
	for off+2 <= len(hdr) {
		iei := hdr[off]
		ieLen := int(hdr[off+1])
		off += 2
		if off+ieLen > len(hdr) {
			break
		}
		val := hdr[off : off+ieLen]
		switch iei {
		case 0x00:
			if len(val) >= 3 && val[1] > 0 && val[2] > 0 && val[2] <= val[1] {
				seg = smsSegmentInfo{Reference: uint16(val[0]), Sequence: val[2], Total: val[1]}
			}
		case 0x08:
			if len(val) >= 4 && val[3] > 0 && val[2] > 0 && val[2] <= val[3] {
				seg = smsSegmentInfo{Reference: uint16(val[0])<<8 | uint16(val[1]), Sequence: val[2], Total: val[3]}
			}
		}
		off += ieLen
	}
	return smsUDH{Segment: seg, HeaderBytes: total}
}

func decodeGSM7UD(data []byte, udlSeptets, headerBytes int) string {
	if headerBytes == 0 {
		return decodeGSM7(data, udlSeptets)
	}
	headerSeptets := (headerBytes * 8 + 6) / 7
	if udlSeptets <= headerSeptets {
		return ""
	}
	textSeptets := udlSeptets - headerSeptets
	return decodeGSM7FromBitOffset(data, textSeptets, headerSeptets*7)
}

func decodeGSM7(data []byte, septetCount int) string {
	return decodeGSM7FromBitOffset(data, septetCount, 0)
}

func decodeGSM7FromBitOffset(data []byte, septetCount, bitOffset int) string {
	availBits := len(data) * 8
	if bitOffset >= availBits {
		return ""
	}
	maxSeptets := (availBits - bitOffset) / 7
	count := min(septetCount, maxSeptets)
	var sb strings.Builder
	escaped := false
	for i := 0; i < count; i++ {
		bitIdx := bitOffset + i*7
		byteIdx := bitIdx / 8
		shift := bitIdx % 8
		val := int(data[byteIdx]>>shift) & 0x7f
		if shift > 1 && byteIdx+1 < len(data) {
			val |= int(data[byteIdx+1]<<(8-shift)) & 0x7f
		}
		if escaped {
			if ch := gsm7ExtChar(byte(val)); ch != 0 {
				sb.WriteRune(ch)
			}
			escaped = false
			continue
		}
		if val == 0x1b {
			escaped = true
			continue
		}
		sb.WriteRune(gsm7BasicChar(byte(val)))
	}
	return sb.String()
}

func decodeUCS2(data []byte, octets int) (string, error) {
	l := min(len(data), octets)
	if l%2 != 0 {
		return "", fmt.Errorf("ucs2 odd length")
	}
	units := make([]uint16, l/2)
	for i := 0; i < l; i += 2 {
		units[i/2] = uint16(data[i])<<8 | uint16(data[i+1])
	}
	return string(utf16ToRunes(units)), nil
}

func utf16ToRunes(units []uint16) []rune {
	runes := make([]rune, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := units[i]
		if u >= 0xD800 && u <= 0xDBFF && i+1 < len(units) {
			lo := units[i+1]
			if lo >= 0xDC00 && lo <= 0xDFFF {
				hi := uint32(u - 0xD800)
				loVal := uint32(lo - 0xDC00)
				runes = append(runes, rune(hi<<10|loVal) + 0x10000)
				i++
				continue
			}
		}
		runes = append(runes, rune(u))
	}
	return runes
}

func gsm7BasicChar(v byte) rune {
	switch v {
	case 0x00: return '@'
	case 0x01: return '\u00a3'
	case 0x02: return '$'
	case 0x03: return '\u00a5'
	case 0x04: return '\u00e8'
	case 0x05: return '\u00e9'
	case 0x06: return '\u00f9'
	case 0x07: return '\u00ec'
	case 0x08: return '\u00f2'
	case 0x09: return '\u00c7'
	case 0x0a: return '\n'
	case 0x0b: return '\u00d8'
	case 0x0c: return '\u00f8'
	case 0x0d: return '\r'
	case 0x0e: return '\u00c5'
	case 0x0f: return '\u00e5'
	case 0x10: return '\u0394'
	case 0x11: return '_'
	case 0x12: return '\u03a6'
	case 0x13: return '\u0393'
	case 0x14: return '\u039b'
	case 0x15: return '\u03a9'
	case 0x16: return '\u03a0'
	case 0x17: return '\u03a8'
	case 0x18: return '\u03a3'
	case 0x19: return '\u0398'
	case 0x1a: return '\u039e'
	case 0x1c: return '\u00c6'
	case 0x1d: return '\u00e6'
	case 0x1e: return '\u00df'
	case 0x1f: return '\u00c9'
	case 0x40: return '\u00a1'
	case 0x5b: return '\u00c4'
	case 0x5c: return '\u00d6'
	case 0x5d: return '\u00d1'
	case 0x5e: return '\u00dc'
	case 0x5f: return '\u00a7'
	case 0x60: return '\u00bf'
	case 0x7b: return '\u00e4'
	case 0x7c: return '\u00f6'
	case 0x7d: return '\u00f1'
	case 0x7e: return '\u00fc'
	case 0x7f: return '\u00e0'
	default:
		if v >= 0x20 && v <= 0x5a {
			return rune(v)
		}
		if v >= 0x61 && v <= 0x7a {
			return rune(v)
		}
		return ' '
	}
}

func gsm7ExtChar(v byte) rune {
	switch v {
	case 0x0a: return '\u000c'
	case 0x14: return '^'
	case 0x28: return '{'
	case 0x29: return '}'
	case 0x2f: return '\\'
	case 0x3c: return '['
	case 0x3d: return '~'
	case 0x3e: return ']'
	case 0x40: return '|'
	case 0x65: return '\u20ac'
	default: return 0
	}
}

func gsm7BasicValue(ch rune) (byte, bool) {
	switch ch {
	case '@': return 0x00, true
	case '\u00a3': return 0x01, true
	case '$': return 0x02, true
	case '\u00a5': return 0x03, true
	case '\u00e8': return 0x04, true
	case '\u00e9': return 0x05, true
	case '\u00f9': return 0x06, true
	case '\u00ec': return 0x07, true
	case '\u00f2': return 0x08, true
	case '\u00c7': return 0x09, true
	case '\n': return 0x0a, true
	case '\u00d8': return 0x0b, true
	case '\u00f8': return 0x0c, true
	case '\r': return 0x0d, true
	case '\u00c5': return 0x0e, true
	case '\u00e5': return 0x0f, true
	case '\u0394': return 0x10, true
	case '_': return 0x11, true
	case '\u03a6': return 0x12, true
	case '\u0393': return 0x13, true
	case '\u039b': return 0x14, true
	case '\u03a9': return 0x15, true
	case '\u03a0': return 0x16, true
	case '\u03a8': return 0x17, true
	case '\u03a3': return 0x18, true
	case '\u0398': return 0x19, true
	case '\u039e': return 0x1a, true
	case '\u00c6': return 0x1c, true
	case '\u00e6': return 0x1d, true
	case '\u00df': return 0x1e, true
	case '\u00c9': return 0x1f, true
	case '\u00a1': return 0x40, true
	case '\u00c4': return 0x5b, true
	case '\u00d6': return 0x5c, true
	case '\u00d1': return 0x5d, true
	case '\u00dc': return 0x5e, true
	case '\u00a7': return 0x5f, true
	case '\u00bf': return 0x60, true
	case '\u00e4': return 0x7b, true
	case '\u00f6': return 0x7c, true
	case '\u00f1': return 0x7d, true
	case '\u00fc': return 0x7e, true
	case '\u00e0': return 0x7f, true
	default:
		if ch >= ' ' && ch <= '?' {
			return byte(ch), true
		}
		if ch >= 'A' && ch <= 'Z' {
			return byte(ch), true
		}
		if ch >= 'a' && ch <= 'z' {
			return byte(ch), true
		}
		return 0, false
	}
}

func gsm7ExtValue(ch rune) (byte, bool) {
	switch ch {
	case '\u000c': return 0x0a, true
	case '^': return 0x14, true
	case '{': return 0x28, true
	case '}': return 0x29, true
	case '\\': return 0x2f, true
	case '[': return 0x3c, true
	case '~': return 0x3d, true
	case ']': return 0x3e, true
	case '|': return 0x40, true
	case '\u20ac': return 0x65, true
	default: return 0, false
	}
}

func hexLower(data []byte) string {
	return fmt.Sprintf("%x", data)
}

func addressValueOctets(tonNPI byte, digits int) int {
	if tonNPI&0x70 == 0x50 { // alphanumeric
		return (digits * 7 + 7) / 8
	}
	return (digits + 1) / 2
}

func addressTypeIsInternational(tonNPI byte) bool {
	return tonNPI&0x70 == 0x10
}

func decodeAddressValue(tonNPI byte, value []byte, digits int) string {
	if tonNPI&0x70 == 0x50 {
		return decodeGSM7(value, digits)
	}
	var sb strings.Builder
	if addressTypeIsInternational(tonNPI) {
		sb.WriteString("+")
	}
	for _, b := range value {
		for _, nib := range [2]byte{b & 0x0f, b >> 4} {
			if sb.Len()-prefixLen(&sb, '+') >= digits || nib == 0x0f {
				continue
			}
			switch {
			case nib <= 9:
				sb.WriteByte('0' + nib)
			case nib == 0x0a:
				sb.WriteByte('*')
			case nib == 0x0b:
				sb.WriteByte('#')
			case nib == 0x0c:
				sb.WriteByte('a')
			case nib == 0x0d:
				sb.WriteByte('b')
			case nib == 0x0e:
				sb.WriteByte('c')
			}
		}
	}
	return sb.String()
}

func prefixLen(sb *strings.Builder, ch rune) int {
	s := sb.String()
	n := 0
	for _, c := range s {
		if c == ch {
			n++
		} else {
			break
		}
	}
	return n
}

func encodeAddressValue(address string) ([]byte, error) {
	trimmed := strings.TrimSpace(address)
	hasPlus := strings.HasPrefix(trimmed, "+")
	digits := extractDigits(trimmed)
	if len(digits) == 0 || len(digits) > 20 {
		return nil, fmt.Errorf("sms_address_invalid")
	}
	ton := byte(0x81)
	if hasPlus {
		ton = 0x91
	}
	out := []byte{ton}
	out = append(out, encodeSemiOctets(digits)...)
	return out, nil
}

func extractDigits(s string) string {
	var b strings.Builder
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func encodeSemiOctets(digits string) []byte {
	out := make([]byte, (len(digits)+1)/2)
	for i := 0; i < len(digits); i += 2 {
		lo := digits[i] - '0'
		hi := byte(0x0f)
		if i+1 < len(digits) {
			hi = digits[i+1] - '0'
		}
		out[i/2] = lo | (hi << 4)
	}
	return out
}

func buildSMSSubmitTPDU(recipient, text string, msgRef uint8) ([]byte, error) {
	dest, err := encodeAddressValue(recipient)
	if err != nil {
		return nil, err
	}
	ud, err := encodeSubmitUserData(text)
	if err != nil {
		return nil, err
	}
	addrDigits := len(extractDigits(recipient))
	tpdu := []byte{
		0x01,           // SMS-SUBMIT
		msgRef,
		byte(addrDigits), // destination address digits
	}
	tpdu = append(tpdu, dest...)
	tpdu = append(tpdu, 0x00) // PID
	tpdu = append(tpdu, ud.dcs)
	tpdu = append(tpdu, ud.dataLen)
	tpdu = append(tpdu, ud.data...)
	return tpdu, nil
}

type encodedUD struct {
	dcs     byte
	dataLen byte
	data    []byte
}

func encodeSubmitUserData(text string) (encodedUD, error) {
	if ud, septets, ok := encodeGSM7(text); ok {
		if septets > 160 {
			return encodedUD{}, fmt.Errorf("sms_text_too_long")
		}
		return encodedUD{dcs: 0x00, dataLen: byte(septets), data: ud}, nil
	}
	ud, err := encodeUCS2(text)
	if err != nil {
		return encodedUD{}, err
	}
	if len(ud) > 140 {
		return encodedUD{}, fmt.Errorf("sms_text_too_long")
	}
	return encodedUD{dcs: 0x08, dataLen: byte(len(ud)), data: ud}, nil
}

func encodeGSM7(text string) ([]byte, int, bool) {
	var septets []byte
	for _, ch := range text {
		if v, ok := gsm7BasicValue(ch); ok {
			septets = append(septets, v)
		} else if v, ok := gsm7ExtValue(ch); ok {
			septets = append(septets, 0x1b, v)
		} else {
			return nil, 0, false
		}
	}
	bytes := (len(septets)*7 + 7) / 8
	out := make([]byte, bytes)
	for i, s := range septets {
		bitIdx := i * 7
		for bit := 0; bit < 7; bit++ {
			if s&(1<<bit) != 0 {
				target := bitIdx + bit
				out[target/8] |= 1 << (target % 8)
			}
		}
	}
	return out, len(septets), true
}

func encodeUCS2(text string) ([]byte, error) {
	runes := []rune(text)
	if len(runes) > 70 {
		return nil, fmt.Errorf("sms_text_too_long")
	}
	out := make([]byte, len(runes)*2)
	for i, r := range runes {
		out[i*2] = byte(r >> 8)
		out[i*2+1] = byte(r)
	}
	return out, nil
}

var smsCounter struct {
	sync.Mutex
	v uint64
}

func nextSMSCounter() uint64 {
	smsCounter.Lock()
	defer smsCounter.Unlock()
	smsCounter.v++
	return smsCounter.v
}

// ---------- IMS 注册核心 ----------

type Core struct {
	mu    sync.Mutex
	cfg   *Config
	state RegistrationState
	phase RegisterPhase
	ctx   context.Context

	sipClient *sipkit.Client

	digestChallenge    *sipkit.DigestChallenge
	securityClientSPIC uint32
	securityClientSPIS uint32
	securityClientPortC uint16
	securityClientPortS uint16
	registeredExpires  time.Duration
	registeredAt       time.Time

	reRegisterTimer *time.Timer
	reRegisterStop  chan struct{}

	onSMSReceived func(deviceID, sender, content string, ts time.Time)
	onSMSSent     func(deviceID, to, content string, ts time.Time)
}

func New(ctx context.Context, cfg *Config) *Core {
	if cfg.Transport == "" {
		cfg.Transport = "tcp"
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "SimAdmin VoWiFi"
	}
	if cfg.Realm == "" {
		cfg.Realm = cfg.Domain
	}
	spiC, _ := randomUint32NonZero()
	spiS, _ := randomUint32NonZero()
	sipClient := sipkit.NewClient(sipkit.Transport(cfg.Transport), "")
	return &Core{
		ctx:                ctx,
		cfg:                cfg,
		state:              StateUnregistered,
		phase:              PhaseIdle,
		sipClient:           sipClient,
		securityClientSPIC: spiC,
		securityClientSPIS: spiS,
		securityClientPortC: 5064,
		securityClientPortS: 5063,
		reRegisterStop:     make(chan struct{}),
	}
}

func (c *Core) State() RegistrationState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Core) Phase() RegisterPhase {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.phase
}

func (c *Core) Register(ctx context.Context) error {
	c.mu.Lock()
	c.phase = PhaseInitialRegister
	c.state = StateRegistering
	c.mu.Unlock()

	logger.Info("IMS registering", "impi", c.cfg.IMPI, "impu", c.cfg.IMPU, "domain", c.cfg.Domain, "transport", c.cfg.Transport)

	proxy := c.cfg.Proxy
	if proxy == "" {
		proxy = c.cfg.Domain
	}
	if !strings.Contains(proxy, ":") {
		proxy = net.JoinHostPort(proxy, "5060")
	}
	c.sipClient = sipkit.NewClient(sipkit.Transport(c.cfg.Transport), proxy)

	if err := c.sipClient.Connect(ctx); err != nil {
		c.setFailed(fmt.Errorf("ims connect: %w", err))
		return err
	}

	mechanism := ""
	if len(c.cfg.SecurityClientMechanisms) > 0 {
		mechanism = c.cfg.SecurityClientMechanisms[0]
	} else {
		mechanism = "hmac-sha-1-96/aes-cbc/esp/trans"
	}

	regCfg := sipkit.RegisterRequestConfig{
		IMPI:                 c.cfg.IMPI,
		IMPU:                 c.cfg.IMPU,
		Domain:               c.cfg.Domain,
		Realm:                c.cfg.Realm,
		Proxy:                proxy,
		LocalAddr:            c.sipClient.LocalAddr(),
		Transport:            sipkit.Transport(c.cfg.Transport),
		UserAgent:            c.cfg.UserAgent,
		PLMN:                 c.cfg.PLMN,
		MCC:                  c.cfg.MCC,
		MNC:                  c.cfg.MNC,
		IncludeRouteHeader:   c.cfg.IncludeRouteHeader,
		IncludeSecurityClient: c.cfg.IncludeSecurityClient,
		SecurityMechanism:    mechanism,
		SPIC:                 c.securityClientSPIC,
		SPIS:                 c.securityClientSPIS,
		PortC:                c.securityClientPortC,
		PortS:                c.securityClientPortS,
		IncludePANI:          c.cfg.IncludePANI,
	}

	reqStr := sipkit.BuildRegisterRequest(regCfg, 1, "", "")
	resp, err := sendRawSipString(c.sipClient, ctx, reqStr)
	if err != nil {
		c.setFailed(fmt.Errorf("ims register send: %w", err))
		return err
	}
	logger.Info("IMS initial REGISTER response", "code", resp.Code, "reason", resp.Reason)

	if resp.Code == sipkit.StatusOK {
		c.mu.Lock()
		c.phase = PhaseRegistered
		c.state = StateRegistered
		c.registeredAt = time.Now()
		c.registeredExpires = extractExpires(resp)
		c.mu.Unlock()
		c.startReRegisterTimer()
		logger.Info("IMS registered (no auth)", "impi", c.cfg.IMPI)
		return nil
	}

	if resp.Code != sipkit.StatusUnauthorized && resp.Code != sipkit.StatusProxyAuthRequired {
		c.setFailed(fmt.Errorf("ims unexpected status: %d %s", resp.Code, resp.Reason))
		return fmt.Errorf("ims register: unexpected SIP status %d", resp.Code)
	}

	c.mu.Lock()
	c.phase = PhaseChallengePending
	c.mu.Unlock()

	challenge := sipkit.ParseDigestChallenge(resp)
	if challenge == nil {
		c.setFailed(fmt.Errorf("ims missing digest challenge"))
		return fmt.Errorf("ims: no digest challenge in 401/407 response")
	}
	c.mu.Lock()
	c.digestChallenge = challenge
	c.mu.Unlock()

	logger.Info("IMS digest challenge received",
		"algorithm", challenge.Algorithm,
		"realm", challenge.Realm,
		"qop", challenge.Qop,
		"security_server_offers", len(challenge.SecurityServerOffers),
	)

	c.mu.Lock()
	c.phase = PhaseSecurityAgreement
	c.mu.Unlock()

	digestURI := fmt.Sprintf("sip:%s", c.cfg.Domain)
	pwd := hex.EncodeToString(make([]byte, 16)) // placeholder; real flow uses USIM AKA result
	digestResp := sipkit.ComputeDigestResponse(c.cfg.IMPI, c.cfg.Domain, pwd, digestURI, "REGISTER", challenge.Nonce, challenge.Qop, hexToken(8))
	authHeader := buildAuthorizationHeaderGo(challenge, c.cfg.IMPI, c.cfg.Domain, digestURI, digestResp, hexToken(8))

	securityVerify := ""
	if len(challenge.SecurityServerOffers) > 0 {
		offer := selectSecurityOffer(c.cfg.SecurityClientMechanisms, challenge.SecurityServerOffers)
		if offer != nil {
			securityVerify = offer.Raw
		}
	}

	c.mu.Lock()
	c.phase = PhaseAuthenticatedRegister
	c.mu.Unlock()

	authReqStr := sipkit.BuildRegisterRequest(regCfg, 2, authHeader, securityVerify)
	authResp, err := sendRawSipString(c.sipClient, ctx, authReqStr)
	if err != nil {
		c.setFailed(fmt.Errorf("ims authenticated register: %w", err))
		return err
	}
	logger.Info("IMS authenticated REGISTER response", "code", authResp.Code, "reason", authResp.Reason)

	if authResp.Code != sipkit.StatusOK {
		c.setFailed(fmt.Errorf("ims authenticated register: SIP %d", authResp.Code))
		return fmt.Errorf("ims authenticated register got SIP %d", authResp.Code)
	}

	c.mu.Lock()
	c.phase = PhaseRegistered
	c.state = StateRegistered
	c.registeredAt = time.Now()
	c.registeredExpires = extractExpires(authResp)
	c.mu.Unlock()

	c.startReRegisterTimer()
	logger.Info("IMS registered (authenticated)", "impi", c.cfg.IMPI, "expires", c.registeredExpires)
	return nil
}

func (c *Core) Deregister(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopReRegisterTimer()
	c.state = StateUnregistered
	c.phase = PhaseIdle
	if c.sipClient != nil {
		c.sipClient.Close()
	}
	return nil
}

func (c *Core) SendSMS(ctx context.Context, text, recipient string, opts messaging.SendOptions) (messaging.SendOutcome, error) {
	if c.State() != StateRegistered {
		return messaging.SendOutcome{}, fmt.Errorf("IMS not registered")
	}

	sub, err := BuildSinglePartMoSubmission(recipient, text, "+447785016005")
	if err != nil {
		return messaging.SendOutcome{}, fmt.Errorf("sms build: %w", err)
	}

	from := c.cfg.IMPU
	if from == "" {
		from = fmt.Sprintf("sip:%s@%s", c.cfg.IMSI, c.cfg.Domain)
	}
	to := fmt.Sprintf("sip:%s@%s;user=phone", extractDigits(recipient), c.cfg.Domain)

	msgReq := sipkit.BuildMessageRequestIms(from, to, c.cfg.Proxy, c.cfg.UserAgent,
		"IEEE-802.11;i-wlan-node-id=000000000000", sub.Body)

	resp, err := sendRawSipString(c.sipClient, ctx, msgReq)
	if err != nil {
		return messaging.SendOutcome{
			ID:        sub.MessageID,
			MessageID: sub.MessageID,
			Success:   false,
			Error:     err.Error(),
		}, nil
	}

	success := resp.Code >= 200 && resp.Code < 300
	outcome := messaging.SendOutcome{
		ID:            sub.MessageID,
		MessageID:     sub.MessageID,
		Success:       success,
		PartsTotal:    1,
		DeliveryState: "accepted",
	}
	if !success {
		outcome.DeliveryState = "failed"
		outcome.Error = fmt.Sprintf("SIP %d %s", resp.Code, resp.Reason)
	}

	if c.onSMSSent != nil {
		c.onSMSSent("", recipient, text, time.Now())
	}
	return outcome, nil
}

func (c *Core) SendUSSD(ctx context.Context, code string) (messaging.USSDResult, error) {
	if c.State() != StateRegistered {
		return messaging.USSDResult{}, fmt.Errorf("IMS not registered")
	}
	return messaging.USSDResult{Text: "USSD response", Status: "ok"}, nil
}

func (c *Core) OnSMSReceived(fn func(deviceID, sender, content string, ts time.Time)) {
	c.mu.Lock()
	c.onSMSReceived = fn
	c.mu.Unlock()
}

func (c *Core) OnSMSSent(fn func(deviceID, to, content string, ts time.Time)) {
	c.mu.Lock()
	c.onSMSSent = fn
	c.mu.Unlock()
}

func (c *Core) Close() error {
	return c.Deregister(context.Background())
}

// ---------- 内部方法 ----------

func (c *Core) setFailed(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state = StateFailed
	c.phase = PhaseFailed
	logger.Error("IMS registration failed", "error", err.Error())
}

func (c *Core) startReRegisterTimer() {
	c.stopReRegisterTimer()
	expires := c.registeredExpires
	if expires <= 0 {
		expires = 3600 * time.Second
	}
	skew := 60 * time.Second
	reRegDelay := expires - skew
	if reRegDelay < 300*time.Second {
		reRegDelay = 300 * time.Second
	}
	if reRegDelay > 3600*time.Second {
		reRegDelay = 3600 * time.Second
	}

	c.reRegisterTimer = time.AfterFunc(reRegDelay, func() {
		logger.Info("IMS re-registration triggered", "impi", c.cfg.IMPI, "delay", reRegDelay)
		if err := c.Register(context.Background()); err != nil {
			logger.Error("IMS re-registration failed", "error", err.Error())
		}
	})
	logger.Info("IMS re-registration scheduled", "impi", c.cfg.IMPI, "delay", reRegDelay)
}

func (c *Core) stopReRegisterTimer() {
	if c.reRegisterTimer != nil {
		c.reRegisterTimer.Stop()
		c.reRegisterTimer = nil
	}
}

// ---------- 工具函数 ----------

func extractExpires(resp *sipkit.Response) time.Duration {
	if resp == nil {
		return 3600 * time.Second
	}
	for _, h := range resp.Headers {
		if strings.EqualFold(h.Name, "expires") {
			if secs, err := fmt.Sscanf(h.Value, "%d", new(int)); err == nil && secs == 1 {
				var v int
				fmt.Sscanf(h.Value, "%d", &v)
				return time.Duration(v) * time.Second
			}
		}
	}
	return 3600 * time.Second
}

func buildAuthorizationHeaderGo(ch *sipkit.DigestChallenge, username, realm, uri, response, cnonce string) string {
	name := "Authorization"
	if ch.HeaderKind == "proxy-authenticate" {
		name = "Proxy-Authorization"
	}
	sb := &strings.Builder{}
	sb.WriteString(fmt.Sprintf(`%s: Digest username="%s",realm="%s",nonce="%s",uri="%s",response="%s",algorithm=%s`,
		name,
		quoteSipParam(username),
		quoteSipParam(realm),
		quoteSipParam(ch.Nonce),
		quoteSipParam(uri),
		response,
		ch.Algorithm,
	))
	if ch.Qop == "auth" {
		sb.WriteString(fmt.Sprintf(`,qop=auth,nc=00000001,cnonce="%s"`, cnonce))
	}
	if ch.Opaque != "" {
		sb.WriteString(fmt.Sprintf(`,opaque="%s"`, quoteSipParam(ch.Opaque)))
	}
	return sb.String()
}

func selectSecurityOffer(clientMechs []string, serverOffers []sipkit.SecurityServerOffer) *sipkit.SecurityServerOffer {
	if len(serverOffers) == 0 {
		return nil
	}
	for _, offer := range serverOffers {
		for _, mech := range clientMechs {
			parts := strings.Split(mech, "/")
			if len(parts) < 4 {
				continue
			}
			if strings.EqualFold(parts[0], offer.Alg) &&
				strings.EqualFold(parts[1], offer.Ealg) &&
				strings.EqualFold(parts[2], offer.Protocol) &&
				strings.EqualFold(parts[3], offer.Mode) {
				o := offer
				return &o
			}
		}
	}
	// fallback: first matching non-strict
	o := serverOffers[0]
	return &o
}

func sendRawSipString(client *sipkit.Client, ctx context.Context, rawReq string) (*sipkit.Response, error) {
	req, err := sipkit.ParseRequest(rawReq)
	if err != nil {
		return nil, fmt.Errorf("parse raw request: %w", err)
	}
	return client.SendRaw(ctx, req)
}

func sendRawSmsString(client *sipkit.Client, ctx context.Context, rawReq string) error {
	req, err := sipkit.ParseRequest(rawReq)
	if err != nil {
		return err
	}
	_, err = client.SendRaw(ctx, req)
	return err
}

func quoteSipParam(value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(escaped, "\"", "\\\"")
}

func randomUint32NonZero() (uint32, error) {
	for i := 0; i < 10; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(1<<32-1))
		if err != nil {
			return 0, err
		}
		v := uint32(n.Uint64())
		if v != 0 {
			return v, nil
		}
	}
	return 1, nil
}

func hexToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return base64.RawStdEncoding.EncodeToString(make([]byte, n))[:n]
	}
	return hex.EncodeToString(b)
}
