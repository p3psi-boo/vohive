package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/iniwex5/vohive/internal/androidagent"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/db"
	"github.com/iniwex5/vohive/internal/device"
	proxyserver "github.com/iniwex5/vohive/internal/proxy/server"
)

func TestAndroidAgentAPIEndToEnd(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	raw := `server:
  port: ":0"
web:
  username: admin
  password: secret
devices:
  - id: android-1
    name: ZTE Test
    device_kind: android
    device_backend: android
    android:
      agent_id: agent-1
`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.InitGlobalManager(configPath); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Init(filepath.Join(t.TempDir(), "vohive.db")); err != nil {
		t.Fatal(err)
	}
	pool := device.NewPool(cfg)
	defer pool.Shutdown()
	proxyManager := proxyserver.NewManager()
	defer proxyManager.Shutdown(context.Background())
	server := New(cfg, pool, nil, proxyManager, nil, nil, configPath)
	authToken, _, err := server.issueSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.newRouter())
	defer httpServer.Close()

	pairing := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodPost,
		httpServer.URL+"/api/devices/android-1/android-agent/pairing-token", nil)
	if pairing.StatusCode != http.StatusOK {
		t.Fatalf("pairing token: status=%d body=%s", pairing.StatusCode, readBody(pairing))
	}
	var pairResponse androidPairingTokenResponse
	decodeResponse(t, pairing, &pairResponse)
	if pairResponse.Token == "" || pairResponse.DeviceID != "android-1" {
		t.Fatalf("unexpected pairing response: %+v", pairResponse)
	}

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/android-agent/connect"
	agentHeaders := http.Header{"Authorization": []string{"VoHivePair " + pairResponse.Token}}
	agent, response, err := websocket.DefaultDialer.Dial(wsURL, agentHeaders)
	if err != nil {
		if response != nil {
			t.Fatalf("agent connect: %v, status=%d", err, response.StatusCode)
		}
		t.Fatal(err)
	}
	defer agent.Close()
	var paired androidagent.Message
	if err := agent.ReadJSON(&paired); err != nil {
		t.Fatal(err)
	}
	if paired.Type != androidagent.MessagePairingComplete || paired.Token == "" {
		t.Fatalf("unexpected pairing handshake: %+v", paired)
	}
	if err := agent.WriteJSON(androidagent.Message{
		Type: androidagent.MessageHello, ProtocolVersion: androidagent.ProtocolVersion,
		DeviceID: "android-1", AgentID: "agent-1",
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := androidagent.StatusSnapshot{
		IMEI: "356000000000001", ICCID: "8986000000000000001",
		MSISDN: "+8613800000000", SignalDBM: -79, SignalRSRP: -98, SignalRSRQ: -11,
		SignalSINR: 20, BatteryPct: 88, RegStatus: 1, RegStatusText: "registered_home",
		SelectedSubscriptionID: 7, DataConnected: true,
		Subscriptions: []androidagent.Subscription{{
			SubscriptionID: 7, ICCID: "8986000000000000001", Active: true, Selected: true,
		}},
	}
	if err := agent.WriteJSON(androidagent.Message{
		Type: androidagent.MessageStatusSnapshot, ProtocolVersion: androidagent.ProtocolVersion,
		Snapshot: &snapshot,
	}); err != nil {
		t.Fatal(err)
	}

	agentErrors := make(chan error, 1)
	go serveFakeAndroidAgent(agent, agentErrors)

	deadline := time.Now().Add(3 * time.Second)
	for {
		status := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodGet,
			httpServer.URL+"/api/devices/android-1/android-agent/status", nil)
		var payload struct {
			Online   bool                        `json:"online"`
			Snapshot androidagent.StatusSnapshot `json:"snapshot"`
		}
		decodeResponse(t, status, &payload)
		if payload.Online && payload.Snapshot.IMEI == snapshot.IMEI {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("status snapshot did not converge: %+v", payload)
		}
		time.Sleep(20 * time.Millisecond)
	}
	subscriptions := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodGet,
		httpServer.URL+"/api/devices/android-1/android-agent/subscriptions", nil)
	if subscriptions.StatusCode != http.StatusOK || !strings.Contains(readBody(subscriptions), `"subscription_id":7`) {
		t.Fatalf("subscriptions endpoint failed: status=%d", subscriptions.StatusCode)
	}

	selectSub := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodPost,
		httpServer.URL+"/api/devices/android-1/android-agent/subscriptions/select",
		[]byte(`{"subscription_id":7}`))
	if selectSub.StatusCode != http.StatusOK {
		t.Fatalf("select subscription: status=%d body=%s", selectSub.StatusCode, readBody(selectSub))
	}

	switchESIM := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodPost,
		httpServer.URL+"/api/devices/android-1/android-agent/esim/switch",
		[]byte(`{"subscription_id":7,"port_index":0}`))
	if switchESIM.StatusCode != http.StatusOK {
		t.Fatalf("switch eSIM: status=%d body=%s", switchESIM.StatusCode, readBody(switchESIM))
	}
	esimDeadline := time.Now().Add(3 * time.Second)
	for {
		status := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodGet,
			httpServer.URL+"/api/devices/android-1/android-agent/status", nil)
		statusBody := readBody(status)
		if status.StatusCode == http.StatusOK && strings.Contains(statusBody, `"esim_operation":{"operation":"switch","state":"completed"`) {
			break
		}
		if time.Now().After(esimDeadline) {
			t.Fatalf("eSIM result did not converge: status=%d body=%s", status.StatusCode, statusBody)
		}
		time.Sleep(20 * time.Millisecond)
	}

	sent := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodPost,
		httpServer.URL+"/api/sms/send",
		[]byte(`{"device_id":"android-1","phone":"+15551234567","message":"multipart test","subscription_id":7}`))
	sentBody := readBody(sent)
	if sent.StatusCode != http.StatusOK || !strings.Contains(sentBody, `"message_id":"sms-test-1"`) ||
		!strings.Contains(sentBody, `"parts_total":2`) {
		t.Fatalf("SMS send: status=%d body=%s", sent.StatusCode, sentBody)
	}
	deliveryDeadline := time.Now().Add(3 * time.Second)
	for {
		delivery := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodGet,
			httpServer.URL+"/api/sms/delivery/sms-test-1", nil)
		deliveryBody := readBody(delivery)
		if delivery.StatusCode == http.StatusOK && strings.Contains(deliveryBody, `"state":"acked"`) &&
			strings.Contains(deliveryBody, `"acks":2`) {
			break
		}
		if time.Now().After(deliveryDeadline) {
			t.Fatalf("SMS delivery did not converge: status=%d body=%s", delivery.StatusCode, deliveryBody)
		}
		time.Sleep(20 * time.Millisecond)
	}
	incomingDeadline := time.Now().Add(3 * time.Second)
	for {
		rows, queryErr := db.GetSMSByIMSI("android:android-1:iccid:8986000000000000001", 20)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		found := false
		for _, row := range rows {
			if row.Content == "incoming synthetic identity" {
				found = true
				break
			}
		}
		if found {
			break
		}
		if time.Now().After(incomingDeadline) {
			t.Fatalf("incoming Android SMS was not persisted under synthetic SIM identity: %+v", rows)
		}
		time.Sleep(20 * time.Millisecond)
	}

	messages := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodGet,
		httpServer.URL+"/api/devices/android-1/android-agent/sms?limit=20", nil)
	messageBody := readBody(messages)
	if messages.StatusCode != http.StatusOK || !strings.Contains(messageBody, `"content":"hello from ZTE"`) {
		t.Fatalf("SMS list: status=%d body=%s", messages.StatusCode, messageBody)
	}
	readLargeID := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodGet,
		httpServer.URL+"/api/devices/android-1/android-agent/sms/4294967297", nil)
	readLargeBody := readBody(readLargeID)
	if readLargeID.StatusCode != http.StatusOK || !strings.Contains(readLargeBody, `"index":4294967297`) ||
		!strings.Contains(readLargeBody, `"content":"one message"`) {
		t.Fatalf("SMS read 64-bit ID: status=%d body=%s", readLargeID.StatusCode, readLargeBody)
	}

	deleted := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodDelete,
		httpServer.URL+"/api/devices/android-1/android-agent/sms/42", nil)
	if deleted.StatusCode != http.StatusOK {
		t.Fatalf("SMS delete: status=%d body=%s", deleted.StatusCode, readBody(deleted))
	}
	deletedLargeID := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodDelete,
		httpServer.URL+"/api/devices/android-1/android-agent/sms/4294967297", nil)
	if deletedLargeID.StatusCode != http.StatusOK {
		t.Fatalf("SMS delete 64-bit ID: status=%d body=%s", deletedLargeID.StatusCode, readBody(deletedLargeID))
	}
	deletedAll := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodDelete,
		httpServer.URL+"/api/devices/android-1/android-agent/sms", nil)
	if deletedAll.StatusCode != http.StatusOK {
		t.Fatalf("SMS delete all: status=%d body=%s", deletedAll.StatusCode, readBody(deletedAll))
	}

	probeListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyPort := probeListener.Addr().(*net.TCPAddr).Port
	probeListener.Close()
	proxyConfig := fmt.Sprintf(`{"instances":[{"id":"android-http","name":"Android HTTP","device_id":"android-1","enabled":true,"mode":"http","listen_addr":"127.0.0.1","listen_port":%d,"auth_enabled":false}]}`, proxyPort)
	proxyUpdate := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodPut,
		httpServer.URL+"/api/proxy-instances/config", []byte(proxyConfig))
	proxyUpdateBody := readBody(proxyUpdate)
	if proxyUpdate.StatusCode != http.StatusOK || !strings.Contains(proxyUpdateBody, `"applied":true`) {
		t.Fatalf("proxy update: status=%d body=%s", proxyUpdate.StatusCode, proxyUpdateBody)
	}
	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", proxyPort))
	if err != nil {
		t.Fatal(err)
	}
	proxyClient := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, Timeout: 3 * time.Second}
	proxyResponse, err := proxyClient.Get("http://example.test/android-agent-proxy")
	if err != nil {
		t.Fatalf("Android HTTP proxy request: %v", err)
	}
	if proxyResponse.StatusCode != http.StatusOK || readBody(proxyResponse) != "OK" {
		t.Fatalf("Android HTTP proxy response status=%d", proxyResponse.StatusCode)
	}

	select {
	case err := <-agentErrors:
		if err != nil {
			t.Fatalf("fake Android agent: %v", err)
		}
	default:
	}
}

