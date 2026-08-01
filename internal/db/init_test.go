package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitCreatesDatabaseDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "data", "vohive.db")
	if err := Init(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database file was not created: %v", err)
	}
}
