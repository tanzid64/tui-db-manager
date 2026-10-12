# dbk Google Drive Storage (Service Account) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Google Drive storage backend to `dbk` that authenticates with a **service account** and backs up into a configured Drive folder (a folder in a Shared Drive, or a My Drive folder shared with the service account), with no interactive OAuth flow.

**Architecture:** A new `storage.GDrive` implements the existing `storage.Backend` interface. The Google API calls are hidden behind a small internal `driveAPI` interface so the backend's key/name/prefix logic is unit-testable with a fake; the real implementation wraps `google.golang.org/api/drive/v3` and authenticates via a service-account JSON key (resolved from the vault or an `env:`/`file:` ref). Backups are stored as Drive files whose `name` is the full storage key (e.g. `main/main-20261012-030405.sql`) parented to the configured folder, mirroring the local and S3 backends' key semantics. OAuth user-account auth is intentionally out of scope (a possible later addition).

**Tech Stack:** Go 1.24+, `google.golang.org/api/drive/v3`, `google.golang.org/api/option`, `google.golang.org/api/googleapi`, existing storage/config/vault/cli packages.

**Spec:** `docs/superpowers/specs/2026-10-12-dbk-design.md`

## Global Constraints

- Module path: `github.com/tanzid64/tui-db-manager`.
- Go version floor: `go 1.24` (raised from 1.23 by the AWS SDK; recorded in the cloud-storage ledger).
- Binary and root command name: `dbk`.
- The service-account JSON (which contains a private key) is never stored in `config.yaml`; it is referenced via `credentials_ref` (`env:NAME`, `file:/path`, or empty = vault key `storage/<name>/credentials`).
- The service-account private key must never be passed as a process argument or written to logs; wrap the backend with `storage.NewMaskingBackend` using the private-key value.
- Drive API calls must set `SupportsAllDrives(true)` (and List also `IncludeItemsFromAllDrives(true)`) so a folder inside a Shared Drive works.
- Integration tests needing live Google Drive are gated behind `//go:build integration` and are not run by default `go test ./...`.
- Each task ends with a green `go build ./...`, passing non-integration tests, and a git commit using Conventional Commits (author `tanzid64 <tanzid3@gmail.com>`).
- Commit messages must NOT include any `Co-Authored-By` trailer or "Generated with Claude Code" footer.

## Review Focus

- **Key ↔ Drive name mapping round-trips:** a backup written under key `main/main-<ts>.sql` must be found by `Get`/`Stat`/`Delete` under the exact same key, and appear in `List("main/")` with that key — even though Drive has no path semantics and allows duplicate names — pinned in Task 2.
- **Prefix listing is job-scoped:** `List("main/")` must return only objects whose key starts with `main/`, never another job's files that happen to share the folder — pinned in Task 2.
- **Streaming upload of unknown size:** `Put` is given an `io.Reader` with size `-1`; the Drive upload must stream it (resumable media) without needing a length or buffering the whole dump — pinned in Task 3 (real impl) and exercised by the integration test.
- **Service-account key never in config/argv/logs:** the credentials JSON is resolved at runtime from the vault/ref, is never written to `config.yaml`, and the private key never appears in a surfaced error — pinned in Tasks 1 and 4.
- **Missing/invalid credentials or folder:** constructing the backend with bad JSON, or `Test()` against a folder the service account cannot see, must return a clear error, not a nil success or panic — pinned in Tasks 3 and 4.

---

### Task 1: Add Google Drive fields to `StorageConfig`

**Files:**
- Modify: `internal/config/config.go` (`StorageConfig`)
- Test: `internal/config/gdriveconfig_test.go`

**Interfaces:**
- Produces: `StorageConfig` gains `FolderID string` (`yaml:"folder_id,omitempty"`) and `CredentialsRef string` (`yaml:"credentials_ref,omitempty"`). No field ever holds the raw service-account JSON.

