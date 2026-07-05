// Package media 提供 VoWiFi 语音媒体 RTP/RTCP 处理。
package media

import (
	"fmt"
	"net"
	"sync"
)

type Codec int

const (
	CodecAMRWB Codec = iota
	CodecAMRNB
	CodecEVS
	CodecOPUS
)

func (c Codec) String() string {
	switch c {
	case CodecAMRWB:
		return "AMR-WB"
	case CodecAMRNB:
		return "AMR-NB"
	case CodecEVS:
		return "EVS"
	case CodecOPUS:
		return "OPUS"
	default:
		return "unknown"
	}
}

type CodecInfo struct {
	Codec     Codec
	Rate      int
	Channels  int
	PT        uint8
}

type RTPPacket struct {
	Version   uint8
	Padding   bool
	Extension bool
	CSRC      []uint32
	Marker    bool
	PT        uint8
	SeqNum    uint16
	Timestamp uint32
	SSRC      uint32
	Payload   []byte
}

type Stream struct {
	mu       sync.Mutex
	localSSRC  uint32
	remoteSSRC uint32
	seqNum    uint16
	timestamp uint32
	codec     CodecInfo
	localAddr *net.UDPAddr
	remoteAddr *net.UDPAddr
	conn      *net.UDPConn
}

func NewStream(codec CodecInfo) *Stream {
	return &Stream{
		localSSRC: 3333,
		seqNum:    0,
		timestamp: 0,
		codec:     codec,
	}
}

func (s *Stream) Bind(local *net.UDPAddr, remote *net.UDPAddr) error {
	conn, err := net.DialUDP("udp", local, remote)
	if err != nil {
		return fmt.Errorf("media bind: %w", err)
	}
	s.conn = conn
	s.localAddr = local
	s.remoteAddr = remote
	return nil
}

func (s *Stream) Send(payload []byte, marker bool) error {
	if s.conn == nil {
		return fmt.Errorf("media: not bound")
	}
	s.mu.Lock()
	s.seqNum++
	seq := s.seqNum
	s.timestamp += 160
	ts := s.timestamp
	s.mu.Unlock()

	rtp := RTPPacket{
		Version:   2,
		Marker:    marker,
		PT:        s.codec.PT,
		SeqNum:    seq,
		Timestamp: ts,
		SSRC:      s.localSSRC,
		Payload:   payload,
	}
	raw := marshalRTP(&rtp)
	_, err := s.conn.Write(raw)
	return err
}

func (s *Stream) Receive(buf []byte) (*RTPPacket, error) {
	if s.conn == nil {
		return nil, fmt.Errorf("media: not bound")
	}
	n, _, err := s.conn.ReadFromUDP(buf)
	if err != nil {
		return nil, err
	}
	return unmarshalRTP(buf[:n])
}

func (s *Stream) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func marshalRTP(pkt *RTPPacket) []byte {
	headerLen := 12 + len(pkt.CSRC)*4
	buf := make([]byte, headerLen+len(pkt.Payload))
	buf[0] = (pkt.Version << 6) | (bool2bit(pkt.Padding) << 5) | (bool2bit(pkt.Extension) << 4) | byte(len(pkt.CSRC))
	buf[1] = (bool2bit(pkt.Marker) << 7) | (pkt.PT & 0x7F)
	buf[2] = byte(pkt.SeqNum >> 8)
	buf[3] = byte(pkt.SeqNum & 0xFF)
	buf[4] = byte(pkt.Timestamp >> 24)
	buf[5] = byte(pkt.Timestamp >> 16)
	buf[6] = byte(pkt.Timestamp >> 8)
	buf[7] = byte(pkt.Timestamp & 0xFF)
	buf[8] = byte(pkt.SSRC >> 24)
	buf[9] = byte(pkt.SSRC >> 16)
	buf[10] = byte(pkt.SSRC >> 8)
	buf[11] = byte(pkt.SSRC & 0xFF)
	copy(buf[headerLen:], pkt.Payload)
	return buf
}

func unmarshalRTP(raw []byte) (*RTPPacket, error) {
	if len(raw) < 12 {
		return nil, fmt.Errorf("media: rtp packet too short: %d", len(raw))
	}
	pkt := &RTPPacket{
		Version:   raw[0] >> 6,
		Padding:   (raw[0]>>5)&1 == 1,
		Marker:    (raw[1]>>7)&1 == 1,
		PT:        raw[1] & 0x7F,
		SeqNum:    uint16(raw[2])<<8 | uint16(raw[3]),
		Timestamp: uint32(raw[4])<<24 | uint32(raw[5])<<16 | uint32(raw[6])<<8 | uint32(raw[7]),
		SSRC:      uint32(raw[8])<<24 | uint32(raw[9])<<16 | uint32(raw[10])<<8 | uint32(raw[11]),
	}
	cc := int(raw[0] & 0x0F)
	off := 12
	for i := 0; i < cc && off+4 <= len(raw); i++ {
		csrc := uint32(raw[off])<<24 | uint32(raw[off+1])<<16 | uint32(raw[off+2])<<8 | uint32(raw[off+3])
		pkt.CSRC = append(pkt.CSRC, csrc)
		off += 4
	}
	pkt.Payload = append([]byte{}, raw[off:]...)
	return pkt, nil
}

func bool2bit(b bool) byte {
	if b {
		return 1
	}
	return 0
}
