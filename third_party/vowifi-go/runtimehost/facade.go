// Package runtimehost 提供 VoWiFi 运行时对外 API。
package runtimehost

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/engine/swu"
	"github.com/iniwex5/vowifi-go/runtimehost/identity"
)

type StartMode int

const (
	StartModeMain   StartMode = iota
	StartModeReader
)

type NetworkMode string

const (
	NetworkMode4G NetworkMode = "4g"
	NetworkMode5G NetworkMode = "5g"
)

type Phase string

const (
	PhaseNotStarted     Phase = "not_started"
	PhaseSIMReady       Phase = "sim_ready"
	PhaseIdentityReady  Phase = "identity_ready"
	PhaseProfileMatched Phase = "profile_matched"
	PhaseFailed         Phase = "failed"
)

type State struct {
	Phase          Phase
	DeviceID       string
	DataplaneMode  string
	NetworkMode    string
	IMSState       string
	SMSService     string
	VoIPService    string
	IsActive       bool
	SIMReady       bool
	AccessReady    bool
	TunnelReady    bool
	IMSReady       bool
	SMSReady       bool
	RegStatus      int
	RegStatusText  string
	LastErrorClass string
	LastError      string
	LastReason     string
	AttachedAt     time.Time
	UpdatedAt      time.Time
}

type EventKind string

const (
	EventKindStateChange EventKind = "state_change"
	EventKindSMSReceived EventKind = "sms_received"
	EventKindSMSSent     EventKind = "sms_sent"
	EventKindError       EventKind = "error"
	EventKindWarning     EventKind = "warning"
)

type Event struct {
	Kind     EventKind
	State    State
	DeviceID string
	Message  string
	Err      error
	Time     time.Time
}

type ProxyConfig struct {
	ID       string
	Addr     string
	Username string
	Password string
	Enabled  bool
}

type SessionConfig struct {
	NetworkMode   NetworkMode
	EPDG          string
	UseProxy      bool
	DataplaneMode string
}

type DataplanePolicy struct {
	Mode         swu.DataplaneMode
	UseKernelESP bool
	MTU          int
	BindPort     int
}

type Modem interface {
	DeviceID() string
	ExecuteATSilent(cmd string, timeout time.Duration) (string, error)
	OpenLogicalChannel(aid string) (int, error)
	CloseLogicalChannel(channel int) error
	TransmitAPDU(channel int, hexAPDU string) (string, error)
	IsHealthy() bool
	IsSimInserted() bool
	QuerySIMInserted() (bool, error)
	GetRegStatus() (int, string)
	GetNetworkMode() string
	GetISIMIdentity() (identity.Identity, error)
	ResolveLogicalChannelAID(app string, fallbackAID string) (string, string, error)
	Stop()
}

var ErrAPDUBusy = errors.New("apdu busy")

type SIMAdapter interface {
	GetIMSI() (string, error)
	GetIdentity(ctx context.Context) (identity map[string]string, err error)
}

type StartRequest struct {
	Mode          StartMode
	DeviceID      string
	TraceID       string
	Profile       interface{}
	Prepared      *identity.PreparedSession
	NetworkMode   string
	VoiceGateway  interface{}
	SIM           SIMAdapter
	Access        interface{}
	Dataplane     DataplanePolicy
	Proxy         *ProxyConfig
	DeliveryStore interface{}
	Dispatch      interface{}
	BeforeStart   func(context.Context, SessionConfig) error
	ShouldRun     func() bool
}

func (r StartRequest) startPolicy() StartMode {
	return r.Mode
}

func (r StartRequest) coreRequest() interface{} {
	return r.Prepared
}

type Observer interface {
	OnRuntimeHostEvent(ctx context.Context, ev Event)
}

type ObserverFunc func(context.Context, Event)

func (f ObserverFunc) OnRuntimeHostEvent(ctx context.Context, ev Event) {
	if f != nil {
		f(ctx, ev)
	}
}

func NewTraceID() string {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		for i := range id {
			id[i] = byte(i)
		}
	}
	return fmt.Sprintf("%032x", id)
}

type traceIDKey struct{}

func WithTraceID(ctx context.Context, traceID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, traceIDKey{}, strings.TrimSpace(traceID))
}

func GetTraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(traceIDKey{}).(string)
	return v
}
