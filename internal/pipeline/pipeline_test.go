package pipeline

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestCompressDecompressRoundTrip(t *testing.T) {
	original := "hello world, this is a test of gzip compression in the pipeline"
	var buf bytes.Buffer
	gw := CompressWriter(&buf)
	if _, err := io.WriteString(gw, original); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("compressed output is empty")
	}
	gr, err := DecompressReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("round-trip mismatch: got %q", got)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	original := "secret backup data"
	var buf bytes.Buffer
	ew, err := EncryptWriter(&buf, id.Recipient().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(ew, original); err != nil {
		t.Fatal(err)
	}
	if err := ew.Close(); err != nil {
		t.Fatal(err)
	}
	// Ciphertext should differ from plaintext.
	if buf.String() == original {
		t.Fatal("data was not encrypted")
	}
	dr, err := DecryptReader(&buf, id.String())
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(dr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("round-trip mismatch: got %q", got)
	}
}

func TestCompressThenEncryptRoundTrip(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	original := strings.Repeat("backup row data\n", 100)

	// Compress then encrypt (backup order).
	var buf bytes.Buffer
	ew, err := EncryptWriter(&buf, id.Recipient().String())
	if err != nil {
		t.Fatal(err)
	}
	gw := CompressWriter(ew)
	if _, err := io.WriteString(gw, original); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ew.Close(); err != nil {
		t.Fatal(err)
	}

	// Decrypt then decompress (restore order).
	dr, err := DecryptReader(&buf, id.String())
	if err != nil {
		t.Fatal(err)
	}
	gr, err := DecompressReader(dr)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("full pipeline round-trip mismatch: got %d bytes, want %d", len(got), len(original))
	}
}

func TestEncryptWriterBadRecipient(t *testing.T) {
	var buf bytes.Buffer
	_, err := EncryptWriter(&buf, "not-a-valid-recipient")
	if err == nil {
		t.Fatal("expected error for invalid recipient")
	}
}

func TestDecryptReaderBadIdentity(t *testing.T) {
	_, err := DecryptReader(strings.NewReader("ciphertext"), "not-a-valid-identity")
	if err == nil {
		t.Fatal("expected error for invalid identity")
	}
}

func TestKeySuffix(t *testing.T) {
	tests := []struct {
		compress, encrypt bool
		want              string
	}{
		{false, false, ""},
		{true, false, ".gz"},
		{false, true, ".age"},
		{true, true, ".gz.age"},
	}
	for _, tt := range tests {
		got := KeySuffix(tt.compress, tt.encrypt)
		if got != tt.want {
			t.Errorf("KeySuffix(%v,%v) = %q, want %q", tt.compress, tt.encrypt, got, tt.want)
		}
	}
}

func TestParseKeySuffix(t *testing.T) {
	tests := []struct {
		key                       string
		wantBase                  string
		wantCompress, wantEncrypt bool
	}{
		{"main/main-20261012-030405.sql", "main/main-20261012-030405.sql", false, false},
		{"main/main-20261012-030405.sql.gz", "main/main-20261012-030405.sql", true, false},
		{"main/main-20261012-030405.sql.age", "main/main-20261012-030405.sql", false, true},
		{"main/main-20261012-030405.sql.gz.age", "main/main-20261012-030405.sql", true, true},
	}
	for _, tt := range tests {
		base, c, e := ParseKeySuffix(tt.key)
		if base != tt.wantBase || c != tt.wantCompress || e != tt.wantEncrypt {
			t.Errorf("ParseKeySuffix(%q) = (%q,%v,%v) want (%q,%v,%v)",
				tt.key, base, c, e, tt.wantBase, tt.wantCompress, tt.wantEncrypt)
		}
	}
}
