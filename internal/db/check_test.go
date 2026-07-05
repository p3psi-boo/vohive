package db

import (
	"path/filepath"
	"testing"
)

func TestCheckSchema(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "schema.db")); err != nil {
		t.Fatalf("Init() error=%v", err)
	}
	var m []map[string]interface{}
	DB.Raw("PRAGMA table_info(devices)").Scan(&m)
	if len(m) == 0 {
		t.Fatal("devices schema is empty")
	}
}
