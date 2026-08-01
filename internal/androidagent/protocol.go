package androidagent

import (
	"bytes"
	"encoding/json"
	"time"
)

const ProtocolVersion = 1

const (
	MessageHello           = "hello"
	MessageHeartbeat       = "heartbeat"
	MessageStatusSnapshot  = "status_snapshot"
	MessagePairingComplete = "pairing_complete"
	MessageSMSReceived     = "sms_received"
	MessageSMSStatus       = "sms_status"
	MessageESIMStatus      = "esim_status"
	MessageEventAck        = "event_ack"
	MessageRPCRequest      = "rpc_request"
	MessageRPCResponse     = "rpc_response"
	MessageStreamOpen      = "stream_open"
	MessageStreamOpened    = "stream_opened"
	MessageStreamData      = "stream_data"
	MessageStreamClose     = "stream_close"
	MessageStreamError     = "stream_error"
)

const (
	CapabilityStatus        = "status"
	CapabilitySMS           = "sms"
	CapabilitySubscriptions = "subscriptions"
	CapabilityESIM          = "esim"
	CapabilityRemoteDialer  = "remote_dialer"
)

type Message struct {
	Type            string          `json:"type"`
	ProtocolVersion int             `json:"protocol_version,omitempty"`
	RequestID       string          `json:"request_id,omitempty"`
	EventID         string          `json:"event_id,omitempty"`
	StreamID        string          `json:"stream_id,omitempty"`
	AgentID         string          `json:"agent_id,omitempty"`
	DeviceID        string          `json:"device_id,omitempty"`
	Model           string          `json:"model,omitempty"`
	AppVersion      string          `json:"app_version,omitempty"`
	Capabilities    []string        `json:"capabilities,omitempty"`
	Snapshot        *StatusSnapshot `json:"snapshot,omitempty"`
	Method          string          `json:"method,omitempty"`
	Params          map[string]any  `json:"params,omitempty"`
	Result          map[string]any  `json:"result,omitempty"`
	Network         string          `json:"network,omitempty"`
	Address         string          `json:"address,omitempty"`
	Data            string          `json:"data,omitempty"`
	Error           string          `json:"error,omitempty"`
	Token           string          `json:"token,omitempty"`
	Timestamp       time.Time       `json:"timestamp,omitempty"`
}

type Subscription struct {
	SubscriptionID int    `json:"subscription_id"`
	SlotIndex      int    `json:"slot_index"`
	PortIndex      int    `json:"port_index,omitempty"`
	CarrierName    string `json:"carrier_name,omitempty"`
	DisplayName    string `json:"display_name,omitempty"`
	ICCID          string `json:"iccid,omitempty"`
	IMSI           string `json:"imsi,omitempty"`
	IMEI           string `json:"imei,omitempty"`
	MSISDN         string `json:"msisdn,omitempty"`
	MCC            string `json:"mcc,omitempty"`
	MNC            string `json:"mnc,omitempty"`
	CountryISO     string `json:"country_iso,omitempty"`
	Embedded       bool   `json:"embedded"`
	Opportunistic  bool   `json:"opportunistic"`
	Active         bool   `json:"active"`
	Selected       bool   `json:"selected"`
	DefaultData    bool   `json:"default_data"`
	DefaultSMS     bool   `json:"default_sms"`
	DefaultVoice   bool   `json:"default_voice"`
}

