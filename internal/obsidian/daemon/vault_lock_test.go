package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
)

func TestVaultLockHelpersReadonlyRoundtrip(t *testing.T) {
	root := filepath.Join(t.TempDir(), "_engram")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	file := filepath.Join(root, "note.md")
	if err := os.WriteFile(file, []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := lockReadonlyTree(root); err != nil {
		t.Fatalf("lock readonly: %v", err)
	}
	if info, err := os.Stat(file); err != nil {
		t.Fatalf("stat locked file: %v", err)
	} else if info.Mode().Perm() != 0444 {
		t.Fatalf("expected locked file mode 0444, got %#o", info.Mode().Perm())
	}

	if err := unlockReadonlyTree(root); err != nil {
		t.Fatalf("unlock readonly: %v", err)
	}
	if info, err := os.Stat(file); err != nil {
		t.Fatalf("stat unlocked file: %v", err)
	} else if info.Mode().Perm() != 0644 {
		t.Fatalf("expected unlocked file mode 0644, got %#o", info.Mode().Perm())
	}
}

func TestStrictModeAttemptsAdvancedWindowsInteropWithoutCrashing(t *testing.T) {
	d := New(Config{Logf: func(string, ...any) {}})
	sel := &obsidian.Selection{Config: obsidian.Config{VaultPath: "/mnt/c/Users/Test/Vault", VaultLock: "strict"}}

	called := 0
	orig := runCommand
	runCommand = func(name string, args ...string) error {
		called++
		return os.ErrNotExist
	}
	t.Cleanup(func() { runCommand = orig })

	d.unlockVaultForSync(sel)
	d.lockVaultAfterSync(sel)

	if called == 0 {
		t.Fatal("expected advanced interop attempts to run in strict mode")
	}
}

func TestNonStrictModeSkipsAdvancedInterop(t *testing.T) {
	d := New(Config{Logf: func(string, ...any) {}})
	sel := &obsidian.Selection{Config: obsidian.Config{VaultPath: "/mnt/c/Users/Test/Vault", VaultLock: "disabled"}}

	called := 0
	orig := runCommand
	runCommand = func(name string, args ...string) error {
		called++
		return nil
	}
	t.Cleanup(func() { runCommand = orig })

	d.unlockVaultForSync(sel)
	d.lockVaultAfterSync(sel)

	if called != 0 {
		t.Fatalf("expected no interop calls in disabled mode, got %d", called)
	}
}
