package vault

import (
	"crypto/rand"

	"golang.org/x/crypto/argon2"
)

type KDFParams struct {
	Salt    []byte `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
}

func DefaultKDFParams() KDFParams {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic("vault: cannot read random salt: " + err.Error())
	}
	return KDFParams{Salt: salt, Time: 3, Memory: 64 * 1024, Threads: 4}
}

// DeriveKey returns a 32-byte key for XChaCha20-Poly1305.
func DeriveKey(passphrase string, p KDFParams) []byte {
	return argon2.IDKey([]byte(passphrase), p.Salt, p.Time, p.Memory, p.Threads, 32)
}
