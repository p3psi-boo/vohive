package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 期望终态:Update/Add 保存设备时不把运行时路径写进 config(只存 IMEI + 意图)。
// 当前实现会写 control_device/interface/at_port → 本测试现在应 FAIL,证明保存侧泄漏。
func TestUpdateDeviceInFileDoesNotPersistRuntimePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	raw := "devices:\n- id: dev1\n  device_backend: qmi\n  modem_imei: \"867383058993207\"\n  vowifi_enabled: true\n"
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// 模拟切运营商/编辑:传入带运行时解析路径的 cfg。
	newDev := DeviceConfig{
		ID:            "dev1",
		ModemIMEI:     "867383058993207",
		DeviceBackend: "qmi",
		VoWiFiEnabled: true,
		ControlDevice: "/dev/cdc-wdm3", // 运行时路径,不应被持久化
		Interface:     "wwan2",
		ATPort:        "/dev/ttyUSB9",
		USBPath:       "/sys/bus/usb/devices/1-7",
	}
	if err := UpdateDeviceInFile(path, "dev1", newDev); err != nil {
		t.Fatalf("UpdateDeviceInFile() error = %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	d := got.Devices[0]
	if d.ModemIMEI != "867383058993207" || d.DeviceBackend != "qmi" {
		t.Fatalf("identity/intent fields lost: %+v", d)
	}
	if d.ControlDevice != "" || d.Interface != "" || d.ATPort != "" || d.USBPath != "" {
		t.Fatalf("runtime paths must not be persisted, got: %+v", d)
	}
}

// Add 新设备时同样不写路径(只 IMEI + 意图)。
func TestAddDeviceInFileDoesNotPersistRuntimePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("devices: []\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	dev := DeviceConfig{
		ID: "dev9", ModemIMEI: "861234567890123", DeviceBackend: "mbim",
		ControlDevice: "/dev/cdc-wdm5", Interface: "wwan5", ATPort: "/dev/ttyUSB1",
		USBPath: "/sys/bus/usb/devices/2-1",
	}
	if err := AddDeviceInFile(path, dev); err != nil {
		t.Fatalf("AddDeviceInFile() error = %v", err)
	}
	got, _ := Load(path)
	d := got.Devices[0]
	if d.ModemIMEI != "861234567890123" || d.DeviceBackend != "mbim" {
		t.Fatalf("identity/intent lost: %+v", d)
	}
	if d.ControlDevice != "" || d.Interface != "" || d.ATPort != "" || d.USBPath != "" {
		t.Fatalf("runtime paths must not be persisted, got: %+v", d)
	}
}

func TestAndroidDeviceBindingRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("devices: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := DeviceConfig{
		DeviceKind:    DeviceKindAndroid,
		Android:       AndroidDeviceConfig{AgentID: "agent-123"},
		ID:            "android-1",
		Name:          "Pixel",
		DeviceBackend: "android",
		SMSEnabled:    true,
	}
	if err := AddDeviceInFile(path, want); err != nil {
		t.Fatalf("AddDeviceInFile: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Devices) != 1 {
		t.Fatalf("devices=%d, want 1", len(got.Devices))
	}
	device := got.Devices[0]
	if !IsAndroidDevice(device) || device.Android.AgentID != "agent-123" || device.DeviceBackend != "android" {
		t.Fatalf("Android binding did not round-trip: %+v", device)
	}
	if device.ModemIMEI != "" || device.ControlDevice != "" || device.Interface != "" {
		t.Fatalf("Android binding persisted modem runtime identity: %+v", device)
	}

	want.Android.AgentID = "agent-456"
	if err := UpdateDeviceInFile(path, want.ID, want); err != nil {
		t.Fatalf("UpdateDeviceInFile: %v", err)
	}
	got, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Devices[0].Android.AgentID != "agent-456" {
		t.Fatalf("updated agent ID=%q", got.Devices[0].Android.AgentID)
	}
}
