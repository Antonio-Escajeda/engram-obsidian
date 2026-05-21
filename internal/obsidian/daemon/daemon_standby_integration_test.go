//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

// TestDaemonStandbyToActive verifies the standby → active → standby state
// transition logic used in Run()'s poll loop.
//
// The transition is driven by SyncConditionState.Met(); this test exercises the
// daemon-level invariants that must hold at each phase boundary:
//
//  1. STANDBY  — conditions not met, vault empty: vaultHasContent is false,
//     cleanup returns true (can execute) but produces no error.
//
//  2. ACTIVE   — conditions met, doSync writes content: vaultHasContent becomes
//     true after a successful export, vault directory contains _engram.
//
//  3. STANDBY  — conditions no longer met: cleanup removes _engram and
//     vaultHasContent returns false again.
//
// This test does NOT call Run() because the ticker loop requires real OS-level
// process detection (ObsidianRunning / RootSessionActive) that cannot be faked
// in a unit context.  Instead it exercises the three methods that Run() delegates
// to — vaultHasContent, doSync, cleanup — which together form the complete
// standby↔active contract.
func TestDaemonStandbyToActive(t *testing.T) {
	// ── setup ────────────────────────────────────────────────────────────────
	vaultPath := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "engram.db")

	// Build a minimal SQLite DB so doSync has something to export.
	makeTestSQLiteDB(t, dbPath)

	selectionPath := filepath.Join(t.TempDir(), "obsidian-selection.json")
	sel := &obsidian.Selection{
		Version:   1,
		Confirmed: true,
		Config: obsidian.Config{
			VaultPath: vaultPath,
			DBPath:    dbPath,
		},
		Selected: map[string]obsidian.ProjectSelection{},
	}
	if err := sel.Save(selectionPath); err != nil {
		t.Fatalf("save selection: %v", err)
	}

	logs := make([]string, 0, 16)
	d := New(Config{
		SelectionPath: selectionPath,
		Logf: func(format string, args ...any) {
			logs = append(logs, format)
		},
	})

	engramRoot := filepath.Join(vaultPath, "_engram")

	// ── phase 1: STANDBY — vault is empty ────────────────────────────────────
	if d.vaultHasContent() {
		t.Fatal("phase 1: vaultHasContent should be false before any sync")
	}

	// cleanup on an empty vault is a valid no-op but returns true (selection has config).
	if ok := d.cleanup(); !ok {
		t.Fatal("phase 1: cleanup on empty vault should still return true (selection has config)")
	}
	if _, err := os.Stat(engramRoot); !os.IsNotExist(err) {
		t.Fatalf("phase 1: _engram should not exist after cleanup on empty vault, err=%v", err)
	}

	// ── phase 2: ACTIVE — write content directly via exporter ────────────────
	// We use obsidian.NewExporter directly rather than calling doSync because
	// doSync calls ObsidianRunning which requires tasklist.exe (not available in CI).
	// This mirrors exactly what doSync does after the gate check passes.
	project := "integration-test"
	obsType := "decision"
	obs := store.Observation{
		ID:      1,
		Project: &project,
		Type:    obsType,
		Title:   "standby to active test observation",
		Content: "body",
	}
	exporter := obsidian.NewExporter(vaultPath, "star", func(string, ...any) {})
	result, err := exporter.Export(&store.ExportData{Observations: []store.Observation{obs}}, sel.Filter)
	if err != nil {
		t.Fatalf("phase 2: exporter.Export: %v", err)
	}
	if result.Created+result.Updated == 0 {
		t.Fatal("phase 2: expected at least one file written during sync")
	}

	if !d.vaultHasContent() {
		t.Fatal("phase 2: vaultHasContent should be true after sync")
	}
	if _, err := os.Stat(engramRoot); err != nil {
		t.Fatalf("phase 2: _engram should exist after sync: %v", err)
	}

	// ── phase 3: back to STANDBY — cleanup must remove vault content ──────────
	if ok := d.cleanup(); !ok {
		t.Fatal("phase 3: cleanup should succeed when selection has config")
	}

	if d.vaultHasContent() {
		t.Fatal("phase 3: vaultHasContent should be false after cleanup")
	}
	if _, err := os.Stat(engramRoot); !os.IsNotExist(err) {
		t.Fatalf("phase 3: _engram should be removed after cleanup, err=%v", err)
	}

	// Verify cleanup did not log any hard errors (WARN is acceptable if cleanup
	// encountered a benign issue, but the test should not see "WARN cleanup: ").
	for _, line := range logs {
		if strings.Contains(line, "WARN cleanup:") {
			t.Errorf("phase 3: unexpected cleanup warning: %s", line)
		}
	}
}
