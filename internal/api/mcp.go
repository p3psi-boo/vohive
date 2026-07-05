package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/iniwex5/vohive/pkg/logger"

	"github.com/gin-gonic/gin"
)

const mcpProtocolVersion = "2025-03-26"

type mcpRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type mcpToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handleMCP(c *gin.Context) {
	if !mcpOriginAllowed(c.Request) {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden origin"})
		return
	}

	switch c.Request.Method {
	case http.MethodPost:
		s.handleMCPPost(c)
	case http.MethodGet:
		c.Header("Allow", "POST, GET")
		c.Status(http.StatusMethodNotAllowed)
	case http.MethodDelete:
		c.Status(http.StatusMethodNotAllowed)
	case http.MethodOptions:
		c.Header("Allow", "POST, GET")
		c.Status(http.StatusNoContent)
	default:
		c.Header("Allow", "POST, GET")
		c.Status(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMCPPost(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, mcpErrorResponse(nil, -32700, "failed to read request body", nil))
		return
	}
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		c.JSON(http.StatusBadRequest, mcpErrorResponse(nil, -32700, "empty request body", nil))
		return
	}

	if body[0] == '[' {
		var reqs []mcpRequest
		if err := json.Unmarshal(body, &reqs); err != nil {
			c.JSON(http.StatusOK, mcpErrorResponse(nil, -32700, "parse error", err.Error()))
			return
		}
		responses := make([]mcpResponse, 0, len(reqs))
		for _, req := range reqs {
			if req.ID == nil {
				_ = s.handleMCPNotification(req)
				continue
			}
			responses = append(responses, s.handleMCPRequest(c, req))
		}
		if len(responses) == 0 {
			c.Status(http.StatusAccepted)
			return
		}
		c.JSON(http.StatusOK, responses)
		return
	}

	var req mcpRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusOK, mcpErrorResponse(nil, -32700, "parse error", err.Error()))
		return
	}
	if req.ID == nil {
		_ = s.handleMCPNotification(req)
		c.Status(http.StatusAccepted)
		return
	}
	c.JSON(http.StatusOK, s.handleMCPRequest(c, req))
}

func (s *Server) handleMCPNotification(req mcpRequest) error {
	switch req.Method {
	case "notifications/initialized":
		return nil
	default:
		return nil
	}
}

func (s *Server) handleMCPRequest(c *gin.Context, req mcpRequest) mcpResponse {
	if req.JSONRPC != "2.0" {
		return mcpErrorResponse(req.ID, -32600, "invalid JSON-RPC version", nil)
	}
	switch req.Method {
	case "initialize":
		return mcpOK(req.ID, gin.H{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": gin.H{
				"tools": gin.H{"listChanged": false},
			},
			"serverInfo": gin.H{
				"name":    "vohive-mcp",
				"version": "1.0.0",
			},
			"instructions": "VoHive MCP exposes side-effecting tools for sending SMS and switching eSIM profiles. Confirm recipient, message, device_id, and ICCID before calling.",
		})
	case "ping":
		return mcpOK(req.ID, gin.H{})
	case "tools/list":
		return mcpOK(req.ID, gin.H{"tools": mcpTools()})
	case "tools/call":
		var params mcpToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return mcpErrorResponse(req.ID, -32602, "invalid tools/call params", err.Error())
		}
		return s.handleMCPToolCall(c, req.ID, params)
	default:
		return mcpErrorResponse(req.ID, -32601, "method not found: "+req.Method, nil)
	}
}

