package crypto_test

import (
	"bytes"
	"testing"

	"github.com/Antonio-Escajeda/engram-obsidian/internal/crypto"
)

// makeKey returns a deterministic 32-byte key for testing.
func makeKey(b byte) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = b
	}
	return key
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key := makeKey(0x42)
	plaintext := []byte("hello engram — this is test data 🔒")

	ciphertext, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: unexpected error: %v", err)
	}

	recovered, err := crypto.Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: unexpected error: %v", err)
	}

	if !bytes.Equal(recovered, plaintext) {
		t.Errorf("roundtrip mismatch: got %q, want %q", recovered, plaintext)
	}
}

func TestEncryptDecryptEmptyPlaintext(t *testing.T) {
	key := makeKey(0x01)
	plaintext := []byte{}

	ciphertext, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt empty: unexpected error: %v", err)
	}

	recovered, err := crypto.Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt empty: unexpected error: %v", err)
	}

	if !bytes.Equal(recovered, plaintext) {
		t.Errorf("roundtrip mismatch on empty plaintext: got %v, want %v", recovered, plaintext)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	key := makeKey(0xAA)
	wrongKey := makeKey(0xBB)

	ciphertext, err := crypto.Encrypt(key, []byte("secret data"))
	if err != nil {
		t.Fatalf("Encrypt: unexpected error: %v", err)
	}

	_, err = crypto.Decrypt(wrongKey, ciphertext)
	if err == nil {
		t.Fatal("Decrypt with wrong key: expected error, got nil")
	}
}

func TestDecryptTruncated(t *testing.T) {
	key := makeKey(0x11)

	// Data shorter than minCipherLen (17 + 16 = 33 bytes)
	truncated := make([]byte, 20)

	_, err := crypto.Decrypt(key, truncated)
	if err == nil {
		t.Fatal("Decrypt truncated data: expected error, got nil")
	}
}

func TestDecryptBadMagic(t *testing.T) {
	key := makeKey(0x22)

	// Build a 40-byte blob that starts with wrong magic but is long enough
	data := make([]byte, 40)
	data[0] = 'X'
	data[1] = 'X'
	data[2] = 'X'
	data[3] = 'X'
	data[4] = 0x01 // version

	_, err := crypto.Decrypt(key, data)
	if err == nil {
		t.Fatal("Decrypt bad magic: expected error, got nil")
	}

	want := "invalid encryption header: not an ENGM file"
	if err.Error() != want {
		t.Errorf("error message mismatch: got %q, want %q", err.Error(), want)
	}
}

func TestDecryptTampering(t *testing.T) {
	key := makeKey(0x33)
	plaintext := []byte("important data that must not be tampered with")

	ciphertext, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: unexpected error: %v", err)
	}

	// Flip a bit in the ciphertext (last byte, which is in the GCM tag area)
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 0xFF

	_, err = crypto.Decrypt(key, tampered)
	if err == nil {
		t.Fatal("Decrypt tampered data: expected error, got nil")
	}
}

func TestEncryptProducesUniqueNonces(t *testing.T) {
	key := makeKey(0x44)
	plaintext := []byte("same plaintext every time")

	out1, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt #1: %v", err)
	}
	out2, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt #2: %v", err)
	}

	// Nonce is at bytes 5–16 (12 bytes). Two encryptions of the same plaintext
	// must produce different outputs (overwhelming probability with crypto/rand).
	if bytes.Equal(out1, out2) {
		t.Error("two successive Encrypt calls produced identical output — nonce must be random")
	}

	// Explicitly check the nonce region (bytes 5–16 inclusive).
	nonce1 := out1[5:17]
	nonce2 := out2[5:17]
	if bytes.Equal(nonce1, nonce2) {
		t.Error("nonces are identical across two Encrypt calls — expected random nonces")
	}
}

func TestEncryptWrongKeySize(t *testing.T) {
	shortKey := []byte("too-short")
	_, err := crypto.Encrypt(shortKey, []byte("data"))
	if err == nil {
		t.Fatal("Encrypt with short key: expected error, got nil")
	}
}

func TestDecryptWrongKeySize(t *testing.T) {
	shortKey := []byte("too-short")
	_, err := crypto.Decrypt(shortKey, make([]byte, 40))
	if err == nil {
		t.Fatal("Decrypt with short key: expected error, got nil")
	}
}

func TestWireFormat(t *testing.T) {
	key := makeKey(0x55)
	plaintext := []byte("wire format test")

	out, err := crypto.Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Validate magic bytes 0–3
	if string(out[0:4]) != "ENGM" {
		t.Errorf("magic bytes mismatch: got %q, want %q", string(out[0:4]), "ENGM")
	}

	// Validate version byte at offset 4
	if out[4] != 0x01 {
		t.Errorf("version byte mismatch: got 0x%02x, want 0x01", out[4])
	}

	// Output must be at least headerSize(17) + tag(16) = 33 bytes
	if len(out) < 33 {
		t.Errorf("output too short: %d bytes", len(out))
	}
}
