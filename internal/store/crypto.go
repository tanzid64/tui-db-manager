package store

import (
	"crypto/rand"
	"errors"

	"github.com/tanzid64/tui-db-manager/internal/vault"
	"golang.org/x/crypto/chacha20poly1305"
)

var ErrBadPassphrase = vault.ErrBadPassphrase

const verifySentinel = "dbk-vault-ok"

// encryptValue encrypts plaintext using XChaCha20-Poly1305 with a random 24-byte nonce.
// Layout: nonce(24) || ciphertext
func encryptValue(key []byte, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := aead.Seal(nil, nonce, plaintext, nil)
	blob := make([]byte, len(nonce)+len(ct))
	copy(blob, nonce)
	copy(blob[len(nonce):], ct)
	return blob, nil
}

// decryptValue decrypts a blob produced by encryptValue using XChaCha20-Poly1305.
func decryptValue(key []byte, blob []byte) ([]byte, error) {
	if len(blob) < chacha20poly1305.NonceSizeX {
		return nil, errors.New("encrypted blob too short")
	}
	nonce := blob[:chacha20poly1305.NonceSizeX]
	ct := blob[chacha20poly1305.NonceSizeX:]
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	pt, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrBadPassphrase
	}
	return pt, nil
}
