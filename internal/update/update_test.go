package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1, v2 string
		want   int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"v1.0.0", "1.0.1", -1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.0.3", "v1.0.2", 1},
		{"1.0.2", "1.0.3", -1},
		{"1.0.0", "dev", 1},
		{"dev", "1.0.0", -1},
		{"dev", "dev", 0},
	}

	for _, tt := range tests {
		got := CompareVersions(tt.v1, tt.v2)
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.v1, tt.v2, got, tt.want)
		}
	}
}

func TestParseChecksum(t *testing.T) {
	input := `d313a44424219c4dcddf234419daea1f1661bddab77247670107e98e959ddb58  dbk_1.0.3_linux_amd64.tar.gz
046e288341277fd19e769c86c0a2c5eb966ed03ee76f8f7cd60d45e6f040546f  dbk_1.0.3_linux_arm64.tar.gz
`
	got := ParseChecksum(input, "dbk_1.0.3_linux_amd64.tar.gz")
	want := "d313a44424219c4dcddf234419daea1f1661bddab77247670107e98e959ddb58"
	if got != want {
		t.Fatalf("ParseChecksum = %q, want %q", got, want)
	}

	missing := ParseChecksum(input, "dbk_1.0.3_darwin_amd64.tar.gz")
	if missing != "" {
		t.Fatalf("expected empty for missing file, got %q", missing)
	}
}

func TestApplyUpdate(t *testing.T) {
	// Create a fake new binary payload
	newBinaryContent := []byte("#!/bin/sh\necho 'dbk v2.0.0'\n")

	// Pack it into a tar.gz archive
	var archiveBuf bytes.Buffer
	gzw := gzip.NewWriter(&archiveBuf)
	tw := tar.NewWriter(gzw)

	hdr := &tar.Header{
		Name: "dbk",
		Mode: 0o755,
		Size: int64(len(newBinaryContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tw.WriteHeader: %v", err)
	}
	if _, err := tw.Write(newBinaryContent); err != nil {
		t.Fatalf("tw.Write: %v", err)
	}
	tw.Close()
	gzw.Close()

	archiveBytes := archiveBuf.Bytes()
	h := sha256.Sum256(archiveBytes)
	archiveHash := hex.EncodeToString(h[:])

	archiveName := fmt.Sprintf("dbk_2.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	checksumsContent := fmt.Sprintf("%s  %s\n", archiveHash, archiveName)

	// Mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + archiveName:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(archiveBytes)
		case "/checksums.txt":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(checksumsContent))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rel := &Release{
		TagName: "v2.0.0",
		Assets: []Asset{
			{
				Name:               archiveName,
				BrowserDownloadURL: server.URL + "/" + archiveName,
				Size:               int64(len(archiveBytes)),
			},
			{
				Name:               "checksums.txt",
				BrowserDownloadURL: server.URL + "/checksums.txt",
			},
		},
	}

	// Create a dummy target binary file
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "dbk")
	if err := os.WriteFile(targetPath, []byte("old-binary-content"), 0o755); err != nil {
		t.Fatalf("create targetPath: %v", err)
	}

	// Apply update
	if err := ApplyUpdate(context.Background(), rel, targetPath); err != nil {
		t.Fatalf("ApplyUpdate failed: %v", err)
	}

	// Verify content was updated
	updated, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read updated binary: %v", err)
	}
	if !bytes.Equal(updated, newBinaryContent) {
		t.Fatalf("unexpected updated content: %q", string(updated))
	}
}
