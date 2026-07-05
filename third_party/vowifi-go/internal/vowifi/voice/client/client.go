// Package client 提供 VoIP 客户端信令处理。
package client

import (
	"context"
	"fmt"
	"sync"

	"github.com/iniwex5/vowifi-go/internal/vowifi/sipkit"
	"github.com/iniwex5/vowifi-go/internal/vowifi/voice/callstate"
)

type Client struct {
	mu     sync.Mutex
	sip    *sipkit.Client
	calls  map[string]*callstate.CallState
	onCall func(cs *callstate.CallState)
}

func New(sip *sipkit.Client) *Client {
	return &Client{
		sip:   sip,
		calls: make(map[string]*callstate.CallState),
	}
}

func (c *Client) Dial(ctx context.Context, caller, callee string) (*callstate.CallState, error) {
	if c.sip == nil {
		return nil, fmt.Errorf("voice client: SIP not connected")
	}
	callID := fmt.Sprintf("%x@voip", syncRand())
	cs := callstate.New(callID, caller, callee)
	cs.Transition(callstate.StateInviteSent)

	c.mu.Lock()
	c.calls[callID] = cs
	c.mu.Unlock()

	_, err := c.sip.SendMessage(ctx, caller, callee, "INVITE placeholder")
	if err != nil {
		cs.Transition(callstate.StateDisconnected)
		cs.CauseCode = callstate.CauseNetworkError
		return cs, fmt.Errorf("dial: %w", err)
	}
	cs.Transition(callstate.StateConnected)

	if c.onCall != nil {
		c.onCall(cs)
	}
	return cs, nil
}

func (c *Client) Hangup(ctx context.Context, callID string) error {
	c.mu.Lock()
	cs, ok := c.calls[callID]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("voice client: call %s not found", callID)
	}
	cs.Transition(callstate.StateDisconnected)
	cs.CauseCode = callstate.CauseNormal
	delete(c.calls, callID)
	c.mu.Unlock()

	if c.onCall != nil {
		c.onCall(cs)
	}
	return nil
}

func (c *Client) GetCall(callID string) *callstate.CallState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[callID]
}

func (c *Client) ActiveCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, cs := range c.calls {
		if cs.IsActive() {
			count++
		}
	}
	return count
}

func (c *Client) OnCallStateChanged(fn func(cs *callstate.CallState)) {
	c.onCall = fn
}

func (c *Client) Close() error {
	c.mu.Lock()
	for callID, cs := range c.calls {
		cs.Transition(callstate.StateDisconnected)
		delete(c.calls, callID)
	}
	c.mu.Unlock()
	if c.sip != nil {
		return c.sip.Close()
	}
	return nil
}

var randCounter uint32

func syncRand() uint32 {
	randCounter++
	return randCounter
}
