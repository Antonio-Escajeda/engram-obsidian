package daemon

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/crypto"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/obsidian/tui"
	"github.com/Antonio-Escajeda/engram-obsidian/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	_ "modernc.org/sqlite"
)

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

	sel, err := d.loadOrBootstrapSelection()
	if err != nil {
		return fmt.Errorf("select cycle: load selection: %w", err)
	}

	dbPath := expandHomePath(sel.Config.DBPath)
	if dbPath == "" {
		dbPath = defaultDBPath()
	}

	var observations []store.Observation
	if dbPath != "" {
		if reader, err := store.Open(dbPath); err != nil {
			d.cfg.Logf("WARN open db: %v", err)
		} else {
			if data, err := reader.Export(); err != nil {
				d.cfg.Logf("WARN export db: %v", err)
			} else {
				observations = data.Observations
			}
			reader.Close()
		}
	}

	model := tui.New(sel, observations)
	prog := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := prog.Run()
	if err != nil {
		return fmt.Errorf("select cycle: tui: %w", err)
	}

	m, ok := finalModel.(tui.Model)
	if !ok || !m.Confirmed {
		d.cfg.Logf("--select cancelled — exiting")
		return nil
	}

	updatedSel := tui.ToSelection(m.Roots, m.Selection)
	updatedSel.Config = m.Selection.Config
	updatedSel.Confirmed = true
	if err := updatedSel.Save(d.cfg.SelectionPath); err != nil {
		d.cfg.Logf("WARN save selection: %v", err)
	}

	if !ShouldSync(d.cfg.Process) {
		d.cfg.Logf("--select selection saved — conditions not active, skipping sync")
		return nil
	}

	_, err = d.doSync(updatedSel)
	if err != nil {
		return fmt.Errorf("select cycle: %w", err)
	}
	d.cfg.Logf("--select sync complete — exiting")
	return nil
}

