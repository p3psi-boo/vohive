package androidagent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type Session struct {
	registry *Registry
	conn     *websocket.Conn
	deviceID string
	agentID  string

	writeMu sync.Mutex
	closeCh chan struct{}
	closed  atomic.Bool

	mu       sync.RWMutex
	snapshot StatusSnapshot
	streams  map[string]*remoteStream
	pending  map[string]chan Message
	nextID   atomic.Uint64
}

func newSession(registry *Registry, conn *websocket.Conn, deviceID, agentID string) *Session {
	return &Session{
		registry: registry,
		conn:     conn,
		deviceID: strings.TrimSpace(deviceID),
		agentID:  strings.TrimSpace(agentID),
		closeCh:  make(chan struct{}),
		streams:  make(map[string]*remoteStream),
		pending:  make(map[string]chan Message),
	}
}

func (s *Session) DeviceID() string { return s.deviceID }
func (s *Session) AgentID() string  { return s.agentID }

func (s *Session) Snapshot() StatusSnapshot {
	if s == nil {
		return StatusSnapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.snapshot)
}

func cloneSnapshot(in StatusSnapshot) StatusSnapshot {
	out := in
	if in.Access != nil {
		out.Access = make(map[string]bool, len(in.Access))
		for capability, enabled := range in.Access {
			out.Access[capability] = enabled
		}
	}
	if in.ESIMOperation != nil {
		operation := *in.ESIMOperation
		out.ESIMOperation = &operation
	}
	out.Subscriptions = append([]Subscription(nil), in.Subscriptions...)
	out.RegistrationDetails = append([]RegistrationDetail(nil), in.RegistrationDetails...)
	for i := range out.RegistrationDetails {
		out.RegistrationDetails[i].AvailableServices = append([]int(nil), in.RegistrationDetails[i].AvailableServices...)
	}
	return out
}

func (s *Session) IsDataConnected() bool { return s.Snapshot().DataConnected }

func (s *Session) Close() error {
	if s == nil || s.closed.Swap(true) {
		return nil
	}
	close(s.closeCh)
	_ = s.conn.Close()
	s.mu.Lock()
	streams := make([]*remoteStream, 0, len(s.streams))
	for _, stream := range s.streams {
		streams = append(streams, stream)
	}
	s.streams = make(map[string]*remoteStream)
	pending := make([]chan Message, 0, len(s.pending))
	for id, ch := range s.pending {
		delete(s.pending, id)
		pending = append(pending, ch)
	}
	s.mu.Unlock()
	for _, stream := range streams {
		stream.closeWithError(io.ErrClosedPipe)
	}
	for _, ch := range pending {
		close(ch)
	}
	return nil
}

func (s *Session) run() {
	defer func() {
		_ = s.Close()
		if s.registry != nil {
			s.registry.removeSession(s)
		}
	}()
	for {
		var msg Message
		if err := s.conn.ReadJSON(&msg); err != nil {
			return
		}
		if msg.Type != MessageHello && msg.ProtocolVersion != 0 && msg.ProtocolVersion != ProtocolVersion {
			return
		}
		s.handleMessage(msg)
	}
}

func (s *Session) handleMessage(msg Message) {
	switch msg.Type {
	case MessageHello:
		if (msg.DeviceID != "" && strings.TrimSpace(msg.DeviceID) != s.deviceID) ||
			(msg.AgentID != "" && strings.TrimSpace(msg.AgentID) != s.agentID) {
			_ = s.Close()
		}
	case MessageHeartbeat, MessageStatusSnapshot:
		if msg.Snapshot != nil {
			s.setSnapshot(*msg.Snapshot)
			if s.registry != nil {
				go s.registry.notifySession(s.deviceID)
			}
		}
	case MessageRPCResponse:
		s.deliverRPCResponse(msg)
	case MessageStreamOpened:
		s.deliverStreamOpened(msg)
	case MessageStreamData:
		s.deliverStreamData(msg)
	case MessageStreamClose, MessageStreamError:
		s.deliverStreamClosed(msg)
	case MessageSMSReceived:
		s.handleEvent(msg, func() { s.handleSMS(msg) })
	case MessageSMSStatus:
		s.handleEvent(msg, func() { s.handleSMSStatus(msg) })
	case MessageESIMStatus:
		s.handleEvent(msg, func() { s.handleESIMStatus(msg) })
	}
}

func (s *Session) handleEvent(msg Message, apply func()) {
	if s.registry == nil || s.registry.claimEvent(s.deviceID, msg.EventID) {
		apply()
	}
	if msg.EventID != "" {
		_ = s.send(Message{Type: MessageEventAck, ProtocolVersion: ProtocolVersion, EventID: msg.EventID})
	}
}

func (s *Session) setSnapshot(snapshot StatusSnapshot) {
	if snapshot.UpdatedAt == "" {
		snapshot.UpdatedAt = time.Now().Format(time.RFC3339)
	}
	s.mu.Lock()
	s.snapshot = cloneSnapshot(snapshot)
	s.mu.Unlock()
}

func (s *Session) handleSMS(msg Message) {
	if s.registry == nil || msg.Result == nil {
		return
	}
	event := SMSReceivedEvent{
		MessageID:      stringFromAny(msg.Result["message_id"]),
		Sender:         stringFromAny(msg.Result["sender"]),
		Content:        stringFromAny(msg.Result["content"]),
		SubscriptionID: intFromAny(msg.Result["subscription_id"]),
		SlotIndex:      intFromAny(msg.Result["slot_index"]),
		Timestamp:      time.Now(),
	}
	if raw := stringFromAny(msg.Result["timestamp"]); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			event.Timestamp = parsed
		}
	}
	if strings.TrimSpace(event.Content) != "" {
		s.registry.notifySMS(s.deviceID, event)
	}
}

