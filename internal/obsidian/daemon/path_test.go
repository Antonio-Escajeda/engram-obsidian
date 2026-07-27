package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHomePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}
	t.Setenv("ENGRAM_DATA_DIR", "")

	tests := []struct {
		name string
		in   string
		out  string
	}{
		{name: "absolute", in: "/tmp/engram.db", out: "/tmp/engram.db"},
		{name: "home-tilde", in: "~/.engram/engram.db", out: filepath.Join(home, ".engram", "engram.db")},
		{name: "dot-relative", in: "./engram/engram.db", out: filepath.Join(home, ".engram", "engram", "engram.db")},
		{name: "relative", in: "engram/engram.db", out: filepath.Join(home, ".engram", "engram", "engram.db")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := expandHomePath(tc.in)
			if got != tc.out {
				t.Fatalf("expandHomePath(%q) = %q, want %q", tc.in, got, tc.out)
			}
		})
	}
}

func TestExpandHomePath_WithEngramDataDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}

	custom := filepath.Join(home, "custom-engram-data")
	t.Setenv("ENGRAM_DATA_DIR", custom)

	got := expandHomePath("engram/engram.db")
	want := filepath.Join(custom, "engram", "engram.db")
	if got != want {
		t.Fatalf("expandHomePath with ENGRAM_DATA_DIR = %q, want %q", got, want)
	}
}
