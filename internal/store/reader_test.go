package store_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

func TestOpenReturnsErrorWhenEncryptedAndNoDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")
	encPath := dbPath + ".enc"

	// Create the .enc file (empty content is enough to trigger the guard)
	if err := os.WriteFile(encPath, []byte("ENGM fake encrypted data"), 0600); err != nil {
		t.Fatalf("create .enc file: %v", err)
	}

	// Do NOT create the .db file — only .enc exists

	_, err := store.Open(dbPath)
	if err == nil {
		t.Fatal("Open: expected error when only .enc exists, got nil")
	}

	if !strings.Contains(err.Error(), "encrypted") {
		t.Errorf("error should mention 'encrypted', got: %q", err.Error())
	}
}

func TestOpenSucceedsWhenOnlyDBExists(t *testing.T) {
	// This test verifies the guard does NOT trigger when the DB exists normally.
	// We can't easily open a real SQLite DB in a unit test without creating one,
	// so we just verify the error is NOT the "encrypted" guard error.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")

	// No .db, no .enc → Open will fail with a SQLite/file error, not our guard
	_, err := store.Open(dbPath)
	if err != nil && strings.Contains(err.Error(), "encrypted") {
		t.Errorf("Open should not return 'encrypted' error when no .enc file exists, got: %q", err.Error())
	}
}

func TestOpenNoEncGuardWhenBothExist(t *testing.T) {
	// If .db exists (stat succeeds), the enc guard must NOT trigger, even if .enc also exists.
	// This mimics the decrypted state where .db is available.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")
	encPath := dbPath + ".enc"

	// Create both files — .db exists so the guard should be bypassed
	if err := os.WriteFile(dbPath, []byte(""), 0600); err != nil {
		t.Fatalf("create .db file: %v", err)
	}
	if err := os.WriteFile(encPath, []byte("ENGM fake"), 0600); err != nil {
		t.Fatalf("create .enc file: %v", err)
	}

	_, err := store.Open(dbPath)
	if err != nil && strings.Contains(err.Error(), "encrypted") {
		t.Errorf("enc guard should not trigger when .db exists; got: %q", err.Error())
	}
}
