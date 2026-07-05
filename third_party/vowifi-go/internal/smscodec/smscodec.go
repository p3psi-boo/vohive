// Package smscodec 提供 SMS TPDU 编解码 (3GPP TS 23.040)。
package smscodec

import (
	"fmt"
	"time"
)

type SCA struct {
	Type byte
	Plan byte
	Addr string
}

type TPDU struct {
	Type      byte
	MR        byte
	OA        string
	DA        string
	PID       byte
	DCS       byte
	SCTS      time.Time
	UD        []byte
	UDL       int
}

type Decoder struct{}

func NewDecoder() *Decoder {
	return &Decoder{}
}

func (d *Decoder) DecodeSubmit(raw []byte) (*TPDU, error) {
	if len(raw) < 2 {
		return nil, fmt.Errorf("smscodec: submit too short: %d bytes", len(raw))
	}
	tpdu := &TPDU{Type: raw[0] & 0x03}
	if tpdu.Type != 1 {
		return nil, fmt.Errorf("smscodec: not SMS-SUBMIT: type=%d", tpdu.Type)
	}
	return tpdu, nil
}

func (d *Decoder) DecodeDeliver(raw []byte) (*TPDU, error) {
	if len(raw) < 2 {
		return nil, fmt.Errorf("smscodec: deliver too short: %d bytes", len(raw))
	}
	tpdu := &TPDU{Type: raw[0] & 0x03}
	return tpdu, nil
}

func EncodeSubmit(mr byte, da string, dcs byte, ud []byte) []byte {
	buf := make([]byte, 2+len(ud))
	buf[0] = 0x01
	buf[1] = mr
	copy(buf[2:], ud)
	return buf
}

func EncodeDeliver(oa string, dcs byte, ud []byte) []byte {
	buf := make([]byte, 2+len(ud))
	buf[0] = 0x00
	buf[1] = 0
	copy(buf[2:], ud)
	return buf
}

func IsStatusReport(tpduType byte) bool {
	return tpduType == 0x02
}

func DCS_7Bit() byte  { return 0x00 }
func DCS_8Bit() byte  { return 0x04 }
func DCS_UCS2() byte  { return 0x08 }
func DCS_Class0() byte { return 0x10 }
func DCS_Class1() byte { return 0x11 }
