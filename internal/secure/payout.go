// Package secure holds small stdlib-only crypto helpers for data that must
// not be stored in plaintext.
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// ErrPayoutKeyNotConfigured means PAYOUT_ENCRYPTION_KEY is missing or is not
// the base64 encoding of exactly 32 bytes (AES-256).
var ErrPayoutKeyNotConfigured = errors.New("payout encryption key not configured: PAYOUT_ENCRYPTION_KEY must be base64 of 32 bytes")

// shortcut: single static key from env, rotate by re-encrypting rows when a KMS lands

type payoutKeyCache struct {
	once sync.Once
	raw  string
	key  []byte
	err  error
}

var (
	payoutKeyMu   sync.Mutex
	payoutKeyLast *payoutKeyCache
)

// payoutKey parses the env key once per distinct value and caches the result.
// Production env never changes; keying the cache by raw value lets tests swap
// keys without a process restart.
func payoutKey() ([]byte, error) {
	raw := os.Getenv("PAYOUT_ENCRYPTION_KEY")

	payoutKeyMu.Lock()
	c := payoutKeyLast
	if c == nil || c.raw != raw {
		c = &payoutKeyCache{raw: raw}
		payoutKeyLast = c
	}
	payoutKeyMu.Unlock()

	c.once.Do(func() {
		key, err := base64.StdEncoding.DecodeString(raw)
		if err != nil || len(key) != 32 {
			c.err = ErrPayoutKeyNotConfigured
			return
		}
		c.key = key
	})
	return c.key, c.err
}

// EncryptAccountNumber encrypts a bank account number with AES-256-GCM and a
// random nonce. Output is base64(nonce||ciphertext).
func EncryptAccountNumber(plain string) (string, error) {
	key, err := payoutKey()
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate payout nonce: %w", err)
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// DecryptAccountNumber reverses EncryptAccountNumber.
func DecryptAccountNumber(encoded string) (string, error) {
	key, err := payoutKey()
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode payout account number: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("payout account number ciphertext too short")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt payout account number: %w", err)
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