- [ ] **Step 1: Write the failing test**

`internal/config/gdriveconfig_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'TestGDrive' -v`
Expected: FAIL (unknown fields FolderID/CredentialsRef).

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, add to the `StorageConfig` struct (after the S3 block, before the closing brace):
```go
	// Google Drive backend (service account).
	FolderID string `yaml:"folder_id,omitempty"` // destination folder (My Drive or Shared Drive)
	// CredentialsRef is "env:NAME", "file:/path", or empty (meaning the vault key
	// "storage/<name>/credentials"). The raw service-account JSON is never stored here.
	CredentialsRef string `yaml:"credentials_ref,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat: add Google Drive fields to storage config"
```

---

### Task 2: GDrive backend logic over a `driveAPI` interface (unit-tested with a fake)

**Files:**
- Create: `internal/storage/gdrive.go`
- Test: `internal/storage/gdrive_test.go`

**Interfaces:**
- Produces:
  - `type driveFile struct { ID, Name string; Size int64; ModTime time.Time }`.
  - `type driveAPI interface { Create(ctx, name, parent string, r io.Reader) error; List(ctx, parent string) ([]driveFile, error); Download(ctx, id string) (io.ReadCloser, error); Delete(ctx, id string) error; Ping(ctx, parent string) error }` (unexported; internal seam).
  - `type GDrive struct { api driveAPI; folderID string }` with `NewGDriveWith(api driveAPI, folderID string) *GDrive` (test/real-shared constructor).
  - `*GDrive` implements `storage.Backend`. Keys are stored verbatim as Drive file names; `Get/Stat/Delete` resolve a key to a file by exact name match within the folder; `List(prefix)` returns objects whose `Name` has the prefix.

- [ ] **Step 1: Write the failing test**

`internal/storage/gdrive_test.go`:
```go
package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// fakeDrive is an in-memory driveAPI: file name -> contents (one parent folder).
type fakeDrive struct {
	files  map[string][]byte
	times  map[string]time.Time
	nextID int
	ids    map[string]string // name -> id
}

func newFakeDrive() *fakeDrive {
	return &fakeDrive{files: map[string][]byte{}, times: map[string]time.Time{}, ids: map[string]string{}}
}

func (f *fakeDrive) Create(ctx context.Context, name, parent string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.files[name] = b
	f.times[name] = time.Now()
	f.nextID++
	f.ids[name] = string(rune('A' + f.nextID))
	return nil
}
func (f *fakeDrive) List(ctx context.Context, parent string) ([]driveFile, error) {
	var out []driveFile
	for name, b := range f.files {
		out = append(out, driveFile{ID: f.ids[name], Name: name, Size: int64(len(b)), ModTime: f.times[name]})
	}
	return out, nil
}
func (f *fakeDrive) Download(ctx context.Context, id string) (io.ReadCloser, error) {
	for name, fid := range f.ids {
		if fid == id {
			return io.NopCloser(bytes.NewReader(f.files[name])), nil
		}
	}
	return nil, io.EOF
}
func (f *fakeDrive) Delete(ctx context.Context, id string) error {
	for name, fid := range f.ids {
		if fid == id {
			delete(f.files, name)
			delete(f.ids, name)
			delete(f.times, name)
			return nil
		}
	}
	return nil
}
func (f *fakeDrive) Ping(ctx context.Context, parent string) error { return nil }

