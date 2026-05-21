package obsidian

import "testing"

func TestWindowsProfileToVaultPathConvertsWindowsPath(t *testing.T) {
	got := windowsProfileToVaultPath(`C:\Users\Alice`)
	want := "/mnt/c/Users/Alice/Documents/EngramVault"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestWindowsProfileToVaultPathRejectsLinuxPath(t *testing.T) {
	if got := windowsProfileToVaultPath("/home/alice"); got != "" {
		t.Fatalf("expected empty conversion for linux path, got %q", got)
	}
}