func TestAndroidEnrollmentCodeCreatesDeviceBindsAgentAndCreatesProxyDefaults(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	raw := `server:
  port: ":7575"
web:
  username: admin
  password: secret
devices: []
`
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.InitGlobalManager(configPath); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Init(filepath.Join(t.TempDir(), "vohive.db")); err != nil {
		t.Fatal(err)
	}
	pool := device.NewPool(cfg)
	defer pool.Shutdown()
	server := New(cfg, pool, nil, nil, nil, nil, configPath)
	authToken, _, err := server.issueSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(server.newRouter())
	defer httpServer.Close()

	response := androidAPIRequest(t, httpServer.Client(), authToken, http.MethodPost,
		httpServer.URL+"/api/android-agents/pairing-code", []byte(`{"name":"书房手机"}`))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("pairing code: status=%d body=%s", response.StatusCode, readBody(response))
	}
	var enrollment androidEnrollmentResponse
	decodeResponse(t, response, &enrollment)
	if len(enrollment.Code) != 6 || enrollment.DeviceID == "" || enrollment.ServerURL != httpServer.URL {
		t.Fatalf("unexpected enrollment: %+v", enrollment)
	}
	reserved, err := config.GetDeviceByID(enrollment.DeviceID)
	if err != nil || reserved != nil {
		t.Fatalf("unused enrollment must not create a visible device: %+v err=%v", reserved, err)
	}

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/android-agent/connect?agent_id=agent-personal"
	headers := http.Header{"Authorization": []string{"VoHivePair " + enrollment.Code}}
	agent, wsResponse, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		if wsResponse != nil {
			t.Fatalf("agent connect: %v status=%d", err, wsResponse.StatusCode)
		}
		t.Fatal(err)
	}
	defer agent.Close()
	var paired androidagent.Message
	if err := agent.ReadJSON(&paired); err != nil {
		t.Fatal(err)
	}
	if paired.DeviceID != enrollment.DeviceID || paired.AgentID != "agent-personal" || paired.Token == "" {
		t.Fatalf("unexpected pairing response: %+v", paired)
	}
	bound, err := config.GetDeviceByID(enrollment.DeviceID)
	if err != nil || bound == nil || bound.Android.AgentID != "agent-personal" || bound.Name != "书房手机" {
		t.Fatalf("agent binding was not persisted: %+v err=%v", bound, err)
	}
	instances, err := server.proxyRepo.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 || instances[0].DeviceID != enrollment.DeviceID || instances[1].DeviceID != enrollment.DeviceID {
		t.Fatalf("unexpected personal proxy defaults: %+v", instances)
	}
	modes := map[string]bool{instances[0].Mode: true, instances[1].Mode: true}
	if !modes[proxyserver.ModeHTTP] || !modes[proxyserver.ModeSocks5] {
		t.Fatalf("expected HTTP and SOCKS5 defaults: %+v", instances)
	}
}

