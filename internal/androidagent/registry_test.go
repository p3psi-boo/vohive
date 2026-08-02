package androidagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRegistryPairReconnectStatusSMSAndRPC(t *testing.T) {
	registry := NewRegistry()
	registry.SetSigningSecret([]byte("test-signing-secret"))
	registry.SetDeviceAuthorizer(func(deviceID, agentID string) bool {
		return deviceID == "android-1" && agentID == "agent-1"
	})
	smsEvents := make(chan SMSReceivedEvent, 1)
	registry.SetSMSHandler(func(deviceID string, event SMSReceivedEvent) {
		if deviceID == "android-1" {
			smsEvents <- event
		}
	})

	server := httptest.NewServer(http.HandlerFunc(registry.HandleWebSocket))
	defer server.Close()
	wsBase := "ws" + strings.TrimPrefix(server.URL, "http")

	pairCode, _, err := registry.CreatePairCode("android-1", "agent-1", time.Minute)
	if err != nil {
		t.Fatalf("CreatePairCode: %v", err)
	}
	client, response, err := websocket.DefaultDialer.Dial(wsBase+"?pair_token="+pairCode, nil)
	if err != nil {
		if response != nil {
			t.Fatalf("pair websocket: %v (status %d)", err, response.StatusCode)
		}
		t.Fatalf("pair websocket: %v", err)
	}
	defer client.Close()

	var paired Message
	if err := client.ReadJSON(&paired); err != nil {
		t.Fatalf("read pairing response: %v", err)
	}
	if paired.Type != MessagePairingComplete || paired.Token == "" {
		t.Fatalf("unexpected pairing response: %#v", paired)
	}

	snapshot := StatusSnapshot{
		IMEI:                   "356000000000001",
		IMSI:                   "460001234567890",
		ICCID:                  "8986000000000000001",
		MSISDN:                 "+8613800000000",
		SignalDBM:              -83,
		SignalRSRP:             -101,
		SignalRSRQ:             -12,
		SignalSINR:             17,
		BatteryPct:             78,
		RegStatus:              1,
		RegStatusText:          "registered_home",
		DataConnected:          true,
		SelectedSubscriptionID: 7,
		Access:                 map[string]bool{"default_sms_app": true, "carrier_privileges": false},
	}
	if err := client.WriteJSON(Message{Type: MessageHello, ProtocolVersion: ProtocolVersion, DeviceID: "android-1", AgentID: "agent-1"}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if err := client.WriteJSON(Message{Type: MessageStatusSnapshot, ProtocolVersion: ProtocolVersion, Snapshot: &snapshot}); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	waitUntil(t, func() bool {
		got, online := registry.Snapshot("android-1")
		return online && got.IMEI == snapshot.IMEI && got.SignalRSRP == -101 && got.BatteryPct == 78 &&
			got.Access["default_sms_app"]
	})
	cloned, _ := registry.Snapshot("android-1")
	cloned.Access["default_sms_app"] = false
	clonedAgain, _ := registry.Snapshot("android-1")
	if !clonedAgain.Access["default_sms_app"] {
		t.Fatal("snapshot access map was not cloned")
	}
	if err := client.WriteJSON(Message{
		Type: MessageESIMStatus, ProtocolVersion: ProtocolVersion, EventID: "event-esim-1",
		Result: map[string]any{
			"operation": "switch", "state": "completed", "subscription_id": 7,
			"port_index": 0, "result_code": -1, "detailed_code": 0,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}); err != nil {
		t.Fatalf("write eSIM event: %v", err)
	}
	assertEventAck(t, client, "event-esim-1")
	waitUntil(t, func() bool {
		got, online := registry.Snapshot("android-1")
		return online && got.ESIMOperation != nil && got.ESIMOperation.State == "completed" &&
			got.ESIMOperation.SubscriptionID == 7
	})

	wantTimestamp := time.Now().UTC().Truncate(time.Second)
	if err := client.WriteJSON(Message{
		Type:            MessageSMSReceived,
		ProtocolVersion: ProtocolVersion,
		EventID:         "event-sms-1",
		Result: map[string]any{
			"message_id": "in-1", "sender": "+10086", "content": "hello",
			"subscription_id": 7, "timestamp": wantTimestamp.Format(time.RFC3339),
		},
	}); err != nil {
		t.Fatalf("write SMS event: %v", err)
	}
	assertEventAck(t, client, "event-sms-1")
	select {
	case got := <-smsEvents:
		if got.Sender != "+10086" || got.Content != "hello" || got.SubscriptionID != 7 || !got.Timestamp.Equal(wantTimestamp) {
			t.Fatalf("unexpected SMS event: %#v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SMS event")
	}
	if err := client.WriteJSON(Message{
		Type: MessageSMSReceived, ProtocolVersion: ProtocolVersion, EventID: "event-sms-1",
		Result: map[string]any{"sender": "+10086", "content": "hello again"},
	}); err != nil {
		t.Fatal(err)
	}
	assertEventAck(t, client, "event-sms-1")
	select {
	case duplicate := <-smsEvents:
		t.Fatalf("duplicate event was processed: %+v", duplicate)
	case <-time.After(50 * time.Millisecond):
	}

	session, online := registry.Session("android-1")
	if !online {
		t.Fatal("paired session is offline")
	}
	type rpcResult struct {
		value map[string]any
		err   error
	}
	rpcDone := make(chan rpcResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		value, err := session.Call(ctx, "subscriptions.list", nil)
		rpcDone <- rpcResult{value: value, err: err}
	}()
	var request Message
	if err := client.ReadJSON(&request); err != nil {
		t.Fatalf("read RPC request: %v", err)
	}
	if request.Type != MessageRPCRequest || request.Method != "subscriptions.list" || request.RequestID == "" {
		t.Fatalf("unexpected RPC request: %#v", request)
	}
	if err := client.WriteJSON(Message{
		Type: MessageRPCResponse, ProtocolVersion: ProtocolVersion, RequestID: request.RequestID,
		Result: map[string]any{"count": float64(2)},
	}); err != nil {
		t.Fatalf("write RPC response: %v", err)
	}
	select {
	case result := <-rpcDone:
		if result.err != nil || result.value["count"] != float64(2) {
			t.Fatalf("unexpected RPC result: %#v err=%v", result.value, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for RPC result")
	}

	if _, response, err := websocket.DefaultDialer.Dial(wsBase+"?pair_token="+pairCode, nil); err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pair token was not single-use: err=%v response=%v", err, response)
	}

	reconnectHeaders := http.Header{"Authorization": []string{"Bearer " + paired.Token}}
	reconnected, response, err := websocket.DefaultDialer.Dial(wsBase, reconnectHeaders)
	if err != nil {
		if response != nil {
			t.Fatalf("agent token reconnect: %v (status %d)", err, response.StatusCode)
		}
		t.Fatalf("agent token reconnect: %v", err)
	}
	defer reconnected.Close()
	if err := reconnected.WriteJSON(Message{Type: MessageHello, ProtocolVersion: ProtocolVersion, DeviceID: "android-1", AgentID: "agent-1"}); err != nil {
		t.Fatalf("reconnect hello: %v", err)
	}
	waitUntil(t, func() bool {
		s, ok := registry.Session("android-1")
		return ok && s != session
	})
}

func assertEventAck(t *testing.T, conn *websocket.Conn, eventID string) {
	t.Helper()
	var ack Message
	if err := conn.ReadJSON(&ack); err != nil {
		t.Fatalf("read event ack: %v", err)
	}
	if ack.Type != MessageEventAck || ack.EventID != eventID {
		t.Fatalf("unexpected event ack: %+v", ack)
	}
}

func TestAgentTokenRejectsTamperingAndExpiry(t *testing.T) {
	registry := NewRegistry()
	registry.SetSigningSecret([]byte("test-secret"))
	token, err := registry.issueAgentToken("device", "agent")
	if err != nil {
		t.Fatalf("issueAgentToken: %v", err)
	}
	claims, err := registry.verifyAgentToken(token)
	if err != nil || claims.DeviceID != "device" || claims.AgentID != "agent" {
		t.Fatalf("verifyAgentToken: claims=%#v err=%v", claims, err)
	}

	parts := strings.Split(token, ".")
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	sig[0] ^= 0xff
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString(sig)
	if _, err := registry.verifyAgentToken(tampered); err == nil {
		t.Fatal("tampered agent token was accepted")
	}

	registry.credentialTTL = -time.Second
	expired, err := registry.issueAgentToken("device", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.verifyAgentToken(expired); err == nil {
		t.Fatal("expired agent token was accepted")
	}
}

func TestEnrollmentCodeBindsFirstAgentAndReturnsDeviceIdentity(t *testing.T) {
	registry := NewRegistry()
	registry.SetSigningSecret([]byte("test-secret"))
	var boundDevice, boundAgent string
	registry.SetPairingHandler(func(deviceID, agentID string) bool {
		boundDevice, boundAgent = deviceID, agentID
		return true
	})
	registry.SetDeviceAuthorizer(func(deviceID, agentID string) bool {
		return deviceID == boundDevice && agentID == boundAgent
	})
	code, _, err := registry.CreateEnrollmentCode("android-auto", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		t.Fatalf("unexpected enrollment code %q", code)
	}
	server := httptest.NewServer(http.HandlerFunc(registry.HandleWebSocket))
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "?pair_token=" + code + "&agent_id=agent-auto"
	client, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var paired Message
	if err := client.ReadJSON(&paired); err != nil {
		t.Fatal(err)
	}
	if paired.Type != MessagePairingComplete || paired.DeviceID != "android-auto" || paired.AgentID != "agent-auto" || paired.Token == "" {
		t.Fatalf("unexpected pairing response: %+v", paired)
	}
	if boundDevice != "android-auto" || boundAgent != "agent-auto" {
		t.Fatalf("pairing handler got device=%q agent=%q", boundDevice, boundAgent)
	}
}

func TestDiscoveryHubRecordsAgentAndSendsApproval(t *testing.T) {
	serverConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	hub := NewDiscoveryHub(7575)
	hub.conn = serverConn
	go hub.readLoop(serverConn)
	defer hub.Close()

	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	request, _ := json.Marshal(discoveryMessage{
		Type: discoveryRequestType, Version: ProtocolVersion, AgentID: "agent-lan",
		Model: "ZTE Test", AppVersion: "1.0", HTTPPort: 8765, Nonce: "nonce-1",
	})
	if _, err := client.WriteToUDP(request, serverConn.LocalAddr().(*net.UDPAddr)); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	buffer := make([]byte, 4096)
	n, _, err := client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	var offer discoveryMessage
	if json.Unmarshal(buffer[:n], &offer) != nil || offer.Type != discoveryOfferType || offer.APIPort != 7575 || offer.Nonce != "nonce-1" {
		t.Fatalf("unexpected offer: %+v", offer)
	}
	agents := hub.List()
	if len(agents) != 1 || agents[0].AgentID != "agent-lan" || agents[0].ManagementURL != "http://127.0.0.1:8765/" {
		t.Fatalf("unexpected candidates: %+v", agents)
	}
	if err := hub.Approve("agent-lan", "android-lan", "123456"); err != nil {
		t.Fatal(err)
	}
	n, _, err = client.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	var approved discoveryMessage
	if json.Unmarshal(buffer[:n], &approved) != nil || approved.Type != discoveryApproveType || approved.DeviceID != "android-lan" || approved.PairCode != "123456" {
		t.Fatalf("unexpected approval: %+v", approved)
	}
}

func TestRemoteStreamRoundTrip(t *testing.T) {
	registry := NewRegistry()
	registry.SetDeviceAuthorizer(func(deviceID, agentID string) bool { return true })
	server := httptest.NewServer(http.HandlerFunc(registry.HandleWebSocket))
	defer server.Close()
	code, _, err := registry.CreatePairCode("device", "agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"?pair_token="+code, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var paired Message
	if err := client.ReadJSON(&paired); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		_, ok := registry.Session("device")
		return ok
	})
	session, _ := registry.Session("device")

	type dialResult struct {
		conn interface {
			Read([]byte) (int, error)
			Write([]byte) (int, error)
			Close() error
		}
		err error
	}
	dialed := make(chan dialResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		conn, err := session.DialContext(ctx, "tcp", "example.test:443")
		dialed <- dialResult{conn: conn, err: err}
	}()
	var open Message
	if err := client.ReadJSON(&open); err != nil {
		t.Fatal(err)
	}
	if open.Type != MessageStreamOpen || open.StreamID == "" || open.Address != "example.test:443" {
		t.Fatalf("unexpected stream open: %#v", open)
	}
	if err := client.WriteJSON(Message{Type: MessageStreamOpened, ProtocolVersion: ProtocolVersion, StreamID: open.StreamID}); err != nil {
		t.Fatal(err)
	}
	result := <-dialed
	if result.err != nil {
		t.Fatalf("DialContext: %v", result.err)
	}
	defer result.conn.Close()

	if err := client.WriteJSON(Message{
		Type: MessageStreamData, ProtocolVersion: ProtocolVersion, StreamID: open.StreamID,
		Data: base64.StdEncoding.EncodeToString([]byte("hello")),
	}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if n, err := result.conn.Read(buf); err != nil || n != 5 || string(buf) != "hello" {
		t.Fatalf("remote read: n=%d data=%q err=%v", n, buf[:n], err)
	}
	if n, err := result.conn.Write([]byte("world")); err != nil || n != 5 {
		t.Fatalf("remote write: n=%d err=%v", n, err)
	}
	var data Message
	if err := client.ReadJSON(&data); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(data.Data)
	if err != nil || data.Type != MessageStreamData || string(decoded) != "world" {
		t.Fatalf("unexpected stream data: %#v decoded=%q err=%v", data, decoded, err)
	}
}

func waitUntil(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become ready")
}
