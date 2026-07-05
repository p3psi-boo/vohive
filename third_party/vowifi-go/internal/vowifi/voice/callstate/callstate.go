// Package callstate 提供 VoWiFi 语音呼叫状态追踪。
package callstate

import "time"

type State string

const (
	StateIdle        State = "idle"
	StateInviteSent  State = "invite_sent"
	StateTrying      State = "trying"
	StateRinging     State = "ringing"
	StateConnected   State = "connected"
	StateOnHold      State = "on_hold"
	StateDisconnected State = "disconnected"
)

type CauseCode int

const (
	CauseNormal        CauseCode = 16
	CauseUserBusy      CauseCode = 17
	CauseNoAnswer      CauseCode = 19
	CauseRejected      CauseCode = 21
	CauseNetworkError  CauseCode = 38
	CauseTimeout       CauseCode = 102
)

type CallState struct {
	CallID    string
	State     State
	Caller    string
	Callee    string
	StartedAt time.Time
	AnsweredAt time.Time
	EndedAt   time.Time
	CauseCode CauseCode
	DurationMs int64
	HoldCount  int
}

func New(callID, caller, callee string) *CallState {
	return &CallState{
		CallID:    callID,
		State:     StateIdle,
		Caller:    caller,
		Callee:    callee,
		StartedAt: time.Now(),
	}
}

func (cs *CallState) Transition(newState State) {
	if cs == nil {
		return
	}
	cs.State = newState
	switch newState {
	case StateConnected:
		cs.AnsweredAt = time.Now()
	case StateDisconnected:
		cs.EndedAt = time.Now()
		if !cs.AnsweredAt.IsZero() {
			cs.DurationMs = cs.EndedAt.Sub(cs.AnsweredAt).Milliseconds()
		}
	}
}

func (cs *CallState) IsActive() bool {
	return cs != nil && cs.State != StateIdle && cs.State != StateDisconnected
}

func (cs *CallState) Duration() time.Duration {
	if cs == nil || cs.AnsweredAt.IsZero() {
		return 0
	}
	if cs.EndedAt.IsZero() {
		return time.Since(cs.AnsweredAt)
	}
	return cs.EndedAt.Sub(cs.AnsweredAt)
}
