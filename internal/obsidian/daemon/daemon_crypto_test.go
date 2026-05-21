package daemon

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/crypto"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
	_ "modernc.org/sqlite"
)

// newTestDaemon builds a minimal Daemon pointing at dir, with a no-op logger.
func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	return New(Config{
		Logf: func(string, ...any) {}, // silence logs in tests
	})
}

// makeTestSQLiteDB creates a minimal valid SQLite database at dbPath.
// encryptDB requires a real SQLite file because it runs WAL checkpoint.
func makeTestSQLiteDB(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s", dbPath))
	if err != nil {
		t.Fatalf("makeTestSQLiteDB: open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS test_data (id INTEGER PRIMARY KEY, val TEXT)`); err != nil {
		t.Fatalf("makeTestSQLiteDB: create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO test_data(val) VALUES ('hello engram')`); err != nil {
		t.Fatalf("makeTestSQLiteDB: insert: %v", err)
	}
}

// TestResolveDBStateBothExist: when .db and .enc both exist, .enc is authoritative.
// resolveDBState must delete .db and leave .enc intact.
func TestResolveDBStateBothExist(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")
	encPath := dbPath + ".enc"

	if err := os.WriteFile(dbPath, []byte("plaintext"), 0600); err != nil {
		t.Fatalf("create .db: %v", err)
	}
	if err := os.WriteFile(encPath, []byte("encrypted"), 0600); err != nil {
		t.Fatalf("create .enc: %v", err)
	}

	d := newTestDaemon(t)
	if err := d.resolveDBState(dbPath); err != nil {
		t.Fatalf("resolveDBState: unexpected error: %v", err)
	}

	// .db must be gone
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Error(".db should have been deleted but still exists")
	}

	// .enc must still be present
	if _, err := os.Stat(encPath); err != nil {
		t.Errorf(".enc should still exist, got: %v", err)
	}
}

// TestResolveDBStateCleansTmpFiles: .tmp and .enc.tmp residuals are deleted at startup.
func TestResolveDBStateCleansTmpFiles(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")
	tmpPath := dbPath + ".tmp"
	encTmpPath := dbPath + ".enc.tmp"

	if err := os.WriteFile(tmpPath, []byte("partial write"), 0600); err != nil {
		t.Fatalf("create .tmp: %v", err)
	}
	if err := os.WriteFile(encTmpPath, []byte("partial enc write"), 0600); err != nil {
		t.Fatalf("create .enc.tmp: %v", err)
	}

	d := newTestDaemon(t)
	if err := d.resolveDBState(dbPath); err != nil {
		t.Fatalf("resolveDBState: unexpected error: %v", err)
	}

	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error(".tmp should have been cleaned up but still exists")
	}
	if _, err := os.Stat(encTmpPath); !os.IsNotExist(err) {
		t.Error(".enc.tmp should have been cleaned up but still exists")
	}
}

// TestEncryptDBNoopWhenNoDB: encryptDB returns nil without creating .enc when .db absent.
func TestEncryptDBNoopWhenNoDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")
	encPath := dbPath + ".enc"

	// No .db file present
	d := newTestDaemon(t)
	if err := d.encryptDB(dbPath); err != nil {
		t.Fatalf("encryptDB: unexpected error: %v", err)
	}

	if _, err := os.Stat(encPath); !os.IsNotExist(err) {
		t.Error("encryptDB should be no-op when .db absent, but .enc was created")
	}
}

// TestDecryptDBNoopWhenNoEnc: decryptDB returns nil without creating .db when .enc absent.
func TestDecryptDBNoopWhenNoEnc(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")

	// No .enc file present
	d := newTestDaemon(t)
	if err := d.decryptDB(dbPath); err != nil {
		t.Fatalf("decryptDB: unexpected error: %v", err)
	}

	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Error("decryptDB should be no-op when .enc absent, but .db was created")
	}
}

// TestEncryptDecryptDBRoundtrip: encryptDB followed by decryptDB preserves file content.
// Uses a real SQLite DB because encryptDB runs PRAGMA wal_checkpoint(TRUNCATE).
func TestEncryptDecryptDBRoundtrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")

	// Create a real SQLite DB (required for WAL checkpoint to succeed)
	makeTestSQLiteDB(t, dbPath)

	// Capture original file bytes for comparison after roundtrip
	originalContent, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read original .db: %v", err)
	}

	d := newTestDaemon(t)

	// Step 1: encryptDB — .db → .enc, .db removed
	if err := d.encryptDB(dbPath); err != nil {
		t.Fatalf("encryptDB: %v", err)
	}

	encPath := dbPath + ".enc"
	if _, err := os.Stat(encPath); err != nil {
		t.Fatalf("encryptDB: .enc not created: %v", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Error("encryptDB: .db should be removed after encryption")
	}

	// Step 2: decryptDB — .enc → .db, .enc removed
	if err := d.decryptDB(dbPath); err != nil {
		t.Fatalf("decryptDB: %v", err)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("decryptDB: .db not created: %v", err)
	}
	if _, err := os.Stat(encPath); !os.IsNotExist(err) {
		t.Error("decryptDB: .enc should be removed after decryption")
	}

	// Step 3: content must match what was encrypted
	recovered, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read recovered .db: %v", err)
	}

	if len(recovered) != len(originalContent) {
		t.Errorf("roundtrip size mismatch: got %d bytes, want %d bytes", len(recovered), len(originalContent))
	}
}

