package androidagent

import (
	"context"
	"fmt"
	"net"
	"strings"
)

type Dialer struct {
	registry *Registry
	deviceID string
}

func NewDialer(registry *Registry, deviceID string) *Dialer {
	return &Dialer{registry: registry, deviceID: strings.TrimSpace(deviceID)}
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if d == nil || d.registry == nil {
		return nil, ErrSessionNotFound
	}
	session, ok := d.registry.Session(d.deviceID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, d.deviceID)
	}
	return session.DialContext(ctx, network, address)
}