type StatusSnapshot struct {
	IMEI                   string               `json:"imei,omitempty"`
	IMSI                   string               `json:"imsi,omitempty"`
	ICCID                  string               `json:"iccid,omitempty"`
	MSISDN                 string               `json:"msisdn,omitempty"`
	Firmware               string               `json:"firmware,omitempty"`
	Baseband               string               `json:"baseband,omitempty"`
	Operator               string               `json:"operator,omitempty"`
	MCC                    uint16               `json:"mcc,omitempty"`
	MNC                    uint16               `json:"mnc,omitempty"`
	NetworkMode            string               `json:"network_mode,omitempty"`
	NetworkDuplex          string               `json:"network_duplex,omitempty"`
	RadioBand              string               `json:"radio_band,omitempty"`
	RadioChannel           uint32               `json:"radio_channel,omitempty"`
	SignalDBM              int                  `json:"signal_dbm,omitempty"`
	SignalRSRP             int                  `json:"signal_rsrp,omitempty"`
	SignalRSRQ             int                  `json:"signal_rsrq,omitempty"`
	SignalSINR             int                  `json:"signal_sinr,omitempty"`
	NR5GRSRP               int                  `json:"nr5g_rsrp,omitempty"`
	NR5GRSRQ               int                  `json:"nr5g_rsrq,omitempty"`
	NR5GSINR               int                  `json:"nr5g_sinr,omitempty"`
	SignalDBMPresent       bool                 `json:"-"`
	SignalRSRPPresent      bool                 `json:"-"`
	SignalRSRQPresent      bool                 `json:"-"`
	SignalSINRPresent      bool                 `json:"-"`
	NR5GRSRPPresent        bool                 `json:"-"`
	NR5GRSRQPresent        bool                 `json:"-"`
	NR5GSINRPresent        bool                 `json:"-"`
	RegStatus              int                  `json:"reg_status"`
	RegStatusText          string               `json:"reg_status_text,omitempty"`
	ServiceState           int                  `json:"service_state"`
	RegistrationDetails    []RegistrationDetail `json:"registration_details,omitempty"`
	PSAttached             bool                 `json:"ps_attached,omitempty"`
	Roaming                bool                 `json:"roaming,omitempty"`
	EmergencyOnly          bool                 `json:"emergency_only,omitempty"`
	SimInserted            bool                 `json:"sim_inserted,omitempty"`
	DataConnected          bool                 `json:"data_connected,omitempty"`
	PrivateIP              string               `json:"private_ip,omitempty"`
	PrivateIPv6            string               `json:"private_ipv6,omitempty"`
	PublicIP               string               `json:"public_ip,omitempty"`
	PublicIPv6             string               `json:"public_ipv6,omitempty"`
	BatteryPct             int                  `json:"battery_pct"`
	BatteryCharging        bool                 `json:"battery_charging"`
	SelectedSubscriptionID int                  `json:"selected_subscription_id"`
	Subscriptions          []Subscription       `json:"subscriptions,omitempty"`
	ESIMSupported          bool                 `json:"esim_supported,omitempty"`
	ESIMEnabled            bool                 `json:"esim_enabled,omitempty"`
	EID                    string               `json:"eid,omitempty"`
	Access                 map[string]bool      `json:"access,omitempty"`
	ESIMOperation          *ESIMStatusEvent     `json:"esim_operation,omitempty"`
	UpdatedAt              string               `json:"updated_at,omitempty"`
}

// UnmarshalJSON tracks signal-field presence separately from the numeric
// value. Zero is a valid SINR reading and must not become indistinguishable
// from an omitted metric when Android reports it.
func (s *StatusSnapshot) UnmarshalJSON(data []byte) error {
	type snapshotAlias StatusSnapshot
	var decoded snapshotAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*s = StatusSnapshot(decoded)

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	s.SignalDBMPresent = jsonFieldPresent(fields, "signal_dbm")
	s.SignalRSRPPresent = jsonFieldPresent(fields, "signal_rsrp")
	s.SignalRSRQPresent = jsonFieldPresent(fields, "signal_rsrq")
	s.SignalSINRPresent = jsonFieldPresent(fields, "signal_sinr")
	s.NR5GRSRPPresent = jsonFieldPresent(fields, "nr5g_rsrp")
	s.NR5GRSRQPresent = jsonFieldPresent(fields, "nr5g_rsrq")
	s.NR5GSINRPresent = jsonFieldPresent(fields, "nr5g_sinr")
	return nil
}

