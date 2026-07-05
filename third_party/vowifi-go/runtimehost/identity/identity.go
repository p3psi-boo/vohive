package identity

import (
	"fmt"
	"strings"

	"github.com/iniwex5/vowifi-go/runtimehost/carrier"
)

type Domain string

const (
	DomainIMS Domain = "ims"
)

type Identity struct {
	IMSI   string
	IMPI   string
	IMPU   []string
	ISIM   bool
	Domain string
}

type IMPI struct {
	Value  string
	Domain Domain
}

type IMPU struct {
	Value  string
	Domain Domain
}

type Profile struct {
	IMSI      string
	IMPI      string
	IMPU      []string
	VoiceMode string
	PLMN      string
	MCC       string
	MNC       string
	IMEI      string
	SMSC      string
}

type PreparedSession struct {
	Profile            Profile
	Config             map[string]string
	EffectiveCarrier   carrier.EffectiveCarrierConfig
	EPDGSource         string
	EPDGAddr           string
	IdentityIMEISource string
	IMSIdentity        IMSIdentityResult
}

type PrepareStartInput struct {
	DeviceID            string
	TraceID             string
	EPDG                string
	Profile             Profile
	RuntimeEPDGOverride string
	Access              interface{}
}

type IMSIdentityResult struct {
	IMPI             string
	IMPU             string
	IMSHome          string
	IsISIM           bool
	RequestedSource  string
	ActualSource     string
	AKAAppPreference string
	Applied          bool
	Domain           string
}

const (
	AKAAppPreferenceISIMStrict = "isim_strict"
	IMSIdentitySourceISIM      = "isim"
)

func NormalizeProfile(p Profile) Profile {
	return p
}

// PrepareStart 准备 VoWiFi 启动画像 (对照 SimAdmin identity.rs + profiles.rs)。
// 根据 MCC/MNC 解析 ePDG 地址和 IMS 域,默认为 3GPP 标准 FQDN。
func PrepareStart(input PrepareStartInput) (PreparedSession, error) {
	mcc := strings.TrimSpace(input.Profile.MCC)
	mnc := strings.TrimSpace(input.Profile.MNC)
	if mcc == "" || mnc == "" {
		return PreparedSession{}, fmt.Errorf("identity: empty MCC or MNC")
	}

	paddedMnc := fmt.Sprintf("%03s", mnc)
	session := PreparedSession{
		Profile: input.Profile,
		EffectiveCarrier: carrier.ResolveEffectiveCarrierConfig(carrier.EffectiveCarrierConfigInput{
			DeviceID: input.DeviceID,
			MCC:      mcc,
			MNC:      mnc,
		}),
	}

	if strings.TrimSpace(input.RuntimeEPDGOverride) != "" {
		session.EPDGAddr = input.RuntimeEPDGOverride
		session.EPDGSource = "redirect"
	} else if input.EPDG != "" {
		session.EPDGAddr = input.EPDG
		session.EPDGSource = "identity_override"
	} else {
		session.EPDGAddr = fmt.Sprintf("epdg.epc.mnc%s.mcc%s.pub.3gppnetwork.org", paddedMnc, mcc)
		session.EPDGSource = "3gpp_standard"
	}

	imsDomain := fmt.Sprintf("ims.mnc%s.mcc%s.3gppnetwork.org", paddedMnc, mcc)
	session.IMSIdentity = IMSIdentityResult{
		IMPU:            fmt.Sprintf("sip:%s@%s", input.Profile.IMSI, imsDomain),
		IMSHome:         imsDomain,
		RequestedSource: IMSIdentitySourceISIM,
		ActualSource:    IMSIdentitySourceISIM,
	}

	if input.Access != nil {
		isim, err := ReadISIMIdentity(input.Access)
		if err != nil {
			return PreparedSession{}, err
		}
		if strings.TrimSpace(isim.IMPI) == "" || len(isim.IMPU) == 0 || strings.TrimSpace(isim.Domain) == "" {
			return PreparedSession{}, fmt.Errorf("ISIM 身份不完整")
		}
		session.IMSIdentity.IMPI = strings.TrimSpace(isim.IMPI)
		session.IMSIdentity.IMPU = strings.TrimSpace(isim.IMPU[0])
		session.IMSIdentity.Domain = strings.TrimSpace(isim.Domain)
		session.IMSIdentity.IsISIM = true
		session.IMSIdentity.AKAAppPreference = AKAAppPreferenceISIMStrict
		session.IMSIdentity.Applied = true
	}

	return session, nil
}

func ReadISIMIdentity(adapter interface{}) (Identity, error) {
	if reader, ok := adapter.(interface{ GetISIMIdentity() (Identity, error) }); ok {
		return reader.GetISIMIdentity()
	}
	return Identity{}, nil
}
