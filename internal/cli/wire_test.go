package cli

import (
	"context"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/config"
)

func TestBuildBackendLocal(t *testing.T) {
	be, err := buildBackend(context.Background(), config.StorageConfig{Name: "l", Type: "local", Path: t.TempDir()}, nil)
	if err != nil || be.Name() != "local" {
		t.Fatalf("local backend: %v name=%v", err, be)
	}
}

func TestBuildBackendS3ResolvesSecretFromEnvRef(t *testing.T) {
	t.Setenv("S3_SECRET", "theSecret")
	be, err := buildBackend(context.Background(), config.StorageConfig{
		Name: "r2", Type: "s3", Bucket: "b", Region: "auto", AccessKeyID: "AK",
		SecretKeyRef: "env:S3_SECRET",
	}, nil)
	if err != nil {
		t.Fatalf("s3 backend: %v", err)
	}
	if be.Name() != "s3" {
		t.Fatalf("expected s3 backend, got %q", be.Name())
	}
}

func TestBuildBackendS3MissingSecretErrors(t *testing.T) {
	_, err := buildBackend(context.Background(), config.StorageConfig{
		Name: "r2", Type: "s3", Bucket: "b", AccessKeyID: "AK", SecretKeyRef: "env:DOES_NOT_EXIST",
	}, nil)
	if err == nil {
		t.Fatal("expected error when the secret key cannot be resolved")
	}
}
