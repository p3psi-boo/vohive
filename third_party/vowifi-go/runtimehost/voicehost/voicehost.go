package voicehost

import (
	"errors"
	"time"

	"context"
)

type SimulateCallRequest struct {
	Callee      string
	HoldSeconds int
	OnConnected func()
}

const (
	DefaultSimulateCallHoldSeconds = 30
	MaxSimulateCallHoldSeconds     = 300
)

type Gateway struct {
	innerGateway    interface{}
	notifier        interface{}
	eventDispatcher interface{}
	devices         map[string]interface{}
}

func NewGateway() *Gateway {
	return &Gateway{devices: make(map[string]interface{})}
}

func (g *Gateway) Start(ctx context.Context) error {
	if g == nil {
		return nil
	}
	return nil
}

func (g *Gateway) Stop() error {
	if g == nil {
		return nil
	}
	return nil
}

func (g *Gateway) SetNotifier(notifier interface{}) {
	if g == nil {
		return
	}
	g.notifier = notifier
}

func (g *Gateway) SetEventDispatcher(dispatcher interface{}) {
	if g == nil {
		return
	}
	g.eventDispatcher = dispatcher
}

func (g *Gateway) SetClientAdapter(adapter interface{}) {
	if g == nil {
		return
	}
}

func (g *Gateway) GetAgent(deviceID string) interface{} {
	if g == nil || g.devices == nil {
		return nil
	}
	return g.devices[deviceID]
}

func (g *Gateway) DeviceStatus(deviceID string) string {
	if g == nil {
		return "inactive"
	}
	return "inactive"
}

type SimulateCallResult struct {
	Success    bool
	Error      string
	DurationMs int64
	Reason     string
}

func (g *Gateway) SimulateCall(ctx context.Context, callerID string, req SimulateCallRequest) (*SimulateCallResult, error) {
	if g == nil {
		return nil, errors.New("gateway not initialized")
	}
	hold := req.HoldSeconds
	if hold <= 0 {
		hold = DefaultSimulateCallHoldSeconds
	}
	if hold > MaxSimulateCallHoldSeconds {
		hold = MaxSimulateCallHoldSeconds
	}
	if req.OnConnected != nil {
		req.OnConnected()
	}
	time.Sleep(time.Duration(hold) * time.Second)
	return &SimulateCallResult{Success: true}, nil
}

type RuntimeLifecycle interface {
	AttachDevice(deviceID string)
	DetachDevice(deviceID string)
}
