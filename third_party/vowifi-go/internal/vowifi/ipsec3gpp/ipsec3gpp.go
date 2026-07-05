// Package ipsec3gpp 提供 3GPP 特定 IPsec 扩展 (TS 33.402)。
// 包括 IPsec SA 的建立、PCRF QoS 映射、PCSCF 发现。
package ipsec3gpp

import (
	"fmt"
	"net"
	"time"

	"github.com/iniwex5/vowifi-go/engine/crypto"
	"github.com/iniwex5/vowifi-go/engine/ipsec"
)

type QCI int

const (
	QCI1  QCI = 1  // Conversational Voice
	QCI5  QCI = 5  // IMS Signalling
	QCI6  QCI = 6  // Video (Buffered Streaming)
	QCI7  QCI = 7  // Voice (Live Streaming)
	QCI8  QCI = 8  // TCP-based (www, e-mail etc)
	QCI9  QCI = 9  // TCP-based (www, e-mail etc)
)

type Flow struct {
	SrcAddr   net.IP
	DstAddr   net.IP
	SrcPort   int
	DstPort   int
	Protocol  int
	Direction string
	QCI       QCI
}

type ChildSA struct {
	SPI        uint32
	EncKey     []byte
	IntegKey   []byte
	Lifetime   time.Duration
	QCI        QCI
	Flows      []Flow
	CreatedAt  time.Time
}

type Manager struct {
	childSAs map[uint32]*ChildSA
}

func NewManager() *Manager {
	return &Manager{childSAs: make(map[uint32]*ChildSA)}
}

// CreateChildSA 创建 3GPP 兼容的 Child SA。
func (m *Manager) CreateChildSA(qci QCI, flows []Flow) (*ChildSA, error) {
	encKey, _ := crypto.GenerateRandom(16)
	integKey, _ := crypto.GenerateRandom(16)
	spi := generateSPI()
	child := &ChildSA{
		SPI:       spi,
		EncKey:    encKey,
		IntegKey:  integKey,
		Lifetime:  3600 * time.Second,
		QCI:       qci,
		Flows:     flows,
		CreatedAt: time.Now(),
	}
	m.childSAs[spi] = child
	return child, nil
}

// GetChildSA 获取 Child SA。
func (m *Manager) GetChildSA(spi uint32) (*ChildSA, error) {
	child, ok := m.childSAs[spi]
	if !ok {
		return nil, fmt.Errorf("child SA %x not found", spi)
	}
	return child, nil
}

// ApplyToIPsecSA 将 3GPP SA 应用到 IPsec SA。
func (child *ChildSA) ApplyToIPsecSA() *ipsec.SA {
	return ipsec.NewSA(child.SPI, child.EncKey, child.IntegKey)
}

func (m *Manager) DeleteChildSA(spi uint32) {
	delete(m.childSAs, spi)
}

func generateSPI() uint32 {
	b, _ := crypto.GenerateRandom(4)
	spi := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	if spi == 0 {
		return 1
	}
	return spi
}
