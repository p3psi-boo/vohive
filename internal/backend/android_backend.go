package backend

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/androidagent"
)

type AndroidBackend struct {
	deviceID string
	registry *androidagent.Registry
}

func NewAndroidBackend(deviceID string, registry *androidagent.Registry) *AndroidBackend {
	return &AndroidBackend{deviceID: strings.TrimSpace(deviceID), registry: registry}
}

func (b *AndroidBackend) Mode() string { return BackendAndroid }
func (b *AndroidBackend) Close() error { return nil }

func (b *AndroidBackend) snapshot() (androidagent.StatusSnapshot, error) {
	if b == nil || b.registry == nil {
		return androidagent.StatusSnapshot{}, androidagent.ErrSessionNotFound
	}
	snap, ok := b.registry.Snapshot(b.deviceID)
	if !ok {
		return androidagent.StatusSnapshot{}, androidagent.ErrSessionNotFound
	}
	return snap, nil
}

// SMSIdentity returns the real IMSI when Android exposes it, otherwise a
// stable per-device/SIM key so SMS history is still persisted on restricted
// Android builds.
func (b *AndroidBackend) SMSIdentity(subscriptionID int) string {
	deviceID := ""
	if b != nil {
		deviceID = b.deviceID
	}
	fallback := "android:" + deviceID
	snapshot, err := b.snapshot()
	if err != nil {
		if subscriptionID >= 0 {
			return fallback + ":sub:" + strconv.Itoa(subscriptionID)
		}
		return fallback
	}
	return smsIdentityForSnapshot(deviceID, snapshot, subscriptionID)
}

func smsIdentityForSnapshot(deviceID string, snapshot androidagent.StatusSnapshot, subscriptionID int) string {
	fallback := "android:" + strings.TrimSpace(deviceID)
	if subscriptionID < 0 {
		subscriptionID = snapshot.SelectedSubscriptionID
	}
	for _, subscription := range snapshot.Subscriptions {
		if subscription.SubscriptionID != subscriptionID {
			continue
		}
		if value := strings.TrimSpace(subscription.IMSI); value != "" {
			return value
		}
		if value := strings.TrimSpace(subscription.ICCID); value != "" {
			return fallback + ":iccid:" + value
		}
	}
	if value := strings.TrimSpace(snapshot.IMSI); value != "" {
		return value
	}
	if value := strings.TrimSpace(snapshot.ICCID); value != "" {
		return fallback + ":iccid:" + value
	}
	if subscriptionID >= 0 {
		return fallback + ":sub:" + strconv.Itoa(subscriptionID)
	}
	return fallback
}

func (b *AndroidBackend) session() (*androidagent.Session, error) {
	if b == nil || b.registry == nil {
		return nil, androidagent.ErrSessionNotFound
	}
	session, ok := b.registry.Session(b.deviceID)
	if !ok {
		return nil, androidagent.ErrSessionNotFound
	}
	return session, nil
}

func (b *AndroidBackend) call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	session, err := b.session()
	if err != nil {
		return nil, err
	}
	return session.Call(ctx, method, params)
}

func (b *AndroidBackend) GetIMEI(context.Context) (string, error) {
	s, err := b.snapshot()
	return s.IMEI, err
}

func (b *AndroidBackend) GetIMSI(context.Context) (string, error) {
	s, err := b.snapshot()
	return s.IMSI, err
}

func (b *AndroidBackend) GetICCID(context.Context) (string, error) {
	s, err := b.snapshot()
	return s.ICCID, err
}

func (b *AndroidBackend) GetMSISDN(context.Context) (string, error) {
	s, err := b.snapshot()
	return s.MSISDN, err
}

func (b *AndroidBackend) GetRevision(context.Context) (string, error) {
	s, err := b.snapshot()
	if s.Baseband != "" && s.Firmware != "" {
		return s.Firmware + " / " + s.Baseband, err
	}
	if s.Firmware != "" {
		return s.Firmware, err
	}
	return s.Baseband, err
}

