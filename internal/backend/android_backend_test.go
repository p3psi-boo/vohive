package backend

import (
	"testing"

	"github.com/iniwex5/vohive/internal/androidagent"
)

func TestAndroidSMSIdentity(t *testing.T) {
	tests := []struct {
		name           string
		snapshot       androidagent.StatusSnapshot
		subscriptionID int
		want           string
	}{
		{
			name: "subscription IMSI",
			snapshot: androidagent.StatusSnapshot{
				SelectedSubscriptionID: 7,
				Subscriptions:          []androidagent.Subscription{{SubscriptionID: 7, IMSI: " 460001234567890 "}},
			},
			subscriptionID: 7,
			want:           "460001234567890",
		},
		{
			name: "selected subscription ICCID fallback",
			snapshot: androidagent.StatusSnapshot{
				SelectedSubscriptionID: 8,
				Subscriptions:          []androidagent.Subscription{{SubscriptionID: 8, ICCID: " 8986000000000000001 "}},
			},
			subscriptionID: -1,
			want:           "android:android-1:iccid:8986000000000000001",
		},
		{
			name: "snapshot IMSI fallback",
			snapshot: androidagent.StatusSnapshot{
				IMSI: "460009999999999",
			},
			subscriptionID: 9,
			want:           "460009999999999",
		},
		{
			name:           "subscription fallback",
			snapshot:       androidagent.StatusSnapshot{},
			subscriptionID: 9,
			want:           "android:android-1:sub:9",
		},
		{
			name:           "device fallback",
			snapshot:       androidagent.StatusSnapshot{SelectedSubscriptionID: -1},
			subscriptionID: -1,
			want:           "android:android-1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := smsIdentityForSnapshot("android-1", test.snapshot, test.subscriptionID); got != test.want {
				t.Fatalf("SMS identity = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAndroidSMSIdentityWithoutSession(t *testing.T) {
	backend := NewAndroidBackend("android-1", nil)
	if got := backend.SMSIdentity(12); got != "android:android-1:sub:12" {
		t.Fatalf("SMS identity = %q", got)
	}
}

func TestSelectedAndroidSubscription(t *testing.T) {
	snapshot := androidagent.StatusSnapshot{
		SelectedSubscriptionID: 8,
		Subscriptions: []androidagent.Subscription{
			{SubscriptionID: 7, CarrierName: "first", Selected: true},
			{SubscriptionID: 8, CarrierName: "configured", MCC: "460", MNC: "01"},
		},
	}
	selected, ok := selectedAndroidSubscription(snapshot)
	if !ok || selected.SubscriptionID != 8 || selected.MCC != "460" || selected.MNC != "01" {
		t.Fatalf("selected subscription = %+v, ok=%v", selected, ok)
	}
}