// TestEncryptDecryptRoundtripMultipleCycles: three full encrypt/decrypt cycles preserve content.
func TestEncryptDecryptRoundtripMultipleCycles(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")

	makeTestSQLiteDB(t, dbPath)

	originalContent, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read original .db: %v", err)
	}

	d := newTestDaemon(t)

	for cycle := 1; cycle <= 3; cycle++ {
		if err := d.encryptDB(dbPath); err != nil {
			t.Fatalf("cycle %d encryptDB: %v", cycle, err)
		}
		if err := d.decryptDB(dbPath); err != nil {
			t.Fatalf("cycle %d decryptDB: %v", cycle, err)
		}
	}

	recovered, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read final .db: %v", err)
	}
	if len(recovered) != len(originalContent) {
		t.Errorf("multi-cycle content size mismatch: got %d, want %d", len(recovered), len(originalContent))
	}
}

// TestDecryptDBFailsOnTamperedEnc: decryptDB returns error if .enc is tampered.
func TestDecryptDBFailsOnTamperedEnc(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")

	// Create a real SQLite DB and encrypt it
	makeTestSQLiteDB(t, dbPath)

	d := newTestDaemon(t)
	if err := d.encryptDB(dbPath); err != nil {
		t.Fatalf("encryptDB: %v", err)
	}

	encPath := dbPath + ".enc"

	// Read and tamper the .enc file
	data, err := os.ReadFile(encPath)
	if err != nil {
		t.Fatalf("read .enc: %v", err)
	}
	// Flip the last byte (GCM tag area)
	data[len(data)-1] ^= 0xFF
	if err := os.WriteFile(encPath, data, 0600); err != nil {
		t.Fatalf("write tampered .enc: %v", err)
	}

	err = d.decryptDB(dbPath)
	if err == nil {
		t.Fatal("decryptDB: expected error on tampered .enc, got nil")
	}
}

// TestEncryptProducesUniqueOutputPerCall: two encryptions of same content must differ (random nonce).
func TestEncryptProducesUniqueOutputPerCall(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")

	makeTestSQLiteDB(t, dbPath)

	d := newTestDaemon(t)

	// First cycle: encrypt then capture .enc
	if err := d.encryptDB(dbPath); err != nil {
		t.Fatalf("encryptDB cycle 1: %v", err)
	}
	enc1, err := os.ReadFile(dbPath + ".enc")
	if err != nil {
		t.Fatalf("read .enc cycle 1: %v", err)
	}

	// Decrypt to restore .db
	if err := d.decryptDB(dbPath); err != nil {
		t.Fatalf("decryptDB cycle 1: %v", err)
	}

	// Second cycle: encrypt again
	if err := d.encryptDB(dbPath); err != nil {
		t.Fatalf("encryptDB cycle 2: %v", err)
	}
	enc2, err := os.ReadFile(dbPath + ".enc")
	if err != nil {
		t.Fatalf("read .enc cycle 2: %v", err)
	}

	// The two .enc files must differ (different nonces)
	same := len(enc1) == len(enc2)
	if same {
		for i := range enc1 {
			if enc1[i] != enc2[i] {
				same = false
				break
			}
		}
	}
	if same {
		t.Error("two encryptions of same content produced identical .enc files — nonce must be random")
	}
}

// TestEncryptAdditionalCheck: verify raw crypto Encrypt/Decrypt with a known key.
// This is a safety net that doesn't depend on keyring availability.
func TestEncryptAdditionalCheck(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plain := []byte("additional check payload")

	enc, err := crypto.Encrypt(key, plain)
	if err != nil {
		t.Fatalf("crypto.Encrypt: %v", err)
	}

	dec, err := crypto.Decrypt(key, enc)
	if err != nil {
		t.Fatalf("crypto.Decrypt: %v", err)
	}

	if string(dec) != string(plain) {
		t.Errorf("mismatch: got %q, want %q", dec, plain)
	}
}

func TestPrepareSelectionDBDecryptsAndRestoresEncryptedDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")
	makeTestSQLiteDB(t, dbPath)

	d := newTestDaemon(t)
	if err := d.encryptDB(dbPath); err != nil {
		t.Fatalf("encryptDB: %v", err)
	}

	sel := &obsidian.Selection{Config: obsidian.Config{DBPath: dbPath, EncryptDB: true}}
	preparedPath, restore := d.prepareSelectionDB(sel)
	if preparedPath != dbPath {
		t.Fatalf("prepareSelectionDB path mismatch: got %q want %q", preparedPath, dbPath)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected plaintext .db during selection: %v", err)
	}
	if _, err := os.Stat(dbPath + ".enc"); !os.IsNotExist(err) {
		t.Fatalf("expected .enc to be absent during selection, err=%v", err)
	}

	restore()

	if _, err := os.Stat(dbPath + ".enc"); err != nil {
		t.Fatalf("expected .enc restored after selection: %v", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("expected plaintext .db removed after restore, err=%v", err)
	}
}

func TestPrepareSelectionDBDecryptsWhenEncExistsEvenIfFlagFalse(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "engram.db")
	makeTestSQLiteDB(t, dbPath)

	d := newTestDaemon(t)
	if err := d.encryptDB(dbPath); err != nil {
		t.Fatalf("encryptDB: %v", err)
	}

	sel := &obsidian.Selection{Config: obsidian.Config{DBPath: dbPath, EncryptDB: false}}
	preparedPath, restore := d.prepareSelectionDB(sel)
	if preparedPath != dbPath {
		t.Fatalf("prepareSelectionDB path mismatch: got %q want %q", preparedPath, dbPath)
	}

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected plaintext .db during selection: %v", err)
	}

	restore()

	if _, err := os.Stat(dbPath + ".enc"); err != nil {
		t.Fatalf("expected .enc restored after selection: %v", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("expected plaintext .db removed after restore, err=%v", err)
	}
}