func (b *AndroidBackend) GetSignalInfo(context.Context) (*SignalInfo, error) {
	s, err := b.snapshot()
	if err != nil {
		return nil, err
	}
	return &SignalInfo{
		RSSI:     s.SignalDBM,
		RSRP:     s.SignalRSRP,
		RSRQ:     s.SignalRSRQ,
		SINR:     s.SignalSINR,
		NR5GRSRP: s.NR5GRSRP,
		NR5GRSRQ: s.NR5GRSRQ,
		NR5GSINR: s.NR5GSINR,
	}, nil
}

func (b *AndroidBackend) GetServingSystem(context.Context) (*ServingSystem, error) {
	s, err := b.snapshot()
	if err != nil {
		return nil, err
	}
	return &ServingSystem{
		RegStatus:     s.RegStatus,
		RegStatusText: s.RegStatusText,
		Operator:      s.Operator,
		MCC:           s.MCC,
		MNC:           s.MNC,
		NetworkMode:   s.NetworkMode,
		NetworkDuplex: s.NetworkDuplex,
		RadioBand:     s.RadioBand,
		RadioChannel:  s.RadioChannel,
		PSAttached:    s.PSAttached,
	}, nil
}

func (b *AndroidBackend) IsSimInserted(context.Context) (bool, error) {
	s, err := b.snapshot()
	return s.SimInserted, err
}

func (b *AndroidBackend) GetNativeMCCMNC(context.Context) (string, string, error) {
	s, err := b.snapshot()
	if err != nil {
		return "", "", err
	}
	if subscription, ok := selectedAndroidSubscription(s); ok {
		mcc := strings.TrimSpace(subscription.MCC)
		mnc := strings.TrimSpace(subscription.MNC)
		if mcc != "" || mnc != "" {
			return mcc, mnc, nil
		}
	}
	if s.MCC == 0 && s.MNC == 0 {
		return "", "", nil
	}
	return fmt.Sprintf("%03d", s.MCC), fmt.Sprintf("%02d", s.MNC), nil
}

func (b *AndroidBackend) GetNativeSPN(context.Context) (string, error) {
	s, err := b.snapshot()
	if subscription, ok := selectedAndroidSubscription(s); ok {
		if value := strings.TrimSpace(subscription.CarrierName); value != "" {
			return value, err
		}
	}
	return s.Operator, err
}

func (b *AndroidBackend) GetSIMMetadata(ctx context.Context) (*SIMMetadata, error) {
	mcc, mnc, err := b.GetNativeMCCMNC(ctx)
	if err != nil {
		return nil, err
	}
	return &SIMMetadata{NativeMCC: mcc, NativeMNC: mnc}, nil
}

func (b *AndroidBackend) GetICCIDLive(ctx context.Context) (string, error) { return b.GetICCID(ctx) }
func (b *AndroidBackend) GetIMSILive(ctx context.Context) (string, error)  { return b.GetIMSI(ctx) }
func (b *AndroidBackend) GetNativeSPNLive(ctx context.Context) (string, error) {
	return b.GetNativeSPN(ctx)
}
func (b *AndroidBackend) GetSIMMetadataLive(ctx context.Context) (*SIMMetadata, error) {
	return b.GetSIMMetadata(ctx)
}

func (b *AndroidBackend) SendSMS(ctx context.Context, to, body string) error {
	_, err := b.SendSMSWithResult(ctx, to, body)
	return err
}

func (b *AndroidBackend) SendSMSWithResult(ctx context.Context, to, body string) (map[string]any, error) {
	return b.call(ctx, "sms.send", map[string]any{"to": strings.TrimSpace(to), "body": body})
}

func (b *AndroidBackend) SendSMSOnSubscription(ctx context.Context, subscriptionID int, to, body string) (map[string]any, error) {
	return b.call(ctx, "sms.send", map[string]any{
		"subscription_id": subscriptionID,
		"to":              strings.TrimSpace(to),
		"body":            body,
	})
}

