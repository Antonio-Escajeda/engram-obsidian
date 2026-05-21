package obsidian

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSelectionLegacyFileMarksConfirmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")
	legacy := `{
  "version": 1,
  "config": {
    "vault_path": "/tmp/vault",
    "db_path": "/tmp/engram.db"
  },
  "selected": {}
}`
	if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
		t.Fatalf("write legacy selection: %v", err)
	}

	sel, err := LoadSelection(path)
	if err != nil {
		t.Fatalf("load legacy selection: %v", err)
	}

	if !sel.IsConfirmed() {
		t.Fatal("expected legacy selection file to be treated as confirmed")
	}
}

func TestLoadSelectionMissingFileStartsUnconfirmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-selection.json")
	sel, err := LoadSelection(path)
	if err != nil {
		t.Fatalf("load missing selection: %v", err)
	}

	if sel.IsConfirmed() {
		t.Fatal("expected missing selection bootstrap state to be unconfirmed")
	}
}

func TestVaultLockDefaultAndStrictPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selection.json")

	sel := &Selection{Version: 1, Selected: map[string]ProjectSelection{}}
	if err := sel.Save(path); err != nil {
		t.Fatalf("save selection: %v", err)
	}

	loaded, err := LoadSelection(path)
	if err != nil {
		t.Fatalf("load selection: %v", err)
	}
	if got := loaded.Config.VaultLockModeOrDefault(); got != "disabled" {
		t.Fatalf("expected default vault lock disabled, got %q", got)
	}

	loaded.Config.VaultLock = "strict"
	if err := loaded.Save(path); err != nil {
		t.Fatalf("save strict selection: %v", err)
	}

	reloaded, err := LoadSelection(path)
	if err != nil {
		t.Fatalf("reload selection: %v", err)
	}
	if got := reloaded.Config.VaultLockModeOrDefault(); got != "strict" {
		t.Fatalf("expected strict persisted mode, got %q", got)
	}
}
