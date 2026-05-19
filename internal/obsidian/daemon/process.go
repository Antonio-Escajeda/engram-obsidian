package daemon

import (
	"os"
	"os/exec"
	"strings"
	"time"
)

// ProcessConfig contiene la configuración para detección de procesos.
type ProcessConfig struct {
	TasklistPath string // path a tasklist.exe, default: /mnt/c/Windows/system32/tasklist.exe
}

// DefaultProcessConfig retorna la config por defecto buscando tasklist.exe.
func DefaultProcessConfig() ProcessConfig {
	for _, mnt := range []string{"/mnt/c", "/mnt/d", "/mnt/e"} {
		tl := mnt + "/Windows/system32/tasklist.exe"
		if _, err := os.Stat(tl); err == nil {
			return ProcessConfig{TasklistPath: tl}
		}
	}
	return ProcessConfig{TasklistPath: "/mnt/c/Windows/system32/tasklist.exe"}
}

// ObsidianRunning devuelve true si Obsidian.exe está en la lista de procesos de Windows.
func ObsidianRunning(cfg ProcessConfig) (bool, error) {
	cmd := exec.Command(cfg.TasklistPath)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "Obsidian.exe"), nil
}

// RootSessionActive devuelve true si hay una sesión root interactiva en un pts device.
// Lee /proc directamente sin subprocesos.
//
// Criterios para considerar "sesión root interactiva":
//   - UID real == 0
//   - El proceso es un shell conocido (bash, zsh, sh, fish, dash)
//   - No es un shell ejecutando un script (argv[1] es un path o flag -c/-s)
//   - Está asociado a un pts device (major 136) con tpgid > 0
//     (tpgid > 0, no >= 0, para descartar procesos con terminal pero sin fg group activo)
func RootSessionActive() bool {
	interactiveShells := map[string]bool{"bash": true, "zsh": true, "sh": true, "fish": true, "dash": true}
	// Flags que indican shell no-interactivo
	nonInteractiveFlags := map[string]bool{"-c": true, "-s": true, "-i": true}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid := entry.Name()
		if !isDigit(pid) {
			continue
		}

		// Verificar UID real == 0 (campo Uid: en /proc/PID/status)
		status, err := os.ReadFile("/proc/" + pid + "/status")
		if err != nil {
			continue
		}
		uid := extractUID(string(status))
		if uid != 0 {
			continue
		}

		// Verificar que sea un shell conocido
		commBytes, err := os.ReadFile("/proc/" + pid + "/comm")
		if err != nil {
			continue
		}
		comm := strings.TrimSpace(string(commBytes))
		if !interactiveShells[comm] {
			continue
		}

		// Verificar cmdline: descartar shells no-interactivos
		cmdlineBytes, err := os.ReadFile("/proc/" + pid + "/cmdline")
		if err != nil {
			continue
		}
		argv := splitNull(cmdlineBytes)
		if len(argv) > 1 && len(argv[1]) > 0 {
			arg1 := string(argv[1])
			// Es un flag no-interactivo (-c, -s, -i cuando va con argumento)
			if nonInteractiveFlags[arg1] {
				continue
			}
			// Es un path absoluto o relativo a un script
			// Un path relativo no empieza con '-' ni es vacío
			if arg1[0] == '/' || (arg1[0] != '-' && strings.ContainsAny(arg1, "./")) {
				continue
			}
		}

		// Verificar tty pts (major 136) con tpgid > 0
		// tpgid > 0 garantiza que hay un proceso en foreground activo en la terminal.
		// tpgid == 0 o < 0 indica sin terminal o terminal sin fg group.
		statBytes, err := os.ReadFile("/proc/" + pid + "/stat")
		if err != nil {
			continue
		}
		ttyNr, tpgid := parseTTY(string(statBytes))
		if ttyNr != 0 && (ttyNr>>8)&0xff == 136 && tpgid > 0 {
			return true
		}
	}
	return false
}

// ShouldSync retorna true si ambas condiciones se cumplen.
func ShouldSync(cfg ProcessConfig) bool {
	running, err := ObsidianRunning(cfg)
	if err != nil || !running {
		return false
	}
	return RootSessionActive()
}

// ShouldCleanup retorna true cuando cualquiera de las condiciones de sync falla:
// Obsidian no está corriendo O no hay sesión root activa.
// Es el complemento exacto de ShouldSync: cleanup = !ShouldSync.
func ShouldCleanup(cfg ProcessConfig) bool {
	running, err := ObsidianRunning(cfg)
	if err != nil {
		// Si no podemos determinar el estado, asumimos que no está corriendo
		// para evitar dejar archivos huérfanos.
		return true
	}
	return !running || !RootSessionActive()
}

// ── helpers ───────────────────────────────────────────────────────────────────

func isDigit(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func extractUID(status string) int {
	for _, line := range strings.Split(status, "\n") {
		if strings.HasPrefix(line, "Uid:") {
			var uid int
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				for _, c := range fields[1] {
					if c >= '0' && c <= '9' {
						uid = uid*10 + int(c-'0')
					}
				}
			}
			return uid
		}
	}
	return -1
}

func splitNull(b []byte) [][]byte {
	// Strip trailing null
	b = []byte(strings.TrimRight(string(b), "\x00"))
	if len(b) == 0 {
		return [][]byte{{}}
	}
	var parts [][]byte
	start := 0
	for i, c := range b {
		if c == 0 {
			parts = append(parts, b[start:i])
			start = i + 1
		}
	}
	if start <= len(b) {
		parts = append(parts, b[start:])
	}
	return parts
}

func parseTTY(stat string) (ttyNr int, tpgid int) {
	// stat format: pid (comm) state ppid pgrp session tty_nr tpgid ...
	// Después del último ')' están los campos numéricos
	idx := strings.LastIndex(stat, ")")
	if idx < 0 {
		return 0, -1
	}
	fields := strings.Fields(stat[idx+2:])
	if len(fields) < 6 {
		return 0, -1
	}
	ttyNr = atoi(fields[4])
	tpgid = atoi(fields[5])
	return ttyNr, tpgid
}

func atoi(s string) int {
	n := 0
	neg := false
	for i, c := range s {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if neg {
		return -n
	}
	return n
}

// Sleep es una función inyectable para tests.
var Sleep = time.Sleep
