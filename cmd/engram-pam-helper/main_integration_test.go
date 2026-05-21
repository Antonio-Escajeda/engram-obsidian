//go:build linux && integration

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/crypto"
)

func TestSessionWrongPasswordExitZero(t *testing.T) {
	home := t.TempDir()
	engramDir := filepath.Join(home, ".engram")
	if err := os.MkdirAll(engramDir, 0700); err != nil {
		t.Fatalf("mkdir .engram: %v", err)
	}

	master := make([]byte, 32)
	for i := range master {
		master[i] = byte(i)
	}
	ks := &crypto.KeySlots{Version: 1}
	if err := ks.SealPasswordSlot(master, []byte("correct-password")); err != nil {
		t.Fatalf("SealPasswordSlot: %v", err)
	}
	if err := ks.Save(filepath.Join(engramDir, "keyslots.json")); err != nil {
		t.Fatalf("Save keyslots: %v", err)
	}

	cmd := exec.Command("go", "run", ".", "session")
	cmd.Dir = "/home/antonioescajeda/engram-obsidian/cmd/engram-pam-helper"
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdin = strings.NewReader("wrong-password\n")

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("expected exit 0 for wrong password, got exit code %d", exitErr.ExitCode())
		}
		t.Fatalf("run helper: %v", err)
	}
}
