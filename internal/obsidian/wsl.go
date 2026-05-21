package obsidian

import (
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// IsWSLEnvironment retorna true cuando el proceso corre dentro de WSL.
func IsWSLEnvironment() bool {
	if strings.TrimSpace(os.Getenv("WSL_DISTRO_NAME")) != "" {
		return true
	}
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false
	}
	release := strings.ToLower(string(data))
	return strings.Contains(release, "microsoft") || strings.Contains(release, "wsl")
}

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

	// Intento 2: cmd.exe USERPROFILE (funciona en servicios systemd de usuario)
	if path := detectViaCmdExe(); path != "" {
		return path
	}

	// Intento 3: variable de entorno USERPROFILE (disponible en algunos setups WSL2)
	if path := detectViaEnv(); path != "" {
		return path
	}

	// Intento 4: resolver usuario por /mnt/c/Users para entornos systemd sin vars.
	if path := detectViaMountCUsers(); path != "" {
		return path
	}

	return ""
}

// detectViaCmdExe obtiene USERPROFILE invocando cmd.exe y convierte el path
// a formato /mnt/<drive>/... sin depender de wslvar/wslpath.
func detectViaCmdExe() string {
	out, err := exec.Command("cmd.exe", "/C", "echo", "%USERPROFILE%").Output()
	if err != nil {
		return ""
	}
	winProfile := strings.TrimSpace(string(out))
	if winProfile == "" {
		return ""
	}
	return windowsProfileToVaultPath(winProfile)
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
	return windowsProfileToVaultPath(winProfile)
}

func windowsProfileToVaultPath(winProfile string) string {
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

func detectViaMountCUsers() string {
	const usersRoot = "/mnt/c/Users"
	entries, err := os.ReadDir(usersRoot)
	if err != nil {
		return ""
	}

	current, err := user.Current()
	if err != nil {
		return ""
	}
	linuxUser := strings.ToLower(strings.TrimSpace(current.Username))
	if linuxUser == "" {
		return ""
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		lname := strings.ToLower(name)
		if lname == "public" || lname == "default" || lname == "default user" || lname == "all users" {
			continue
		}
		if lname != linuxUser {
			continue
		}
		return usersRoot + "/" + name + "/Documents/EngramVault"
	}

	return ""
}
