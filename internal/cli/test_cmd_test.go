package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTestCommandWithStorageAndNotifier(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfgDir := t.TempDir()
	storageDir := t.TempDir()

	cfgYAML := `version: 1
storages:
  - name: local-store
    type: local
    path: ` + storageDir + `
notifiers:
  - name: test-hook
    type: webhook
    url: ` + srv.URL + `
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)

	// Test storage target
	root.SetArgs([]string{"--config", cfgDir, "test", "local-store"})
	if err := root.Execute(); err != nil {
		t.Fatalf("test storage failed: %v", err)
	}
	if !strings.Contains(out.String(), "storage \"local-store\" connection OK") {
		t.Fatalf("unexpected output for storage: %s", out.String())
	}

	// Test notifier target
	out.Reset()
	root.SetArgs([]string{"--config", cfgDir, "test", "test-hook"})
	if err := root.Execute(); err != nil {
		t.Fatalf("test notifier failed: %v", err)
	}
	if !strings.Contains(out.String(), "notifier \"test-hook\" test OK") {
		t.Fatalf("unexpected output for notifier: %s", out.String())
	}

	// Test unknown target
	out.Reset()
	root.SetArgs([]string{"--config", cfgDir, "test", "does-not-exist"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected error for non-existent target")
	}
}
