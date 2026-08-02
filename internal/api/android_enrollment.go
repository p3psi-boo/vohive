package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/androidagent"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	proxyserver "github.com/iniwex5/vohive/internal/proxy/server"
)

type androidEnrollmentRequest struct {
	Name string `json:"name"`
}

type androidEnrollmentResponse struct {
	Status    string `json:"status"`
	DeviceID  string `json:"device_id"`
	Code      string `json:"code"`
	ServerURL string `json:"server_url"`
	ExpiresAt string `json:"expires_at"`
}

type pendingAndroidEnrollment struct {
	Name    string
	Expires time.Time
}

func (s *Server) handleDiscoveredAndroidAgents(c *gin.Context) {
	if s.androidDiscovery == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "agents": []androidagent.DiscoveredAgent{}})
		return
	}
	agents := s.androidDiscovery.List()
	sort.Slice(agents, func(i, j int) bool { return agents[i].LastSeen.After(agents[j].LastSeen) })
	c.JSON(http.StatusOK, gin.H{"status": "ok", "agents": agents})
}

func (s *Server) handleApproveDiscoveredAndroidAgent(c *gin.Context) {
	if s.androidDiscovery == nil || s.androidAgents == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Android Agent 发现服务未启用"})
		return
	}
	agentID := strings.TrimSpace(c.Param("agent_id"))
	var request androidEnrollmentRequest
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "参数错误"})
		return
	}
	candidate, ok := discoveredAgentByID(s.androidDiscovery.List(), agentID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "设备已离开发现窗口，请确认 Agent 正在运行"})
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		name = strings.TrimSpace(candidate.Model)
	}
	cfg, created, err := s.reserveAndroidDevice(agentID, name)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"status": "error", "message": err.Error()})
		return
	}
	code, expires, err := s.androidAgents.CreatePairCode(cfg.ID, agentID, 5*time.Minute)
	if err != nil {
		if created {
			_ = config.DeleteDeviceInFile(s.configPath, cfg.ID)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	if err := s.androidDiscovery.Approve(agentID, cfg.ID, code); err != nil {
		c.JSON(http.StatusConflict, gin.H{"status": "error", "message": "设备已保存，但自动下发失败；请重试发现或使用配对码"})
		return
	}
	proxyWarning := s.ensurePersonalProxyDefaults(c.Request.Context(), *cfg)
	c.JSON(http.StatusOK, gin.H{
		"status": "ok", "device_id": cfg.ID, "name": cfg.Name,
		"expires_at": expires.UTC().Format(time.RFC3339), "proxy_warning": proxyWarning,
	})
}

func (s *Server) handleCreateAndroidEnrollmentCode(c *gin.Context) {
	if s.androidAgents == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Android Agent 服务未启用"})
		return
	}
	var request androidEnrollmentRequest
	if err := c.ShouldBindJSON(&request); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "参数错误"})
		return
	}
	deviceID, err := s.reservePendingAndroidEnrollment(strings.TrimSpace(request.Name), 5*time.Minute)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"status": "error", "message": err.Error()})
		return
	}
	code, expires, err := s.androidAgents.CreateEnrollmentCode(deviceID, 5*time.Minute)
	if err != nil {
		s.deletePendingAndroidEnrollment(deviceID)
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return
	}
	s.scheduleUnusedEnrollmentCleanup(deviceID, expires)
	c.JSON(http.StatusOK, androidEnrollmentResponse{
		Status: "ok", DeviceID: deviceID, Code: code,
		ServerURL: requestServerURL(c), ExpiresAt: expires.UTC().Format(time.RFC3339),
	})
}

func (s *Server) scheduleUnusedEnrollmentCleanup(deviceID string, expires time.Time) {
	go func() {
		timer := time.NewTimer(time.Until(expires) + time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-s.shutdownCh:
			return
		}
		s.deletePendingAndroidEnrollment(deviceID)
	}()
}