// Run ejecuta el daemon hasta que el contexto sea cancelado.
func (d *Daemon) Run(ctx context.Context) error {
	d.cfg.Logf("engram-obsidian daemon starting (poll: %s, sync interval: %s)", d.cfg.PollInterval, d.cfg.SyncInterval)

	// T3.5 — flock para instancia única.
	// Cargar selección aquí para obtener dbPath antes de cualquier otra operación.
	startupSel, err := d.loadOrBootstrapSelection()
	if err != nil {
		return fmt.Errorf("run: load selection for lock: %w", err)
	}
	dbPath := expandHomePath(startupSel.Config.DBPath)
	if dbPath == "" {
		dbPath = defaultDBPath()
	}
	lockPath := filepath.Join(filepath.Dir(dbPath), "engram-obsidian.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another engram-obsidian instance is already running")
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) //nolint:errcheck

	// T3.1 — resolveDBState al inicio, antes de cualquier ShouldSync o acceso a DB.
	if err := d.resolveDBState(dbPath); err != nil {
		d.cfg.Logf("WARN resolveDBState: %v", err)
	}

	// wasSynced refleja si este proceso creó contenido en el vault.
	// Al arrancar, se inicializa en true si el vault ya tiene contenido
	// (puede ocurrir si el daemon fue reiniciado con el vault aún poblado).
	wasSynced := d.vaultHasContent()
	if wasSynced {
		d.cfg.Logf("Vault content detected on startup — will enforce cleanup if conditions not met")
	}

	var lastSync time.Time
	var prevConditionsMet bool
	hasPrevConditions := false
	selectionUnconfirmedLogged := false

	// Bootstrap: evaluar condiciones y actuar en consecuencia.
	conditionState := ReadSyncConditionState(d.cfg.Process)
	conditionsMet := conditionState.Met()
	d.cfg.Logf("Condition check (startup): %s -> met=%t", conditionState.String(), conditionsMet)
	prevConditionsMet = conditionsMet
	hasPrevConditions = true
	if conditionsMet {
		// T3.2 — decryptDB en el path de bootstrap cuando conditionsMet es true.
		if startupSel.Config.EncryptDBEnabled() {
			if err := d.decryptDB(dbPath); err != nil {
				d.cfg.Logf("WARN decryptDB (bootstrap): %v — skipping startup sync", err)
				conditionsMet = false // forzar standby; no sincronizar con DB cifrada
			}
		}
	}
	if !startupSel.IsConfirmed() {
		d.cfg.Logf("Selection not confirmed — run --select first; skipping sync")
		selectionUnconfirmedLogged = true
	} else if conditionsMet {
		selectionUnconfirmedLogged = false
		if d.cfg.DaemonMode {
			d.cfg.Logf("Conditions MET on startup — syncing (daemon mode)")
			synced, err := d.syncOnly()
			if err != nil {
				d.cfg.Logf("WARN startup sync: %v", err)
			} else if synced {
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
		if d.cleanup() {
			wasSynced = false
		}
	} else {
		d.cfg.Logf("Conditions not met — standby")
	}

	ticker := time.NewTicker(d.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.cfg.Logf("Context cancelled — shutting down")
			if wasSynced || d.vaultHasContent() {
				if !d.cleanup() {
					d.cfg.Logf("WARN shutdown cleanup: could not resolve selection, vault may be orphaned")
				}
			}
			return ctx.Err()

		case <-ticker.C:
			conditionState = ReadSyncConditionState(d.cfg.Process)
			conditionsMet = conditionState.Met()
			if !hasPrevConditions || prevConditionsMet != conditionsMet {
				d.cfg.Logf("Condition transition: %s -> met=%t", conditionState.String(), conditionsMet)
			}
			prevConditionsMet = conditionsMet
			hasPrevConditions = true

			if !wasSynced && conditionsMet {
				tickSel, selErr := obsidian.LoadSelection(d.cfg.SelectionPath)
				if selErr != nil {
					d.cfg.Logf("WARN load selection: %v", selErr)
					continue
				}
				if !tickSel.IsConfirmed() {
					if !selectionUnconfirmedLogged {
						d.cfg.Logf("Selection not confirmed — run --select first; skipping sync")
						selectionUnconfirmedLogged = true
					}
					continue
				}
				selectionUnconfirmedLogged = false

				// T3.2 — decryptDB en el loop cuando conditionsMet pasa a true.
				// Necesitamos el config actualizado — recargar selección para obtener EncryptDB.
				if tickSel.Config.EncryptDBEnabled() {
					tickDBPath := expandHomePath(tickSel.Config.DBPath)
					if tickDBPath == "" {
						tickDBPath = defaultDBPath()
					}
					if err := d.decryptDB(tickDBPath); err != nil {
						d.cfg.Logf("WARN decryptDB (loop): %v — skipping sync", err)
						continue
					}
				}

				if d.cfg.DaemonMode {
					d.cfg.Logf("Conditions MET — syncing (daemon mode)")
					synced, err := d.syncOnly()
					if err != nil {
						d.cfg.Logf("WARN sync: %v", err)
					} else if synced {
						wasSynced = true
						lastSync = time.Now()
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
					}
				}

			} else if !conditionsMet && (wasSynced || d.vaultHasContent()) {
				d.cfg.Logf("Conditions not met — cleaning _engram vault content")
				// T3.3 — encryptDB antes de cleanup cuando EncryptDB está habilitado.
				cleanupSel, selErr := obsidian.LoadSelection(d.cfg.SelectionPath)
				if selErr == nil && cleanupSel.Config.EncryptDBEnabled() {
					cleanupDBPath := expandHomePath(cleanupSel.Config.DBPath)
					if cleanupDBPath == "" {
						cleanupDBPath = defaultDBPath()
					}
					if err := d.encryptDB(cleanupDBPath); err != nil {
						d.cfg.Logf("WARN encryptDB: %v — retrying next tick", err)
						continue
					}
				}
				if d.cleanup() {
					wasSynced = false
				}

			} else if wasSynced && conditionsMet {
				// Re-sync periódico
				if time.Since(lastSync) >= d.cfg.SyncInterval {
					d.cfg.Logf("Periodic re-sync")
					synced, err := d.syncOnly()
					if err != nil {
						d.cfg.Logf("WARN periodic sync: %v", err)
					} else if synced {
						lastSync = time.Now()
					}
				}

			}
		}
	}
}

