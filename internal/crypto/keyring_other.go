//go:build !linux

package crypto

import "errors"

// ErrKeyringUnavailable is returned when the Linux Keyring is not accessible.
var ErrKeyringUnavailable = errors.New("linux keyring unavailable")

// ErrLocked is returned when the keyring is empty or unavailable and no key can be loaded.
// Callers must check with errors.Is(err, ErrLocked) — it is never wrapped.
var ErrLocked = errors.New("engram: keyring empty — run su/sudo or engram-obsidian unlock")

// LoadKey en plataformas no-Linux retorna ErrLocked.
func LoadKey(dbDir string) ([]byte, error) {
	return nil, ErrLocked
}

// LoadMasterFromKeyring en plataformas no-Linux retorna ErrLocked.
func LoadMasterFromKeyring() ([]byte, error) {
	return nil, ErrLocked
}

// StoreKeyInKeyring en plataformas no-Linux retorna ErrKeyringUnavailable.
func StoreKeyInKeyring(masterKey []byte) error {
	return ErrKeyringUnavailable
}

// GetOrCreateKey en plataformas no-Linux retorna ErrKeyringUnavailable.
// La implementación completa de key-file fallback para plataformas no-Linux
// está diferida — se implementará en una fase posterior.
//
// Deprecated: use LoadKey. Will be removed when daemon.go migrates.
func GetOrCreateKey(dbDir string) ([]byte, error) {
	return nil, ErrKeyringUnavailable
}
