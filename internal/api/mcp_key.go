package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/pkg/logger"

	"github.com/gin-gonic/gin"
)

const mcpKeyPrefix = "vh_mcp_"

type mcpKeyStatusResponse struct {
	Enabled   bool   `json:"enabled"`
	KeySuffix string `json:"key_suffix,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type mcpKeyGenerateResponse struct {
	Status    string `json:"status"`
	Key       string `json:"key"`
	KeySuffix string `json:"key_suffix"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) handleGetMCPKey(c *gin.Context) {
	c.JSON(http.StatusOK, s.mcpKeyStatus())
}

func (s *Server) handleGenerateMCPKey(c *gin.Context) {
	key, err := generateMCPKey()
	if err != nil {
		logger.Error("生成 MCP key 失败", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "生成 MCP Key 失败"})
		return
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	cfg := config.MCPConfig{
		KeyHash:   hashMCPKey(key),
		KeySuffix: keySuffix(key),
		CreatedAt: createdAt,
	}
	if err := config.UpdateMCPKeyInFile(s.configPath, cfg); err != nil {
		logger.Error("写入 MCP key 配置失败", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "写入配置文件失败: " + err.Error()})
		return
	}

	s.mcpKeyMu.Lock()
	s.fullCfg.MCP = cfg
	s.mcpKeyMu.Unlock()

	logger.Info("MCP key 已生成", "suffix", cfg.KeySuffix, "ip", c.ClientIP())
	c.JSON(http.StatusOK, mcpKeyGenerateResponse{
		Status:    "ok",
		Key:       key,
		KeySuffix: cfg.KeySuffix,
		CreatedAt: cfg.CreatedAt,
	})
}

func (s *Server) handleRevokeMCPKey(c *gin.Context) {
	if err := config.UpdateMCPKeyInFile(s.configPath, config.MCPConfig{}); err != nil {
		logger.Error("撤销 MCP key 配置失败", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "写入配置文件失败: " + err.Error()})
		return
	}

	s.mcpKeyMu.Lock()
	s.fullCfg.MCP = config.MCPConfig{}
	s.mcpKeyMu.Unlock()

	logger.Info("MCP key 已撤销", "ip", c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) mcpKeyStatus() mcpKeyStatusResponse {
	s.mcpKeyMu.RLock()
	defer s.mcpKeyMu.RUnlock()

	cfg := s.fullCfg.MCP
	return mcpKeyStatusResponse{
		Enabled:   strings.TrimSpace(cfg.KeyHash) != "",
		KeySuffix: strings.TrimSpace(cfg.KeySuffix),
		CreatedAt: strings.TrimSpace(cfg.CreatedAt),
	}
}

func (s *Server) mcpAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := s.requestSessionToken(c)
		now := time.Now()
		if token != "" && (s.isSessionTokenValid(token, now) || s.isMCPKeyValid(token)) {
			c.Next()
			return
		}

		c.JSON(http.StatusUnauthorized, gin.H{
			"status":     "error",
			"code":       "unauthorized",
			"message":    "未授权",
			"request_id": requestID(c),
		})
		c.Abort()
	}
}

func (s *Server) isMCPKeyValid(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}

	s.mcpKeyMu.RLock()
	stored := strings.TrimSpace(s.fullCfg.MCP.KeyHash)
	s.mcpKeyMu.RUnlock()
	if stored == "" {
		return false
	}

	got := hashMCPKey(key)
	return subtle.ConstantTimeCompare([]byte(got), []byte(stored)) == 1
}

func generateMCPKey() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return mcpKeyPrefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func hashMCPKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func keySuffix(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return key
	}
	return key[len(key)-8:]
}
