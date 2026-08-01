package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSMSDeliveryPartUpsertUsesMessageAndPartIdentity(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "delivery.db")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := CreateSMSDelivery("message-1", "imsi-1", "device-1", "+10086", "hello", 2, now); err != nil {
		t.Fatal(err)
	}
	if err := UpsertSMSDeliveryPart("message-1", 1, "", -1, SMSDeliveryPartStatePending, now); err != nil {
		t.Fatal(err)
	}
	if err := UpsertSMSDeliveryPart("message-1", 1, "", -1, SMSDeliveryPartStateAcked, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := UpsertSMSDeliveryPart("message-1", 2, "", -1, SMSDeliveryPartStateAcked, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := RecomputeSMSDelivery("message-1", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	status, err := GetSMSDeliveryStatus("message-1")
	if err != nil {
		t.Fatal(err)
	}
	if status == nil || status.State != SMSDeliveryStateAcked || status.Acks != 2 || len(status.Parts) != 2 {
		t.Fatalf("unexpected delivery status: %+v", status)
	}
	var duplicates int64
	if err := DB.Model(&SMSDeliveryPart{}).
		Where("message_id = ? AND part_no = ?", "message-1", 1).Count(&duplicates).Error; err != nil {
		t.Fatal(err)
	}
	if duplicates != 1 {
		t.Fatalf("part upsert created %d rows, want 1", duplicates)
	}
}

func TestSMSDeliveryPartMigrationRepairsLegacyIndex(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "legacy-delivery.db")); err != nil {
		t.Fatal(err)
	}
	if err := DB.Exec(`DROP INDEX IF EXISTS idx_sms_delivery_part_mid_no`).Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Exec(`CREATE INDEX idx_sms_delivery_part_mid_no ON sms_delivery_part(message_id, part_no)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Exec(`INSERT INTO sms_delivery_part(message_id, part_no, state) VALUES
		('legacy-message', 1, 'pending'), ('legacy-message', 1, 'acked')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateSMSDeliveryPartUniqueIndex(DB); err != nil {
		t.Fatal(err)
	}
	var parts []SMSDeliveryPart
	if err := DB.Where("message_id = ? AND part_no = ?", "legacy-message", 1).Find(&parts).Error; err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0].State != SMSDeliveryPartStateAcked {
		t.Fatalf("migration kept unexpected rows: %+v", parts)
	}
	if err := DB.Exec(`INSERT INTO sms_delivery_part(message_id, part_no, state) VALUES
		('legacy-message', 1, 'pending')`).Error; err == nil {
		t.Fatal("unique delivery-part identity was not enforced after migration")
	}
}
