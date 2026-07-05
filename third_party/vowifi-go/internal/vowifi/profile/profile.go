package profile

import (
	"fmt"
	"strings"
	"sync"

	"github.com/iniwex5/vowifi-go/internal/vowifi/dns"
)

type CarrierProfileMeta struct {
	ProfileID         string
	MCC               string
	MNC               string
	MNCLen            int
	PLMN              string
	CountryISO2       string
	Brand             string
	OperatorLegalName string
	Aliases           []string
}

type ProfileIdentityPolicy struct {
	DeviceModelHint string
	SpoofIMEI       bool
}

type EpdgPolicy struct {
	Host      string
	Port      int
	APN       string
	IPStack   string
	DNSServer string
}

type Ikev2Policy struct {
	NATKeepAliveSeconds  int
	DPDIntervalSeconds   int
	ReauthIntervalSeconds int
	IKEProposals         []string
	ESPProposals         []string
	AKAChallengeMode     string
	IncludeEPDGIDR       bool
}

type RegisterPolicy struct {
	SupportedHeader             string
	IncludePaniAuthenticated    bool
	StrictSecurityServerOffer   bool
	EnableInitialRejectFallback bool
	UsePlainDigestPlaceholder   bool
	RequireSecAgreeHeaders      bool
	SecurityClientMechanisms    []string
	LiveHeaderVariantSet        string
}

type ImsPolicy struct {
	Domain         string
	Realm          string
	Registrar      string
	PCSCF          string
	Transport      string
	LocalPort      int
	UserAgent      string
	IdentitySource string
	Register       RegisterPolicy
}

type SmsPolicy struct {
	ReceiverTransport string
	SMSCAuthRequired  bool
}

type E911Policy struct {
	Enabled            bool
	Provider           string
	EntitlementURL     string
	WebsheetHostPolicy string
}

type CarrierProfile struct {
	Meta     CarrierProfileMeta
	Identity ProfileIdentityPolicy
	Epdg     EpdgPolicy
	Ikev2    Ikev2Policy
	Ims      ImsPolicy
	Sms      SmsPolicy
	E911     E911Policy
}

type Manager struct {
	mu       sync.RWMutex
	profiles map[string]*CarrierProfile
	builtins []*CarrierProfile
}

func NewManager() *Manager {
	m := &Manager{profiles: make(map[string]*CarrierProfile)}
	m.loadBuiltins()
	return m
}

