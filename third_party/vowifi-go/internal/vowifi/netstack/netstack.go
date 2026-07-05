//go:build linux

// Package netstack 提供 IP 包的读写抽象（通过 TUN 设备）。
package netstack

import (
	"fmt"
	"sync"

	"github.com/iniwex5/vowifi-go/engine/driver"
)

type Stack struct {
	mu    sync.Mutex
	tun   *driver.TUN
	running bool
	stop   chan struct{}
}

func New() *Stack {
	return &Stack{stop: make(chan struct{})}
}

func (s *Stack) Attach(tun *driver.TUN) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return fmt.Errorf("stack already running")
	}
	s.tun = tun
	s.running = true
	go s.loop()
	return nil
}

func (s *Stack) Write(packet []byte) (int, error) {
	s.mu.Lock()
	tun := s.tun
	s.mu.Unlock()
	if tun == nil {
		return 0, fmt.Errorf("netstack: not attached")
	}
	return tun.Write(packet)
}

func (s *Stack) OnPacket(handler func([]byte)) {
	// 由外部 ESP 解封装后调用
}

func (s *Stack) loop() {
	s.mu.Lock()
	tun := s.tun
	s.mu.Unlock()
	if tun == nil {
		return
	}
	buf := make([]byte, tun.MTU())
	for {
		select {
		case <-s.stop:
			return
		default:
			n, err := tun.Read(buf)
			if err != nil {
				continue
			}
			if n > 0 {
				_ = buf[:n]
			}
		}
	}
}

func (s *Stack) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.running = false
	close(s.stop)
	return nil
}
