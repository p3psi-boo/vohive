package androidagent

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var ErrSessionNotFound = errors.New("android agent session not found")

type SMSHandler func(deviceID string, event SMSReceivedEvent)
type SMSStatusHandler func(deviceID string, event SMSStatusEvent)
type SessionHandler func(deviceID string)
type DeviceAuthorizer func(deviceID, agentID string) bool

type Registry struct {
	mu            sync.RWMutex
	sessions      map[string]*Session
	pairCodes     map[string]pairCode
	seenEvents    map[string]time.Time
	onSMS         SMSHandler
	onSMSStatus   SMSStatusHandler
	onSession     SessionHandler
	authorizer    DeviceAuthorizer
	signingSecret []byte
	credentialTTL time.Duration
	upgrader      websocket.Upgrader
}

type pairCode struct {
	DeviceID string
	AgentID  string
	Expires  time.Time
}

type agentClaims struct {
	DeviceID string `json:"device_id"`
	AgentID  string `json:"agent_id"`
	Expires  int64  `json:"expires"`
	Nonce    string `json:"nonce"`
}

func NewRegistry() *Registry {
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	return &Registry{
		sessions:      make(map[string]*Session),
		pairCodes:     make(map[string]pairCode),
		seenEvents:    make(map[string]time.Time),
		signingSecret: secret,
		credentialTTL: 365 * 24 * time.Hour,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (r *Registry) claimEvent(deviceID, eventID string) bool {
	if r == nil || strings.TrimSpace(eventID) == "" {
		return true
	}
	key := strings.TrimSpace(deviceID) + "\x00" + strings.TrimSpace(eventID)
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.seenEvents[key]; exists {
		return false
	}
	r.seenEvents[key] = now
	if len(r.seenEvents) > 10_000 {
		cutoff := now.Add(-24 * time.Hour)
		for id, seenAt := range r.seenEvents {
			if seenAt.Before(cutoff) {
				delete(r.seenEvents, id)
			}
		}
		if len(r.seenEvents) > 10_000 {
			oldestID := ""
			oldestAt := now
			for id, seenAt := range r.seenEvents {
				if oldestID == "" || seenAt.Before(oldestAt) {
					oldestID, oldestAt = id, seenAt
				}
			}
			delete(r.seenEvents, oldestID)
		}
	}
	return true
}

func (r *Registry) SetSigningSecret(secret []byte) {
	if r == nil || len(secret) == 0 {
		return
	}
	sum := sha256.Sum256(append([]byte("vohive/android-agent/v1/"), secret...))
	r.mu.Lock()
	r.signingSecret = append([]byte(nil), sum[:]...)
	r.mu.Unlock()
}

func (r *Registry) SetDeviceAuthorizer(authorizer DeviceAuthorizer) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.authorizer = authorizer
	r.mu.Unlock()
}

func (r *Registry) SetSMSHandler(handler SMSHandler) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.onSMS = handler
	r.mu.Unlock()
}

func (r *Registry) SetSMSStatusHandler(handler SMSStatusHandler) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.onSMSStatus = handler
	r.mu.Unlock()
}

func (r *Registry) SetSessionHandler(handler SessionHandler) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.onSession = handler
	r.mu.Unlock()
}

