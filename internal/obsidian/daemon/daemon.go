package daemon

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian/tui"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
)

const cleanupConfirmPolls = 2

// Config es la configuración del daemon.
type Config struct {
	SelectionPath string
	PollInterval  time.Duration
	SyncInterval  time.Duration
	Process       ProcessConfig
	Logf          func(string, ...any)
	// ForceSelect indica que la TUI debe abrirse siempre al detectar Obsidian,
	// ignorando cualquier selección preexistente. Reservado para uso futuro.
	ForceSelect bool
	DaemonMode  bool
}

// DefaultConfig retorna una configuración por defecto.
func DefaultConfig() Config {
	return Config{
		SelectionPath: obsidian.DefaultSelectionPath(),
		PollInterval:  2500 * time.Millisecond,
		SyncInterval:  10 * time.Minute,
		Process:       DefaultProcessConfig(),
		Logf:          log.Printf,
	}
}

// Daemon orquesta el ciclo completo: detección → TUI → sync → cleanup.
type Daemon struct {
	cfg Config
}

// New crea un Daemon con la configuración dada.
func New(cfg Config) *Daemon {
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	return &Daemon{cfg: cfg}
}

// RunOnce ejecuta exactamente un ciclo: abre la TUI, el usuario confirma,
// sincroniza y retorna. Usado por --select. No entra al loop del daemon
// y nunca llama a cleanup, sin importar cómo termine.
func (d *Daemon) RunOnce() error {
	d.cfg.Logf("engram-obsidian --select: one-shot mode")
	synced, err := d.runCycle()
	if err != nil {
		return fmt.Errorf("select cycle: %w", err)
	}
	if synced {
		d.cfg.Logf("--select sync complete — exiting")
	} else {
		d.cfg.Logf("--select cancelled — exiting")
	}
	return nil
}

// Run ejecuta el daemon hasta que el contexto sea cancelado.
func (d *Daemon) Run(ctx context.Context) error {
	d.cfg.Logf("engram-obsidian daemon starting (poll: %s, sync interval: %s)", d.cfg.PollInterval, d.cfg.SyncInterval)

	// wasSynced refleja si este proceso creó contenido en el vault.
	// Al arrancar, se inicializa en true si el vault ya tiene contenido
	// (puede ocurrir si el daemon fue reiniciado con el vault aún poblado).
	wasSynced := d.vaultHasContent()
	if wasSynced {
		d.cfg.Logf("Vault content detected on startup — will enforce cleanup if conditions not met")
	}

	cleanupCountdown := 0
	var lastSync time.Time

	// Bootstrap: evaluar condiciones y actuar en consecuencia.
	conditionsMet := ShouldSync(d.cfg.Process)
	if conditionsMet {
		if d.cfg.DaemonMode {
			d.cfg.Logf("Conditions MET on startup — syncing (daemon mode)")
			if err := d.syncOnly(); err != nil {
				d.cfg.Logf("WARN startup sync: %v", err)
			} else {
				wasSynced = true
				lastSync = time.Now()
			}
		} else {
			d.cfg.Logf("Conditions MET on startup — launching TUI")
			synced, err := d.runCycle()
			if err != nil {
				d.cfg.Logf("WARN startup cycle: %v", err)
			}
			wasSynced = synced
			if synced {
				lastSync = time.Now()
			}
		}
	} else if wasSynced {
		// Condiciones no se cumplen pero hay vault huérfano → limpiar inmediatamente.
		d.cfg.Logf("Conditions not met on startup but vault has content — cleaning up orphan vault")
		d.cleanup()
		wasSynced = false
	} else {
		d.cfg.Logf("Conditions not met — standby")
	}

	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.cfg.Logf("Context cancelled — shutting down")
			if wasSynced {
				d.cleanup()
			}
			return ctx.Err()

		case <-ticker.C:
			// Evaluar condiciones UNA sola vez por tick para evitar
			// dobles llamadas a ObsidianRunning() (tasklist.exe es costoso).
			conditionsMet = ShouldSync(d.cfg.Process)

			if !wasSynced && conditionsMet {
				if d.cfg.DaemonMode {
					d.cfg.Logf("Conditions MET — syncing (daemon mode)")
					if err := d.syncOnly(); err != nil {
						d.cfg.Logf("WARN sync: %v", err)
					} else {
						wasSynced = true
						lastSync = time.Now()
						cleanupCountdown = 0
					}
				} else {
					d.cfg.Logf("Conditions MET — launching TUI")
					synced, err := d.runCycle()
					if err != nil {
						d.cfg.Logf("WARN cycle: %v", err)
					}
					wasSynced = synced
					if synced {
						lastSync = time.Now()
						cleanupCountdown = 0
					}
				}

			} else if wasSynced && !conditionsMet {
				// Usar !conditionsMet en vez de ShouldCleanup() para evitar
				// una segunda llamada a ObsidianRunning() en el mismo tick.
				cleanupCountdown++
				d.cfg.Logf("Conditions lost (%d/%d) — waiting to confirm cleanup", cleanupCountdown, cleanupConfirmPolls)
				if cleanupCountdown >= cleanupConfirmPolls {
					d.cfg.Logf("Cleaning up vault")
					d.cleanup()
					wasSynced = false
					cleanupCountdown = 0
				}

			} else if wasSynced && conditionsMet {
				// Re-sync periódico
				cleanupCountdown = 0
				if time.Since(lastSync) >= d.cfg.SyncInterval {
					d.cfg.Logf("Periodic re-sync")
					if err := d.syncOnly(); err != nil {
						d.cfg.Logf("WARN periodic sync: %v", err)
					} else {
						lastSync = time.Now()
					}
				}

			} else {
				// wasSynced=false, conditionsMet=false
				cleanupCountdown = 0
			}
		}
	}
}