func TestGDrivePutGetListDeleteByKey(t *testing.T) {
	g := NewGDriveWith(newFakeDrive(), "folder123")
	ctx := context.Background()
	if g.Name() != "gdrive" {
		t.Fatalf("name: %q", g.Name())
	}
	data := []byte("dump-bytes")
	if err := g.Put(ctx, "main/main-20261012-030405.sql", bytes.NewReader(data), -1); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Get round-trips by the exact key.
	rc, err := g.Get(ctx, "main/main-20261012-030405.sql")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch: %q", got)
	}
	// Stat returns the key and size.
	obj, err := g.Stat(ctx, "main/main-20261012-030405.sql")
	if err != nil || obj.Key != "main/main-20261012-030405.sql" || obj.Size != int64(len(data)) {
		t.Fatalf("stat: %+v err %v", obj, err)
	}
	// Delete removes it.
	if err := g.Delete(ctx, "main/main-20261012-030405.sql"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := g.Get(ctx, "main/main-20261012-030405.sql"); err == nil {
		t.Fatal("expected error getting a deleted key")
	}
}

func TestGDriveListIsPrefixScoped(t *testing.T) {
	g := NewGDriveWith(newFakeDrive(), "f")
	ctx := context.Background()
	g.Put(ctx, "main/a.sql", bytes.NewReader([]byte("1")), -1)
	g.Put(ctx, "main/b.sql", bytes.NewReader([]byte("2")), -1)
	g.Put(ctx, "other/c.sql", bytes.NewReader([]byte("3")), -1)
	objs, err := g.List(ctx, "main/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("expected 2 objects under main/, got %d: %+v", len(objs), objs)
	}
	for _, o := range objs {
		if o.Key != "main/a.sql" && o.Key != "main/b.sql" {
			t.Fatalf("list leaked a non-main key: %q", o.Key)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestGDrive -v`
Expected: FAIL (undefined NewGDriveWith/driveAPI/driveFile).

- [ ] **Step 3: Write the implementation**

`internal/storage/gdrive.go`:
```go
package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

type driveFile struct {
	ID      string
	Name    string
	Size    int64
	ModTime time.Time
}

// driveAPI is the internal seam over google.golang.org/api/drive. The backend's
// key/name/prefix logic is tested against a fake implementation.
type driveAPI interface {
	Create(ctx context.Context, name, parent string, r io.Reader) error
	List(ctx context.Context, parent string) ([]driveFile, error)
	Download(ctx context.Context, id string) (io.ReadCloser, error)
	Delete(ctx context.Context, id string) error
	Ping(ctx context.Context, parent string) error
}

type GDrive struct {
	api      driveAPI
	folderID string
}

var _ Backend = (*GDrive)(nil)

// NewGDriveWith builds a backend over a given driveAPI (used by both the real
// constructor and tests).
func NewGDriveWith(api driveAPI, folderID string) *GDrive {
	return &GDrive{api: api, folderID: folderID}
}

func (g *GDrive) Name() string { return "gdrive" }

func (g *GDrive) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	return g.api.Create(ctx, key, g.folderID, r)
}

func (g *GDrive) findByName(ctx context.Context, key string) (driveFile, error) {
	files, err := g.api.List(ctx, g.folderID)
	if err != nil {
		return driveFile{}, err
	}
	for _, f := range files {
		if f.Name == key {
			return f, nil
		}
	}
	return driveFile{}, fmt.Errorf("gdrive: object %q not found", key)
}

func (g *GDrive) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	f, err := g.findByName(ctx, key)
	if err != nil {
		return nil, err
	}
	return g.api.Download(ctx, f.ID)
}

func (g *GDrive) List(ctx context.Context, prefix string) ([]Object, error) {
	files, err := g.api.List(ctx, g.folderID)
	if err != nil {
		return nil, err
	}
	var out []Object
	for _, f := range files {
		if strings.HasPrefix(f.Name, prefix) {
			out = append(out, Object{Key: f.Name, Size: f.Size, ModTime: f.ModTime})
		}
	}
	return out, nil
}

func (g *GDrive) Delete(ctx context.Context, key string) error {
	f, err := g.findByName(ctx, key)
	if err != nil {
		return err
	}
	return g.api.Delete(ctx, f.ID)
}

func (g *GDrive) Stat(ctx context.Context, key string) (Object, error) {
	f, err := g.findByName(ctx, key)
	if err != nil {
		return Object{}, err
	}
	return Object{Key: f.Name, Size: f.Size, ModTime: f.ModTime}, nil
}

func (g *GDrive) Test(ctx context.Context) error {
	if g.folderID == "" {
		return fmt.Errorf("gdrive: folder_id is required")
	}
	return g.api.Ping(ctx, g.folderID)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/ -run TestGDrive -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage
git commit -m "feat: Google Drive backend logic over a testable driveAPI seam"
```

---

### Task 3: Real `driveAPI` backed by drive/v3 + service-account constructor

**Files:**
- Create: `internal/storage/gdrive_api.go`
- Test: `internal/storage/gdrive_integration_test.go`

**Interfaces:**
- Consumes: `GDrive`, `driveAPI`, `driveFile` from Task 2.
- Produces:
  - `type GDriveOptions struct { CredentialsJSON []byte; FolderID string }`.
  - `storage.NewGDrive(ctx context.Context, o GDriveOptions) (*GDrive, error)` — builds a `*drive.Service` from the service-account JSON (scope `drive`), wraps it in the real `driveAPI`, and returns `NewGDriveWith(real, o.FolderID)`.
  - `driveService` (unexported) implements `driveAPI` using `*drive.Service` with `SupportsAllDrives(true)` / `IncludeItemsFromAllDrives(true)`.

- [ ] **Step 1: Add the Google API dependencies**

Run:
```bash
go get google.golang.org/api/drive/v3@latest
go get google.golang.org/api/option@latest
go get google.golang.org/api/googleapi@latest
```

- [ ] **Step 2: Write the implementation**

`internal/storage/gdrive_api.go`:
```go
package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type GDriveOptions struct {
	CredentialsJSON []byte
	FolderID        string
}

// NewGDrive builds a Google Drive backend authenticated with a service account.
func NewGDrive(ctx context.Context, o GDriveOptions) (*GDrive, error) {
	if o.FolderID == "" {
		return nil, fmt.Errorf("gdrive: folder_id is required")
	}
	if len(o.CredentialsJSON) == 0 {
		return nil, fmt.Errorf("gdrive: service-account credentials are required")
	}
	srv, err := drive.NewService(ctx,
		option.WithCredentialsJSON(o.CredentialsJSON),
		option.WithScopes(drive.DriveScope),
	)
	if err != nil {
		return nil, fmt.Errorf("gdrive: %w", err)
	}
	return NewGDriveWith(&driveService{srv: srv}, o.FolderID), nil
}

type driveService struct{ srv *drive.Service }

func (d *driveService) Create(ctx context.Context, name, parent string, r io.Reader) error {
	_, err := d.srv.Files.
		Create(&drive.File{Name: name, Parents: []string{parent}}).
		Media(r).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	return err
}

func (d *driveService) List(ctx context.Context, parent string) ([]driveFile, error) {
	var out []driveFile
	q := fmt.Sprintf("'%s' in parents and trashed=false", parent)
	call := d.srv.Files.List().
		Q(q).
		Fields("nextPageToken, files(id,name,size,modifiedTime)").
		SupportsAllDrives(true).
		IncludeItemsFromAllDrives(true).
		PageSize(1000)
	err := call.Pages(ctx, func(page *drive.FileList) error {
		for _, f := range page.Files {
			var mt time.Time
			if f.ModifiedTime != "" {
				mt, _ = time.Parse(time.RFC3339, f.ModifiedTime)
			}
			out = append(out, driveFile{ID: f.Id, Name: f.Name, Size: f.Size, ModTime: mt})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (d *driveService) Download(ctx context.Context, id string) (io.ReadCloser, error) {
	resp, err := d.srv.Files.Get(id).SupportsAllDrives(true).Context(ctx).Download()
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (d *driveService) Delete(ctx context.Context, id string) error {
	return d.srv.Files.Delete(id).SupportsAllDrives(true).Context(ctx).Do()
}

func (d *driveService) Ping(ctx context.Context, parent string) error {
	// Verify the folder is reachable by the service account.
	_, err := d.srv.Files.Get(parent).SupportsAllDrives(true).Fields("id").Context(ctx).Do()
	if ae, ok := err.(*googleapi.Error); ok && ae.Code == 404 {
		return fmt.Errorf("gdrive: folder %q not found or not shared with the service account", parent)
	}
	return err
}
```

- [ ] **Step 3: Verify it compiles and satisfies the interface**

Run: `go build ./... `
Expected: builds. The `var _ Backend = (*GDrive)(nil)` from Task 2 plus successful compilation confirms `driveService` satisfies `driveAPI`.

- [ ] **Step 4: Write the gated integration test**

`internal/storage/gdrive_integration_test.go`:
```go
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
	json, err := os.ReadFile(saPath)
	if err != nil {
		t.Fatalf("read sa: %v", err)
	}
	ctx := context.Background()
	be, err := NewGDrive(ctx, GDriveOptions{CredentialsJSON: json, FolderID: folder})
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
```

- [ ] **Step 5: Verify the integration test compiles, run the default suite**

Run: `go vet -tags integration ./internal/storage/ && go test ./internal/storage/ && go build ./...`
Expected: vet clean; unit tests pass; builds.

- [ ] **Step 6: Commit**

```bash
git add internal/storage go.mod go.sum
git commit -m "feat: real Google Drive driveAPI via drive/v3 and service-account auth"
```

---

### Task 4: Wire GDrive into the CLI (`buildBackend` + credential resolution + masking)

**Files:**
- Modify: `internal/cli/wire.go` (`buildBackend` handles `type: gdrive`)
- Test: `internal/cli/gdrive_wire_test.go`

**Interfaces:**
- Consumes: `storage.NewGDrive`, `config.StorageConfig`, `secretref.Resolve`, `vault.Store`, `storage.NewMaskingBackend`.
- Produces: `buildBackend` (same signature as the cloud-storage plan: `(ctx, sc, v)`) builds a GDrive backend for `type: gdrive`, resolving the service-account JSON from `sc.CredentialsRef` (or vault key `storage/<name>/credentials`), and wraps it with `NewMaskingBackend` keyed on the JSON's `private_key` value so the key can never surface in an error.

- [ ] **Step 1: Write the failing test**

`internal/cli/gdrive_wire_test.go`:
```go
package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/config"
)

// A syntactically valid service-account JSON is required by NewGDrive only for
// field presence here; drive.NewService does not call the network at construct
// time, but it does parse credentials, so use a minimal well-formed JSON.
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestBuildBackendGDrive -v`
Expected: FAIL (gdrive case not handled).

- [ ] **Step 3: Write the implementation**

In `internal/cli/wire.go`, add a `case "gdrive":` to `buildBackend` (before `default:`):
```go
	case "gdrive":
		jsonCreds, err := resolveGDriveCredentials(sc, v)
		if err != nil {
			return nil, err
		}
		be, err := storage.NewGDrive(ctx, storage.GDriveOptions{
			CredentialsJSON: []byte(jsonCreds), FolderID: sc.FolderID,
		})
		if err != nil {
			return nil, err
		}
		return storage.NewMaskingBackend(be, extractPrivateKey(jsonCreds)), nil
```
Add these helpers to `internal/cli/wire.go`:
```go
func resolveGDriveCredentials(sc config.StorageConfig, v *vault.Store) (string, error) {
	var lookup func(string) (string, bool)
	if v != nil {
		lookup = v.Get
	}
	ref := sc.CredentialsRef
	if ref == "" {
		ref = "storage/" + sc.Name + "/credentials"
	}
	return secretref.Resolve(ref, lookup)
}

// extractPrivateKey pulls the private_key value out of a service-account JSON so
// it can be masked from errors. Returns "" if not found (masking then no-ops).
func extractPrivateKey(saJSON string) string {
	var m struct {
		PrivateKey string `json:"private_key"`
	}
	if err := json.Unmarshal([]byte(saJSON), &m); err != nil {
		return ""
	}
	return m.PrivateKey
}
```
Add `"encoding/json"` to the imports of `wire.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v && go build ./...`
Expected: PASS (the missing-folder test reaches `NewGDrive` and gets the folder_id error; the missing-credential test fails at resolution).

- [ ] **Step 5: Commit**

```bash
git add internal/cli
git commit -m "feat: wire Google Drive backend into CLI with masked service-account creds"
```

---

### Task 5: Documentation

**Files:**
- Modify: `docs/cloud-storage.md` (add a Google Drive section)

**Interfaces:** none (docs only).

- [ ] **Step 1: Add the Google Drive section**

Append to `docs/cloud-storage.md`:
```markdown
## Google Drive (service account)

dbk uses a Google Cloud **service account** to back up to Google Drive — no
interactive login, ideal for servers and cron.

Setup:
1. In Google Cloud Console, create a service account and a JSON key; enable the
   Google Drive API for the project.
2. Decide where backups go:
   - **Shared Drive (recommended):** add the service account's email
     (`...@...iam.gserviceaccount.com`) as a member with Content Manager access,
     and copy the destination folder's ID from its URL.
   - **My Drive folder:** share the folder with the service account's email
     (Editor), and copy the folder ID. (Note: files are owned by the service
     account and count against its quota; a Shared Drive avoids this.)
3. Store the JSON key in the vault and configure the storage:

        dbk secret set storage/gd/credentials --stdin < service-account.json

        storages:
          - name: gd
            type: gdrive
            folder_id: <destination-folder-id>

   Or reference the file directly with `credentials_ref: file:/etc/dbk/sa.json`.

Test it:

        dbk test <job>    # (DB connectivity; storage verification is planned)
```

- [ ] **Step 2: Verify the whole suite and commit**

Run: `go test ./... && go vet ./... && go vet -tags integration ./...`
Expected: all PASS / clean.

```bash
git add docs/cloud-storage.md
git commit -m "docs: Google Drive (service account) storage setup"
```

---

## Self-Review Notes

- **Spec coverage (this plan's slice):** Google Drive storage backend via service account (Tasks 2–4), satisfying the spec's Google Drive destination; credentials kept out of config and resolved from vault/ref with private-key masking (Tasks 1, 4). The spec mentioned OAuth; per the approved design decision this plan uses a service account instead, with OAuth user-account auth deferred. Not in scope: compression, at-rest encryption, notifications, scheduling, export/import, TUI, installers.
- **Placeholder scan:** no TBD/TODO; every code step carries real code.
- **Type consistency:** `driveAPI`/`driveFile`/`GDrive`/`NewGDriveWith` defined in Task 2 are consumed unchanged by Task 3's `driveService` and `NewGDrive`; `buildBackend`'s signature matches the one established in the cloud-storage plan; `GDriveOptions` fields match what Task 4 passes. `var _ Backend = (*GDrive)(nil)` guards the interface.
- **Review Focus coverage:** key↔name round-trip and job-scoped prefix listing (Task 2 `TestGDrivePutGetListDeleteByKey`, `TestGDriveListIsPrefixScoped`); streaming unknown-size upload (Task 3 `Media(r)` resumable upload, exercised by the integration round-trip with `size=-1`); credentials never in config/argv/logs (Task 1 `TestGDriveConfigNeverSerializesCredentials`, Task 4 masking with the extracted private key); missing/invalid credentials or folder (Task 3 `Ping` 404 handling + `NewGDrive` validation, Task 4 `TestBuildBackendGDriveMissingCredentialErrors`/`TestBuildBackendGDriveMissingFolderErrors`).
```
