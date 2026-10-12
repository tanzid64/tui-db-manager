package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/config"
)

// A minimal well-formed service-account JSON (field presence only; credential
// parsing is not reached by these tests).
const fakeSA = `{"type":"service_account","project_id":"p","private_key_id":"k","private_key":"-----BEGIN PRIVATE KEY-----\nMIIBVwIBADANBg\n-----END PRIVATE KEY-----\n","client_email":"sa@p.iam.gserviceaccount.com","client_id":"1","token_uri":"https://oauth2.googleapis.com/token"}`

func TestBuildBackendGDriveMissingCredentialErrors(t *testing.T) {
	_, err := buildBackend(context.Background(), config.StorageConfig{
		Name: "gd", Type: "gdrive", FolderID: "f", CredentialsRef: "env:DBK_NO_SUCH_SA",
	}, nil)
	if err == nil {
		t.Fatal("expected error when the credentials ref cannot be resolved")
	}
}

func TestBuildBackendGDriveMissingFolderErrors(t *testing.T) {
	t.Setenv("DBK_SA_JSON", fakeSA)
	_, err := buildBackend(context.Background(), config.StorageConfig{
		Name: "gd", Type: "gdrive", FolderID: "", CredentialsRef: "env:DBK_SA_JSON",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "folder") {
		t.Fatalf("expected a folder_id error, got %v", err)
	}
}
