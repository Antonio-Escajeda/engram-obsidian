package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// browseWindowsFolder abre el selector de carpetas nativo de Windows vía PowerShell
// y retorna el path convertido al formato WSL (/mnt/c/...).
// Solo funciona en WSL; en otros entornos retorna error.
func browseWindowsFolder() (string, error) {
	psScript := `Add-Type -AssemblyName System.Windows.Forms; $f = New-Object System.Windows.Forms.FolderBrowserDialog; $f.Description = 'Select Obsidian Vault folder'; if ($f.ShowDialog() -eq 'OK') { $f.SelectedPath }`

	// Buscar powershell.exe en los montajes WSL típicos
	psPath, err := findPowerShell()
	if err != nil {
		return "", fmt.Errorf("powershell.exe no encontrado: %w", err)
	}

	cmd := exec.Command(psPath, "-NoProfile", "-NonInteractive", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("error ejecutando PowerShell: %w", err)
	}

	winPath := strings.TrimSpace(string(out))
	if winPath == "" {
		return "", fmt.Errorf("no se seleccionó ninguna carpeta")
	}

	return windowsToWSLPath(winPath), nil
}

// findPowerShell busca powershell.exe en los montajes WSL estándar.
func findPowerShell() (string, error) {
	candidates := []string{
		"/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe",
		"/mnt/c/Windows/SysWOW64/WindowsPowerShell/v1.0/powershell.exe",
		"/mnt/d/Windows/System32/WindowsPowerShell/v1.0/powershell.exe",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("ningún candidato encontrado en %v", candidates)
}

// windowsToWSLPath convierte un path Windows (C:\Users\foo\bar) al formato WSL (/mnt/c/Users/foo/bar).
func windowsToWSLPath(winPath string) string {
	// Normalizar separadores
	normalized := strings.ReplaceAll(winPath, "\\", "/")

	// Manejar letra de unidad: "C:/..." → "/mnt/c/..."
	if len(normalized) >= 2 && normalized[1] == ':' {
		drive := strings.ToLower(string(normalized[0]))
		rest := normalized[2:]
		rest = strings.TrimPrefix(rest, "/")
		return "/mnt/" + drive + "/" + rest
	}

	return normalized
}

// vaultHasEngramDir retorna true si el vault ya tiene un subdirectorio _engram/,
// lo que indica que es un vault existente ya sincronizado.
func vaultHasEngramDir(vaultPath string) bool {
	engramDir := filepath.Join(vaultPath, "_engram")
	info, err := os.Stat(engramDir)
	return err == nil && info.IsDir()
}
