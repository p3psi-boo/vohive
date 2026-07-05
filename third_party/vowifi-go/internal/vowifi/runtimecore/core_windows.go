//go:build !linux

package runtimecore

import (
	"context"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

type Core struct{}

type Config struct {
	DeviceID, TraceID, MCC, MNC, IMSI, EPDGOverride string
	AKA    eap.AKAProvider
	Store   messaging.DeliveryStore
	Dispatch eventhost.Dispatcher
}

func New(ctx context.Context, cfg *Config) *Core { return &Core{} }
func (c *Core) Phase() string { return "init" }
func (c *Core) State() runtimehost.State { return runtimehost.State{} }
func (c *Core) AddObserver(obs runtimehost.Observer) {}
func (c *Core) Start() error { return fmt.Errorf("runtimecore: Linux only") }
func (c *Core) Stop() error { return nil }
func (c *Core) HandleIPPacket(packet []byte) error { return fmt.Errorf("linux only") }
func (c *Core) ReceiveESP(raw []byte) ([]byte, error) { return nil, fmt.Errorf("linux only") }
func (c *Core) RegenerateChildSA() error { return fmt.Errorf("linux only") }
