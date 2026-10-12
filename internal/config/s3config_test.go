package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS3StorageConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := &Config{
		Version: 1,
		Storages: []StorageConfig{{
			Name: "r2", Type: "s3", Bucket: "backups", Region: "auto",
			Endpoint: "https://acct.r2.cloudflarestorage.com", Prefix: "dbk/",
			AccessKeyID: "AKIAEXAMPLE", ForcePathStyle: true, SecretKeyRef: "",
		}},
	}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	s, ok := got.Storage("r2")
	if !ok || s.Bucket != "backups" || s.Endpoint == "" || !s.ForcePathStyle {
		t.Fatalf("s3 config round-trip mismatch: %+v ok=%v", s, ok)
	}
}

func TestStorageConfigNeverSerializesASecretKey(t *testing.T) {
	// There must be no field that holds the raw secret access key.
	dir := t.TempDir()
	c := &Config{Version: 1, Storages: []StorageConfig{{
		Name: "s3", Type: "s3", Bucket: "b", AccessKeyID: "AKIA", SecretKeyRef: "env:S3_SECRET",
	}}}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if strings.Contains(strings.ToLower(string(b)), "secret_access_key") {
		t.Fatalf("config must not contain a raw secret key field: %s", b)
	}
}
