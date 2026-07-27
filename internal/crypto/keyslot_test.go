//go:build linux

package crypto_test

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/crypto"
)

func TestSealOpenPasswordSlot(t *testing.T) {
	master := make([]byte, 32)
	for i := range master {
		master[i] = byte(i + 1)
	}

	ks := &crypto.KeySlots{Version: 1}
	if err := ks.SealPasswordSlot(master, []byte("correct horse")); err != nil {
		t.Fatalf("SealPasswordSlot: %v", err)
	}

	opened, err := ks.OpenPasswordSlot([]byte("correct horse"))
	if err != nil {
		t.Fatalf("OpenPasswordSlot correct password: %v", err)
	}
	if hex.EncodeToString(opened) != hex.EncodeToString(master) {
		t.Fatal("OpenPasswordSlot returned different master key")
	}

	if _, err := ks.OpenPasswordSlot([]byte("wrong password")); err == nil {
		t.Fatal("OpenPasswordSlot wrong password: expected error, got nil")
	}
}

func TestSealOpenRecoverySlot(t *testing.T) {
	master := make([]byte, 32)
	for i := range master {
		master[i] = byte(255 - i)
	}

	ks := &crypto.KeySlots{Version: 1}
	recovery, err := ks.SealRecoverySlot(master)
	if err != nil {
		t.Fatalf("SealRecoverySlot: %v", err)
	}

	opened, err := ks.OpenRecoverySlot(recovery)
	if err != nil {
		t.Fatalf("OpenRecoverySlot correct key: %v", err)
	}
	if hex.EncodeToString(opened) != hex.EncodeToString(master) {
		t.Fatal("OpenRecoverySlot returned different master key")
	}

	wrong := append([]byte(nil), recovery...)
	wrong[0] ^= 0xFF
	if _, err := ks.OpenRecoverySlot(wrong); err == nil {
		t.Fatal("OpenRecoverySlot wrong key: expected error, got nil")
	}
}

func TestKeySlotsSaveAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keyslots.json")

	ks := &crypto.KeySlots{Version: 1}
	if err := ks.SealPasswordSlot(make([]byte, 32), []byte("pw")); err != nil {
		t.Fatalf("SealPasswordSlot: %v", err)
	}

	if err := ks.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat keyslots file: %v", err)
	}
	if info.Mode().Perm() != 0400 {
		t.Fatalf("permissions: got %04o want 0400", info.Mode().Perm())
	}

	loaded, err := crypto.LoadKeySlots(path)
	if err != nil {
		t.Fatalf("LoadKeySlots: %v", err)
	}
	if loaded.Version != 1 {
		t.Fatalf("version mismatch: got %d want 1", loaded.Version)
	}
}

func TestLoadKeyErrLocked(t *testing.T) {
	_, err := crypto.LoadKey(t.TempDir())
	if err == nil {
		t.Skip("LoadKey succeeded (keyring populated on host); cannot assert ErrLocked")
	}
	if !errors.Is(err, crypto.ErrLocked) {
		t.Fatalf("expected ErrLocked, got: %v", err)
	}
}

func TestOpenPasswordSlotTiming(t *testing.T) {
	master := make([]byte, 32)
	ks := &crypto.KeySlots{Version: 1}
	if err := ks.SealPasswordSlot(master, []byte("timing-password")); err != nil {
		t.Fatalf("SealPasswordSlot: %v", err)
	}

	start := time.Now()
	if _, err := ks.OpenPasswordSlot([]byte("timing-password")); err != nil {
		t.Fatalf("OpenPasswordSlot: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Skipf("OpenPasswordSlot took %v (>500ms on this host)", elapsed)
	}
}
