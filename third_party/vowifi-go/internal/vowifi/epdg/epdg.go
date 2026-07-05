// Package epdg 提供 ePDG 选择与连接管理 (对照 SimAdmin epdg.rs + transport.rs)。
package epdg

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/iniwex5/vowifi-go/internal/vowifi/dns"
)

const (
	systemDNSTimeout   = 4 * time.Second
	fallbackDNSTimeout = 2 * time.Second
	ikev2Port          = 500
	natTPort           = 4500
)

var publicDNSFallbacks = []string{"1.1.1.1", "8.8.8.8", "223.5.5.5"}

type ProxyKind string

const (
	ProxyDirect           ProxyKind = "direct"
	ProxySocks5UDP        ProxyKind = "socks5_udp_associate"
	ProxyConnectUDPMasque ProxyKind = "connect_udp_masque"
	ProxyUDPRelay         ProxyKind = "udp_relay"
)

type NetworkRoutePolicy struct {
	Kind     ProxyKind
	PolicyID string
	Note     string
}

var defaultRoutePolicy = NetworkRoutePolicy{
	Kind:     ProxyDirect,
	PolicyID: "direct",
	Note:     "direct UDP path",
}

type EpdgConnectionPlan struct {
	ProfileID   string
	PLMN        string
	Host        string
	Port        int
	IPStack     string
	APN         string
	DNSServer   string
	RoutePolicy NetworkRoutePolicy
}

type EpdgResolutionStatus struct {
	Plan           EpdgConnectionPlan
	Addresses      []net.IP
	Ready          bool
	DegradedReason string
}

type Selector struct {
	resolver *dns.Resolver
}

func NewSelector(resolver *dns.Resolver) *Selector {
	return &Selector{resolver: resolver}
}

func (s *Selector) Select(ctx context.Context, mcc, mnc, staticHost string) (*EpdgConnectionPlan, error) {
	plan := &EpdgConnectionPlan{
		PLMN:        mcc + mnc,
		IPStack:     "ipv4",
		Port:        ikev2Port,
		RoutePolicy: defaultRoutePolicy,
	}

	if staticHost != "" {
		plan.Host = staticHost
		plan.ProfileID = "static"
		return plan, nil
	}

	if s.resolver != nil {
		fqdn := dns.BuildEPDGFQDN(mcc, mnc)
		plan.Host = fqdn
		plan.ProfileID = "dns"
		return plan, nil
	}

	return nil, fmt.Errorf("epdg: no ePDG found for MCC=%s MNC=%s", mcc, mnc)
}

func (s *Selector) Resolve(ctx context.Context, plan *EpdgConnectionPlan) (*EpdgResolutionStatus, error) {
	status := &EpdgResolutionStatus{Plan: *plan}

	if s.resolver == nil {
		return nil, fmt.Errorf("epdg: resolver required")
	}

	ips, err := s.resolveWithFallback(ctx, plan.Host, plan.Port)
	if err != nil {
		status.DegradedReason = err.Error()
		return status, fmt.Errorf("epdg resolve: %w", err)
	}

	status.Addresses = ips
	status.Ready = true
	return status, nil
}

func (s *Selector) resolveWithFallback(ctx context.Context, host string, port int) ([]net.IP, error) {
	if s.resolver == nil {
		return nil, fmt.Errorf("no resolver")
	}

	mcc := extractMCC(host)
	mnc := extractMNC(host)
	if mcc != "" && mnc != "" {
		epdgAddrs, err := s.resolver.ResolveEPDG(mcc, mnc)
		if err == nil {
			var ips []net.IP
			for _, addrStr := range epdgAddrs {
				hostPort := strings.Split(addrStr, ":")
				ip := net.ParseIP(hostPort[0])
				if ip != nil {
					ips = append(ips, ip)
				}
			}
			if len(ips) > 0 {
				return ips, nil
			}
		}
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		return s.fallbackDNSLookup(ctx, host)
	}
	if len(ips) == 0 {
		return s.fallbackDNSLookup(ctx, host)
	}
	return ips, nil
}

func (s *Selector) fallbackDNSLookup(ctx context.Context, host string) ([]net.IP, error) {
	for _, server := range publicDNSFallbacks {
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(dialCtx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: fallbackDNSTimeout}
				return d.DialContext(dialCtx, network, server+":53")
			},
		}
		ips, err := r.LookupIP(ctx, "ip4", host)
		if err == nil && len(ips) > 0 {
			return ips, nil
		}
	}
	return nil, fmt.Errorf("dns: all fallbacks exhausted for %s", host)
}

func extractMCC(fqdn string) string {
	i := strings.Index(fqdn, "mcc")
	if i < 0 {
		return ""
	}
	rest := fqdn[i+3:]
	j := 0
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	if j >= 3 {
		return rest[:3]
	}
	return ""
}

func extractMNC(fqdn string) string {
	i := strings.Index(fqdn, "mnc")
	if i < 0 {
		return ""
	}
	rest := fqdn[i+3:]
	j := 0
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	if j >= 2 {
		return fmt.Sprintf("%03s", rest[:j])
	}
	return ""
}
