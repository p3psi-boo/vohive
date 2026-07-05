// Package voice 提供 VoWiFi 语音呼叫状态机。
package voice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/engine/logger"
	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
)

type State string

const (
	StateIdle        State = "idle"
	StateDialing     State = "dialing"
	StateRinging     State = "ringing"
	StateConnected   State = "connected"
	StateHeld        State = "held"
	StateDisconnected State = "disconnected"
)

type CallInfo struct {
	CallID    string
	Caller    string
	Callee    string
	State     State
	StartedAt time.Time
	Duration  time.Duration
}

type CallbackCall struct {
	Caller  string
	Callee  string
	HoldSec int
	OnConnect func()
}

type Engine struct {
	mu       sync.Mutex
	sip      *sipkit.Client
	active   map[string]*CallInfo
	onRing   func(callID, caller, callee string)
	onAnswer func(callID string)
	onHangup func(callID string, durationMs int64, reason string)
}

func New(sip *sipkit.Client) *Engine {
	return &Engine{
		sip:    sip,
		active: make(map[string]*CallInfo),
	}
}

func (e *Engine) Dial(ctx context.Context, caller, callee string, holdSec int, onConnect func()) (*CallInfo, error) {
	callID := fmt.Sprintf("%x@voip", time.Now().UnixNano())
	info := &CallInfo{
		CallID:    callID,
		Caller:    caller,
		Callee:    callee,
		State:     StateDialing,
		StartedAt: time.Now(),
	}
	e.mu.Lock()
	e.active[callID] = info
	e.mu.Unlock()

	logger.Info("VoIP dialing", "caller", caller, "callee", callee, "hold", holdSec)
	time.Sleep(200 * time.Millisecond)

	e.mu.Lock()
	info.State = StateConnected
	e.mu.Unlock()

	start := time.Now()
	if onConnect != nil {
		onConnect()
	}
	if holdSec > 0 {
		time.Sleep(time.Duration(holdSec) * time.Second)
	}
	duration := time.Since(start)

	e.mu.Lock()
	info.State = StateDisconnected
	info.Duration = duration
	delete(e.active, callID)
	e.mu.Unlock()

	if e.onHangup != nil {
		e.onHangup(callID, duration.Milliseconds(), "normal")
	}
	return info, nil
}

func (e *Engine) Hangup(ctx context.Context, callID string) error {
	e.mu.Lock()
	info, ok := e.active[callID]
	if !ok {
		e.mu.Unlock()
		return fmt.Errorf("call %s not found", callID)
	}
	info.State = StateDisconnected
	info.Duration = time.Since(info.StartedAt)
	delete(e.active, callID)
	e.mu.Unlock()
	if e.onHangup != nil {
		e.onHangup(callID, info.Duration.Milliseconds(), "user")
	}
	return nil
}

func (e *Engine) ActiveCall() *CallInfo {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, info := range e.active {
		return info
	}
	return nil
}

func (e *Engine) OnRing(fn func(callID, caller, callee string)) { e.onRing = fn }
func (e *Engine) OnAnswer(fn func(callID string))               { e.onAnswer = fn }
func (e *Engine) OnHangup(fn func(callID string, durationMs int64, reason string)) { e.onHangup = fn }

func (e *Engine) SimulateCall(ctx context.Context, caller string, req CallbackCall) (*CallResult, error) {
	info, err := e.Dial(ctx, caller, req.Callee, req.HoldSec, req.OnConnect)
	if err != nil {
		return &CallResult{Success: false, Reason: err.Error()}, nil
	}
	return &CallResult{Success: true, DurationMs: info.Duration.Milliseconds()}, nil
}

type CallResult struct {
	Success    bool
	DurationMs int64
	Reason     string
}
