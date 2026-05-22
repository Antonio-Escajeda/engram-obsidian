package obsidian

import (
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

func TestCanonicalExportTitleAlreadyPrefixed(t *testing.T) {
	obs := store.Observation{
		ID:        9,
		Type:      "bugfix",
		Title:     "2026-05-21 [bugfix] - Fix boleta salida",
		CreatedAt: "2026-05-21T10:00:00Z",
	}

	got := CanonicalExportTitle(obs)
	if got != obs.Title {
		t.Fatalf("expected title unchanged, got %q", got)
	}
}

func TestCanonicalExportTitlePrefixesWhenMissing(t *testing.T) {
	obs := store.Observation{
		ID:        389,
		Type:      "bugfix",
		Title:     "Bug: factura 10109 sin valor comercial",
		CreatedAt: "2026-05-22T16:00:00Z",
	}

	got := CanonicalExportTitle(obs)
	want := "2026-05-22 [bugfix] - Bug: factura 10109 sin valor comercial"
	if got != want {
		t.Fatalf("unexpected canonical title\nwant: %q\n got: %q", want, got)
	}
}

func TestCanonicalExportTitleFallbacks(t *testing.T) {
	obs := store.Observation{ID: 42}

	got := CanonicalExportTitle(obs)
	want := "0000-00-00 [manual] - observation-42"
	if got != want {
		t.Fatalf("unexpected fallback title\nwant: %q\n got: %q", want, got)
	}
}
