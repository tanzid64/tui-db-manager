package vault

import (
	"bytes"
	"testing"
)

func TestDeriveKeyDeterministicForSameParams(t *testing.T) {
	p := DefaultKDFParams()
	k1 := DeriveKey("correct horse", p)
	k2 := DeriveKey("correct horse", p)
	if len(k1) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(k1))
	}
	if !bytes.Equal(k1, k2) {
		t.Fatal("same passphrase+params must derive the same key")
	}
}

func TestDeriveKeyDiffersByPassphrase(t *testing.T) {
	p := DefaultKDFParams()
	if bytes.Equal(DeriveKey("a", p), DeriveKey("b", p)) {
		t.Fatal("different passphrases must derive different keys")
	}
}

func TestDefaultParamsFreshSalt(t *testing.T) {
	if bytes.Equal(DefaultKDFParams().Salt, DefaultKDFParams().Salt) {
		t.Fatal("each DefaultKDFParams call must produce a fresh salt")
	}
}