func (m *Manager) loadBuiltins() {
	// 标准 3GPP 默认值
	defaultIke := Ikev2Policy{
		NATKeepAliveSeconds:  20,
		DPDIntervalSeconds:   600,
		IKEProposals:         []string{"aes128-sha256-modp2048"},
		ESPProposals:         []string{"aes128-sha256"},
		AKAChallengeMode:     "standard",
	}
	defaultSms := SmsPolicy{ReceiverTransport: "tcp"}

	m.builtins = []*CarrierProfile{
		// UK
		makeProfile("gb_ee_23433", "EE", "GB", "234", "33", 2, nil, defaultIke, &ImsPolicy{
			Domain: "ims.mnc033.mcc234.3gppnetwork.org", Realm: "ims.mnc033.mcc234.3gppnetwork.org",
			Transport: "tcp", LocalPort: 5060, UserAgent: "SimAdmin VoWiFi",
			IdentitySource: "isim",
			Register: RegisterPolicy{
				SupportedHeader: "path,sec-agree,gruu", IncludePaniAuthenticated: true,
				StrictSecurityServerOffer: true, EnableInitialRejectFallback: true,
				RequireSecAgreeHeaders: true, LiveHeaderVariantSet: "ee_ims_features",
				SecurityClientMechanisms: []string{"ipsec-3gpp;hmac-sha-1-96;aes-cbc;esp;trans"},
			},
		}, defaultSms),

		// US
		makeProfile("us_tmobile_310260", "T-Mobile", "US", "310", "260", 3, nil, defaultIke, &ImsPolicy{
			Domain: "ims.mnc260.mcc310.3gppnetwork.org", Realm: "ims.mnc260.mcc310.3gppnetwork.org",
			Transport: "tcp", LocalPort: 5060, UserAgent: "SimAdmin VoWiFi",
			IdentitySource: "isim",
			Register: RegisterPolicy{
				SupportedHeader: "path,sec-agree,gruu", IncludePaniAuthenticated: true,
				StrictSecurityServerOffer: true, RequireSecAgreeHeaders: true,
				SecurityClientMechanisms: []string{"ipsec-3gpp;hmac-sha-1-96;aes-cbc;esp;trans"},
			},
		}, defaultSms),

		makeProfile("us_att_310410", "AT&T", "US", "310", "410", 3, &EpdgPolicy{
			Host: "epdg.epc.att.net", Port: 500, APN: "ims", IPStack: "ipv4v6",
		}, defaultIke, &ImsPolicy{
			Domain: "ims.mnc410.mcc310.3gppnetwork.org", Realm: "ims.mnc410.mcc310.3gppnetwork.org",
			Transport: "tcp", LocalPort: 5060, UserAgent: "SimAdmin VoWiFi",
			IdentitySource: "isim",
			Register: RegisterPolicy{
				SupportedHeader: "path,sec-agree,gruu", IncludePaniAuthenticated: true,
				StrictSecurityServerOffer: true, EnableInitialRejectFallback: true,
				RequireSecAgreeHeaders: true,
				SecurityClientMechanisms: []string{"ipsec-3gpp;hmac-sha-1-96;aes-cbc;esp;trans"},
			},
		}, defaultSms),

		// DE
		makeProfile("de_o2_26207", "O2", "DE", "262", "07", 2, nil, defaultIke, &ImsPolicy{
			Domain: "ims.mnc007.mcc262.3gppnetwork.org", Realm: "ims.mnc007.mcc262.3gppnetwork.org",
			Transport: "tcp", LocalPort: 5060, UserAgent: "SimAdmin VoWiFi",
			IdentitySource: "isim",
			Register: RegisterPolicy{
				SupportedHeader: "path,sec-agree,gruu", IncludePaniAuthenticated: true,
				StrictSecurityServerOffer: true, RequireSecAgreeHeaders: true,
				SecurityClientMechanisms: []string{"ipsec-3gpp;hmac-sha-1-96;aes-cbc;esp;trans"},
			},
		}, defaultSms),

		// NZ
		makeProfile("nz_spark_53005", "Spark", "NZ", "530", "05", 2, &EpdgPolicy{
			Host: "epdg.epc.mnc005.mcc530.pub.3gppnetwork.spark.co.nz", Port: 500, IPStack: "ipv4v6",
		}, defaultIke, &ImsPolicy{
			Domain: "ims.mnc005.mcc530.3gppnetwork.org", Realm: "ims.mnc005.mcc530.3gppnetwork.org",
			Transport: "tcp", LocalPort: 5060, UserAgent: "SimAdmin VoWiFi",
			IdentitySource: "isim",
			Register: RegisterPolicy{
				SupportedHeader: "path,sec-agree,gruu", IncludePaniAuthenticated: true,
				StrictSecurityServerOffer: true, RequireSecAgreeHeaders: true,
				SecurityClientMechanisms: []string{"ipsec-3gpp;hmac-sha-1-96;aes-cbc;esp;trans"},
			},
		}, defaultSms),

		// NL
		makeProfile("nl_vodafone_20404", "Vodafone", "NL", "204", "04", 2, nil, defaultIke, &ImsPolicy{
			Domain: "ims.mnc004.mcc204.3gppnetwork.org", Realm: "ims.mnc004.mcc204.3gppnetwork.org",
			Transport: "tcp", LocalPort: 5060, UserAgent: "SimAdmin VoWiFi",
			IdentitySource: "isim",
			Register: RegisterPolicy{
				SupportedHeader: "path,sec-agree,gruu", IncludePaniAuthenticated: true,
				StrictSecurityServerOffer: true, RequireSecAgreeHeaders: true,
				SecurityClientMechanisms: []string{"ipsec-3gpp;hmac-sha-1-96;aes-cbc;esp;trans"},
			},
		}, defaultSms),
	}

	for _, p := range m.builtins {
		m.profiles[p.Meta.PLMN] = p
	}
}