// MarshalJSON restores present zero-valued signal metrics that omitempty
// would otherwise discard. Non-zero values continue to work for snapshots
// assembled directly in Go without setting presence metadata.
func (s StatusSnapshot) MarshalJSON() ([]byte, error) {
	type snapshotAlias StatusSnapshot
	base, err := json.Marshal(snapshotAlias(s))
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(base, &fields); err != nil {
		return nil, err
	}
	putPresentInt(fields, "signal_dbm", s.SignalDBM, s.SignalDBMPresent)
	putPresentInt(fields, "signal_rsrp", s.SignalRSRP, s.SignalRSRPPresent)
	putPresentInt(fields, "signal_rsrq", s.SignalRSRQ, s.SignalRSRQPresent)
	putPresentInt(fields, "signal_sinr", s.SignalSINR, s.SignalSINRPresent)
	putPresentInt(fields, "nr5g_rsrp", s.NR5GRSRP, s.NR5GRSRPPresent)
	putPresentInt(fields, "nr5g_rsrq", s.NR5GRSRQ, s.NR5GRSRQPresent)
	putPresentInt(fields, "nr5g_sinr", s.NR5GSINR, s.NR5GSINRPresent)
	return json.Marshal(fields)
}

func jsonFieldPresent(fields map[string]json.RawMessage, name string) bool {
	value, ok := fields[name]
	return ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}

func putPresentInt(fields map[string]json.RawMessage, name string, value int, present bool) {
	if !present || value != 0 {
		return
	}
	fields[name] = json.RawMessage("0")
}

type RegistrationDetail struct {
	Domain            string `json:"domain,omitempty"`
	Transport         string `json:"transport,omitempty"`
	Registered        bool   `json:"registered"`
	Roaming           bool   `json:"roaming"`
	Searching         bool   `json:"searching"`
	RejectCause       int    `json:"reject_cause,omitempty"`
	RegisteredPLMN    string `json:"registered_plmn,omitempty"`
	NetworkMode       string `json:"network_mode,omitempty"`
	AvailableServices []int  `json:"available_services,omitempty"`
	CellIdentity      string `json:"cell_identity,omitempty"`
}

type SMSReceivedEvent struct {
	MessageID      string    `json:"message_id,omitempty"`
	Sender         string    `json:"sender"`
	Content        string    `json:"content"`
	SubscriptionID int       `json:"subscription_id,omitempty"`
	SlotIndex      int       `json:"slot_index,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

type SMSStatusEvent struct {
	MessageID      string    `json:"message_id"`
	Part           int       `json:"part,omitempty"`
	PartsTotal     int       `json:"parts_total,omitempty"`
	State          string    `json:"state"`
	ResultCode     int       `json:"result_code"`
	SubscriptionID int       `json:"subscription_id,omitempty"`
	Timestamp      time.Time `json:"timestamp"`
}

type ESIMStatusEvent struct {
	Operation      string    `json:"operation,omitempty"`
	State          string    `json:"state,omitempty"`
	Error          string    `json:"error,omitempty"`
	ResultCode     int       `json:"result_code"`
	DetailedCode   int       `json:"detailed_code"`
	SubscriptionID int       `json:"subscription_id"`
	PortIndex      int       `json:"port_index"`
	Timestamp      time.Time `json:"timestamp"`
}

// SMSMessage is a row in the Android system SMS provider. Index is the
// provider's stable _id and is used by the read/delete RPC methods.
type SMSMessage struct {
	Index          int64  `json:"index"`
	Sender         string `json:"sender,omitempty"`
	Recipient      string `json:"recipient,omitempty"`
	Content        string `json:"content"`
	Timestamp      string `json:"timestamp,omitempty"`
	Type           int    `json:"type"`
	Tag            int    `json:"tag"`
	SubscriptionID int    `json:"subscription_id,omitempty"`
}