// loadOrBootstrapSelection garantiza que exista una selección utilizable para el daemon.
// Política:
// - Si vault_path configurado existe, se respeta tal cual.
// - Si falta o es inválido, se crea un vault por default en Documents/EngramVault.
// - Siempre asegura que vault, db dir y selection file existan cuando realiza bootstrap.
func (d *Daemon) loadOrBootstrapSelection() (*obsidian.Selection, error) {
	sel, err := obsidian.LoadSelection(d.cfg.SelectionPath)
	if err != nil {
		return nil, err
	}

	changed := false
	vaultPath := strings.TrimSpace(expandHomePath(sel.Config.VaultPath))
	if vaultPath != "" {
		if info, statErr := os.Stat(vaultPath); statErr == nil && info.IsDir() {
			sel.Config.VaultPath = vaultPath
		} else {
			vaultPath = ""
		}
	}

	if vaultPath == "" {
		vaultPath = defaultVaultPath()
		sel.Config.VaultPath = vaultPath
		changed = true
	}

	dbPath := strings.TrimSpace(expandHomePath(sel.Config.DBPath))
	if dbPath == "" {
		dbPath = defaultDBPath()
		sel.Config.DBPath = dbPath
		changed = true
	}

	if err := os.MkdirAll(vaultPath, 0755); err != nil {
		return nil, fmt.Errorf("bootstrap vault dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("bootstrap db dir: %w", err)
	}

	if changed || !sel.HasConfig() {
		if err := sel.Save(d.cfg.SelectionPath); err != nil {
			return nil, fmt.Errorf("bootstrap selection save: %w", err)
		}
	}

	return sel, nil
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
	dbPath := expandHomePath(sel.Config.DBPath)
	if dbPath == "" {
		dbPath = defaultDBPath()
	}

	var observations []store.Observation
	if dbPath != "" {
		if reader, err := store.Open(dbPath); err != nil {
			d.cfg.Logf("WARN open db: %v", err)
		} else {
			if data, err := reader.Export(); err != nil {
				d.cfg.Logf("WARN export db: %v", err)
			} else {
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
	updatedSel.Confirmed = true
	if err := updatedSel.Save(d.cfg.SelectionPath); err != nil {
		d.cfg.Logf("WARN save selection: %v", err)
	}

	// Sincronizar
	return d.doSync(updatedSel)
}

// syncOnly re-sincroniza sin abrir la TUI (para re-syncs periódicos).
func (d *Daemon) syncOnly() (bool, error) {
	sel, err := obsidian.LoadSelection(d.cfg.SelectionPath)
	if err != nil {
		return false, fmt.Errorf("load selection: %w", err)
	}
	return d.doSync(sel)
}

// doSync lee el DB y exporta al vault.
func (d *Daemon) doSync(sel *obsidian.Selection) (bool, error) {
	if !sel.HasConfig() {
		return false, fmt.Errorf("selection has no config (vault/db path missing)")
	}
	if !sel.IsConfirmed() {
		d.cfg.Logf("Selection not confirmed — run --select first; skipping sync")
		return false, nil
	}

	if obsidian.IsWSLEnvironment() {
		conditionState := ReadSyncConditionState(d.cfg.Process)
		if !conditionState.Met() {
			d.cfg.Logf("Vault population gate: skip export on WSL (%s)", conditionState.String())
			return false, nil
		}
	}

	dbPath := expandHomePath(sel.Config.DBPath)
	reader, err := store.Open(dbPath)
	if err != nil {
		return false, fmt.Errorf("open db: %w", err)
	}
	defer reader.Close()

	data, err := reader.Export()
	if err != nil {
		return false, fmt.Errorf("export db: %w", err)
	}

	exporter := obsidian.NewExporter(sel.Config.VaultPath, sel.Config.GraphModeOrDefault(), d.cfg.Logf)
	result, err := exporter.Export(data, sel.Filter)
	if err != nil {
		return false, fmt.Errorf("export vault: %w", err)
	}

	d.cfg.Logf("Sync complete: created=%d updated=%d deleted=%d skipped=%d errors=%d",
		result.Created, result.Updated, result.Deleted, result.Skipped, len(result.Errors))

	return true, nil
}

// cleanup elimina _engram/ y graph.json del vault.
// Retorna true si exp.Cleanup() fue invocado (independientemente de si tuvo
// error interno), false si no se pudo resolver la configuración y no se hizo nada.
func (d *Daemon) cleanup() bool {
	sel, err := obsidian.LoadSelection(d.cfg.SelectionPath)
	if err != nil || !sel.HasConfig() {
		return false
	}
	exp := obsidian.NewExporter(sel.Config.VaultPath, sel.Config.GraphModeOrDefault(), d.cfg.Logf)
	if err := exp.Cleanup(); err != nil {
		d.cfg.Logf("WARN cleanup: %v", err)
	}
	return true
}

// vaultHasContent devuelve true si el directorio _engram/ del vault ya existe
// con contenido. Se usa al iniciar para detectar vaults huérfanos de ejecuciones
// anteriores y forzar cleanup si las condiciones no se cumplen.
func (d *Daemon) vaultHasContent() bool {
	sel, err := obsidian.LoadSelection(d.cfg.SelectionPath)
	if err != nil || !sel.HasConfig() {
		return false
	}
	exp := obsidian.NewExporter(sel.Config.VaultPath, sel.Config.GraphModeOrDefault(), d.cfg.Logf)
	info, err := os.Stat(exp.EngramRoot())
	return err == nil && info.IsDir()
}

// resolveDBState verifica el estado del DB en disco al inicio de Run().
// Si ambos .db y .db.enc existen simultáneamente, el .enc es autoritativo:
// borra el plaintext y sus WAL files.
// Siempre limpia archivos temporales residuales de crashes anteriores.
func (d *Daemon) resolveDBState(dbPath string) error {
	encPath := dbPath + ".enc"
	walPath := dbPath + "-wal"
	shmPath := dbPath + "-shm"
	tmpPath := dbPath + ".tmp"
	encTmpPath := dbPath + ".enc.tmp"

	// Limpiar archivos temporales residuales de crashes anteriores (ignorar errores).
	_ = os.Remove(tmpPath)
	_ = os.Remove(encTmpPath)

	_, dbErr := os.Stat(dbPath)
	_, encErr := os.Stat(encPath)

	dbExists := dbErr == nil
	encExists := encErr == nil

	if dbExists && encExists {
		// Ambos presentes: .enc es autoritativo. Borrar plaintext y WAL files.
		d.cfg.Logf("WARN resolveDBState: both .db and .db.enc exist — .enc is authoritative, removing plaintext")
		if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("resolveDBState: remove plaintext db: %w", err)
		}
		_ = os.Remove(walPath)
		_ = os.Remove(shmPath)
	}
	// StateNone (ninguno existe), StateDB (solo .db), StateEnc (solo .enc) → OK, sin acción extra.
	return nil
}

// decryptDB descifra .enc → .db de forma atómica.
// Es no-op si .enc no existe (DB ya está en plaintext o primera instalación).
func (d *Daemon) decryptDB(dbPath string) error {
	encPath := dbPath + ".enc"
	tmpPath := dbPath + ".tmp"
	dbDir := filepath.Dir(dbPath)

	if _, err := os.Stat(encPath); os.IsNotExist(err) {
		// No hay .enc — nada que descifrar.
		return nil
	}

	key, err := crypto.GetOrCreateKey(dbDir)
	if err != nil {
		return fmt.Errorf("decryptDB: get key: %w", err)
	}

	data, err := os.ReadFile(encPath)
	if err != nil {
		return fmt.Errorf("decryptDB: read enc file: %w", err)
	}

	plaintext, err := crypto.Decrypt(key, data)
	if err != nil {
		return fmt.Errorf("decryptDB: decrypt: %w", err)
	}

	// Atomic write: escribir a .db.tmp luego renombrar.
	if err := os.WriteFile(tmpPath, plaintext, 0600); err != nil {
		return fmt.Errorf("decryptDB: write tmp: %w", err)
	}

	if err := os.Rename(tmpPath, dbPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("decryptDB: rename tmp to db: %w", err)
	}

	// Borrar .enc solo si la escritura del plaintext fue exitosa.
	if err := os.Remove(encPath); err != nil && !os.IsNotExist(err) {
		d.cfg.Logf("WARN decryptDB: remove enc file: %v", err)
	}

	d.cfg.Logf("decryptDB: DB decrypted successfully")
	return nil
}

// encryptDB hace checkpoint WAL, cifra .db → .db.enc de forma atómica,
// y borra el plaintext y sus WAL files.
// Es no-op si .db no existe.
func (d *Daemon) encryptDB(dbPath string) error {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// No hay .db — nada que cifrar.
		return nil
	}

	encTmpPath := dbPath + ".enc.tmp"
	encPath := dbPath + ".enc"
	walPath := dbPath + "-wal"
	shmPath := dbPath + "-shm"
	dbDir := filepath.Dir(dbPath)

	// Checkpoint WAL para asegurar que .db sea un snapshot consistente.
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_busy_timeout=5000", dbPath))
	if err != nil {
		return fmt.Errorf("encryptDB: open db for checkpoint: %w", err)
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		db.Close()
		return fmt.Errorf("encryptDB: wal_checkpoint: %w", err)
	}
	db.Close()

	key, err := crypto.GetOrCreateKey(dbDir)
	if err != nil {
		return fmt.Errorf("encryptDB: get key: %w", err)
	}

	plaintext, err := os.ReadFile(dbPath)
	if err != nil {
		return fmt.Errorf("encryptDB: read db: %w", err)
	}

	ciphertext, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		return fmt.Errorf("encryptDB: encrypt: %w", err)
	}

	// Atomic write: escribir a .enc.tmp luego renombrar.
	if err := os.WriteFile(encTmpPath, ciphertext, 0600); err != nil {
		return fmt.Errorf("encryptDB: write enc tmp: %w", err)
	}

	if err := os.Rename(encTmpPath, encPath); err != nil {
		_ = os.Remove(encTmpPath)
		return fmt.Errorf("encryptDB: rename enc tmp to enc: %w", err)
	}

	// Borrar plaintext y WAL files — ignorar "no existe".
	_ = os.Remove(dbPath)
	_ = os.Remove(walPath)
	_ = os.Remove(shmPath)

	d.cfg.Logf("encryptDB: DB encrypted successfully")
	return nil
}

func defaultDBPath() string {
	home, _ := os.UserHomeDir()
	return home + "/.engram/engram.db"
}

func defaultVaultPath() string {
	if detected := obsidian.DetectWSLVaultPath(); strings.TrimSpace(detected) != "" {
		return detected
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "EngramVault")
}

func expandHomePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
