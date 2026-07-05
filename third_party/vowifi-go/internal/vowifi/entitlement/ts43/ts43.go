// Package ts43 提供 3GPP TS 43.318 定义的 Generic Access Network (GAN) 协议。
// 用于 Legacy 2G/3G 网络的 UMA (Unlicensed Mobile Access) 兼容。
package ts43

import "fmt"

type GANHeader struct {
	Version     byte
	PD          byte
	MessageType byte
	Length      uint16
}

type GANMessage struct {
	Header  GANHeader
	Payload []byte
}

func NewDiscoverRequest(gci []byte) *GANMessage {
	return &GANMessage{
		Header: GANHeader{
			Version:     1,
			PD:          0,
			MessageType: 1,
			Length:      uint16(len(gci)),
		},
		Payload: append([]byte{}, gci...),
	}
}

func NewRegisterRequest(imsi string) *GANMessage {
	payload := []byte(imsi)
	return &GANMessage{
		Header: GANHeader{
			Version:     1,
			PD:          0,
			MessageType: 2,
			Length:      uint16(len(payload)),
		},
		Payload: payload,
	}
}

func ParseGANMessage(raw []byte) (*GANMessage, error) {
	if len(raw) < 4 {
		return nil, fmt.Errorf("gan: message too short: %d", len(raw))
	}
	return &GANMessage{
		Header: GANHeader{
			Version:     raw[0],
			PD:          raw[1],
			MessageType: raw[2],
			Length:      uint16(raw[3])<<8 | uint16(raw[4]),
		},
		Payload: append([]byte{}, raw[5:]...),
	}, nil
}
