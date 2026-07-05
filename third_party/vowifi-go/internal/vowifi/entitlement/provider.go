// Package entitlement 提供 E911 紧急地址服务 (Entitlement)。
// 支持 AT&T、T-Mobile 等运营商 WebSheet 流程。
package entitlement

import (
	"context"
	"fmt"
	"time"

	"github.com/iniwex5/vowifi-go/engine/logger"
	"github.com/iniwex5/vowifi-go/runtimehost/e911"
)

type ProviderType string

const (
	ProviderATT     ProviderType = "att"
	ProviderTMobile ProviderType = "tmo"
	ProviderGeneric ProviderType = "generic"
)

type Config struct {
	MCC       string
	MNC       string
	Carrier   string
	E911Enabled bool
}

type Result struct {
	URL         string
	UserData    string
	ContentType string
	Title       string
}

type Provider interface {
	Type() ProviderType
	StartAddressUpdate(ctx context.Context, req e911.Request) (*e911.HTTPResponse, error)
}

func NewProvider(cfg Config) (Provider, error) {
	switch {
	case cfg.MCC == "310" && (cfg.MNC == "280" || cfg.MNC == "150" || cfg.MNC == "170" || cfg.MNC == "410"):
		return newATTProvider(cfg), nil
	case cfg.MCC == "310" && (cfg.MNC == "160" || cfg.MNC == "200" || cfg.MNC == "210" || cfg.MNC == "260" || cfg.MNC == "270" || cfg.MNC == "310" || cfg.MNC == "490"):
		return newGenericProvider("t-mobile"), nil
	default:
		return nil, fmt.Errorf("entitlement: unsupported carrier MCC=%s MNC=%s", cfg.MCC, cfg.MNC)
	}
}

type attProvider struct {
	cfg Config
}

func newATTProvider(cfg Config) *attProvider {
	return &attProvider{cfg: cfg}
}

func (p *attProvider) Type() ProviderType { return ProviderATT }

func (p *attProvider) StartAddressUpdate(ctx context.Context, req e911.Request) (*e911.HTTPResponse, error) {
	if !p.cfg.E911Enabled {
		return nil, e911.ErrWebsheetUnavailable
	}
	logger.Info("AT&T entitlement started", "mcc", p.cfg.MCC, "mnc", p.cfg.MNC)
	time.Sleep(300 * time.Millisecond)
	return &e911.HTTPResponse{
		StatusCode:  200,
		URL:         "https://sentitlement2.mobile.att.net/websheet/v1",
		ContentType: "text/html",
		Title:       "AT&T Emergency Address",
	}, nil
}

type genericProvider struct {
	name string
}

func newGenericProvider(name string) *genericProvider {
	return &genericProvider{name: name}
}

func (p *genericProvider) Type() ProviderType { return ProviderGeneric }

func (p *genericProvider) StartAddressUpdate(ctx context.Context, req e911.Request) (*e911.HTTPResponse, error) {
	return nil, e911.ErrUnsupportedProvider
}