// runCycle lanza la TUI, obtiene la selección, y sincroniza.
// Retorna (true, nil) si el sync fue exitoso.
func (d *Daemon) runCycle() (bool, error) {
	// Cargar selección
	sel, err := obsidian.LoadSelection(d.cfg.SelectionPath)
	if err != nil {
		return false, fmt.Errorf("load selection: %w", err)
	}

	// Necesitamos observaciones para la TUI — abrir DB
	dbPath := sel.Config.DBPath
	if dbPath == "" {
		dbPath = defaultDBPath()
	}

	var observations []store.Observation
	if dbPath != "" {
		if reader, err := store.Open(dbPath); err == nil {
			if data, err := reader.Export(); err == nil {
				observations = data.Observations
			}
			reader.Close()
		}
	}

	// Lanzar TUI
	model := tui.New(sel, observations)
	prog := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := prog.Run()
	if err != nil {
		return false, fmt.Errorf("tui: %w", err)
	}

	m, ok := finalModel.(tui.Model)
	if !ok || !m.Confirmed {
		d.cfg.Logf("TUI cancelled — no sync")
		return false, nil
	}

	// Guardar selección
	updatedSel := tui.ToSelection(m.Roots, m.Selection)
	updatedSel.Config = m.Selection.Config
	if err := updatedSel.Save(d.cfg.SelectionPath); err != nil {
		d.cfg.Logf("WARN save selection: %v", err)
	}

	// Sincronizar
	return d.doSync(updatedSel)
}

// syncOnly re-sincroniza sin abrir la TUI (para re-syncs periódicos).
func (d *Daemon) syncOnly() error {
	sel, err := obsidian.LoadSelection(d.cfg.SelectionPath)
	if err != nil {
		return fmt.Errorf("load selection: %w", err)
	}
	_, err = d.doSync(sel)
	return err
}

// doSync lee el DB y exporta al vault.
func (d *Daemon) doSync(sel *obsidian.Selection) (bool, error) {
	if !sel.HasConfig() {
		return false, fmt.Errorf("selection has no config (vault/db path missing)")
	}

	reader, err := store.Open(sel.Config.DBPath)
	if err != nil {
		return false, fmt.Errorf("open db: %w", err)
	}
	defer reader.Close()

	data, err := reader.Export()
	if err != nil {
		return false, fmt.Errorf("export db: %w", err)
	}

	exporter := obsidian.NewExporter(sel.Config.VaultPath, d.cfg.Logf)
	result, err := exporter.Export(data, sel.Filter)
	if err != nil {
		return false, fmt.Errorf("export vault: %w", err)
	}

	d.cfg.Logf("Sync complete: created=%d updated=%d deleted=%d skipped=%d errors=%d",
		result.Created, result.Updated, result.Deleted, result.Skipped, len(result.Errors))

	return true, nil
}

// cleanup elimina _engram/ y graph.json del vault.
func (d *Daemon) cleanup() {
	sel, err := obsidian.LoadSelection(d.cfg.SelectionPath)
	if err != nil || !sel.HasConfig() {
		return
	}
	exp := obsidian.NewExporter(sel.Config.VaultPath, d.cfg.Logf)
	if err := exp.Cleanup(); err != nil {
		d.cfg.Logf("WARN cleanup: %v", err)
	}
}

// vaultHasContent devuelve true si el directorio _engram/ del vault ya existe
// con contenido. Se usa al iniciar para detectar vaults huérfanos de ejecuciones
// anteriores y forzar cleanup si las condiciones no se cumplen.
func (d *Daemon) vaultHasContent() bool {
	sel, err := obsidian.LoadSelection(d.cfg.SelectionPath)
	if err != nil || !sel.HasConfig() {
		return false
	}
	exp := obsidian.NewExporter(sel.Config.VaultPath, d.cfg.Logf)
	info, err := os.Stat(exp.EngramRoot())
	return err == nil && info.IsDir()
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.engram/engram.db"
}
