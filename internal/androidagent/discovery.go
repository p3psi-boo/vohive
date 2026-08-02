package androidagent

import (
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DiscoveryPort = 8764

const (
	discoveryRequestType = "vohive_agent_discover"
	discoveryOfferType   = "vohive_server_offer"
	discoveryApproveType = "vohive_pair_approved"
)

type DiscoveredAgent struct {
	AgentID       string    `json:"agent_id"`
	Model         string    `json:"model"`
	AppVersion    string    `json:"app_version"`
	Address       string    `json:"address"`
	ManagementURL string    `json:"management_url"`
	LastSeen      time.Time `json:"last_seen"`

	remote *net.UDPAddr
	nonce  string
}

type discoveryMessage struct {
	Type        string `json:"type"`
	Version     int    `json:"version"`
	AgentID     string `json:"agent_id,omitempty"`
	Model       string `json:"model,omitempty"`
	AppVersion  string `json:"app_version,omitempty"`
	HTTPPort    int    `json:"http_port,omitempty"`
	APIPort     int    `json:"api_port,omitempty"`
	Nonce       string `json:"nonce,omitempty"`
	DeviceID    string `json:"device_id,omitempty"`
	PairCode    string `json:"pair_code,omitempty"`
	ServerLabel string `json:"server_label,omitempty"`
}

// DiscoveryHub implements the small UDP protocol used by unpaired Android
// agents. Discovery is intentionally only a hint; pairing still requires an
// authenticated approval in the VoHive web console.
type DiscoveryHub struct {
	mu         sync.RWMutex
	conn       *net.UDPConn
	candidates map[string]DiscoveredAgent
	apiPort    int
}

func NewDiscoveryHub(apiPort int) *DiscoveryHub {
	return &DiscoveryHub{candidates: make(map[string]DiscoveredAgent), apiPort: apiPort}
}

func (h *DiscoveryHub) Start() error {
	if h == nil {
		return errors.New("android discovery hub is unavailable")
	}
	h.mu.Lock()
	if h.conn != nil {
		h.mu.Unlock()
		return nil
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: DiscoveryPort})
	if err != nil {
		h.mu.Unlock()
		return err
	}
	h.conn = conn
	h.mu.Unlock()
	go h.readLoop(conn)
	return nil
}

func (h *DiscoveryHub) Close() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	conn := h.conn
	h.conn = nil
	h.mu.Unlock()
	if conn == nil {
		return nil
	}
	return conn.Close()
}

func (h *DiscoveryHub) List() []DiscoveredAgent {
	if h == nil {
		return []DiscoveredAgent{}
	}
	cutoff := time.Now().Add(-30 * time.Second)
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]DiscoveredAgent, 0, len(h.candidates))
	for id, candidate := range h.candidates {
		if candidate.LastSeen.Before(cutoff) {
			delete(h.candidates, id)
			continue
		}
		candidate.remote = nil
		candidate.nonce = ""
		out = append(out, candidate)
	}
	return out
}

func (h *DiscoveryHub) Approve(agentID, deviceID, pairCode string) error {
	if h == nil {
		return errors.New("android discovery hub is unavailable")
	}
	h.mu.RLock()
	candidate, ok := h.candidates[strings.TrimSpace(agentID)]
	conn := h.conn
	h.mu.RUnlock()
	if !ok || candidate.remote == nil || candidate.LastSeen.Before(time.Now().Add(-30*time.Second)) {
		return errors.New("android agent is no longer discoverable")
	}
	if conn == nil {
		return errors.New("android discovery listener is not running")
	}
	payload, err := json.Marshal(discoveryMessage{
		Type: discoveryApproveType, Version: ProtocolVersion,
		AgentID: agentID, DeviceID: deviceID, PairCode: pairCode,
		APIPort: h.apiPort, Nonce: candidate.nonce,
	})
	if err != nil {
		return err
	}
	_, err = conn.WriteToUDP(payload, candidate.remote)
	return err
}

func (h *DiscoveryHub) readLoop(conn *net.UDPConn) {
	buffer := make([]byte, 4096)
	for {
		n, remote, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		var message discoveryMessage
		if json.Unmarshal(buffer[:n], &message) != nil || message.Type != discoveryRequestType || message.Version != ProtocolVersion {
			continue
		}
		agentID := strings.TrimSpace(message.AgentID)
		if agentID == "" || len(agentID) > 128 || message.HTTPPort < 1 || message.HTTPPort > 65535 {
			continue
		}
		candidate := DiscoveredAgent{
			AgentID: agentID, Model: strings.TrimSpace(message.Model),
			AppVersion: strings.TrimSpace(message.AppVersion), Address: remote.IP.String(),
			ManagementURL: "http://" + net.JoinHostPort(remote.IP.String(), strconv.Itoa(message.HTTPPort)) + "/",
			LastSeen:      time.Now().UTC(), remote: remote, nonce: strings.TrimSpace(message.Nonce),
		}
		h.mu.Lock()
		h.candidates[agentID] = candidate
		h.mu.Unlock()

		offer, _ := json.Marshal(discoveryMessage{
			Type: discoveryOfferType, Version: ProtocolVersion, APIPort: h.apiPort,
			Nonce: candidate.nonce, ServerLabel: "VoHive",
		})
		_, _ = conn.WriteToUDP(offer, remote)
	}
}
