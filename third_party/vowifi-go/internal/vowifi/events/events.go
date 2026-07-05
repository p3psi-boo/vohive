// Package events 提供 VoWiFi 内部事件类型。
package events

import "time"

type EventType string

const (
	EventIKEInit          EventType = "ike_init"
	EventIKEAuth          EventType = "ike_auth"
	EventChildSACreated   EventType = "child_sa_created"
	EventChildSAExpired   EventType = "child_sa_expired"
	EventIMSRegistered    EventType = "ims_registered"
	EventIMSDeregistered  EventType = "ims_deregistered"
	EventSMSReceived      EventType = "sms_received"
	EventSMSSent          EventType = "sms_sent"
	EventUSSDResponse     EventType = "ussd_response"
	EventIPChanged        EventType = "ip_changed"
	EventMOBIKETriggered  EventType = "mobike_triggered"
	EventError            EventType = "error"
)

type Event struct {
	Type      EventType
	DeviceID  string
	Message   string
	Data      map[string]interface{}
	Timestamp time.Time
}

func New(typ EventType, deviceID, message string) Event {
	return Event{
		Type:      typ,
		DeviceID:  deviceID,
		Message:   message,
		Data:      make(map[string]interface{}),
		Timestamp: time.Now(),
	}
}