func (s *Server) handleMCPToolCall(c *gin.Context, id *json.RawMessage, params mcpToolCallParams) mcpResponse {
	switch params.Name {
	case "send_sms":
		var args sendSMSRequest
		if err := decodeMCPArguments(params.Arguments, &args); err != nil {
			return mcpErrorResponse(id, -32602, "invalid send_sms arguments", err.Error())
		}
		result, _, err := s.sendSMS(c.Request.Context(), args)
		if err != nil {
			logger.Warn("MCP send_sms failed", "device_id", args.DeviceID, "phone", args.Phone, "err", err)
			return mcpOK(id, mcpToolTextResult(result, true))
		}
		logger.Info("MCP send_sms completed", "device_id", result.Device, "phone", result.Phone, "message_id", result.MessageID)
		return mcpOK(id, mcpToolTextResult(result, false))
	case "switch_esim_profile":
		var args struct {
			DeviceID string `json:"device_id"`
			ICCID    string `json:"iccid"`
			AIDHex   string `json:"aid_hex"`
		}
		if err := decodeMCPArguments(params.Arguments, &args); err != nil {
			return mcpErrorResponse(id, -32602, "invalid switch_esim_profile arguments", err.Error())
		}
		result, _, err := s.switchESIMProfile(c.Request.Context(), args.DeviceID, esimSwitchRequest{ICCID: args.ICCID, AIDHex: args.AIDHex})
		if err != nil {
			logger.Warn("MCP switch_esim_profile failed", "device_id", args.DeviceID, "iccid", args.ICCID, "err", err)
			return mcpOK(id, mcpToolTextResult(gin.H{"error": err.Error(), "device_id": args.DeviceID, "iccid": args.ICCID}, true))
		}
		logger.Info("MCP switch_esim_profile completed", "device_id", args.DeviceID, "target_iccid", result.TargetICCID, "switch_token", result.SwitchToken)
		return mcpOK(id, mcpToolTextResult(result, false))
	default:
		return mcpErrorResponse(id, -32602, "unknown tool: "+params.Name, nil)
	}
}

func decodeMCPArguments(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return errors.New("arguments must be an object")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return err
	}
	return nil
}

func mcpToolTextResult(v any, isError bool) gin.H {
	payload, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		payload = []byte(fmt.Sprint(v))
	}
	return gin.H{
		"content": []gin.H{{
			"type": "text",
			"text": string(payload),
		}},
		"isError": isError,
	}
}

func mcpTools() []gin.H {
	return []gin.H{
		{
			"name":        "send_sms",
			"description": "Send an SMS through a VoHive-managed modem. If device_id is omitted, VoHive uses the only online device; otherwise provide device_id or imsi. This sends a real SMS.",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"device_id": gin.H{"type": "string", "description": "VoHive device ID. Required when multiple devices are online unless imsi is provided."},
					"imsi":      gin.H{"type": "string", "description": "SIM IMSI selector. Optional alternative to device_id."},
					"phone":     gin.H{"type": "string", "description": "Recipient phone number."},
					"message":   gin.H{"type": "string", "description": "SMS message body."},
					"encoding":  gin.H{"type": "string", "description": "Optional SMS encoding override.", "enum": []string{"", "auto", "gsm7", "ucs2"}},
				},
				"required":             []string{"phone", "message"},
				"additionalProperties": false,
			},
			"annotations": gin.H{
				"title":           "Send SMS",
				"destructiveHint": true,
				"openWorldHint":   true,
			},
		},
		{
			"name":        "switch_esim_profile",
			"description": "Switch the active eSIM profile on a VoHive-managed device. This changes the SIM identity used by the modem and may interrupt service.",
			"inputSchema": gin.H{
				"type": "object",
				"properties": gin.H{
					"device_id": gin.H{"type": "string", "description": "VoHive device ID."},
					"iccid":     gin.H{"type": "string", "description": "Target eSIM profile ICCID."},
					"aid_hex":   gin.H{"type": "string", "description": "Optional eUICC AID hex when known."},
				},
				"required":             []string{"device_id", "iccid"},
				"additionalProperties": false,
			},
			"annotations": gin.H{
				"title":           "Switch eSIM Profile",
				"destructiveHint": true,
				"openWorldHint":   false,
			},
		},
	}
}

func mcpOK(id *json.RawMessage, result any) mcpResponse {
	return mcpResponse{JSONRPC: "2.0", ID: mcpID(id), Result: result}
}

func mcpErrorResponse(id *json.RawMessage, code int, message string, data any) mcpResponse {
	return mcpResponse{
		JSONRPC: "2.0",
		ID:      mcpID(id),
		Error:   &mcpError{Code: code, Message: message, Data: data},
	}
}

func mcpID(id *json.RawMessage) json.RawMessage {
	if id == nil || len(*id) == 0 {
		return json.RawMessage("null")
	}
	return *id
}

func mcpOriginAllowed(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := normalizeHost(u.Host)
	requestHost := normalizeHost(r.Host)
	if originHost == "" || requestHost == "" {
		return false
	}
	return originHost == requestHost || (isLocalHost(originHost) && isLocalHost(requestHost))
}

func normalizeHost(hostport string) string {
	host := strings.TrimSpace(hostport)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.ToLower(strings.Trim(host, "[]"))
}

func isLocalHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
