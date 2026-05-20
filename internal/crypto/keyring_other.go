//go:build !linux

package crypto

import "errors"

// ErrKeyringUnavailable is returned when the Linux Keyring is not accessible.
var ErrKeyringUnavailable = errors.New("linux keyring unavailable")

// GetOrCreateKey en plataformas no-Linux retorna ErrKeyringUnavailable.
// La implementación completa de key-file fallback para plataformas no-Linux
// está diferida — se implementará en una fase posterior.
func GetOrCreateKey(dbDir string) ([]byte, error) {
	return nil, ErrKeyringUnavailable
}
