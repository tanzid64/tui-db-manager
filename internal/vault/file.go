package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

var ErrBadPassphrase = errors.New("invalid passphrase or corrupted vault")

var magic = []byte("DBKV")

const formatVersion = 1

// header is the authenticated, plaintext-readable file preamble.
type header struct {
	Version int       `json:"v"`
	KDF     KDFParams `json:"kdf"`
}

// seal layout: magic(4) | hlen(4, big-endian) | header-json | nonce(24) | ciphertext.
// The header JSON is used as AEAD additional data so KDF params cannot be swapped.
func seal(key, plaintext []byte, p KDFParams) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	hb, err := json.Marshal(header{Version: formatVersion, KDF: p})
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := aead.Seal(nil, nonce, plaintext, hb)

	var buf bytes.Buffer
	buf.Write(magic)
	buf.Write(uint32be(len(hb)))
	buf.Write(hb)
	buf.Write(nonce)
	buf.Write(ct)
	return buf.Bytes(), nil
}

func open(key, blob []byte) ([]byte, KDFParams, error) {
	if len(blob) < 8 || !bytes.Equal(blob[:4], magic) {
		return nil, KDFParams{}, fmt.Errorf("not a dbk vault file")
	}
	hlen := int(uint32(blob[4])<<24 | uint32(blob[5])<<16 | uint32(blob[6])<<8 | uint32(blob[7]))
	rest := blob[8:]
	if hlen < 0 || hlen > len(rest) {
		return nil, KDFParams{}, fmt.Errorf("corrupt vault header")
	}
	hb := rest[:hlen]
	var h header
	if err := json.Unmarshal(hb, &h); err != nil {
		return nil, KDFParams{}, fmt.Errorf("corrupt vault header: %w", err)
	}
	body := rest[hlen:]
	if len(body) < chacha20poly1305.NonceSizeX {
		return nil, KDFParams{}, fmt.Errorf("corrupt vault body")
	}
	nonce := body[:chacha20poly1305.NonceSizeX]
	ct := body[chacha20poly1305.NonceSizeX:]
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, KDFParams{}, err
	}
	pt, err := aead.Open(nil, nonce, ct, hb)
	if err != nil {
		return nil, KDFParams{}, ErrBadPassphrase
	}
	return pt, h.KDF, nil
}

func uint32be(n int) []byte {
	return []byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
}

// peekParams reads the (authenticated-on-open) KDF header so the caller can
// derive the key. The values are not trusted until open() verifies the AEAD tag.
func peekParams(blob []byte) (KDFParams, error) {
	if len(blob) < 8 || !bytes.Equal(blob[:4], magic) {
		return KDFParams{}, fmt.Errorf("not a dbk vault file")
	}
	hlen := int(uint32(blob[4])<<24 | uint32(blob[5])<<16 | uint32(blob[6])<<8 | uint32(blob[7]))
	rest := blob[8:]
	if hlen < 0 || hlen > len(rest) {
		return KDFParams{}, fmt.Errorf("corrupt vault header")
	}
	var h header
	if err := json.Unmarshal(rest[:hlen], &h); err != nil {
		return KDFParams{}, fmt.Errorf("corrupt vault header: %w", err)
	}
	return h.KDF, nil
}