func serveFakeAndroidAgent(conn *websocket.Conn, errors chan<- error) {
	respondedStreams := make(map[string]bool)
	for {
		var request androidagent.Message
		if err := conn.ReadJSON(&request); err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				errors <- nil
			} else {
				errors <- err
			}
			return
		}
		if request.Type == androidagent.MessageStreamOpen {
			if err := conn.WriteJSON(androidagent.Message{
				Type: androidagent.MessageStreamOpened, ProtocolVersion: androidagent.ProtocolVersion,
				StreamID: request.StreamID,
			}); err != nil {
				errors <- err
				return
			}
			continue
		}
		if request.Type == androidagent.MessageStreamData {
			if !respondedStreams[request.StreamID] {
				respondedStreams[request.StreamID] = true
				response := "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nOK"
				if err := conn.WriteJSON(androidagent.Message{
					Type: androidagent.MessageStreamData, ProtocolVersion: androidagent.ProtocolVersion,
					StreamID: request.StreamID, Data: base64.StdEncoding.EncodeToString([]byte(response)),
				}); err != nil {
					errors <- err
					return
				}
				if err := conn.WriteJSON(androidagent.Message{
					Type: androidagent.MessageStreamClose, ProtocolVersion: androidagent.ProtocolVersion,
					StreamID: request.StreamID,
				}); err != nil {
					errors <- err
					return
				}
			}
			continue
		}
		if request.Type != androidagent.MessageRPCRequest {
			continue
		}
		result := map[string]any{}
		switch request.Method {
		case "subscriptions.list":
			result["subscriptions"] = []any{map[string]any{
				"subscription_id": 7, "slot_index": 0, "carrier_name": "Test Carrier",
				"active": true, "selected": true,
			}}
		case "sms.list":
			result["messages"] = []any{map[string]any{
				"index": 42, "sender": "+10086", "content": "hello from ZTE",
				"timestamp": "2026-07-31T00:00:00Z", "type": 1, "tag": 1,
				"subscription_id": 7,
			}}
		case "sms.read":
			result["index"] = 4294967297
			result["sender"] = "+10010"
			result["content"] = "one message"
			result["timestamp"] = "2026-07-31T00:00:00Z"
			result["type"] = 1
			result["tag"] = 1
			result["subscription_id"] = 7
		case "sms.delete":
			result["deleted"] = 1
		case "sms.delete_all":
			result["deleted"] = 1
		case "sms.send":
			result["message_id"] = "sms-test-1"
			result["parts_total"] = 2
			result["subscription_id"] = 7
			result["state"] = "queued"
		case "subscriptions.select":
			result["subscription_id"] = 7
		case "esim.switch":
			result["state"] = "requested"
		}
		if err := conn.WriteJSON(androidagent.Message{
			Type: androidagent.MessageRPCResponse, ProtocolVersion: androidagent.ProtocolVersion,
			RequestID: request.RequestID, Result: result,
		}); err != nil {
			errors <- err
			return
		}
		if request.Method == "sms.send" {
			time.Sleep(50 * time.Millisecond)
			for part := 1; part <= 2; part++ {
				if err := conn.WriteJSON(androidagent.Message{
					Type: androidagent.MessageSMSStatus, ProtocolVersion: androidagent.ProtocolVersion,
					EventID: fmt.Sprintf("sms-status-%d", part),
					Result: map[string]any{
						"message_id": "sms-test-1", "part": part, "parts_total": 2,
						"state": "delivered", "result_code": -1, "subscription_id": 7,
						"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
					},
				}); err != nil {
					errors <- err
					return
				}
			}
			if err := conn.WriteJSON(androidagent.Message{
				Type: androidagent.MessageSMSReceived, ProtocolVersion: androidagent.ProtocolVersion,
				EventID: "sms-received-synthetic-1",
				Result: map[string]any{
					"message_id": "received-1", "sender": "+10086",
					"content": "incoming synthetic identity", "subscription_id": 7,
					"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
				},
			}); err != nil {
				errors <- err
				return
			}
		}
		if request.Method == "esim.switch" {
			if err := conn.WriteJSON(androidagent.Message{
				Type: androidagent.MessageESIMStatus, ProtocolVersion: androidagent.ProtocolVersion,
				EventID: "esim-status-1",
				Result: map[string]any{
					"operation": "switch", "state": "completed", "subscription_id": 7,
					"port_index": 0, "result_code": -1, "detailed_code": 0,
					"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
				},
			}); err != nil {
				errors <- err
				return
			}
		}
	}
}

func androidAPIRequest(t *testing.T, client *http.Client, token, method, url string, body []byte) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func decodeResponse(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode status=%d: %v", response.StatusCode, err)
	}
}

func readBody(response *http.Response) string {
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Sprintf("<read error: %v>", err)
	}
	return string(body)
}
