package runtimehost

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

type Instance struct {
	mu sync.RWMutex

	deviceID string
	state    State
	service  *serviceAdapter
	stopChan chan struct{}
	stopOnce sync.Once

	observers   []Observer
	notifier    func(string)
	smsNotifier func(string, string, string, time.Time)
}

type serviceAdapter struct {
	inst *Instance
}

func Start(ctx context.Context, req StartRequest) (*Instance, error) {
	inst := &Instance{
		deviceID: req.DeviceID,
		stopChan: make(chan struct{}),
	}
	inst.service = &serviceAdapter{inst: inst}
	return inst, nil
}

func (inst *Instance) State() State {
	if inst == nil {
		return State{}
	}
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	return inst.state
}

func (inst *Instance) Service() *serviceAdapter {
	if inst == nil {
		return nil
	}
	return inst.service
}

func (inst *Instance) AddObserver(obs Observer) func() {
	if inst == nil || obs == nil {
		return func() {}
	}
	inst.mu.Lock()
	inst.observers = append(inst.observers, obs)
	inst.mu.Unlock()
	return func() {
		inst.mu.Lock()
		defer inst.mu.Unlock()
		for i, o := range inst.observers {
			if o == obs {
				inst.observers = append(inst.observers[:i], inst.observers[i+1:]...)
				return
			}
		}
	}
}

func (inst *Instance) SetNotifier(fn func(string)) {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	inst.notifier = fn
	inst.mu.Unlock()
}

func (inst *Instance) SetSMSNotifier(fn func(deviceID, sender, content string, ts time.Time)) {
	if inst == nil {
		return
	}
	inst.mu.Lock()
	inst.smsNotifier = fn
	inst.mu.Unlock()
}

func (inst *Instance) Stop(ctx context.Context) error {
	if inst == nil {
		return nil
	}
	inst.stopOnce.Do(func() {
		if inst.stopChan != nil {
			close(inst.stopChan)
		}
	})
	return nil
}

func (inst *Instance) StopShared() {
	if inst == nil {
		return
	}
	inst.Stop(context.Background())
}

func (inst *Instance) RuntimeState() State {
	return inst.State()
}

func (inst *Instance) SendSMSWithResult(ctx context.Context, text, recipient string) (messaging.SendOutcome, error) {
	return inst.Service().SendSMSWithResult(ctx, text, recipient)
}

func (inst *Instance) SendSMSWithOptions(ctx context.Context, text, recipient string, opts messaging.SendOptions) (messaging.SendOutcome, error) {
	return inst.Service().SendSMSWithOptions(ctx, text, recipient, opts)
}

func (inst *Instance) GetSMSDeliveryStatus(messageID string) (*messaging.DeliveryStatus, error) {
	return nil, messaging.ErrDeliveryNotFound
}

func (inst *Instance) SendUSSD(ctx context.Context, code string) (*messaging.USSDResult, error) {
	return inst.Service().SendUSSD(ctx, code)
}

func (inst *Instance) ContinueUSSD(ctx context.Context, sessionID, input string) (*messaging.USSDResult, error) {
	return inst.Service().ContinueUSSD(ctx, sessionID, input)
}

func (inst *Instance) CancelUSSD(ctx context.Context, sessionID string) error {
	return inst.Service().CancelUSSD(ctx, sessionID)
}

func (inst *Instance) TriggerMOBIKE(oldIP, newIP string) error {
	return nil
}

func (inst *Instance) Status() string {
	return "not_started"
}

func (inst *Instance) Obs() map[string]interface{} {
	return nil
}

func (inst *Instance) OnRuntimeHostEvent(ctx context.Context, ev Event) {
	fn := inst.smsNotifier
	if fn != nil {
		fn(ev.DeviceID, "", ev.Message, ev.Time)
	}
}

// ----- serviceAdapter methods -----

func (s *serviceAdapter) SendSMSWithResult(ctx context.Context, text, recipient string) (messaging.SendOutcome, error) {
	return messaging.SendOutcome{}, fmt.Errorf("vowifi-go service not yet implemented")
}

func (s *serviceAdapter) SendSMSWithOptions(ctx context.Context, text, recipient string, opts messaging.SendOptions) (messaging.SendOutcome, error) {
	return messaging.SendOutcome{}, fmt.Errorf("vowifi-go service not yet implemented")
}

func (s *serviceAdapter) GetSMSDeliveryStatus(ctx context.Context, id string) (messaging.DeliveryPartStatus, error) {
	return messaging.DeliveryPartStatus{}, messaging.ErrDeliveryNotFound
}

func (s *serviceAdapter) SendUSSD(ctx context.Context, code string) (*messaging.USSDResult, error) {
	return &messaging.USSDResult{}, fmt.Errorf("vowifi-go service not yet implemented")
}

func (s *serviceAdapter) ContinueUSSD(ctx context.Context, sessionID, input string) (*messaging.USSDResult, error) {
	return &messaging.USSDResult{}, fmt.Errorf("vowifi-go service not yet implemented")
}

func (s *serviceAdapter) CancelUSSD(ctx context.Context, sessionID string) error {
	return fmt.Errorf("vowifi-go service not yet implemented")
}

func (s *serviceAdapter) StatusSnapshot() messaging.ServiceStatus {
	return messaging.ServiceStatus{}
}

func (s *serviceAdapter) TriggerRegisterImmediate() {}

func (s *serviceAdapter) Stop(ctx context.Context) error {
	return nil
}

func SetLogger(logger interface{}) {}

func NewModemAccessAdapter(modem Modem) interface{} {
	return modem
}

func NewReaderSIMAdapter(provider interface{}) SIMAdapter {
	if adapter, ok := provider.(SIMAdapter); ok {
		return adapter
	}
	if provider == nil {
		return nil
	}
	return readerSIMAdapter{provider: provider}
}

type readerSIMAdapter struct {
	provider interface{}
}

func (a readerSIMAdapter) GetIMSI() (string, error) {
	if p, ok := a.provider.(interface{ GetIMSI() (string, error) }); ok {
		return p.GetIMSI()
	}
	return "", fmt.Errorf("reader SIM provider does not expose IMSI")
}

func (a readerSIMAdapter) GetIdentity(ctx context.Context) (map[string]string, error) {
	imsi, err := a.GetIMSI()
	if err != nil {
		return nil, err
	}
	return map[string]string{"imsi": imsi}, nil
}

func defaultMainReconnectDelay() time.Duration {
	return 5 * time.Second
}

func defaultReaderReconnectDelay() time.Duration {
	return 30 * time.Second
}
