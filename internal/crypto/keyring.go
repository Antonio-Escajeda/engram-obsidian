//go:build linux

package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/hkdf"
	"golang.org/x/sys/unix"
)

const (
	keyringDesc = "engram-obsidian-master-key"
	keyFile     = ".engram-key"
	keySize     = 32
)

// ErrKeyringUnavailable is returned when the Linux Keyring is not accessible.
var ErrKeyringUnavailable = errors.New("linux keyring unavailable")

// ErrLocked is returned when the keyring is empty or unavailable and no key can be loaded.
// Callers must check with errors.Is(err, ErrLocked) — it is never wrapped.
var ErrLocked = errors.New("engram: keyring empty — run su/sudo or engram-obsidian unlock")

// LoadKey returns the derived encryption key for dbDir.
// 1. Tries the Linux kernel keyring — if found, derives and returns.
// 2. If keyring empty or unavailable — returns ErrLocked (unwrapped sentinel).
// No file fallback. No key generation.
func LoadKey(dbDir string) ([]byte, error) {
	if !keyringAvailable() {
		return nil, ErrLocked
	}
	masterKey, err := loadFromKeyring()
	if err != nil {
		return nil, ErrLocked
	}
	return deriveKey(masterKey, "engram-obsidian-v1")
}

// LoadMasterFromKeyring returns the raw masterKey from the Linux kernel keyring.
// Used by the PAM helper which needs to re-wrap masterKey with a new password.
// Returns ErrLocked if the keyring is empty or unavailable.
func LoadMasterFromKeyring() ([]byte, error) {
	if !keyringAvailable() {
		return nil, ErrLocked
	}
	masterKey, err := loadFromKeyring()
	if err != nil {
		return nil, ErrLocked
	}
	return masterKey, nil
}

// StoreKeyInKeyring stores masterKey in the Linux kernel user keyring.
// This is the public wrapper for the PAM helper and CLI subcommands.
func StoreKeyInKeyring(masterKey []byte) error {
	return storeInKeyring(masterKey)
}

// GetOrCreateKey obtiene o crea la clave de cifrado de 32 bytes.
//
// Deprecated: use LoadKey. Will be removed when daemon.go migrates.
//
// Estrategia:
//  1. Si Linux Keyring disponible: intentar loadFromKeyring().
//     OK → deriveKey(masterKey)
//  2. Si keyring disponible pero sin clave: generar 32 bytes random,
//     storeInKeyring(), saveKeyFile() (backup), return deriveKey(masterKey)
//  3. Si keyring NO disponible: intentar loadKeyFile().
//     OK → deriveKey(masterKey)
//  4. Si no hay key file: generar, saveKeyFile(), return deriveKey(masterKey)
func GetOrCreateKey(dbDir string) ([]byte, error) {
	if keyringAvailable() {
		masterKey, err := loadFromKeyring()
		if err == nil {
			return deriveKey(masterKey, "engram-obsidian-v1")
		}

		// Keyring disponible pero no tiene la clave — verificar si hay DB cifrada
		// antes de generar una nueva clave (evitar orphan de datos existentes)
		encPath := filepath.Join(dbDir, "engram.db.enc")
		if _, statErr := os.Stat(encPath); statErr == nil {
			return nil, fmt.Errorf("encrypted DB found but no key in keyring — key may have been lost on system restart; cannot generate new key (would orphan existing encrypted data)")
		}

		// No hay DB cifrada — generar y almacenar nueva clave
		masterKey = make([]byte, keySize)
		if _, err := rand.Read(masterKey); err != nil {
			return nil, fmt.Errorf("GetOrCreateKey: generate key: %w", err)
		}

		if err := storeInKeyring(masterKey); err != nil {
			// No fatal — seguir con key file como fallback
			_ = err
		}

		// Guardar key file como backup para sobrevivir reboots
		if err := saveKeyFile(masterKey, dbDir); err != nil {
			// Log but don't fail — keyring is primary
			_ = err
		}

		return deriveKey(masterKey, "engram-obsidian-v1")
	}

	// Keyring no disponible — usar key file
	masterKey, err := loadKeyFile(dbDir)
	if err == nil {
		return deriveKey(masterKey, "engram-obsidian-v1")
	}

	// Si el key file existe pero falló (p.ej. GCM tag mismatch), no regenerar
	keyPath := filepath.Join(dbDir, keyFile)
	if _, statErr := os.Stat(keyPath); statErr == nil {
		// El archivo existe pero loadKeyFile falló — integridad comprometida
		return nil, fmt.Errorf("key file integrity check failed — cannot recover encryption key")
	}

	// No existe key file — verificar si hay DB cifrada antes de generar nueva clave
	encPath := filepath.Join(dbDir, "engram.db.enc")
	if _, statErr := os.Stat(encPath); statErr == nil {
		return nil, fmt.Errorf("encrypted DB found but no key in keyring — key may have been lost on system restart; cannot generate new key (would orphan existing encrypted data)")
	}

	// No hay DB cifrada — generar nueva clave
	masterKey = make([]byte, keySize)
	if _, err := rand.Read(masterKey); err != nil {
		return nil, fmt.Errorf("GetOrCreateKey: generate key: %w", err)
	}

	if err := saveKeyFile(masterKey, dbDir); err != nil {
		return nil, fmt.Errorf("GetOrCreateKey: save key file: %w", err)
	}

	return deriveKey(masterKey, "engram-obsidian-v1")
}

