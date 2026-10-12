//go:build integration

package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

// Requires DBK_IT_GDRIVE_SA (path to a service-account JSON) and
// DBK_IT_GDRIVE_FOLDER (a folder ID the service account can write to).
func TestGDriveRoundTripIntegration(t *testing.T) {
	saPath := os.Getenv("DBK_IT_GDRIVE_SA")
	folder := os.Getenv("DBK_IT_GDRIVE_FOLDER")
	if saPath == "" || folder == "" {
		t.Skip("DBK_IT_GDRIVE_SA / DBK_IT_GDRIVE_FOLDER not set")
	}
	jsonBytes, err := os.ReadFile(saPath)
	if err != nil {
		t.Fatalf("read sa: %v", err)
	}
	ctx := context.Background()
	be, err := NewGDrive(ctx, GDriveOptions{CredentialsJSON: jsonBytes, FolderID: folder})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := be.Test(ctx); err != nil {
		t.Fatalf("test folder: %v", err)
	}
	data := []byte("hello gdrive backup")
	if err := be.Put(ctx, "it/x.sql", bytes.NewReader(data), -1); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := be.Get(ctx, "it/x.sql")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
	if err := be.Delete(ctx, "it/x.sql"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
