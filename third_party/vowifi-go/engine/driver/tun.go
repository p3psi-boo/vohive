// Package driver 提供 Linux TUN 设备创建与读写抽象。
// 通过 ioctl 创建 TUN 接口，支持原始 IP 包的收发。
//
//go:build linux

package driver

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/iniwex5/vowifi-go/engine/bufferpool"
)

const (
	TUNSETIFF   = 0x400454ca
	IFF_TUN     = 0x0001
	IFF_NO_PI   = 0x1000
	IFNAMSIZ    = 16
	IFREQ_BYTES = 40

	DefaultMTU        = 1360
	ReadBufferSize    = 4096
	defaultTunDevice  = "/dev/net/tun"
)

type TUN struct {
	name     string
	mtu      int
	fd       *os.File
	shutdown atomic.Bool
}

func New(name string, mtu int) (*TUN, error) {
	if len(name) == 0 || len(name) >= IFNAMSIZ {
		return nil, fmt.Errorf("tun: invalid interface name: %q", name)
	}
	for _, b := range []byte(name) {
		if !validIfnameByte(b) {
			return nil, fmt.Errorf("tun: invalid interface name byte: 0x%02x in %q", b, name)
		}
	}

	fd, err := os.OpenFile(defaultTunDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("tun: open %s: %w", defaultTunDevice, err)
	}

	var ifreq [IFREQ_BYTES]byte
	copy(ifreq[:], []byte(name))
	flags := uint16(IFF_TUN | IFF_NO_PI)
	binary.NativeEndian.PutUint16(ifreq[IFNAMSIZ:], flags)

	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd.Fd(),
		uintptr(TUNSETIFF),
		uintptr(unsafe.Pointer(&ifreq[0])),
	)
	if errno != 0 {
		fd.Close()
		return nil, fmt.Errorf("tun: ioctl TUNSETIFF failed: %v", errno)
	}

	if mtu <= 0 {
		mtu = DefaultMTU
	}

	return &TUN{
		name: name,
		mtu:  mtu,
		fd:   fd,
	}, nil
}

func (t *TUN) Name() string {
	if t == nil {
		return ""
	}
	return t.name
}

func (t *TUN) MTU() int {
	if t == nil {
		return 0
	}
	return t.mtu
}

func (t *TUN) IsAttached() bool {
	return t != nil && t.fd != nil
}

func (t *TUN) Read(buf []byte) (int, error) {
	if t == nil || t.fd == nil {
		return 0, fmt.Errorf("tun: not initialized")
	}
	return t.fd.Read(buf)
}

func (t *TUN) Write(packet []byte) (int, error) {
	if t == nil || t.fd == nil {
		return 0, fmt.Errorf("tun: not initialized")
	}
	return t.fd.Write(packet)
}

func (t *TUN) ReadPacket() ([]byte, error) {
	if t == nil || t.fd == nil {
		return nil, fmt.Errorf("tun: not initialized")
	}
	buf := bufferpool.Get(ReadBufferSize)
	n, err := t.fd.Read(buf)
	if err != nil {
		bufferpool.Put(buf)
		return nil, fmt.Errorf("tun: read: %w", err)
	}
	packet := make([]byte, n)
	copy(packet, buf[:n])
	bufferpool.Put(buf)
	return packet, nil
}

func (t *TUN) WritePacket(packet []byte) error {
	if t == nil || t.fd == nil {
		return fmt.Errorf("tun: not initialized")
	}
	_, err := t.fd.Write(packet)
	return err
}

func (t *TUN) Shutdown() {
	if t == nil {
		return
	}
	t.shutdown.Store(true)
}

func (t *TUN) IsShutdown() bool {
	if t == nil {
		return true
	}
	return t.shutdown.Load()
}

func (t *TUN) Close() error {
	t.Shutdown()
	if t == nil || t.fd == nil {
		return nil
	}
	return t.fd.Close()
}

func validIfnameByte(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_' || b == '-'
}
