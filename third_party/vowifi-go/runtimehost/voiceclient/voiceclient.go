// Package voiceclient 提供 VoWiFi 语音客户端接口。
package voiceclient

// Client VoIP 语音客户端抽象。
type Client interface {
	Dial(caller, callee string, holdSeconds int, onConnected func()) error
	Hangup() error
	Status() string
}
