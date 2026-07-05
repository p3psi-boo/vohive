//go:build linux

// Package startup 提供 VoWiFi 启动序列编排。
package startup

import (
	"context"
	"fmt"
	"time"

	"github.com/iniwex5/vowifi-go/engine/driver"
	"github.com/iniwex5/vowifi-go/engine/eap"
	"github.com/iniwex5/vowifi-go/engine/logger"
	"github.com/iniwex5/vowifi-go/internal/vowifi/dns"
	"github.com/iniwex5/vowifi-go/internal/vowifi/epdg"
	"github.com/iniwex5/vowifi-go/internal/vowifi/imscore"
	"github.com/iniwex5/vowifi-go/internal/vowifi/runtimecore"
	"github.com/iniwex5/vowifi-go/runtimehost/eventhost"
	"github.com/iniwex5/vowifi-go/runtimehost/messaging"
)

type Result struct {
	Core *runtimecore.Core
	IMS  *imscore.Core
	TUN  *driver.TUN
}

type Config struct {
	DeviceID     string
	TraceID      string
	MCC          string
	MNC          string
	IMSI         string
	IMPI         string
	IMPU         string
	Domain       string
	EPDGOverride string
	AKA          eap.AKAProvider
	Store        messaging.DeliveryStore
	Dispatch     eventhost.Dispatcher
}

func Start(ctx context.Context, cfg *Config) (*Result, error) {
	logger.Info("VoWiFi startup starting", "device", cfg.DeviceID, "mcc", cfg.MCC, "mnc", cfg.MNC)

	resolver := dns.NewResolver()
	sel := epdg.NewSelector(resolver)
	ep, err := sel.Select(ctx, cfg.MCC, cfg.MNC, cfg.EPDGOverride)
	if err != nil {
		return nil, fmt.Errorf("startup: ePDG select: %w", err)
	}
	logger.Info("ePDG selected", "epdg", ep.Host)

	runtimeCfg := &runtimecore.Config{
		DeviceID:     cfg.DeviceID,
		TraceID:      cfg.TraceID,
		MCC:          cfg.MCC,
		MNC:          cfg.MNC,
		IMSI:         cfg.IMSI,
		EPDGOverride: ep.Host,
		AKA:          cfg.AKA,
		Store:        cfg.Store,
		Dispatch:     cfg.Dispatch,
	}
	core := runtimecore.New(ctx, runtimeCfg)
	if err := core.Start(); err != nil {
		return nil, fmt.Errorf("startup: runtime core: %w", err)
	}

	ims := imscore.New(ctx, &imscore.Config{
		IMPI:   cfg.IMPI,
		IMPU:   cfg.IMPU,
		Domain: cfg.Domain,
		IMSI:   cfg.IMSI,
		MCC:    cfg.MCC,
		MNC:    cfg.MNC,
	})
	if err := ims.Register(ctx); err != nil {
		logger.Warn("IMS registration failed, continuing", "err", err)
	} else {
		logger.Info("IMS registered", "impi", cfg.IMPI)
	}

	tun, _ := driver.New(fmt.Sprintf("vowifi-%s", cfg.DeviceID), 1500)
	logger.Info("VoWiFi startup complete", "device", cfg.DeviceID, "cost_ms", time.Since(time.Now()).Milliseconds())

	return &Result{Core: core, IMS: ims, TUN: tun}, nil
}
