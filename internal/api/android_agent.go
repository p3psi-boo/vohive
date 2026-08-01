package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"

	"github.com/gin-gonic/gin"
)

type androidPairingTokenResponse struct {
	Status    string `json:"status"`
	DeviceID  string `json:"device_id"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type androidSelectSubscriptionRequest struct {
	SubscriptionID int `json:"subscription_id"`
}

type androidSwitchESIMRequest struct {
	SubscriptionID int `json:"subscription_id"`
	PortIndex      int `json:"port_index"`
}

func (s *Server) handleAndroidAgentConnect(c *gin.Context) {
	if s == nil || s.androidAgents == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Android Agent 服务未启用"})
		return
	}
	s.androidAgents.HandleWebSocket(c.Writer, c.Request)
}

func (s *Server) configuredAndroidDevice(c *gin.Context) (*config.DeviceConfig, bool) {
	id := deviceIDParam(c)
	cfg, err := config.GetDeviceByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": err.Error()})
		return nil, false
	}
	if cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "message": "设备未找到"})
		return nil, false
	}
	if !config.IsAndroidDevice(*cfg) {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "该设备不是 Android Agent 设备"})
		return nil, false
	}
	return cfg, true
}

func (s *Server) androidBackend(c *gin.Context) (*backend.AndroidBackend, bool) {
	cfg, ok := s.configuredAndroidDevice(c)
	if !ok {
		return nil, false
	}
	if s.androidAgents == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Android Agent 服务未启用"})
		return nil, false
	}
	if _, online := s.androidAgents.Session(cfg.ID); !online {
		c.JSON(http.StatusConflict, gin.H{"status": "error", "message": "Android Agent 当前离线"})
		return nil, false
	}
	return backend.NewAndroidBackend(cfg.ID, s.androidAgents), true
}

func (s *Server) handleAndroidAgentPairingToken(c *gin.Context) {
	cfg, ok := s.configuredAndroidDevice(c)
	if !ok {
		return
	}
	if s.androidAgents == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Android Agent 服务未启用"})
		return
	}
	token, expires, err := s.androidAgents.CreatePairCode(cfg.ID, cfg.Android.AgentID, 5*time.Minute)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, androidPairingTokenResponse{
		Status: "ok", DeviceID: cfg.ID, Token: token, ExpiresAt: expires.Format(time.RFC3339),
	})
}

func (s *Server) handleAndroidAgentStatus(c *gin.Context) {
	cfg, ok := s.configuredAndroidDevice(c)
	if !ok {
		return
	}
	if s.androidAgents == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "Android Agent 服务未启用"})
		return
	}
	snapshot, online := s.androidAgents.Snapshot(cfg.ID)
	c.JSON(http.StatusOK, gin.H{
		"status": "ok", "online": online, "device_id": cfg.ID,
		"agent_id": cfg.Android.AgentID, "snapshot": snapshot,
	})
}

func (s *Server) handleAndroidAgentSubscriptions(c *gin.Context) {
	b, ok := s.androidBackend(c)
	if !ok {
		return
	}
	ctx, cancel := contextWithTimeout(c, 12*time.Second)
	defer cancel()
	subscriptions, err := b.ListSubscriptions(ctx)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "subscriptions": subscriptions})
}

func (s *Server) handleAndroidAgentSelectSubscription(c *gin.Context) {
	b, ok := s.androidBackend(c)
	if !ok {
		return
	}
	var req androidSelectSubscriptionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.SubscriptionID < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "subscription_id 无效"})
		return
	}
	ctx, cancel := contextWithTimeout(c, 12*time.Second)
	defer cancel()
	if err := b.SelectSubscription(ctx, req.SubscriptionID); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "subscription_id": req.SubscriptionID})
}

func (s *Server) handleAndroidAgentSwitchESIM(c *gin.Context) {
	b, ok := s.androidBackend(c)
	if !ok {
		return
	}
	var req androidSwitchESIMRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.SubscriptionID < 0 || req.PortIndex < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "subscription_id 或 port_index 无效"})
		return
	}
	ctx, cancel := contextWithTimeout(c, 15*time.Second)
	defer cancel()
	result, err := b.SwitchESIM(ctx, req.SubscriptionID, req.PortIndex)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "result": result})
}

func (s *Server) handleAndroidAgentOpenESIMSettings(c *gin.Context) {
	b, ok := s.androidBackend(c)
	if !ok {
		return
	}
	ctx, cancel := contextWithTimeout(c, 8*time.Second)
	defer cancel()
	if err := b.OpenESIMSettings(ctx); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) handleAndroidAgentListSMS(c *gin.Context) {
	b, ok := s.androidBackend(c)
	if !ok {
		return
	}
	limit := 500
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 2000 {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "limit 必须在 1 到 2000 之间"})
			return
		}
		limit = parsed
	}
	ctx, cancel := contextWithTimeout(c, 20*time.Second)
	defer cancel()
	messages, err := b.ListAndroidSMS(ctx, limit)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "messages": messages})
}

func (s *Server) handleAndroidAgentDeleteSMS(c *gin.Context) {
	b, ok := s.androidBackend(c)
	if !ok {
		return
	}
	index64, err := strconv.ParseInt(c.Param("index"), 10, 64)
	if err != nil || index64 < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "短信 index 无效"})
		return
	}
	ctx, cancel := contextWithTimeout(c, 12*time.Second)
	defer cancel()
	if err := b.DeleteAndroidSMS(ctx, index64); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "index": index64})
}

func (s *Server) handleAndroidAgentReadSMS(c *gin.Context) {
	b, ok := s.androidBackend(c)
	if !ok {
		return
	}
	index64, err := strconv.ParseInt(c.Param("index"), 10, 64)
	if err != nil || index64 < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "短信 index 无效"})
		return
	}
	ctx, cancel := contextWithTimeout(c, 12*time.Second)
	defer cancel()
	message, err := b.ReadAndroidSMS(ctx, index64)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": message})
}

func (s *Server) handleAndroidAgentDeleteAllSMS(c *gin.Context) {
	b, ok := s.androidBackend(c)
	if !ok {
		return
	}
	ctx, cancel := contextWithTimeout(c, 20*time.Second)
	defer cancel()
	if err := b.DeleteAllSMS(ctx); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func contextWithTimeout(c *gin.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), timeout)
}
