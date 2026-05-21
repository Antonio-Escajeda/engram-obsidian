package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

func testSelectionPath(t *testing.T, vaultPath string) string {
	t.Helper()
	sel := &obsidian.Selection{
		Version: 1,
		Config: obsidian.Config{
			VaultPath: vaultPath,
			DBPath:    filepath.Join(vaultPath, "engram.db"),
		},
		Selected: map[string]obsidian.ProjectSelection{},
	}
	selectionPath := filepath.Join(t.TempDir(), "obsidian-selection.json")
	if err := sel.Save(selectionPath); err != nil {
		t.Fatalf("save selection: %v", err)
	}
	return selectionPath
}

func writeEngramContent(t *testing.T, vaultPath string) {
	t.Helper()
	project := "demo"
	obsType := "decision"
	obs := store.Observation{
		ID:        1,
		Project:   &project,
		Type:      obsType,
		Title:     "seed",
		Content:   "seed",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	exporter := obsidian.NewExporter(vaultPath, "star", func(string, ...any) {})
	_, err := exporter.Export(&store.ExportData{Observations: []store.Observation{obs}}, nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
}

func TestVaultPolicyConditionsMetCreatesContent(t *testing.T) {
	vaultPath := t.TempDir()
	if got := (SyncConditionState{ObsidianRunning: true, RootSession: true}).Met(); !got {
		t.Fatal("expected condition double to be met")
	}

	writeEngramContent(t, vaultPath)

	engramRoot := filepath.Join(vaultPath, "_engram")
	if _, err := os.Stat(engramRoot); err != nil {
		t.Fatalf("_engram should exist when conditions are met: %v", err)
	}
}

func TestVaultPolicyAnyFalseCleansEngram(t *testing.T) {
	vaultPath := t.TempDir()
	writeEngramContent(t, vaultPath)

	d := New(Config{
		SelectionPath: testSelectionPath(t, vaultPath),
		Logf:          func(string, ...any) {},
	})

	if got := (SyncConditionState{ObsidianRunning: true, RootSession: false}).Met(); got {
		t.Fatal("expected condition to be false when root session is missing")
	}

	if ok := d.cleanup(); !ok {
		t.Fatal("cleanup should run when selection has config")
	}

	engramRoot := filepath.Join(vaultPath, "_engram")
	if _, err := os.Stat(engramRoot); !os.IsNotExist(err) {
		t.Fatalf("_engram should be removed when condition is not met, err=%v", err)
	}
}

func TestVaultPolicyCleanupIsIdempotent(t *testing.T) {
	vaultPath := t.TempDir()
	writeEngramContent(t, vaultPath)

	d := New(Config{
		SelectionPath: testSelectionPath(t, vaultPath),
		Logf:          func(string, ...any) {},
	})

	if ok := d.cleanup(); !ok {
		t.Fatal("first cleanup should run")
	}
	if ok := d.cleanup(); !ok {
		t.Fatal("second cleanup should be idempotent and run")
	}

	engramRoot := filepath.Join(vaultPath, "_engram")
	if _, err := os.Stat(engramRoot); !os.IsNotExist(err) {
		t.Fatalf("_engram should not exist after repeated cleanup, err=%v", err)
	}
}

func TestBootstrapSelectionCreatesDefaultVaultWhenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	selectionPath := filepath.Join(t.TempDir(), "obsidian-selection.json")
	d := New(Config{SelectionPath: selectionPath, Logf: func(string, ...any) {}})

	sel, err := d.loadOrBootstrapSelection()
	if err != nil {
		t.Fatalf("bootstrap selection: %v", err)
	}

	if sel.Config.VaultPath == "" {
		t.Fatal("expected vault path to be auto-created")
	}
	if sel.Config.DBPath == "" {
		t.Fatal("expected db path to be auto-created")
	}

	if _, err := os.Stat(sel.Config.VaultPath); err != nil {
		t.Fatalf("expected default vault directory to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(sel.Config.DBPath)); err != nil {
		t.Fatalf("expected db parent directory to exist: %v", err)
	}
	if _, err := os.Stat(selectionPath); err != nil {
		t.Fatalf("expected selection file to be created: %v", err)
	}

	expectedSuffix := filepath.Join("Documents", "EngramVault")
	if !strings.HasSuffix(sel.Config.VaultPath, expectedSuffix) {
		t.Fatalf("expected default vault under Documents, got %q", sel.Config.VaultPath)
	}
}

func TestBootstrapSelectionReplacesInvalidVaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	selectionPath := testSelectionPath(t, filepath.Join(t.TempDir(), "missing-vault"))
	d := New(Config{SelectionPath: selectionPath, Logf: func(string, ...any) {}})

	sel, err := d.loadOrBootstrapSelection()
	if err != nil {
		t.Fatalf("bootstrap selection with invalid vault: %v", err)
	}

	if got := sel.Config.VaultPath; !strings.HasSuffix(got, filepath.Join("Documents", "EngramVault")) {
		t.Fatalf("expected invalid vault to be replaced with default, got %q", got)
	}
	if _, err := os.Stat(sel.Config.VaultPath); err != nil {
		t.Fatalf("expected replacement vault directory to exist: %v", err)
	}
}

func TestDoSyncSkipsPopulationOnWSLWhenGateIsNotMet(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")

	vaultPath := t.TempDir()
	logs := make([]string, 0, 4)
	d := New(Config{
		Process: ProcessConfig{TasklistPath: "/path/that/does/not/exist/tasklist.exe"},
		Logf: func(format string, args ...any) {
			logs = append(logs, fmt.Sprintf(format, args...))
		},
	})

	sel := &obsidian.Selection{
		Version: 1,
		Config: obsidian.Config{
			VaultPath: vaultPath,
			DBPath:    filepath.Join(vaultPath, "engram.db"),
		},
		Selected: map[string]obsidian.ProjectSelection{},
	}

	synced, err := d.doSync(sel)
	if err != nil {
		t.Fatalf("doSync should skip without error when gate fails: %v", err)
	}
	if synced {
		t.Fatal("expected doSync to skip vault population when gate is not met")
	}

	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "Vault population gate: skip export on WSL") {
		t.Fatalf("expected gate skip log, got: %s", joined)
	}
}
