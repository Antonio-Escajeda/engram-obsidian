package tui

import (
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
)

func TestConfigConfirmPersistsVaultLock(t *testing.T) {
	sel := &obsidian.Selection{Version: 1, Selected: map[string]obsidian.ProjectSelection{}}
	m := New(sel, nil)
	m.ConfigFocus = 4
	m.VaultLock = "disabled"

	updatedModel, _ := m.handleConfigKey("enter")
	updated := updatedModel.(Model)
	if updated.VaultLock != "strict" {
		t.Fatalf("expected enter on vault lock to toggle strict, got %q", updated.VaultLock)
	}

	updated.ConfigFocus = 5
	updated.VaultInput.SetValue("/mnt/c/Users/test/Vault")
	updated.DBInput.SetValue("~/.engram/engram.db")
	confirmedModel, _ := updated.handleConfigKey("enter")
	confirmed := confirmedModel.(Model)

	if confirmed.Selection.Config.VaultLock != "strict" {
		t.Fatalf("expected strict vault lock in persisted config, got %q", confirmed.Selection.Config.VaultLock)
	}
}
