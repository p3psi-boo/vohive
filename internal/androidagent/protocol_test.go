package androidagent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStatusSnapshotPreservesMeaningfulZeroValues(t *testing.T) {
	payload, err := json.Marshal(StatusSnapshot{
		BatteryPct:             0,
		BatteryCharging:        false,
		RegStatus:              0,
		ServiceState:           0,
		SelectedSubscriptionID: 0,
		SignalSINR:             0,
		SignalSINRPresent:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, field := range []string{
		`"battery_pct":0`,
		`"battery_charging":false`,
		`"reg_status":0`,
		`"service_state":0`,
		`"selected_subscription_id":0`,
		`"signal_sinr":0`,
	} {
		if !strings.Contains(text, field) {
			t.Fatalf("snapshot JSON %s does not contain %s", text, field)
		}
	}
}

func TestStatusSnapshotRoundTripPreservesReportedZeroSignal(t *testing.T) {
	var snapshot StatusSnapshot
	if err := json.Unmarshal([]byte(`{"signal_dbm":-75,"signal_sinr":0,"nr5g_sinr":null}`), &snapshot); err != nil {
		t.Fatal(err)
	}
	if !snapshot.SignalDBMPresent || !snapshot.SignalSINRPresent || snapshot.NR5GSINRPresent {
		t.Fatalf("unexpected presence flags: %+v", snapshot)
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if !strings.Contains(text, `"signal_sinr":0`) {
		t.Fatalf("snapshot JSON %s dropped reported zero SINR", text)
	}
	if strings.Contains(text, `"nr5g_sinr"`) {
		t.Fatalf("snapshot JSON %s retained null NR SINR", text)
	}
}

func TestESIMStatusPreservesZeroResultCodes(t *testing.T) {
	payload, err := json.Marshal(ESIMStatusEvent{})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, field := range []string{`"result_code":0`, `"detailed_code":0`, `"subscription_id":0`, `"port_index":0`} {
		if !strings.Contains(text, field) {
			t.Fatalf("eSIM JSON %s does not contain %s", text, field)
		}
	}
}
