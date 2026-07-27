//go:build linux

package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/crypto"
	"golang.org/x/term"
)

// runSetupKeys initializes or migrates the PAM key slot file at ~/.engram/keyslots.json.
// If a legacy .engram-key file is present it migrates the masterKey from there (via the
// live keyring when the daemon is running, or via the encrypted file directly).
// If no legacy key exists it generates a fresh 32-byte masterKey.
// It then seals slot 0 (password) and slot 1 (recovery), prints the recovery key once,
// and requires the user to confirm they saved it before writing the file.
func runSetupKeys() error {
	home, _ := os.UserHomeDir()
	dbDir := filepath.Join(home, ".engram")
	keyslotPath := filepath.Join(dbDir, "keyslots.json")
	oldKeyPath := filepath.Join(dbDir, ".engram-key")

	// Determine masterKey: migrate from legacy system or generate fresh.
	var masterKey []byte
	if _, err := os.Stat(oldKeyPath); err == nil {
		// Legacy .engram-key file present — prefer live keyring first (daemon running).
		mk, err := crypto.LoadMasterFromKeyring()
		if err == nil {
			masterKey = mk
			fmt.Println("Migration: loaded masterKey from live keyring.")
		} else {
			// Keyring empty — try loading directly from the encrypted key file.
			mk, err = crypto.MigrateLegacyKey(dbDir)
			if err == nil {
				masterKey = mk
				fmt.Println("Migration: loaded masterKey from legacy .engram-key file.")
			} else {
				fmt.Fprintln(os.Stderr, "Cannot migrate automatically: keyring is empty and .engram-key decryption failed.")
				fmt.Fprintln(os.Stderr, "Start the daemon first (su -) to populate the keyring, then re-run setup-keys.")
				return fmt.Errorf("migration requires live keyring or readable .engram-key: %w", err)
			}
		}
	} else {
		// Fresh setup — generate a new random 32-byte masterKey.
		masterKey = make([]byte, 32)
		if _, err := rand.Read(masterKey); err != nil {
			return fmt.Errorf("generate masterKey: %w", err)
		}
		fmt.Println("No legacy key found — generating fresh masterKey.")
	}

	// Prompt for password (twice).
	password, err := promptPassword("Enter new unlock password: ")
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	confirm, err := promptPassword("Confirm password: ")
	if err != nil {
		return fmt.Errorf("read password confirmation: %w", err)
	}
	if string(password) != string(confirm) {
		return fmt.Errorf("passwords do not match")
	}
	if len(password) == 0 {
		return fmt.Errorf("password must not be empty")
	}

	// Build key slots.
	ks := &crypto.KeySlots{Version: 1}

	if err := ks.SealPasswordSlot(masterKey, password); err != nil {
		return fmt.Errorf("seal password slot: %w", err)
	}

	recoveryKey, err := ks.SealRecoverySlot(masterKey)
	if err != nil {
		return fmt.Errorf("seal recovery slot: %w", err)
	}

	// Display recovery key and require explicit acknowledgment.
	fmt.Printf("\n*** RECOVERY KEY (save this NOW — it will NOT be shown again) ***\n")
	fmt.Printf("%s\n\n", hex.EncodeToString(recoveryKey))

	fmt.Print("Type SAVED to continue: ")
	reader := bufio.NewReader(os.Stdin)
	ack, _ := reader.ReadString('\n')
	ack = strings.TrimSpace(ack)
	if ack != "SAVED" {
		return fmt.Errorf("aborted: recovery key not acknowledged")
	}

	// Write key slots atomically.
	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return fmt.Errorf("create dbDir: %w", err)
	}
	if err := ks.Save(keyslotPath); err != nil {
		return fmt.Errorf("save keyslots: %w", err)
	}

	// Migration cleanup: zero and remove the legacy key file.
	if _, err := os.Stat(oldKeyPath); err == nil {
		zeros := make([]byte, 65)
		_ = os.WriteFile(oldKeyPath, zeros, 0400)
		_ = os.Remove(oldKeyPath)
		fmt.Println("Legacy .engram-key removed.")
	}

	fmt.Println("Setup complete. Run su - (or restart the daemon) to unlock on next login.")
	return nil
}

// runRecover replaces the password in slot 0 using a recovery key, then populates
// the live keyring so the daemon can unlock immediately on the next sync tick.
func runRecover() error {
	home, _ := os.UserHomeDir()
	dbDir := filepath.Join(home, ".engram")
	keyslotPath := filepath.Join(dbDir, "keyslots.json")

	// Prompt for the recovery key (hex).
	fmt.Print("Enter recovery key (hex): ")
	reader := bufio.NewReader(os.Stdin)
	hexInput, _ := reader.ReadString('\n')
	hexInput = strings.TrimSpace(hexInput)

	recoveryKeyBytes, err := hex.DecodeString(hexInput)
	if err != nil {
		return fmt.Errorf("invalid recovery key (not valid hex): %w", err)
	}

	// Load key slots from disk.
	ks, err := crypto.LoadKeySlots(keyslotPath)
	if err != nil {
		return fmt.Errorf("load keyslots: %w", err)
	}

	// Decrypt masterKey via the recovery slot.
	masterKey, err := ks.OpenRecoverySlot(recoveryKeyBytes)
	if err != nil {
		return fmt.Errorf("invalid recovery key")
	}

	// Prompt for new password (twice).
	newPassword, err := promptPassword("Enter new unlock password: ")
	if err != nil {
		return fmt.Errorf("read new password: %w", err)
	}
	confirm, err := promptPassword("Confirm new password: ")
	if err != nil {
		return fmt.Errorf("read password confirmation: %w", err)
	}
	if string(newPassword) != string(confirm) {
		return fmt.Errorf("passwords do not match")
	}
	if len(newPassword) == 0 {
		return fmt.Errorf("password must not be empty")
	}

	// Re-seal slot 0 with the new password (recovery slot untouched).
	if err := ks.SealPasswordSlot(masterKey, newPassword); err != nil {
		return fmt.Errorf("seal new password slot: %w", err)
	}

	// Save updated key slots atomically.
	if err := ks.Save(keyslotPath); err != nil {
		return fmt.Errorf("save keyslots: %w", err)
	}

	// Populate the live keyring so the daemon unlocks on next sync tick.
	if err := crypto.StoreKeyInKeyring(masterKey); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not store key in keyring (%v) — daemon will unlock on next su/login.\n", err)
	}

	fmt.Println("Recovery complete. Daemon will unlock on next sync tick.")
	return nil
}

// promptPassword reads a password securely (no echo) from stdin using golang.org/x/term.
// Falls back to plain bufio read if stdin is not a terminal (e.g. piped input in tests).
func promptPassword(prompt string) ([]byte, error) {
	fmt.Print(prompt)
	fd := int(syscall.Stdin)
	if term.IsTerminal(fd) {
		pw, err := term.ReadPassword(fd)
		fmt.Println() // newline after hidden input
		return pw, err
	}
	// Non-terminal fallback (pipes, scripts).
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(line, "\r\n")), nil
}
