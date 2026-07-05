// Package runtime 提供 VoWiFi 运行时公共桥接入口。
// 连接 runtimehost 接口层与 internal 引擎实现。
package runtime

import (
	"context"
	"fmt"

	"github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/engine/sim"
	"github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"
	"github.com/iniwex5/vowifi-go/runtimehost"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

// Start 启动真实 VoWiFi 运行时,建立 IKEv2/IPsec 隧道。
func Start(ctx context.Context, req runtimehost.StartRequest) (*runtimehost.Instance, error) {
	prep := req.Prepared

	var aka eap.AKAProvider
	if req.SIM != nil {
		aka = &simAKAAdapter{sim: req.SIM}
	}

	var store messaging.DeliveryStore
	if ds, ok := req.DeliveryStore.(messaging.DeliveryStore); ok {
		store = ds
	}

	var disp eventhost.Dispatcher
	if d, ok := req.Dispatch.(eventhost.Dispatcher); ok {
		disp = d
	}

	core := runtimecore.New(ctx, &runtimecore.Config{
		DeviceID:     req.DeviceID,
		TraceID:      req.TraceID,
		MCC:          prep.Profile.MCC,
		MNC:          prep.Profile.MNC,
		IMSI:         prep.Profile.IMSI,
		EPDGOverride: prep.EPDGAddr,
		AKA:          aka,
		Store:        store,
		Dispatch:     disp,
		Proxy:        proxyAddr(req.Proxy),
	})

	if err := core.Start(); err != nil {
		return nil, fmt.Errorf("vowifi runtime: %w", err)
	}

	return &runtimehost.Instance{}, nil
}

type simAKAAdapter struct {
	sim runtimehost.SIMAdapter
}

type akaCalcProvider interface {
	CalculateAKA(rand, autn []byte) (sim.AKAResult, error)
}

func (a *simAKAAdapter) GetAKA(rand, autn []byte) (*eap.AKAResult, error) {
	if a.sim == nil {
		return nil, fmt.Errorf("no SIM adapter")
	}
	provider, ok := a.sim.(akaCalcProvider)
	if !ok {
		return nil, fmt.Errorf("SIM adapter does not support CalculateAKA (%T)", a.sim)
	}
	result, err := provider.CalculateAKA(rand, autn)
	if err != nil {
		return nil, err
	}
	return &eap.AKAResult{
		RES:  result.RES,
		CK:   result.CK,
		IK:   result.IK,
		AUTS: result.AUTS,
	}, nil
}

func (a *simAKAAdapter) GetIMSI() string {
	if a.sim == nil {
		return ""
	}
	imsi, _ := a.sim.GetIMSI()
	return imsi
}

func proxyAddr(p *runtimehost.ProxyConfig) string {
	if p == nil || !p.Enabled || p.Addr == "" {
		return ""
	}
	return p.Addr
}