func makeProfile(id, brand, country, mcc, mnc string, mncLen int, epdgOverride *EpdgPolicy, ike Ikev2Policy, ims *ImsPolicy, sms SmsPolicy) *CarrierProfile {
	if mncLen == 0 {
		mncLen = len(mnc)
	}
	paddedMnc := fmt.Sprintf("%03s", mnc)
	p := &CarrierProfile{
		Meta: CarrierProfileMeta{
			ProfileID: id, Brand: brand, CountryISO2: country,
			MCC: mcc, MNC: mnc, MNCLen: mncLen, PLMN: mcc + mnc,
			OperatorLegalName: brand,
		},
		Identity: ProfileIdentityPolicy{DeviceModelHint: "generic_android_class", SpoofIMEI: false},
		Epdg: EpdgPolicy{
			Host:    dns.BuildEPDGFQDN(mcc, paddedMnc),
			Port:    500,
			APN:     "ims",
			IPStack: "ipv4v6",
		},
		Ikev2:  ike,
		Ims:    *ims,
		Sms:    sms,
	}
	if epdgOverride != nil {
		p.Epdg = *epdgOverride
	}
	return p
}

// Match 根据 MCC/MNC 匹配运营商配置。
// 先查内置列表,未命中则动态生成标准 3GPP Profile(对照 SimAdmin resolve_by_plmn)。
func (m *Manager) Match(mcc, mnc string) *CarrierProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := mcc + mnc
	if p, ok := m.profiles[key]; ok {
		return p
	}
	for _, p := range m.builtins {
		if p.Meta.MCC == mcc && p.Meta.MNC == mnc {
			return p
		}
	}

	if mccLen := len(mcc); mccLen >= 3 && isValidPLMN(mcc, mnc) {
		return generateStandard3GPP(mcc, mnc)
	}
	return nil
}

// generateStandard3GPP 动态生成标准 3GPP Profile (对照 SimAdmin generate_standard_3gpp_profile)。
func generateStandard3GPP(mcc, mnc string) *CarrierProfile {
	paddedMnc := fmt.Sprintf("%03s", mnc)
	return &CarrierProfile{
		Meta: CarrierProfileMeta{
			ProfileID:         fmt.Sprintf("dynamic_3gpp_%s%s", mcc, mnc),
			MCC:               mcc,
			MNC:               mnc,
			MNCLen:            len(mnc),
			PLMN:              mcc + mnc,
			CountryISO2:       "unknown",
			Brand:             "Standard 3GPP",
			OperatorLegalName: "Generic 3GPP Carrier",
		},
		Identity: ProfileIdentityPolicy{DeviceModelHint: "generic_android_class", SpoofIMEI: false},
		Epdg: EpdgPolicy{
			Host:    dns.BuildEPDGFQDN(mcc, paddedMnc),
			Port:    500,
			APN:     "ims",
			IPStack: "ipv4v6",
		},
		Ikev2: Ikev2Policy{
			NATKeepAliveSeconds: 20,
			DPDIntervalSeconds:  600,
			IKEProposals:        []string{"aes128-sha256-modp2048"},
			ESPProposals:        []string{"aes128-sha256"},
			AKAChallengeMode:    "standard",
		},
		Ims: ImsPolicy{
			Domain:         fmt.Sprintf("ims.mnc%s.mcc%s.3gppnetwork.org", paddedMnc, mcc),
			Realm:          fmt.Sprintf("ims.mnc%s.mcc%s.3gppnetwork.org", paddedMnc, mcc),
			Transport:      "tcp",
			LocalPort:      5060,
			UserAgent:      "SimAdmin VoWiFi",
			IdentitySource: "isim",
			Register: RegisterPolicy{
				SupportedHeader: "path,sec-agree,gruu",
				IncludePaniAuthenticated: true,
				RequireSecAgreeHeaders:   true,
				SecurityClientMechanisms: []string{"ipsec-3gpp;hmac-sha-1-96;aes-cbc;esp;trans"},
			},
		},
		Sms: SmsPolicy{ReceiverTransport: "tcp"},
	}
}

func (m *Manager) GetByProfileID(profileID string) *CarrierProfile {
	normalized := strings.TrimSpace(profileID)
	if normalized == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, p := range m.builtins {
		if p.Meta.ProfileID == normalized {
			return p
		}
	}
	if len(normalized) >= 5 {
		prefix := normalized[len(normalized)-5:]
		if isValidPLMN(prefix[:3], prefix[3:]) {
			return generateStandard3GPP(prefix[:3], prefix[3:])
		}
	}
	return nil
}

func (m *Manager) List() []*CarrierProfile {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]*CarrierProfile{}, m.builtins...)
}

func isValidPLMN(mcc, mnc string) bool {
	if len(mcc) != 3 || len(mnc) == 0 || len(mnc) > 3 {
		return false
	}
	for _, c := range mcc + mnc {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