// keyringAvailable detecta si el Linux Keyring está disponible en runtime.
// Intenta KeyctlSearch: ENOKEY significa keyring accesible (clave no existe aún).
// Cualquier otro error (EACCES, ENOSYS, etc.) indica keyring no disponible.
func keyringAvailable() bool {
	_, err := unix.KeyctlSearch(unix.KEY_SPEC_USER_KEYRING, "user", keyringDesc, 0)
	if err == nil {
		return true // clave encontrada — keyring disponible
	}
	return errors.Is(err, unix.ENOKEY)
}

// getMachineID lee /etc/machine-id con fallback a hostname+UID+username.
func getMachineID() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err == nil {
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id
		}
	}

	// Fallback: hostname + UID + username
	hostname, _ := os.Hostname()
	u, _ := user.Current()
	uid := ""
	username := ""
	if u != nil {
		uid = u.Uid
		username = u.Username
	}
	return fmt.Sprintf("%s-%s-%s", hostname, uid, username)
}

// deriveKey deriva una clave de 32 bytes de masterKey usando HKDF-SHA256.
// salt = machineID + UID
func deriveKey(masterKey []byte, info string) ([]byte, error) {
	u, _ := user.Current()
	uid := ""
	if u != nil {
		uid = u.Uid
	}
	salt := []byte(getMachineID() + uid)

	h := hkdf.New(sha256.New, masterKey, salt, []byte(info))
	derived := make([]byte, keySize)
	if _, err := io.ReadFull(h, derived); err != nil {
		return nil, fmt.Errorf("deriveKey: HKDF: %w", err)
	}
	return derived, nil
}

// storeInKeyring guarda key en el user keyring.
func storeInKeyring(key []byte) error {
	_, err := unix.AddKey("user", keyringDesc, key, unix.KEY_SPEC_USER_KEYRING)
	if err != nil {
		return fmt.Errorf("storeInKeyring: add_key: %w", err)
	}
	return nil
}

// loadFromKeyring carga key del user keyring.
func loadFromKeyring() ([]byte, error) {
	keyID, err := unix.KeyctlSearch(unix.KEY_SPEC_USER_KEYRING, "user", keyringDesc, 0)
	if err != nil {
		return nil, fmt.Errorf("loadFromKeyring: search: %w", err)
	}

	buf := make([]byte, keySize*2) // buffer generoso
	n, err := unix.KeyctlBuffer(unix.KEYCTL_READ, keyID, buf, len(buf))
	if err != nil {
		return nil, fmt.Errorf("loadFromKeyring: read: %w", err)
	}

	if n != keySize {
		return nil, fmt.Errorf("loadFromKeyring: unexpected key size %d (expected %d)", n, keySize)
	}

	return buf[:n], nil
}

// MigrateLegacyKey loads the masterKey from the legacy .engram-key file in dbDir.
// Used by setup-keys when migrating from the old file-based key to the PAM key slot scheme.
// Returns an error if the file does not exist or decryption fails.
func MigrateLegacyKey(dbDir string) ([]byte, error) {
	return loadKeyFile(dbDir)
}

// saveKeyFile cifra masterKey con machine-derived key y la guarda en dbDir/.engram-key (0400).
func saveKeyFile(masterKey []byte, dbDir string) error {
	machineKey, err := machineKey()
	if err != nil {
		return fmt.Errorf("saveKeyFile: derive machine key: %w", err)
	}

	encrypted, err := Encrypt(machineKey, masterKey)
	if err != nil {
		return fmt.Errorf("saveKeyFile: encrypt: %w", err)
	}

	if err := os.MkdirAll(dbDir, 0700); err != nil {
		return fmt.Errorf("saveKeyFile: mkdir: %w", err)
	}

	path := filepath.Join(dbDir, keyFile)
	if err := os.WriteFile(path, encrypted, 0400); err != nil {
		return fmt.Errorf("saveKeyFile: write: %w", err)
	}

	return nil
}

// loadKeyFile carga y descifra la key file de dbDir/.engram-key.
func loadKeyFile(dbDir string) ([]byte, error) {
	path := filepath.Join(dbDir, keyFile)
	encrypted, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("loadKeyFile: read: %w", err)
	}

	machineKey, err := machineKey()
	if err != nil {
		return nil, fmt.Errorf("loadKeyFile: derive machine key: %w", err)
	}

	masterKey, err := Decrypt(machineKey, encrypted)
	if err != nil {
		return nil, fmt.Errorf("loadKeyFile: decrypt: %w", err)
	}

	return masterKey, nil
}

// machineKey deriva una clave de 32 bytes del machine ID (para cifrar el key file).
func machineKey() ([]byte, error) {
	machineID := getMachineID()
	// Usar HKDF con el machine ID como IKM y sin salt externa
	h := hkdf.New(sha256.New, []byte(machineID), nil, []byte("engram-obsidian-machine-key-v1"))
	key := make([]byte, keySize)
	if _, err := io.ReadFull(h, key); err != nil {
		return nil, fmt.Errorf("machineKey: HKDF: %w", err)
	}
	return key, nil
}
