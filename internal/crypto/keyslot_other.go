//go:build !linux

package crypto

import "errors"

// ErrSlotUnavailable is returned when key slot operations are called on a non-Linux platform.
var ErrSlotUnavailable = errors.New("key slots unavailable on this platform")

// SlotParams holds Argon2id tuning parameters stored in the key slot.
type SlotParams struct {
	T uint32 `json:"t"`
	M uint32 `json:"m"`
	P uint8  `json:"p"`
}

// Slot represents a single key slot that wraps the masterKey under an independent secret.
type Slot struct {
	ID         int         `json:"id"`
	Type       string      `json:"type"`
	KDF        string      `json:"kdf,omitempty"`
	Params     *SlotParams `json:"params,omitempty"`
	Salt       string      `json:"salt,omitempty"`
	Nonce      string      `json:"nonce"`
	Ciphertext string      `json:"ciphertext"`
}

// KeySlots is the top-level on-disk structure for keyslots.json.
type KeySlots struct {
	Version int    `json:"version"`
	Slots   []Slot `json:"slots"`
}

// LoadKeySlots is unavailable on non-Linux platforms.
func LoadKeySlots(path string) (*KeySlots, error) {
	return nil, ErrSlotUnavailable
}

// Save is unavailable on non-Linux platforms.
func (ks *KeySlots) Save(path string) error {
	return ErrSlotUnavailable
}

// SealPasswordSlot is unavailable on non-Linux platforms.
func (ks *KeySlots) SealPasswordSlot(masterKey, password []byte) error {
	return ErrSlotUnavailable
}

// OpenPasswordSlot is unavailable on non-Linux platforms.
func (ks *KeySlots) OpenPasswordSlot(password []byte) ([]byte, error) {
	return nil, ErrSlotUnavailable
}

// SealRecoverySlot is unavailable on non-Linux platforms.
func (ks *KeySlots) SealRecoverySlot(masterKey []byte) ([]byte, error) {
	return nil, ErrSlotUnavailable
}

// OpenRecoverySlot is unavailable on non-Linux platforms.
func (ks *KeySlots) OpenRecoverySlot(recoveryKey []byte) ([]byte, error) {
	return nil, ErrSlotUnavailable
}
