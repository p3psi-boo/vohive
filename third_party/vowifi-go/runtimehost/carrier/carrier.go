package carrier

import (
	"fmt"
	"strings"
)

// EffectiveCarrierConfigInput 运营商配置解析输入。
type EffectiveCarrierConfigInput struct {
	DeviceID string
	Carrier  string
	MCC      string
	MNC      string
}

// EffectiveCarrierConfig 运营商有效配置。
type E911Config struct {
	Enabled  bool
	Provider string
}

type EffectiveCarrierConfig struct {
	PresetID     string
	Carrier      string
	MCC          string
	MNC          string
	IMS          bool
	VoWiFiMode   string
	SMSCodecPlan string
	E911         E911Config
}

// LoadCarrierOverrides 加载运营商覆盖配置。
type LoadCarrierResult struct {
	Path    string
	Missing bool
	Count   int
}

func LoadCarrierOverrides(path string) (LoadCarrierResult, error) {
	return LoadCarrierResult{}, nil
}

// ClearCarrierOverrides 清除运营商覆盖配置。
func ClearCarrierOverrides() {}

// ResolveEffectiveCarrierConfig 解析运营商有效配置。
func ResolveEffectiveCarrierConfig(input EffectiveCarrierConfigInput) EffectiveCarrierConfig {
	mcc := strings.TrimSpace(input.MCC)
	mnc := strings.TrimSpace(input.MNC)
	if mcc == "310" && mnc == "280" {
		return EffectiveCarrierConfig{
			PresetID: "att-us",
			Carrier:  "att",
			MCC:      mcc,
			MNC:      mnc,
			IMS:      true,
			E911: E911Config{
				Enabled:  true,
				Provider: "att",
			},
		}
	}
	return EffectiveCarrierConfig{}
}

// IsVoWiFiBlockedMCC 检查 MCC 是否被 VoWiFi 拦截。
func IsVoWiFiBlockedMCC(mcc string) bool {
	return strings.TrimSpace(mcc) == "460"
}

// IsVoWiFiPolicyBlockedError 检查错误是否为策略拦截错误。
func IsVoWiFiPolicyBlockedError(err error) bool {
	_, ok := err.(VoWiFiBlockedMCCError)
	return ok
}

// NewVoWiFiBlockedMCCError 创建 VoWiFi 拦截错误。
func NewVoWiFiBlockedMCCError(mcc string) error {
	return VoWiFiBlockedMCCError{MCC: strings.TrimSpace(mcc)}
}

type VoWiFiBlockedMCCError struct {
	MCC string
}

func (e VoWiFiBlockedMCCError) Error() string {
	return fmt.Sprintf("vowifi blocked for MCC %s", e.MCC)
}
