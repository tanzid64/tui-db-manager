// Package pipeline provides streaming compression and encryption wrappers
// for the backup/restore pipeline.
//
// Backup order:  dump -> compress -> encrypt -> upload
// Restore order: download -> decrypt -> decompress -> restore
package pipeline

import (
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
)

// CompressWriter wraps w with gzip compression. The caller must Close the
// returned writer to flush and finalize the gzip stream.
func CompressWriter(w io.Writer) *gzip.Writer {
	return gzip.NewWriter(w)
}

// DecompressReader wraps r with gzip decompression. The caller must Close the
// returned reader when done.
func DecompressReader(r io.Reader) (*gzip.Reader, error) {
	return gzip.NewReader(r)
}

// EncryptWriter wraps w with age encryption for the given recipient string
// (a bech32-encoded X25519 public key starting with "age1...").
// The caller must Close the returned writer to finalize the encryption.
func EncryptWriter(w io.Writer, recipient string) (io.WriteCloser, error) {
	r, err := age.ParseX25519Recipient(recipient)
	if err != nil {
		return nil, fmt.Errorf("parse age recipient: %w", err)
	}
	return age.Encrypt(w, r)
}

// DecryptReader wraps r with age decryption using the given identity string
// (a bech32-encoded X25519 private key starting with "AGE-SECRET-KEY-1...").
func DecryptReader(r io.Reader, identity string) (io.Reader, error) {
	id, err := age.ParseX25519Identity(identity)
	if err != nil {
		return nil, fmt.Errorf("parse age identity: %w", err)
	}
	return age.Decrypt(r, id)
}

// KeySuffix returns the file extension suffix encoding the pipeline format.
// Examples: ".gz", ".age", ".gz.age" (compress then encrypt).
func KeySuffix(compress, encrypt bool) string {
	var s string
	if compress {
		s += ".gz"
	}
	if encrypt {
		s += ".age"
	}
	return s
}

// ParseKeySuffix detects whether a key has compression and/or encryption
// suffixes and returns the base key stripped of those suffixes.
func ParseKeySuffix(key string) (base string, compressed, encrypted bool) {
	if strings.HasSuffix(key, ".gz.age") {
		return strings.TrimSuffix(key, ".gz.age"), true, true
	}
	if strings.HasSuffix(key, ".age") {
		return strings.TrimSuffix(key, ".age"), false, true
	}
	if strings.HasSuffix(key, ".gz") {
		return strings.TrimSuffix(key, ".gz"), true, false
	}
	return key, false, false
}
