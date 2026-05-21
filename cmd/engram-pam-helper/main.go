//go:build linux

// engram-pam-helper is a small binary invoked by PAM hooks to integrate
// engram-obsidian's keyslot management with the Linux login flow.
//
// Usage:
//
//	engram-pam-helper session    — reads password from stdin, opens keyslot, stores masterKey in keyring
//	engram-pam-helper password   — reads new password from stdin, re-wraps keyslot with it
//
// All error paths exit 0 (non-fatal to PAM) except chauthtok with empty keyring (exit 1).
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/crypto"
)

func main() {
	// Suppress timestamp; prefix all log output for syslog clarity.
	log.SetFlags(0)
	log.SetPrefix("engram-pam-helper: ")

	if len(os.Args) < 2 {
		os.Exit(0)
	}

	switch os.Args[1] {
	case "session":
		runSession()
	case "password":
		runChauthtok()
	default:
		os.Exit(0)
	}
}

// runSession is the PAM session hook.
// It reads the user's login password from stdin, opens the keyslot, and stores
// the master key in the kernel keyring so engram-obsidian can access it.
func runSession() {
	// 1. Read password from stdin.
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Printf("session: read stdin: %v", err)
		os.Exit(0)
	}
	password := strings.TrimSpace(string(raw))

	// 2. Empty password → non-interactive context (e.g. sudo, cron). Exit silently.
	if password == "" {
		os.Exit(0)
	}

	// 3. Load keyslots from disk.
	ksPath := keyslotPath()
	ks, err := crypto.LoadKeySlots(ksPath)
	if err != nil {
		log.Printf("session: load keyslots %q: %v", ksPath, err)
		os.Exit(0)
	}

	// 4. Derive masterKey from password slot.
	masterKey, err := ks.OpenPasswordSlot([]byte(password))
	if err != nil {
		// EC-07: wrong password MUST NOT break login — exit 0.
		log.Printf("session: wrong password or corrupt slot")
		os.Exit(0)
	}

	// 5. Store masterKey in the kernel keyring.
	if err := crypto.StoreKeyInKeyring(masterKey); err != nil {
		log.Printf("session: store key in keyring: %v", err)
		os.Exit(0)
	}

	os.Exit(0)
}

// runChauthtok is the PAM password-change hook.
// It reads the new password from stdin and re-wraps the keyslot with it,
// preserving the masterKey already loaded in the kernel keyring.
func runChauthtok() {
	// 1. Read new password from stdin.
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Printf("chauthtok: read stdin: %v", err)
		os.Exit(0)
	}
	newPassword := strings.TrimSpace(string(raw))

	// 2. Empty password → nothing to do.
	if newPassword == "" {
		os.Exit(0)
	}

	// 3. Load masterKey from keyring — must be present (user just logged in).
	masterKey, err := crypto.LoadMasterFromKeyring()
	if err != nil {
		if errors.Is(err, crypto.ErrLocked) {
			// Keyring empty means the session hook never ran (e.g. graphical login
			// that didn't go through PAM session, or this is a passwd(1) call
			// outside of a logged-in session). The user must recover manually.
			fmt.Fprintln(os.Stderr, "chauthtok: keyring empty, cannot re-wrap slot; run engram-obsidian recover after login")
			os.Exit(1)
		}
		log.Printf("chauthtok: load master key: %v", err)
		os.Exit(0)
	}

	// 4. Load keyslots — if not set up yet, skip silently.
	ksPath := keyslotPath()
	ks, err := crypto.LoadKeySlots(ksPath)
	if err != nil {
		log.Printf("chauthtok: load keyslots %q: %v — skipping re-wrap", ksPath, err)
		os.Exit(0)
	}

	// 5. Re-wrap slot 0 with the new password.
	if err := ks.SealPasswordSlot(masterKey, []byte(newPassword)); err != nil {
		log.Printf("chauthtok: seal password slot: %v", err)
		os.Exit(0)
	}

	// 6. Atomically persist updated keyslots.
	if err := ks.Save(ksPath); err != nil {
		log.Printf("chauthtok: save keyslots: %v", err)
		os.Exit(0)
	}

	os.Exit(0)
}

// keyslotPath returns the default path for keyslots.json (~/.engram/keyslots.json).
func keyslotPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".engram", "keyslots.json")
}
