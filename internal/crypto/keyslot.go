//go:build linux

package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/crypto/argon2"
)

// SlotParams holds Argon2id tuning parameters stored in the key slot.
type SlotParams struct {
	T uint32 `json:"t"`
	M uint32 `json:"m"`
	P uint8  `json:"p"`
}

// Slot represents a single key slot that wraps the masterKey under an independent secret.
type Slot struct {
	ID         int         `json:"id"`
	Type       string      `json:"type"`             // "password" or "recovery"
	KDF        string      `json:"kdf,omitempty"`    // "argon2id" or "none" (omit for recovery)
	Params     *SlotParams `json:"params,omitempty"` // only for argon2id slots
	Salt       string      `json:"salt,omitempty"`   // base64-encoded random salt (argon2id only)
	Nonce      string      `json:"nonce"`            // base64-encoded 12-byte GCM nonce
	Ciphertext string      `json:"ciphertext"`       // base64-encoded AES-256-GCM ciphertext+tag
}

// KeySlots is the top-level on-disk structure for keyslots.json.
type KeySlots struct {
	Version int    `json:"version"`
	Slots   []Slot `json:"slots"`
}

// LoadKeySlots reads and unmarshals keyslots.json from path.
// Returns a descriptive error on malformed JSON; nil error on success.
func LoadKeySlots(path string) (*KeySlots, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("LoadKeySlots: read %q: %w", path, err)
	}

	var ks KeySlots
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, fmt.Errorf("LoadKeySlots: unmarshal %q: %w", path, err)
	}

	return &ks, nil
}

// Save writes ks to path atomically: write to path+".tmp", chmod 0400, then os.Rename.
func (ks *KeySlots) Save(path string) error {
	data, err := json.MarshalIndent(ks, "", "  ")
	if err != nil {
		return fmt.Errorf("keyslots Save: marshal: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0400); err != nil {
		return fmt.Errorf("keyslots Save: write tmp %q: %w", tmp, err)
	}

	if err := os.Rename(tmp, path); err != nil {
		// Best-effort cleanup of the tmp file on rename failure.
		_ = os.Remove(tmp)
		return fmt.Errorf("keyslots Save: rename %q → %q: %w", tmp, path, err)
	}

	return nil
}

// sealWithKey encrypts plaintext using AES-256-GCM with slotKey.
// Returns nonce (12 bytes) and ciphertext+tag (len(plaintext)+16 bytes) separately.
func sealWithKey(slotKey, plaintext []byte) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(slotKey)
	if err != nil {
		return nil, nil, fmt.Errorf("sealWithKey: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("sealWithKey: new GCM: %w", err)
	}

	nonce = make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("sealWithKey: generate nonce: %w", err)
	}

	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

// openWithKey decrypts ciphertext+tag using AES-256-GCM with slotKey and nonce.
func openWithKey(slotKey, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(slotKey)
	if err != nil {
		return nil, fmt.Errorf("openWithKey: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("openWithKey: new GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("openWithKey: decryption failed — authentication tag mismatch")
	}

	return plaintext, nil
}

// upsertSlot replaces the slot with the given id, or appends if not found.
func (ks *KeySlots) upsertSlot(s Slot) {
	for i, existing := range ks.Slots {
		if existing.ID == s.ID {
			ks.Slots[i] = s
			return
		}
	}
	ks.Slots = append(ks.Slots, s)
}

// findSlot returns the slot with the given id, or an error if not found.
func (ks *KeySlots) findSlot(id int) (*Slot, error) {
	for i := range ks.Slots {
		if ks.Slots[i].ID == id {
			return &ks.Slots[i], nil
		}
	}
	return nil, fmt.Errorf("key slot %d not found", id)
}

// SealPasswordSlot wraps masterKey under password using Argon2id and upserts slot id=0.
// Argon2id parameters: t=3, m=65536, p=4, 32-byte output key, random 16-byte salt.
func (ks *KeySlots) SealPasswordSlot(masterKey, password []byte) error {
	// Generate random 16-byte salt.
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("SealPasswordSlot: generate salt: %w", err)
	}

	params := SlotParams{T: 3, M: 65536, P: 4}
	slotKey := argon2.IDKey(password, salt, params.T, params.M, params.P, 32)

	nonce, ciphertext, err := sealWithKey(slotKey, masterKey)
	if err != nil {
		return fmt.Errorf("SealPasswordSlot: seal: %w", err)
	}

	ks.upsertSlot(Slot{
		ID:         0,
		Type:       "password",
		KDF:        "argon2id",
		Params:     &params,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	})

	return nil
}

// OpenPasswordSlot derives slotKey from password via Argon2id and decrypts slot id=0 → masterKey.
func (ks *KeySlots) OpenPasswordSlot(password []byte) ([]byte, error) {
	slot, err := ks.findSlot(0)
	if err != nil {
		return nil, fmt.Errorf("OpenPasswordSlot: %w", err)
	}

	if slot.Params == nil {
		return nil, errors.New("OpenPasswordSlot: slot 0 has no KDF params")
	}

	salt, err := base64.StdEncoding.DecodeString(slot.Salt)
	if err != nil {
		return nil, fmt.Errorf("OpenPasswordSlot: decode salt: %w", err)
	}

	slotKey := argon2.IDKey(password, salt, slot.Params.T, slot.Params.M, slot.Params.P, 32)

	nonce, err := base64.StdEncoding.DecodeString(slot.Nonce)
	if err != nil {
		return nil, fmt.Errorf("OpenPasswordSlot: decode nonce: %w", err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(slot.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("OpenPasswordSlot: decode ciphertext: %w", err)
	}

	masterKey, err := openWithKey(slotKey, nonce, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("OpenPasswordSlot: %w", err)
	}

	return masterKey, nil
}

// SealRecoverySlot generates a random 32-byte recoveryKey, wraps masterKey with it,
// and upserts slot id=1 (kdf="none", no params/salt).
// Returns the generated recoveryKey — display it once to the user.
func (ks *KeySlots) SealRecoverySlot(masterKey []byte) (recoveryKey []byte, err error) {
	recoveryKey = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, recoveryKey); err != nil {
		return nil, fmt.Errorf("SealRecoverySlot: generate recovery key: %w", err)
	}

	nonce, ciphertext, err := sealWithKey(recoveryKey, masterKey)
	if err != nil {
		return nil, fmt.Errorf("SealRecoverySlot: seal: %w", err)
	}

	ks.upsertSlot(Slot{
		ID:         1,
		Type:       "recovery",
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	})

	return recoveryKey, nil
}

// OpenRecoverySlot decrypts slot id=1 using recoveryKey → masterKey.
func (ks *KeySlots) OpenRecoverySlot(recoveryKey []byte) ([]byte, error) {
	slot, err := ks.findSlot(1)
	if err != nil {
		return nil, fmt.Errorf("OpenRecoverySlot: %w", err)
	}

	nonce, err := base64.StdEncoding.DecodeString(slot.Nonce)
	if err != nil {
		return nil, fmt.Errorf("OpenRecoverySlot: decode nonce: %w", err)
	}

	ciphertext, err := base64.StdEncoding.DecodeString(slot.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("OpenRecoverySlot: decode ciphertext: %w", err)
	}

	masterKey, err := openWithKey(recoveryKey, nonce, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("OpenRecoverySlot: %w", err)
	}

	return masterKey, nil
}
