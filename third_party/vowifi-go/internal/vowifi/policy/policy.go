// Package policy 提供 VoWiFi 策略决策 (对照 SimAdmin profiles.rs policy structs)。
package policy

type Policy struct {
	EnableVoWiFi       bool
	EnableSMSOverIP    bool
	EnableUSSDOverIP   bool
	PreferWiFiCalling  bool
	RoamingBlockedMCCs []string

	IKEProposals       []string
	ESPProposals       []string
	NATKeepAliveSec    int
	DPDIntervalSec     int
	AKAChallengeMode   string
}

func DefaultPolicy() *Policy {
	return &Policy{
		EnableVoWiFi:      true,
		EnableSMSOverIP:   true,
		EnableUSSDOverIP:  true,
		NATKeepAliveSec:    20,
		DPDIntervalSec:     60,
		AKAChallengeMode:   "resync_capable",
		IKEProposals:      []string{"aes256-sha256-modp2048"},
		ESPProposals:      []string{"aes256-sha256"},
	}
}

func (p *Policy) IsVoWiFiEnabled() bool { return p != nil && p.EnableVoWiFi }
func (p *Policy) IsSMSOverIPEnabled() bool { return p != nil && p.EnableSMSOverIP }
func (p *Policy) IsMCCRoaming(mcc string) bool {
	if p == nil { return false }
	for _, blocked := range p.RoamingBlockedMCCs {
		if blocked == mcc { return true }
	}
	return false
}
