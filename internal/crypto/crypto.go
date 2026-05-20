package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
)

const (
	magic   = "ENGM"
	version = byte(0x01)

	// headerSize = 4 magic + 1 version + 12 nonce
	headerSize = 17
	// minCipherLen = headerSize + 16 (minimum GCM tag size)
	minCipherLen = headerSize + 16
)

// Encrypt cifra plaintext con AES-256-GCM.
// Wire format: [4 magic "ENGM"] [1 version 0x01] [12 nonce] [AES-256-GCM ciphertext+tag]
// key must be exactly 32 bytes (AES-256).
func Encrypt(key, plaintext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: generate nonce: %w", err)
	}

	// Build output: magic(4) + version(1) + nonce(12) + ciphertext+tag
	header := make([]byte, 0, headerSize)
	header = append(header, []byte(magic)...)
	header = append(header, version)
	header = append(header, nonce...)

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	out := make([]byte, 0, len(header)+len(ciphertext))
	out = append(out, header...)
	out = append(out, ciphertext...)

	return out, nil
}

// Decrypt descifra data cifrada con Encrypt.
// Valida magic header, versión, y GCM authentication tag.
// Retorna error descriptivo si cualquier validación falla.
func Decrypt(key, data []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes, got %d", len(key))
	}

	if len(data) < minCipherLen {
		return nil, fmt.Errorf("crypto: data too short (%d bytes, minimum %d)", len(data), minCipherLen)
	}

	// Validate magic header
	if string(data[:4]) != magic {
		return nil, errors.New("invalid encryption header: not an ENGM file")
	}

	// Validate version
	if data[4] != version {
		return nil, fmt.Errorf("crypto: unsupported version 0x%02x (expected 0x%02x)", data[4], version)
	}

	nonce := data[5:17] // bytes 5-16 inclusive (12 bytes)
	ciphertext := data[17:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decryption failed: authentication tag mismatch — possible tampering")
	}

	return plaintext, nil
}
