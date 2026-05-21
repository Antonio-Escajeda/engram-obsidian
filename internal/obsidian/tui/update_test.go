package tui

import (
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
)

func TestConfigConfirmForcesVaultLockDisabled(t *testing.T) {
	sel := &obsidian.Selection{Version: 1, Selected: map[string]obsidian.ProjectSelection{}}
	m := New(sel, nil)
	m.ConfigFocus = 4
	m.VaultLock = "strict"
	m.VaultInput.SetValue("/mnt/c/Users/test/Vault")
	m.DBInput.SetValue("~/.engram/engram.db")

	confirmedModel, _ := m.handleConfigKey("enter")
	confirmed := confirmedModel.(Model)

	if confirmed.Selection.Config.VaultLock != "disabled" {
		t.Fatalf("expected disabled vault lock in persisted config, got %q", confirmed.Selection.Config.VaultLock)
	}
}
