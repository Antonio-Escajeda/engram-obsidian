package obsidian

import (
	"os"
	"os/exec"
	"strings"
)

// DetectWSLVaultPath intenta detectar el path del vault de Obsidian para el
// usuario Windows actual en WSL2. Usa wslvar + wslpath como método primario,
// USERPROFILE como fallback, y retorna "" si ninguno funciona.
//
// El path resultante tiene el formato /mnt/c/Users/<usuario>/Documents/EngramVault.
// Si ya existe una config guardada, NO llamar esta función — respetar lo que hay.
func DetectWSLVaultPath() string {
	// Intento 1: wslvar USERPROFILE → wslpath
	if path := detectViaWslvar(); path != "" {
		return path
	}

	// Intento 2: variable de entorno USERPROFILE (disponible en algunos setups WSL2)
	if path := detectViaEnv(); path != "" {
		return path
	}

	return ""
}

// detectViaWslvar usa wslvar para obtener USERPROFILE de Windows y wslpath
// para convertirlo al formato /mnt/c/...
func detectViaWslvar() string {
	out, err := exec.Command("wslvar", "USERPROFILE").Output()
	if err != nil {
		return ""
	}
	winProfile := strings.TrimSpace(string(out))
	if winProfile == "" {
		return ""
	}

	// wslpath convierte el path Windows → Linux
	converted, err := exec.Command("wslpath", winProfile).Output()
	if err != nil {
		return ""
	}
	linuxProfile := strings.TrimSpace(string(converted))
	if linuxProfile == "" {
		return ""
	}

	return linuxProfile + "/Documents/EngramVault"
}

// detectViaEnv lee USERPROFILE del entorno y aplica conversión manual.
// En WSL2, USERPROFILE a veces viene como C:\Users\Antonio.
func detectViaEnv() string {
	winProfile := os.Getenv("USERPROFILE")
	if winProfile == "" {
		return ""
	}

	// Solo convertir si parece un path Windows (tiene letra de unidad)
	normalized := strings.ReplaceAll(winProfile, "\\", "/")
	if len(normalized) < 2 || normalized[1] != ':' {
		return ""
	}

	drive := strings.ToLower(string(normalized[0]))
	rest := strings.TrimPrefix(normalized[2:], "/")
	linuxProfile := "/mnt/" + drive + "/" + rest

	return linuxProfile + "/Documents/EngramVault"
}