func (s *Server) bindAndroidEnrollment(deviceID, agentID string) bool {
	deviceID, agentID = strings.TrimSpace(deviceID), strings.TrimSpace(agentID)
	if deviceID == "" || agentID == "" {
		return false
	}
	cfg, err := config.GetDeviceByID(deviceID)
	if err != nil {
		return false
	}
	if cfg == nil {
		pending, ok := s.takePendingAndroidEnrollment(deviceID)
		if !ok || time.Now().After(pending.Expires) {
			return false
		}
		if err := validateFreeDeviceConfigLimit(config.ListDevices()); err != nil {
			return false
		}
		name := strings.TrimSpace(pending.Name)
		if name == "" {
			name = "Android 手机"
		}
		created := config.DeviceConfig{
			ID: deviceID, Name: name, DeviceKind: config.DeviceKindAndroid,
			DeviceBackend: backend.BackendAndroid, Android: config.AndroidDeviceConfig{AgentID: agentID},
			SMSEnabled: true,
		}
		if conflict := detectDeviceBindingConflict(created, deviceID); conflict != nil {
			return false
		}
		if err := config.AddDeviceInFile(s.configPath, created); err != nil {
			return false
		}
		_ = s.ensurePersonalProxyDefaults(context.Background(), created)
		return true
	}
	if !config.IsAndroidDevice(*cfg) {
		return false
	}
	current := strings.TrimSpace(cfg.Android.AgentID)
	if current != "" && current != agentID {
		return false
	}
	if conflict := detectDeviceBindingConflict(config.DeviceConfig{Android: config.AndroidDeviceConfig{AgentID: agentID}}, deviceID); conflict != nil {
		return false
	}
	cfg.Android.AgentID = agentID
	if err := config.UpdateDeviceInFile(s.configPath, deviceID, *cfg); err != nil {
		return false
	}
	if s.pool != nil {
		s.pool.UpdateWorkerConfig(deviceID, *cfg, true)
	}
	return true
}

func (s *Server) reservePendingAndroidEnrollment(name string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	s.androidEnrollmentMu.Lock()
	defer s.androidEnrollmentMu.Unlock()
	now := time.Now()
	for id, pending := range s.pendingAndroidEnrollments {
		if now.After(pending.Expires) {
			delete(s.pendingAndroidEnrollments, id)
		}
	}
	devices := config.ListDevices()
	for range s.pendingAndroidEnrollments {
		devices = append(devices, config.DeviceConfig{})
	}
	if err := validateFreeDeviceConfigLimit(devices); err != nil {
		return "", err
	}
	id := nextAndroidDeviceID("")
	base := id
	for index := 2; ; index++ {
		if _, exists := s.pendingAndroidEnrollments[id]; !exists {
			break
		}
		id = fmt.Sprintf("%s-%d", base, index)
	}
	s.pendingAndroidEnrollments[id] = pendingAndroidEnrollment{Name: name, Expires: now.Add(ttl)}
	return id, nil
}

func (s *Server) takePendingAndroidEnrollment(deviceID string) (pendingAndroidEnrollment, bool) {
	s.androidEnrollmentMu.Lock()
	defer s.androidEnrollmentMu.Unlock()
	pending, ok := s.pendingAndroidEnrollments[deviceID]
	if ok {
		delete(s.pendingAndroidEnrollments, deviceID)
	}
	return pending, ok
}

func (s *Server) deletePendingAndroidEnrollment(deviceID string) {
	s.androidEnrollmentMu.Lock()
	delete(s.pendingAndroidEnrollments, deviceID)
	s.androidEnrollmentMu.Unlock()
}

