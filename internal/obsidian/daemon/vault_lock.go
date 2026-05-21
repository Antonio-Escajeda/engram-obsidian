package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
)

var runCommand = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func (d *Daemon) shouldUseStrictVaultLock(sel *obsidian.Selection) bool {
	return sel != nil && sel.Config.VaultLockModeOrDefault() == "strict"
}

func (d *Daemon) unlockVaultForSync(sel *obsidian.Selection) {
	if !d.shouldUseStrictVaultLock(sel) {
		return
	}
	engramRoot := filepath.Join(sel.Config.VaultPath, "_engram")
	if err := unlockReadonlyTree(engramRoot); err != nil {
		d.cfg.Logf("WARN Vault Lock: unlock readonly best-effort failed: %v", err)
	}
	if err := windowsInteropUnlock(engramRoot); err != nil {
		d.cfg.Logf("WARN Vault Lock: advanced unlock unavailable/failed (%v); continuing with readonly unlock only", err)
	}
}

func (d *Daemon) lockVaultAfterSync(sel *obsidian.Selection) {
	if !d.shouldUseStrictVaultLock(sel) {
		return
	}
	engramRoot := filepath.Join(sel.Config.VaultPath, "_engram")
	if err := lockReadonlyTree(engramRoot); err != nil {
		d.cfg.Logf("WARN Vault Lock: readonly lock failed: %v", err)
		return
	}
	if err := windowsInteropLock(engramRoot); err != nil {
		d.cfg.Logf("WARN Vault Lock: advanced lock unavailable/failed (%v); readonly lock kept", err)
	}
}

func lockReadonlyTree(root string) error {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0555)
		if !entry.IsDir() {
			mode = 0444
		}
		if chmodErr := os.Chmod(path, mode); chmodErr != nil && !os.IsPermission(chmodErr) {
			return chmodErr
		}
		return nil
	})
}

func unlockReadonlyTree(root string) error {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0755)
		if !entry.IsDir() {
			mode = 0644
		}
		if chmodErr := os.Chmod(path, mode); chmodErr != nil && !os.IsPermission(chmodErr) {
			return chmodErr
		}
		return nil
	})
}

func windowsInteropLock(root string) error {
	winPath, ok := toWindowsPath(root)
	if !ok {
		return fmt.Errorf("path is not under /mnt/<drive>")
	}
	if err := runCommand("cmd.exe", "/C", "attrib", "+R", winPath+`\*`, "/S", "/D"); err != nil {
		return err
	}
	if err := runCommand("cmd.exe", "/C", "icacls", winPath, "/inheritance:r", "/grant:r", "Users:RX", "/T", "/C"); err != nil {
		return err
	}
	return nil
}

func windowsInteropUnlock(root string) error {
	winPath, ok := toWindowsPath(root)
	if !ok {
		return fmt.Errorf("path is not under /mnt/<drive>")
	}
	if err := runCommand("cmd.exe", "/C", "attrib", "-R", winPath+`\*`, "/S", "/D"); err != nil {
		return err
	}
	if err := runCommand("cmd.exe", "/C", "icacls", winPath, "/inheritance:e", "/grant:r", "Users:M", "/T", "/C"); err != nil {
		return err
	}
	return nil
}

func toWindowsPath(path string) (string, bool) {
	if !strings.HasPrefix(path, "/mnt/") || len(path) < 7 {
		return "", false
	}
	drive := path[5]
	if path[6] != '/' {
		return "", false
	}
	upperDrive := unicode.ToUpper(rune(drive))
	rest := strings.ReplaceAll(path[7:], "/", `\`)
	if rest == "" {
		return fmt.Sprintf("%c:\\", upperDrive), true
	}
	return fmt.Sprintf("%c:\\%s", upperDrive, rest), true
}
