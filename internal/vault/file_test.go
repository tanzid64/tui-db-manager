package vault

import (
	"bytes"
	"errors"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	p := DefaultKDFParams()
	key := DeriveKey("pw", p)
	blob, err := seal(key, []byte(`{"k":"v"}`), p)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	pt, gotP, err := open(key, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(pt) != `{"k":"v"}` {
		t.Fatalf("plaintext mismatch: %s", pt)
	}
	if !bytes.Equal(gotP.Salt, p.Salt) {
		t.Fatal("KDF salt must survive the round trip")
	}
}

func TestOpenWrongKeyFails(t *testing.T) {
	p := DefaultKDFParams()
	blob, _ := seal(DeriveKey("right", p), []byte("secret"), p)
	_, _, err := open(DeriveKey("wrong", p), blob)
	if !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("expected ErrBadPassphrase, got %v", err)
	}
}

func TestOpenTamperedBlobFails(t *testing.T) {
	p := DefaultKDFParams()
	key := DeriveKey("pw", p)
	blob, _ := seal(key, []byte("secret"), p)
	blob[len(blob)-1] ^= 0xFF // flip a ciphertext byte
	if _, _, err := open(key, blob); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("tampered blob must fail auth, got %v", err)
	}
}