func (s *Server) reserveAndroidDevice(agentID, name string) (*config.DeviceConfig, bool, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID != "" {
		for _, existing := range config.ListDevices() {
			if config.IsAndroidDevice(existing) && strings.TrimSpace(existing.Android.AgentID) == agentID {
				copy := existing
				return &copy, false, nil
			}
		}
	}
	if err := validateFreeDeviceConfigLimit(config.ListDevices()); err != nil {
		return nil, false, err
	}
	id := nextAndroidDeviceID(agentID)
	if strings.TrimSpace(name) == "" {
		name = "Android 手机"
	}
	cfg := config.DeviceConfig{
		ID: id, Name: name, DeviceKind: config.DeviceKindAndroid,
		DeviceBackend: backend.BackendAndroid, Android: config.AndroidDeviceConfig{AgentID: agentID},
		SMSEnabled: true,
	}
	if err := config.AddDeviceInFile(s.configPath, cfg); err != nil {
		return nil, false, err
	}
	return &cfg, true, nil
}

func nextAndroidDeviceID(agentID string) string {
	suffix := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, agentID)
	if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	}
	if suffix == "" {
		suffix = fmt.Sprintf("%d", time.Now().UnixNano()%1_000_000)
	}
	base := "android-" + suffix
	id := base
	for index := 2; ; index++ {
		if existing, _ := config.GetDeviceByID(id); existing == nil {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, index)
	}
}

func discoveredAgentByID(agents []androidagent.DiscoveredAgent, agentID string) (androidagent.DiscoveredAgent, bool) {
	for _, agent := range agents {
		if strings.TrimSpace(agent.AgentID) == strings.TrimSpace(agentID) {
			return agent, true
		}
	}
	return androidagent.DiscoveredAgent{}, false
}

func requestServerURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

func (s *Server) ensurePersonalProxyDefaults(ctx context.Context, cfg config.DeviceConfig) string {
	if s == nil || s.proxyRepo == nil {
		return ""
	}
	s.proxyRepoMu.Lock()
	defer s.proxyRepoMu.Unlock()
	instances, err := s.proxyRepo.List(ctx)
	if err != nil {
		return err.Error()
	}
	hasMode := map[string]bool{}
	usedPorts := map[int]bool{}
	for _, instance := range instances {
		usedPorts[instance.ListenPort] = true
		if instance.DeviceID == cfg.ID {
			hasMode[instance.Mode] = true
		}
	}
	changed := false
	for _, item := range []struct {
		mode string
		port int
	}{{proxyserver.ModeSocks5, 1080}, {proxyserver.ModeHTTP, 8080}} {
		if hasMode[item.mode] {
			continue
		}
		port := nextFreeProxyPort(item.port, usedPorts)
		usedPorts[port] = true
		label := cfg.Name
		if strings.TrimSpace(label) == "" {
			label = cfg.ID
		}
		instances = append(instances, config.ProxyInstance{
			ID: "auto-" + cfg.ID + "-" + item.mode, Name: label + " " + strings.ToUpper(item.mode),
			DeviceID: cfg.ID, Enabled: true, Mode: item.mode,
			ListenAddr: "0.0.0.0", ListenPort: port,
		})
		changed = true
	}
	if !changed {
		return ""
	}
	if err := s.proxyRepo.ReplaceAll(ctx, instances); err != nil {
		return err.Error()
	}
	if err := s.SyncProxyConfigs(); err != nil {
		return err.Error()
	}
	return ""
}

func nextFreeProxyPort(start int, used map[int]bool) int {
	for port := start; port <= 65535; port++ {
		if !used[port] {
			return port
		}
	}
	return start
}

func (s *Server) removeDeviceProxyInstances(ctx context.Context, deviceID string) error {
	if s == nil || s.proxyRepo == nil {
		return nil
	}
	s.proxyRepoMu.Lock()
	defer s.proxyRepoMu.Unlock()
	instances, err := s.proxyRepo.List(ctx)
	if err != nil {
		return err
	}
	filtered := instances[:0]
	for _, instance := range instances {
		if instance.DeviceID != deviceID {
			filtered = append(filtered, instance)
		}
	}
	if len(filtered) == len(instances) {
		return nil
	}
	if err := s.proxyRepo.ReplaceAll(ctx, filtered); err != nil {
		return err
	}
	return s.SyncProxyConfigs()
}
