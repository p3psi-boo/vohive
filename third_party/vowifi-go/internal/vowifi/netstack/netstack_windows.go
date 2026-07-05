//go:build !linux

package netstack

import "fmt"

type Stack struct{}

func New() *Stack { return &Stack{} }

func (s *Stack) Attach(tun interface{}) error {
	return fmt.Errorf("netstack: TUN driver requires Linux")
}

func (s *Stack) Write(packet []byte) (int, error) { return 0, fmt.Errorf("linux only") }
func (s *Stack) Close() error                       { return nil }
