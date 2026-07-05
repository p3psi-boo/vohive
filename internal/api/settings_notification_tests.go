package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/internal/notify"
)

type testTelegramRequest struct {
	Enabled  bool   `json:"enabled"`
	BotToken string `json:"bot_token"`
	ChatID   int64  `json:"chat_id"`
	BaseURL  string `json:"base_url"`
	Proxy    string `json:"proxy"`
}

type testFeishuRequest struct {
	Enabled   bool     `json:"enabled"`
	AppID     string   `json:"app_id"`
	AppSecret string   `json:"app_secret"`
	ChatIDs   []string `json:"chat_ids"`
}

type testQQRequest struct {
	Enabled   bool   `json:"enabled"`
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
	GroupIDs  string `json:"group_ids"`
	DirectIDs string `json:"direct_ids"`
}

type testPushplusRequest struct {
	Enabled bool   `json:"enabled"`
	Token   string `json:"token"`
	Topic   string `json:"topic"`
	Channel string `json:"channel"`
}

type testNotificationResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func testNotificationText(channel string) string {
	now := time.Now().Format("2006-01-02 15:04:05")
	return "这是一条 Vohive " + channel + " 测试通知，发送时间：" + now
}

func (s *Server) handleTestTelegramNotification(c *gin.Context) {
	var req testTelegramRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	if !req.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先启用 Telegram 后再测试"})
		return
	}
	if strings.TrimSpace(req.BotToken) == "" || req.ChatID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Telegram 测试需要 Bot Token 与 Chat ID"})
		return
	}

	ch, err := notify.NewTelegramChannel(config.TelegramConfig{
		Enabled:  true,
		BotToken: strings.TrimSpace(req.BotToken),
		ChatID:   req.ChatID,
		BaseURL:  strings.TrimSpace(req.BaseURL),
		Proxy:    strings.TrimSpace(req.Proxy),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "初始化 Telegram 测试发送器失败: " + err.Error()})
		return
	}
	if ch == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Telegram 测试发送器未初始化"})
		return
	}
	defer ch.Close()

	if err := ch.Send(testNotificationText("Telegram")); err != nil {
		c.JSON(http.StatusOK, testNotificationResponse{OK: false, Message: "测试通知发送失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, testNotificationResponse{OK: true, Message: "测试通知已发送"})
}

func (s *Server) handleTestFeishuNotification(c *gin.Context) {
	var req testFeishuRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	if !req.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先启用飞书后再测试"})
		return
	}
	if strings.TrimSpace(req.AppID) == "" || strings.TrimSpace(req.AppSecret) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "飞书测试需要 App ID 与 App Secret"})
		return
	}

	chatIDs := make([]string, 0, len(req.ChatIDs))
	for _, id := range req.ChatIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			chatIDs = append(chatIDs, id)
		}
	}
	if len(chatIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"message": "飞书测试至少需要一个 Chat ID"})
		return
	}

	ch, err := notify.NewFeishuChannel(config.FeishuConfig{
		Enabled:   true,
		AppID:     strings.TrimSpace(req.AppID),
		AppSecret: strings.TrimSpace(req.AppSecret),
		ChatIDs:   chatIDs,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "初始化飞书测试发送器失败: " + err.Error()})
		return
	}
	if ch == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "飞书测试发送器未初始化"})
		return
	}
	defer ch.Close()

	if err := ch.Send(testNotificationText("飞书")); err != nil {
		c.JSON(http.StatusOK, testNotificationResponse{OK: false, Message: "测试通知发送失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, testNotificationResponse{OK: true, Message: "测试通知已发送"})
}

func (s *Server) handleTestQQNotification(c *gin.Context) {
	var req testQQRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	if !req.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先启用 QQ 后再测试"})
		return
	}
	if strings.TrimSpace(req.AppID) == "" || strings.TrimSpace(req.AppSecret) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "QQ 测试需要 App ID 与 App Secret"})
		return
	}
	if strings.TrimSpace(req.GroupIDs) == "" && strings.TrimSpace(req.DirectIDs) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "QQ 测试至少需要一个群聊或私聊 OpenID"})
		return
	}

	ch, err := notify.NewQQChannel(config.QQConfig{
		Enabled:   true,
		AppID:     strings.TrimSpace(req.AppID),
		AppSecret: strings.TrimSpace(req.AppSecret),
		GroupIDs:  strings.TrimSpace(req.GroupIDs),
		DirectIDs: strings.TrimSpace(req.DirectIDs),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "初始化 QQ 测试发送器失败: " + err.Error()})
		return
	}
	if ch == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "QQ 测试发送器未初始化"})
		return
	}
	defer ch.Close()

	if err := ch.Send(testNotificationText("QQ")); err != nil {
		c.JSON(http.StatusOK, testNotificationResponse{OK: false, Message: "测试通知发送失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, testNotificationResponse{OK: true, Message: "测试通知已发送"})
}

func (s *Server) handleTestPushplusNotification(c *gin.Context) {
	var req testPushplusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "参数错误"})
		return
	}
	if !req.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"message": "请先启用 Pushplus 后再测试"})
		return
	}
	if strings.TrimSpace(req.Token) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Pushplus 测试需要 Token"})
		return
	}

	ch, err := notify.NewPushplusChannel(config.PushplusConfig{
		Enabled: true,
		Token:   strings.TrimSpace(req.Token),
		Topic:   strings.TrimSpace(req.Topic),
		Channel: strings.TrimSpace(req.Channel),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": "初始化 Pushplus 测试发送器失败: " + err.Error()})
		return
	}
	defer ch.Close()

	if err := ch.Send(testNotificationText("Pushplus")); err != nil {
		c.JSON(http.StatusOK, testNotificationResponse{OK: false, Message: "测试通知发送失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, testNotificationResponse{OK: true, Message: "测试通知已发送"})
}
