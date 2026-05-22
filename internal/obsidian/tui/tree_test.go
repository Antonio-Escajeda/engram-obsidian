package tui

import (
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

func TestBuildTreeUnionsDBAndSelectionProjects(t *testing.T) {
	sel := &obsidian.Selection{
		Version: 1,
		Selected: map[string]obsidian.ProjectSelection{
			"selected-only": {Mode: obsidian.SelectionFull},
		},
	}

	obs := []store.Observation{
		{ID: 1, Project: strPtr("obs-only"), CreatedAt: "2026-05-21T10:00:00Z", Title: "one", Type: "note"},
	}

	roots := BuildTree(obs, sel, []string{"db-only"})
	if len(roots) != 3 {
		t.Fatalf("expected 3 project roots, got %d", len(roots))
	}

	if roots[0].Key != "db-only" || roots[1].Key != "obs-only" || roots[2].Key != "selected-only" {
		t.Fatalf("unexpected sorted project keys: %q, %q, %q", roots[0].Key, roots[1].Key, roots[2].Key)
	}
}

func strPtr(s string) *string { return &s }

func TestToSelectionPreservesZeroChildrenSelectedProject(t *testing.T) {
	current := &obsidian.Selection{
		Version: 1,
		Selected: map[string]obsidian.ProjectSelection{
			"selected-only": {Mode: obsidian.SelectionFull},
		},
	}

	roots := BuildTree(nil, current, nil)
	out := ToSelection(roots, current)

	if _, ok := out.Selected["selected-only"]; !ok {
		t.Fatal("expected selected-only project to be preserved in selection")
	}
}
