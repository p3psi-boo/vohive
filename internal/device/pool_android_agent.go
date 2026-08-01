package device

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iniwex5/vohive/internal/androidagent"
	"github.com/iniwex5/vohive/internal/backend"
	"github.com/iniwex5/vohive/internal/config"
	"github.com/iniwex5/vohive/pkg/logger"
	"github.com/iniwex5/vohive/pkg/smscodec"
)

type androidNetworkController struct {
	deviceID string
	registry *androidagent.Registry
}

func (c *androidNetworkController) snapshot() (androidagent.StatusSnapshot, bool) {
	if c == nil || c.registry == nil {
		return androidagent.StatusSnapshot{}, false
	}
	return c.registry.Snapshot(c.deviceID)
}

func (c *androidNetworkController) Connect() error {
	s, ok := c.registry.Session(c.deviceID)
	if !ok {
		return androidagent.ErrSessionNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.Call(ctx, "network.connect", nil)
	return err
}

func (c *androidNetworkController) Disconnect() error {
	s, ok := c.registry.Session(c.deviceID)
	if !ok {
		return androidagent.ErrSessionNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := s.Call(ctx, "network.disconnect", nil)
	return err
}

func (c *androidNetworkController) IsConnected() bool {
	s, ok := c.snapshot()
	return ok && s.DataConnected
}

func (c *androidNetworkController) RotateIP() error {
	s, ok := c.registry.Session(c.deviceID)
	if !ok {
		return androidagent.ErrSessionNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	_, err := s.Call(ctx, "network.rotate", nil)
	return err
}

func (c *androidNetworkController) GetPrivateIP() string {
	s, _ := c.snapshot()
	return s.PrivateIP
}

func (c *androidNetworkController) GetPrivateIPv6() string {
	s, _ := c.snapshot()
	return s.PrivateIPv6
}

func (c *androidNetworkController) GetPublicIPv4AndV6NoCache() (string, string) {
	s, _ := c.snapshot()
	return s.PublicIP, s.PublicIPv6
}

func (p *Pool) addAndroidWorkerFromConfig(devCfg config.DeviceConfig) (*Worker, error) {
	if p == nil || p.androidAgents == nil {
		return nil, fmt.Errorf("Android Agent 服务未初始化")
	}
	devCfg.ID = strings.TrimSpace(devCfg.ID)
	if devCfg.ID == "" {
		return nil, fmt.Errorf("设备 ID 不能为空")
	}
	if _, ok := p.androidAgents.Session(devCfg.ID); !ok {
		return nil, fmt.Errorf("Android Agent 尚未在线: %s", devCfg.ID)
	}

	p.mu.Lock()
	if _, exists := p.workers[devCfg.ID]; exists {
		p.mu.Unlock()
		return nil, fmt.Errorf("设备已存在")
	}
	if p.rebuilding[devCfg.ID] {
		p.mu.Unlock()
		return nil, fmt.Errorf("设备 %s 正在初始化中，请勿重复触发", devCfg.ID)
	}
	if FreeDeviceLimitReached(len(p.workers)) {
		p.mu.Unlock()
		return nil, fmt.Errorf("%s", FreeDeviceWorkerLimitMessage())
	}
	p.rebuilding[devCfg.ID] = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.rebuilding, devCfg.ID)
		p.mu.Unlock()
	}()

	devCfg.DeviceKind = config.DeviceKindAndroid
	devCfg.DeviceBackend = backend.BackendAndroid
	devCfg.Interface = "android:" + devCfg.ID
	devCfg.SMSEnabled = true
	w := &Worker{
		ID:          devCfg.ID,
		Config:      devCfg,
		Backend:     backend.NewAndroidBackend(devCfg.ID, p.androidAgents),
		netOverride: &androidNetworkController{deviceID: devCfg.ID, registry: p.androidAgents},
		Pool:        p,
		stop:        make(chan struct{}),
		reassembler: smscodec.NewReassembler(),
	}
	p.assignWorkerGeneration(w)
	if err := p.registerWorkerStarting(w); err != nil {
		return nil, err
	}
	w.setCachedHealthy(true)
	go w.PreWarmCache()
	logger.Info("Android Agent Worker 已启动", "device", devCfg.ID, "agent_id", devCfg.Android.AgentID)
	return w, nil
}

var _ NetworkController = (*androidNetworkController)(nil)
