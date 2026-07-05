package eventhost

import (
	"context"
	"time"
)

type Event interface{}

type Dispatcher interface {
	Dispatch(ctx context.Context, event Event)
}

type SMSReceived struct {
	DevID   string
	Sender  string
	Content string
	Time    time.Time
}

type SMSSent struct {
	DevID      string
	TargetURI  string
	Content    string
	Time       time.Time
	TotalParts int
}

type LocalNumberLearned struct {
	DevID  string
	IMSI   string
	Number string
	Source string
}

type LogNotify struct {
	Message string
}