func (r *Registry) CreatePairCode(deviceID, agentID string, ttl time.Duration) (string, time.Time, error) {
	if r == nil {
		return "", time.Time{}, errors.New("android agent registry not available")
	}
	deviceID = strings.TrimSpace(deviceID)
	agentID = strings.TrimSpace(agentID)
	if deviceID == "" || agentID == "" {
		return "", time.Time{}, errors.New("device_id and agent_id are required")
	}
	if !r.authorize(deviceID, agentID) {
		return "", time.Time{}, errors.New("android agent binding is not configured")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	token, err := randomToken(24)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().Add(ttl)
	r.mu.Lock()
	r.pairCodes[token] = pairCode{DeviceID: deviceID, AgentID: agentID, Expires: expires}
	for code, entry := range r.pairCodes {
		if time.Now().After(entry.Expires) {
			delete(r.pairCodes, code)
		}
	}
	r.mu.Unlock()
	return token, expires, nil
}

func (r *Registry) HandleWebSocket(w http.ResponseWriter, req *http.Request) {
	if r == nil {
		http.Error(w, "android agent registry not available", http.StatusServiceUnavailable)
		return
	}
	deviceID, agentID, paired, ok := r.authenticate(req)
	if !ok {
		http.Error(w, "invalid or expired android agent credential", http.StatusUnauthorized)
		return
	}
	conn, err := r.upgrader.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	session := newSession(r, conn, deviceID, agentID)
	r.addSession(session)
	if paired {
		token, tokenErr := r.issueAgentToken(deviceID, agentID)
		if tokenErr != nil || session.send(Message{Type: MessagePairingComplete, Token: token}) != nil {
			_ = session.Close()
			return
		}
	}
	go session.run()
	go r.notifySession(deviceID)
}

func (r *Registry) authenticate(req *http.Request) (deviceID, agentID string, paired, ok bool) {
	if deviceID, agentID, ok = r.consumePairCode(req); ok {
		return deviceID, agentID, true, true
	}
	token := strings.TrimSpace(req.URL.Query().Get("agent_token"))
	if token == "" {
		token = authorizationCredential(req, "Bearer")
	}
	claims, err := r.verifyAgentToken(token)
	if err != nil || !r.authorize(claims.DeviceID, claims.AgentID) {
		return "", "", false, false
	}
	return claims.DeviceID, claims.AgentID, false, true
}

func (r *Registry) consumePairCode(req *http.Request) (deviceID, agentID string, ok bool) {
	token := strings.TrimSpace(req.URL.Query().Get("pair_token"))
	if token == "" {
		token = authorizationCredential(req, "VoHivePair")
	}
	if token == "" {
		return "", "", false
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, exists := r.pairCodes[token]
	delete(r.pairCodes, token)
	if !exists || now.After(entry.Expires) {
		return "", "", false
	}
	return entry.DeviceID, entry.AgentID, true

}

func authorizationCredential(req *http.Request, scheme string) string {
	if req == nil {
		return ""
	}
	value := strings.TrimSpace(req.Header.Get("Authorization"))
	name, credential, ok := strings.Cut(value, " ")
	if !ok || !strings.EqualFold(strings.TrimSpace(name), scheme) {
		return ""
	}
	return strings.TrimSpace(credential)
}

func (r *Registry) authorize(deviceID, agentID string) bool {
	r.mu.RLock()
	authorizer := r.authorizer
	r.mu.RUnlock()
	return authorizer == nil || authorizer(strings.TrimSpace(deviceID), strings.TrimSpace(agentID))
}

func (r *Registry) issueAgentToken(deviceID, agentID string) (string, error) {
	nonce, err := randomToken(12)
	if err != nil {
		return "", err
	}
	r.mu.RLock()
	secret := append([]byte(nil), r.signingSecret...)
	ttl := r.credentialTTL
	r.mu.RUnlock()
	claims := agentClaims{DeviceID: deviceID, AgentID: agentID, Expires: time.Now().Add(ttl).Unix(), Nonce: nonce}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	sig := signAgentPayload(secret, payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (r *Registry) verifyAgentToken(token string) (agentClaims, error) {
	var claims agentClaims
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return claims, errors.New("invalid agent token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, err
	}
	r.mu.RLock()
	secret := append([]byte(nil), r.signingSecret...)
	r.mu.RUnlock()
	if !hmac.Equal(sig, signAgentPayload(secret, payload)) {
		return claims, errors.New("invalid agent token signature")
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return claims, err
	}
	if claims.DeviceID == "" || claims.AgentID == "" || time.Now().Unix() >= claims.Expires {
		return agentClaims{}, errors.New("expired agent token")
	}
	return claims, nil
}

func signAgentPayload(secret, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func (r *Registry) addSession(session *Session) {
	r.mu.Lock()
	old := r.sessions[session.deviceID]
	r.sessions[session.deviceID] = session
	r.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
}

func (r *Registry) removeSession(session *Session) {
	if r == nil || session == nil {
		return
	}
	r.mu.Lock()
	if r.sessions[session.deviceID] == session {
		delete(r.sessions, session.deviceID)
	}
	r.mu.Unlock()
}

func (r *Registry) Session(deviceID string) (*Session, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	session := r.sessions[strings.TrimSpace(deviceID)]
	r.mu.RUnlock()
	return session, session != nil && !session.closed.Load()
}

func (r *Registry) Disconnect(deviceID string) {
	if r == nil {
		return
	}
	session, ok := r.Session(deviceID)
	if ok {
		_ = session.Close()
		r.removeSession(session)
	}
}

func (r *Registry) Snapshot(deviceID string) (StatusSnapshot, bool) {
	session, ok := r.Session(deviceID)
	if !ok {
		return StatusSnapshot{}, false
	}
	return session.Snapshot(), true
}

func (r *Registry) notifySMS(deviceID string, event SMSReceivedEvent) {
	r.mu.RLock()
	handler := r.onSMS
	r.mu.RUnlock()
	if handler != nil {
		handler(deviceID, event)
	}
}

func (r *Registry) notifySMSStatus(deviceID string, event SMSStatusEvent) {
	r.mu.RLock()
	handler := r.onSMSStatus
	r.mu.RUnlock()
	if handler != nil {
		handler(deviceID, event)
	}
}

func (r *Registry) notifySession(deviceID string) {
	r.mu.RLock()
	handler := r.onSession
	r.mu.RUnlock()
	if handler != nil {
		handler(deviceID)
	}
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