func (s *Session) handleSMSStatus(msg Message) {
	if s.registry == nil || msg.Result == nil {
		return
	}
	event := SMSStatusEvent{
		MessageID:      stringFromAny(msg.Result["message_id"]),
		Part:           intFromAny(msg.Result["part"]),
		PartsTotal:     intFromAny(msg.Result["parts_total"]),
		State:          stringFromAny(msg.Result["state"]),
		ResultCode:     intFromAny(msg.Result["result_code"]),
		SubscriptionID: intFromAny(msg.Result["subscription_id"]),
		Timestamp:      time.Now(),
	}
	if raw := stringFromAny(msg.Result["timestamp"]); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			event.Timestamp = parsed
		}
	}
	if event.MessageID != "" {
		s.registry.notifySMSStatus(s.deviceID, event)
	}
}

func (s *Session) handleESIMStatus(msg Message) {
	if msg.Result == nil {
		return
	}
	event := ESIMStatusEvent{
		Operation:      stringFromAny(msg.Result["operation"]),
		State:          stringFromAny(msg.Result["state"]),
		Error:          stringFromAny(msg.Result["error"]),
		ResultCode:     intFromAny(msg.Result["result_code"]),
		DetailedCode:   intFromAny(msg.Result["detailed_code"]),
		SubscriptionID: intFromAny(msg.Result["subscription_id"]),
		PortIndex:      intFromAny(msg.Result["port_index"]),
		Timestamp:      time.Now(),
	}
	if raw := stringFromAny(msg.Result["timestamp"]); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			event.Timestamp = parsed
		}
	}
	s.mu.Lock()
	s.snapshot.ESIMOperation = &event
	s.snapshot.UpdatedAt = time.Now().Format(time.RFC3339)
	s.mu.Unlock()
	if s.registry != nil {
		go s.registry.notifySession(s.deviceID)
	}
}

func (s *Session) deliverRPCResponse(msg Message) {
	s.mu.Lock()
	ch := s.pending[msg.RequestID]
	delete(s.pending, msg.RequestID)
	s.mu.Unlock()
	if ch != nil {
		ch <- msg
		close(ch)
	}
}

func (s *Session) deliverStreamOpened(msg Message) {
	stream := s.stream(msg.StreamID)
	if stream == nil {
		return
	}
	if msg.Error != "" {
		stream.openResult(errors.New(msg.Error))
		return
	}
	stream.openResult(nil)
}

func (s *Session) deliverStreamData(msg Message) {
	stream := s.stream(msg.StreamID)
	if stream == nil {
		return
	}
	payload, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		stream.closeWithError(err)
		s.removeStream(msg.StreamID)
		return
	}
	stream.enqueue(payload)
}

func (s *Session) deliverStreamClosed(msg Message) {
	stream := s.stream(msg.StreamID)
	if stream == nil {
		return
	}
	var streamErr error
	if msg.Error != "" {
		streamErr = errors.New(msg.Error)
	} else {
		streamErr = io.EOF
	}
	// A dial failure arrives as stream_error before stream_opened. Wake a
	// DialContext waiter with the actual Android-side error instead of a
	// generic closed-pipe error, while remaining harmless after open.
	stream.openResult(streamErr)
	stream.closeWithError(streamErr)
	s.removeStream(msg.StreamID)
}

func (s *Session) stream(id string) *remoteStream {
	s.mu.RLock()
	stream := s.streams[id]
	s.mu.RUnlock()
	return stream
}

func (s *Session) send(msg Message) error {
	if s == nil || s.closed.Load() {
		return io.ErrClosedPipe
	}
	msg.ProtocolVersion = ProtocolVersion
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.WriteJSON(msg)
}

func (s *Session) Call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	if s == nil || s.closed.Load() {
		return nil, ErrSessionNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	requestID := s.nextRequestID("rpc")
	ch := make(chan Message, 1)
	s.mu.Lock()
	s.pending[requestID] = ch
	s.mu.Unlock()
	if err := s.send(Message{Type: MessageRPCRequest, RequestID: requestID, Method: method, Params: params}); err != nil {
		s.removePending(requestID)
		return nil, err
	}
	select {
	case <-ctx.Done():
		s.removePending(requestID)
		return nil, ctx.Err()
	case msg, open := <-ch:
		if !open {
			return nil, io.ErrClosedPipe
		}
		if msg.Error != "" {
			return nil, errors.New(msg.Error)
		}
		return msg.Result, nil
	}
}

func (s *Session) removePending(requestID string) {
	s.mu.Lock()
	delete(s.pending, requestID)
	s.mu.Unlock()
}

func (s *Session) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if s == nil || s.closed.Load() {
		return nil, ErrSessionNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("android agent only supports TCP, got %s", network)
	}
	streamID := s.nextRequestID("stream")
	stream := newRemoteStream(s, streamID, network, address)
	s.mu.Lock()
	s.streams[streamID] = stream
	s.mu.Unlock()
	if err := s.send(Message{Type: MessageStreamOpen, StreamID: streamID, Network: network, Address: address}); err != nil {
		s.removeStream(streamID)
		return nil, err
	}
	if err := stream.waitOpened(ctx); err != nil {
		s.removeStream(streamID)
		_ = stream.Close()
		return nil, err
	}
	return stream, nil
}

func (s *Session) removeStream(streamID string) {
	s.mu.Lock()
	delete(s.streams, streamID)
	s.mu.Unlock()
}

func (s *Session) nextRequestID(prefix string) string {
	next := s.nextID.Add(1)
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), next)
}

func stringFromAny(v any) string {
	s, _ := v.(string)
	return s
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}
