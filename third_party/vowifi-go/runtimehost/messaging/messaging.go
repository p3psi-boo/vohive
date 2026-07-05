package messaging

import (
	"context"
	"time"
)

type SendOutcome struct {
	ID            string
	MessageID     string
	Success       bool
	Error         string
	PartsTotal    int
	DeliveryState string
}

type USSDResult struct {
	Text   string
	Status string
}

type DeliveryStatus struct {
	MessageID  string
	IMSI       string
	DeviceID   string
	Peer       string
	Content    string
	PartsTotal int
	Acks       int
	State      string
	LastError  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Parts      []DeliveryPartStatus
}

type DeliveryPartStatus struct {
	PartNo      int
	CallID      string
	InReplyTo   string
	RPMR        int
	State       string
	SIPCode     int
	RPCause     int
	RPCauseText string
	ErrorText   string
	SentAt      time.Time
	ReportAt    *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type DeliveryPartMatch struct {
	MessageID string
	PartNo    int
	State     string
}

var ErrDeliveryNotFound = &deliveryNotFoundError{}

type deliveryNotFoundError struct{}

func (e *deliveryNotFoundError) Error() string { return "delivery not found" }

type ServiceStatus struct {
	IMSRegistered bool
	SMSCapable    bool
}

func (s *ServiceStatus) IsRegistered() bool {
	return s != nil && s.IMSRegistered
}

type DeliveryStore interface {
	CreateSMSDelivery(messageID, imsi, deviceID, peer, content string, partsTotal int, at time.Time) error
	UpsertSMSDeliveryPart(messageID string, partNo int, callID string, rpMR int, state string, sentAt time.Time) error
	MarkSMSDeliveryPartReport(inReplyTo, callID, deviceID string, rpMR int, state string, sipCode int, rpCause int, errText string, at time.Time) (DeliveryPartMatch, error)
	RecomputeSMSDelivery(messageID string, at time.Time) error
	UpdateSMSDeliveryState(messageID, state, lastError string, acks int, at time.Time) error
	GetSMSDeliveryStatus(messageID string) (*DeliveryStatus, error)
}

func RPCauseText(code int) string {
	return ""
}

type SendOptions struct {
	SuppressTGSuccess bool
	Encoding          string
}

func WithSuppressSendTGSuccess(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressTGSuccessKey{}, true)
}

type suppressTGSuccessKey struct{}

func IsSuppressTGSuccess(ctx context.Context) bool {
	v, _ := ctx.Value(suppressTGSuccessKey{}).(bool)
	return v
}
