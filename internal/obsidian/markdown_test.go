package obsidian

import (
	"strings"
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

func TestObservationPathUsesCanonicalTitle(t *testing.T) {
	project := "terminal-wms-oma"
	obs := store.Observation{
		ID:        389,
		Type:      "bugfix",
		Title:     "Bug: factura 10109 sin valor comercial",
		Project:   &project,
		CreatedAt: "2026-05-22T16:00:00Z",
	}

	got := ObservationPath("_engram", obs)
	if !strings.Contains(got, "2026-05-22-bugfix-bug-factura-10109-sin-valor-comercial-389") {
		t.Fatalf("expected canonical slug in path, got %q", got)
	}
}

func TestObservationToMarkdownUsesCanonicalHeadingAndAlias(t *testing.T) {
	project := "terminal-wms-oma"
	obs := store.Observation{
		ID:            389,
		Type:          "bugfix",
		Title:         "Bug: factura 10109 sin valor comercial",
		Project:       &project,
		Scope:         "project",
		SessionID:     "manual-save-terminal-wms-oma",
		RevisionCount: 1,
		CreatedAt:     "2026-05-22T16:00:00Z",
		UpdatedAt:     "2026-05-22T16:05:00Z",
		Content:       "contenido",
	}

	md := ObservationToMarkdown(obs, "", "_engram", nil)
	expected := "2026-05-22 [bugfix] - Bug: factura 10109 sin valor comercial"
	if !strings.Contains(md, "  - \""+expected+"\"") {
		t.Fatalf("expected canonical alias in markdown\n%s", md)
	}
	if !strings.Contains(md, "# "+expected) {
		t.Fatalf("expected canonical heading in markdown\n%s", md)
	}
}