func (b *AndroidBackend) ReadSMS(ctx context.Context, index int) (*SMS, error) {
	result, err := b.call(ctx, "sms.read", map[string]any{"index": index})
	if err != nil {
		return nil, err
	}
	return smsFromAndroidResult(result), nil
}

func (b *AndroidBackend) DeleteSMS(ctx context.Context, index int) error {
	return b.DeleteAndroidSMS(ctx, int64(index))
}

func (b *AndroidBackend) DeleteAndroidSMS(ctx context.Context, index int64) error {
	result, err := b.call(ctx, "sms.delete", map[string]any{"index": index})
	if err != nil {
		return err
	}
	if androidInt(result["deleted"]) < 1 {
		return fmt.Errorf("Android SMS %d was not deleted", index)
	}
	return nil
}

func (b *AndroidBackend) ReadAndroidSMS(ctx context.Context, index int64) (*androidagent.SMSMessage, error) {
	result, err := b.call(ctx, "sms.read", map[string]any{"index": index})
	if err != nil {
		return nil, err
	}
	message := androidSMSFromResult(result)
	return &message, nil
}

func (b *AndroidBackend) ListSMS(ctx context.Context) ([]SMSSummary, error) {
	result, err := b.call(ctx, "sms.list", nil)
	if err != nil {
		return nil, err
	}
	raw, _ := result["messages"].([]any)
	out := make([]SMSSummary, 0, len(raw))
	for _, item := range raw {
		m, _ := item.(map[string]any)
		out = append(out, SMSSummary{Index: androidInt(m["index"]), Tag: androidInt(m["tag"])})
	}
	return out, nil
}

func (b *AndroidBackend) DeleteAllSMS(ctx context.Context) error {
	_, err := b.call(ctx, "sms.delete_all", nil)
	return err
}

// ListAndroidSMS returns complete Android provider rows. The generic
// SMSProvider.ListSMS method intentionally keeps its legacy summary shape.
func (b *AndroidBackend) ListAndroidSMS(ctx context.Context, limit int) ([]androidagent.SMSMessage, error) {
	if limit <= 0 {
		limit = 500
	}
	result, err := b.call(ctx, "sms.list", map[string]any{"limit": limit})
	if err != nil {
		return nil, err
	}
	raw, _ := result["messages"].([]any)
	out := make([]androidagent.SMSMessage, 0, len(raw))
	for _, item := range raw {
		m, _ := item.(map[string]any)
		out = append(out, androidSMSFromResult(m))
	}
	return out, nil
}

func (b *AndroidBackend) SetOperatingMode(ctx context.Context, mode OperatingMode) error {
	_, err := b.call(ctx, "radio.set_mode", map[string]any{"mode": int(mode)})
	return err
}

func (b *AndroidBackend) GetOperatingMode(context.Context) (OperatingMode, error) {
	s, err := b.snapshot()
	if err != nil {
		return ModeLowPower, err
	}
	if s.DataConnected || s.PSAttached || s.SimInserted {
		return ModeOnline, nil
	}
	return ModeLowPower, nil
}

func (b *AndroidBackend) Reboot(ctx context.Context) error {
	_, err := b.call(ctx, "device.reboot", nil)
	return err
}

func (b *AndroidBackend) OpenLogicalChannel(ctx context.Context, aid string) (int, error) {
	result, err := b.call(ctx, "sim.open_channel", map[string]any{"aid": strings.TrimSpace(aid)})
	if err != nil {
		return 0, err
	}
	return androidInt(result["channel_id"]), nil
}

func (b *AndroidBackend) CloseLogicalChannel(ctx context.Context, channelID int) error {
	_, err := b.call(ctx, "sim.close_channel", map[string]any{"channel_id": channelID})
	return err
}

func (b *AndroidBackend) TransmitAPDU(ctx context.Context, channelID int, command string) (string, error) {
	result, err := b.call(ctx, "sim.transmit_apdu", map[string]any{"channel_id": channelID, "command": command})
	if err != nil {
		return "", err
	}
	response, _ := result["response"].(string)
	return response, nil
}

