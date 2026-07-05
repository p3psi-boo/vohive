// Package dns 为 VoWiFi 提供 DNS 解析辅助函数。
// 包括 ePDG FQDN 构造、SRV 解析、NAPTR 记录查询。
package dns

import (
	"fmt"
	"net"
	"strings"
)

type Resolver struct {
	servers []string
}

func NewResolver(servers ...string) *Resolver {
	return &Resolver{servers: servers}
}

// ResolveSRV 解析 SRV 记录，返回主机名和端口。
func (r *Resolver) ResolveSRV(service, proto, name string) (string, int, error) {
	_, addrs, err := net.LookupSRV(service, proto, name)
	if err != nil {
		return "", 0, fmt.Errorf("srv lookup %s: %w", name, err)
	}
	if len(addrs) == 0 {
		return "", 0, fmt.Errorf("no SRV records for %s", name)
	}
	return strings.TrimSuffix(addrs[0].Target, "."), int(addrs[0].Port), nil
}

// ResolveA 解析 A 记录。
func (r *Resolver) ResolveA(host string) ([]string, error) {
	ips, err := net.LookupHost(host)
	if err != nil {
		return nil, fmt.Errorf("a lookup %s: %w", host, err)
	}
	return ips, nil
}

// BuildEPDGFQDN 构造 ePDG FQDN (3GPP TS 23.003, §12.5.5)。
// 格式: epdg.epc.mnc<MNC>.mcc<MCC>.pub.3gppnetwork.org
func BuildEPDGFQDN(mcc, mnc string) string {
	return fmt.Sprintf("epdg.epc.mnc%s.mcc%s.pub.3gppnetwork.org", fmt.Sprintf("%03s", mnc), fmt.Sprintf("%03s", mcc))
}

// BuildEmergencyEPDGFQDN 构造紧急 ePDG FQDN。
// 格式: sos.epdg.epc.mnc<MNC>.mcc<MCC>.pub.3gppnetwork.org
func BuildEmergencyEPDGFQDN(mcc, mnc string) string {
	return fmt.Sprintf("sos.epdg.epc.mnc%s.mcc%s.pub.3gppnetwork.org", fmt.Sprintf("%03s", mnc), fmt.Sprintf("%03s", mcc))
}

// ResolveEPDG 解析 ePDG 地址（先 SRV 后 A 回退）。
func (r *Resolver) ResolveEPDG(mcc, mnc string) ([]string, error) {
	fqdn := BuildEPDGFQDN(mcc, mnc)
	host, port, err := r.ResolveSRV("ipsec", "ikev2", fqdn)
	if err == nil {
		ips, err := r.ResolveA(host)
		if err == nil && len(ips) > 0 {
			result := make([]string, len(ips))
			for i, ip := range ips {
				result[i] = fmt.Sprintf("%s:%d", ip, port)
			}
			return result, nil
		}
	}
	ips, err := r.ResolveA(fqdn)
	if err != nil {
		return nil, fmt.Errorf("epdg resolve %s: %w", fqdn, err)
	}
	result := make([]string, len(ips))
	for i, ip := range ips {
		result[i] = fmt.Sprintf("%s:500", ip)
	}
	return result, nil
}
