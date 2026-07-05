// Package imsheaders 提供 IMS SIP 头域构造与解析 (3GPP TS 24.229)。
package imsheaders

import "fmt"

type PAccessNetworkInfo struct {
	AccessType string
	CellID     string
	MCC        string
	MNC        string
	TAC        string
}

func BuildPANI(accessType, cellID string) *PAccessNetworkInfo {
	return &PAccessNetworkInfo{
		AccessType: accessType,
		CellID:     cellID,
	}
}

func (p *PAccessNetworkInfo) String() string {
	if p == nil {
		return ""
	}
	return fmt.Sprintf("3GPP-E-UTRAN;utran-cell-id-3gpp=%s", p.CellID)
}

func BuildServiceRoute(proxy string) string {
	return fmt.Sprintf("<sip:%s:5070;lr>", proxy)
}

func BuildPath(proxy string) string {
	return fmt.Sprintf("<sip:%s:5070;lr;transport=tcp>", proxy)
}

func BuildPAssociatedURI(impu string) string {
	return fmt.Sprintf("<sip:%s>", impu)
}

func BuildPCalledPartyID(callee string) string {
	return fmt.Sprintf("<tel:%s>", callee)
}

func BuildPAccessNetworkInfoHeader(info *PAccessNetworkInfo) string {
	if info == nil {
		return ""
	}
	return info.String()
}

type SecurityClient struct {
	Mechanism string
	IPsecSPI  uint32
	Port      int
}

func BuildSecurityClient(spi uint32, port int) *SecurityClient {
	return &SecurityClient{
		Mechanism: "ipsec-3gpp",
		IPsecSPI:  spi,
		Port:      port,
	}
}

func (s *SecurityClient) String() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("%s;prot=esp;mode=transport;spi-c=%d;port-c=%d", s.Mechanism, s.IPsecSPI, s.Port)
}

type SecurityVerify struct {
	Mechanism string
	IPsecSPI  uint32
	Port      int
}

func BuildSecurityVerify(spi uint32, port int) *SecurityVerify {
	return &SecurityVerify{
		Mechanism: "ipsec-3gpp",
		IPsecSPI:  spi,
		Port:      port,
	}
}

func (s *SecurityVerify) String() string {
	if s == nil {
		return ""
	}
	return fmt.Sprintf("%s;prot=esp;mode=transport;spi-s=%d;port-s=%d", s.Mechanism, s.IPsecSPI, s.Port)
}