func (b *AndroidBackend) ListSubscriptions(ctx context.Context) ([]androidagent.Subscription, error) {
	result, err := b.call(ctx, "subscriptions.list", nil)
	if err != nil {
		return nil, err
	}
	raw, _ := result["subscriptions"].([]any)
	out := make([]androidagent.Subscription, 0, len(raw))
	for _, item := range raw {
		m, _ := item.(map[string]any)
		out = append(out, subscriptionFromAndroidResult(m))
	}
	return out, nil
}

func (b *AndroidBackend) SelectSubscription(ctx context.Context, subscriptionID int) error {
	_, err := b.call(ctx, "subscriptions.select", map[string]any{"subscription_id": subscriptionID})
	return err
}

func (b *AndroidBackend) SwitchESIM(ctx context.Context, subscriptionID, portIndex int) (map[string]any, error) {
	return b.call(ctx, "esim.switch", map[string]any{"subscription_id": subscriptionID, "port_index": portIndex})
}

func (b *AndroidBackend) OpenESIMSettings(ctx context.Context) error {
	_, err := b.call(ctx, "esim.open_settings", nil)
	return err
}

func smsFromAndroidResult(result map[string]any) *SMS {
	if result == nil {
		return nil
	}
	out := &SMS{
		Index:   androidInt(result["index"]),
		Sender:  androidString(result["sender"]),
		Content: androidString(result["content"]),
	}
	if raw := androidString(result["timestamp"]); raw != "" {
		if ts, err := time.Parse(time.RFC3339, raw); err == nil {
			out.Timestamp = ts
		}
	}
	return out
}

func androidSMSFromResult(result map[string]any) androidagent.SMSMessage {
	return androidagent.SMSMessage{
		Index:          androidInt64(result["index"]),
		Sender:         androidString(result["sender"]),
		Recipient:      androidString(result["recipient"]),
		Content:        androidString(result["content"]),
		Timestamp:      androidString(result["timestamp"]),
		Type:           androidInt(result["type"]),
		Tag:            androidInt(result["tag"]),
		SubscriptionID: androidInt(result["subscription_id"]),
	}
}

func selectedAndroidSubscription(snapshot androidagent.StatusSnapshot) (androidagent.Subscription, bool) {
	for _, subscription := range snapshot.Subscriptions {
		if subscription.SubscriptionID == snapshot.SelectedSubscriptionID {
			return subscription, true
		}
	}
	for _, subscription := range snapshot.Subscriptions {
		if subscription.Selected {
			return subscription, true
		}
	}
	return androidagent.Subscription{}, false
}

func subscriptionFromAndroidResult(m map[string]any) androidagent.Subscription {
	return androidagent.Subscription{
		SubscriptionID: androidInt(m["subscription_id"]),
		SlotIndex:      androidInt(m["slot_index"]),
		PortIndex:      androidInt(m["port_index"]),
		CarrierName:    androidString(m["carrier_name"]),
		DisplayName:    androidString(m["display_name"]),
		ICCID:          androidString(m["iccid"]),
		IMSI:           androidString(m["imsi"]),
		IMEI:           androidString(m["imei"]),
		MSISDN:         androidString(m["msisdn"]),
		MCC:            androidString(m["mcc"]),
		MNC:            androidString(m["mnc"]),
		CountryISO:     androidString(m["country_iso"]),
		Embedded:       androidBool(m["embedded"]),
		Opportunistic:  androidBool(m["opportunistic"]),
		Active:         androidBool(m["active"]),
		Selected:       androidBool(m["selected"]),
		DefaultData:    androidBool(m["default_data"]),
		DefaultSMS:     androidBool(m["default_sms"]),
		DefaultVoice:   androidBool(m["default_voice"]),
	}
}

func androidString(v any) string {
	s, _ := v.(string)
	return s
}

func androidBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func androidInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}

func androidInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return parsed
	default:
		return 0
	}
}
