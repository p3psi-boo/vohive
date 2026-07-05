package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/device"
)

func newTestMCPServer(t *testing.T) (*Server, string, http.Handler) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("web:\n  username: admin\n  password: secret\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := &config.Config{
		Server: config.ServerConfig{Port: ":0"},
		Web:    config.WebConfig{Username: "admin", Password: "secret"},
	}
	pool := device.NewPool(cfg)
	s := New(cfg, pool, nil, nil, nil, nil, configPath)
	token, _, err := s.issueSessionToken()
	if err != nil {
		t.Fatalf("issueSessionToken() error = %v", err)
	}
	return s, token, s.newRouter()
}

func postMCP(t *testing.T, handler http.Handler, token string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestMCPRequiresAuth(t *testing.T) {
	_, _, handler := newTestMCPServer(t)

	rr := postMCP(t, handler, "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("POST /mcp status=%d body=%s, want 401", rr.Code, rr.Body.String())
	}
}

func TestMCPInitializeAndListTools(t *testing.T) {
	_, token, handler := newTestMCPServer(t)

	rr := postMCP(t, handler, token, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("initialize status=%d body=%s", rr.Code, rr.Body.String())
	}
	var initResp struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			Capabilities    struct {
				Tools map[string]any `json:"tools"`
			} `json:"capabilities"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &initResp); err != nil {
		t.Fatalf("decode initialize response: %v", err)
	}
	if initResp.Result.ProtocolVersion != mcpProtocolVersion {
		t.Fatalf("protocolVersion=%q, want %q", initResp.Result.ProtocolVersion, mcpProtocolVersion)
	}
	if initResp.Result.Capabilities.Tools == nil {
		t.Fatal("initialize response did not advertise tools capability")
	}

	rr = postMCP(t, handler, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("tools/list status=%d body=%s", rr.Code, rr.Body.String())
	}
	var listResp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode tools/list response: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range listResp.Result.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"send_sms", "switch_esim_profile"} {
		if !got[want] {
			t.Fatalf("tools/list missing %q in %#v", want, got)
		}
	}
	if len(listResp.Result.Tools) != 2 {
		t.Fatalf("tools/list returned %d tools, want 2", len(listResp.Result.Tools))
	}
}

func TestMCPToolExecutionErrorsUseToolResult(t *testing.T) {
	_, token, handler := newTestMCPServer(t)

	rr := postMCP(t, handler, token, `{
		"jsonrpc":"2.0",
		"id":3,
		"method":"tools/call",
		"params":{
			"name":"send_sms",
			"arguments":{"phone":"+15551234567","message":"hello"}
		}
	}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("tools/call status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Error  any `json:"error"`
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode tools/call response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("expected tool result error, got protocol error: %#v", resp.Error)
	}
	if !resp.Result.IsError {
		t.Fatalf("isError=false body=%s", rr.Body.String())
	}
	if len(resp.Result.Content) != 1 || resp.Result.Content[0].Type != "text" {
		t.Fatalf("unexpected content: %#v", resp.Result.Content)
	}
}

func TestMCPRejectsCrossOrigin(t *testing.T) {
	_, token, handler := newTestMCPServer(t)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Host = "vohive.local"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", rr.Code, rr.Body.String())
	}
}

func TestMCPKeyLifecycle(t *testing.T) {
	_, token, handler := newTestMCPServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/mcp-key", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("generate key status=%d body=%s", rr.Code, rr.Body.String())
	}
	var generated struct {
		Key       string `json:"key"`
		KeySuffix string `json:"key_suffix"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &generated); err != nil {
		t.Fatalf("decode generated key: %v", err)
	}
	if generated.Key == "" || generated.KeySuffix == "" {
		t.Fatalf("generated key response missing fields: %#v", generated)
	}

	rr = postMCP(t, handler, generated.Key, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("mcp with generated key status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settings/mcp-key", nil)
	req.Header.Set("Authorization", "Bearer "+generated.Key)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("settings accepted MCP key status=%d body=%s, want 401", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/settings/mcp-key", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("revoke key status=%d body=%s", rr.Code, rr.Body.String())
	}

	rr = postMCP(t, handler, generated.Key, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key status=%d body=%s, want 401", rr.Code, rr.Body.String())
	}
}
