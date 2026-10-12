package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGDriveStorageConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := &Config{
		Version: 1,
		Storages: []StorageConfig{{
			Name: "gd", Type: "gdrive", FolderID: "0ABCdef123", CredentialsRef: "file:/etc/dbk/sa.json",
		}},
	}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s, ok := got.Storage("gd")
	if !ok || s.FolderID != "0ABCdef123" || s.CredentialsRef != "file:/etc/dbk/sa.json" {
		t.Fatalf("gdrive config round-trip mismatch: %+v ok=%v", s, ok)
	}
}

func TestGDriveConfigNeverSerializesCredentials(t *testing.T) {
	dir := t.TempDir()
	c := &Config{Version: 1, Storages: []StorageConfig{{
		Name: "gd", Type: "gdrive", FolderID: "x", CredentialsRef: "env:GOOGLE_SA",
	}}}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if strings.Contains(string(b), "private_key") || strings.Contains(string(b), "BEGIN PRIVATE KEY") {
		t.Fatalf("config must not contain service-account key material: %s", b)
	}
}
