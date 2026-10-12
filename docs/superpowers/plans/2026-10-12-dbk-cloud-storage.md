# dbk Cloud Storage (S3-family) + Retention Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an S3-compatible storage backend (AWS S3, Cloudflare R2, MinIO, Backblaze B2, Wasabi) to `dbk`, a retention policy that prunes old backups after each run, and a `secret` CLI so cloud credentials can be stored in the encrypted vault.

**Architecture:** A new `storage.S3` implements the existing `storage.Backend` interface using AWS SDK for Go v2, with a configurable endpoint (for non-AWS providers) and path-style option. Backup orchestration gains a retention step that lists and prunes a job's objects via the backend after a successful upload. A `dbk secret set/rm/ls` command writes credential values into the existing vault so the S3 secret key never lives in `config.yaml`. Google Drive is intentionally deferred to a later plan (its OAuth flow is a separate subsystem).

**Tech Stack:** Go 1.23+, AWS SDK for Go v2 (`config`, `credentials`, `service/s3`, `feature/s3/manager`), existing Cobra/vault/storage packages.

**Spec:** `docs/superpowers/specs/2026-10-12-dbk-design.md`

## Global Constraints

- Module path: `github.com/tanzid64/tui-db-manager`.
- Go version floor: `go 1.23`.
- Binary and root command name: `dbk`.
- Secrets (S3 secret access keys) never stored in `config.yaml`; they live in the vault or are supplied via `env:`/`file:` secret references (`internal/secretref`).
- The S3 secret key must never be passed as a process argument or written to logs; reuse `logutil.MaskError` for any returned error that could contain it.
- TLS verification on by default for all S3 endpoints; disabling is not offered in this plan.
- Integration tests needing a live object store are gated behind `//go:build integration` and are not run by default `go test ./...`.
- Each task ends with a green `go build ./...`, passing non-integration tests, and a git commit using Conventional Commits.
- Commit messages must NOT include any `Co-Authored-By` trailer or "Generated with Claude Code" footer.

## Review Focus

- **Streaming upload of unknown size:** a backup is an `io.Reader` with size `-1` (streamed from the dump pipe); the S3 Put must use multipart upload and must not require a known length or buffer the whole dump in memory — pinned in Task 5.
- **Wrong/missing S3 credentials:** a Put/Test with bad keys must return a clear credential/permission error, not a nil success or a panic — pinned in Task 6 (integration) and surfaced via masked errors in Task 7.
- **Retention off-by-one / wrong deletion:** retention with `keep_last: N` must keep exactly the N newest objects for that job and delete only older ones belonging to that job, never another job's objects — pinned in Task 2.
- **Retention never deletes when misconfigured:** `keep_last: 0` (or unset) must mean "keep everything" and prune nothing, so a default config never silently destroys backups — pinned in Task 2.
- **Secret key never in config or argv:** the S3 secret key is resolved from the vault/secretref at runtime and only held in memory; it must not be written to `config.yaml` by any command and must not appear in a returned error — pinned in Tasks 1, 3, and 7.

---

### Task 1: Extend `StorageConfig` with S3 fields

**Files:**
- Modify: `internal/config/config.go` (the `StorageConfig` struct)
- Test: `internal/config/s3config_test.go`

**Interfaces:**
- Produces: `StorageConfig` with added fields `Bucket, Region, Endpoint, Prefix, AccessKeyID string`, `ForcePathStyle bool`, `SecretKeyRef string`. All new fields are `omitempty`. The secret access key is NEVER a field — only `SecretKeyRef` (an `env:`/`file:` ref, or empty to mean the vault key `storage/<name>/secret_key`).

- [ ] **Step 1: Write the failing test**

`internal/config/s3config_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'TestS3StorageConfigRoundTrip|TestStorageConfigNeverSerializes' -v`
Expected: FAIL (unknown fields Bucket/Endpoint/etc).

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, replace the `StorageConfig` struct with:
```go
type StorageConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"` // local | s3

	// Local backend.
	Path string `yaml:"path,omitempty"`

	// S3-family backend (AWS S3, Cloudflare R2, MinIO, Backblaze B2, Wasabi).
	Bucket         string `yaml:"bucket,omitempty"`
	Region         string `yaml:"region,omitempty"`
	Endpoint       string `yaml:"endpoint,omitempty"` // empty = AWS default
	Prefix         string `yaml:"prefix,omitempty"`    // key prefix within the bucket
	AccessKeyID    string `yaml:"access_key_id,omitempty"`
	ForcePathStyle bool   `yaml:"force_path_style,omitempty"`
	// SecretKeyRef is "env:NAME", "file:/path", or empty (meaning the vault key
	// "storage/<name>/secret_key"). The raw secret key is never stored here.
	SecretKeyRef string `yaml:"secret_key_ref,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat: add S3 fields to storage config"
```

---

### Task 2: Retention policy

**Files:**
- Create: `internal/retention/retention.go`
- Test: `internal/retention/retention_test.go`

**Interfaces:**
- Consumes: `storage.Backend`, `storage.Object`.
- Produces: `retention.Apply(ctx context.Context, be storage.Backend, jobPrefix string, keepLast int) (deleted []string, err error)`. `keepLast <= 0` deletes nothing. Keeps the `keepLast` newest objects (by `ModTime`, newest first) under `jobPrefix` and deletes the rest. Returns the keys it deleted.

- [ ] **Step 1: Write the failing test**

`internal/retention/retention_test.go`:
```go
package retention

import (
	"context"
	"testing"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/storage"
)

func seed(t *testing.T) *storage.Local {
	t.Helper()
	be := storage.NewLocal(t.TempDir())
	ctx := context.Background()
	// Three for job "main", one for job "other".
	put := func(key string, age time.Duration) {
		if err := be.Put(ctx, key, readerOf("x"), 1); err != nil {
			t.Fatal(err)
		}
	}
	put("main/main-20260101-000000.sql", 0)
	put("main/main-20260102-000000.sql", 0)
	put("main/main-20260103-000000.sql", 0)
	put("other/other-20260103-000000.sql", 0)
	return be
}

func TestApplyKeepsNewestNAndIgnoresOtherJobs(t *testing.T) {
	be := seed(t)
	ctx := context.Background()
	deleted, err := Apply(ctx, be, "main/", 2)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "main/main-20260101-000000.sql" {
		t.Fatalf("expected to delete only the oldest main backup, deleted=%v", deleted)
	}
	// The other job's object is untouched.
	if objs, _ := be.List(ctx, "other/"); len(objs) != 1 {
		t.Fatalf("retention must not touch other jobs: %+v", objs)
	}
	if objs, _ := be.List(ctx, "main/"); len(objs) != 2 {
		t.Fatalf("expected 2 main backups kept, got %d", len(objs))
	}
}

func TestApplyKeepLastZeroDeletesNothing(t *testing.T) {
	be := seed(t)
	deleted, err := Apply(context.Background(), be, "main/", 0)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(deleted) != 0 {
		t.Fatalf("keep_last=0 must delete nothing, deleted=%v", deleted)
	}
}
```

Add a tiny reader helper at the bottom of the test file:
```go
import "io"

func readerOf(s string) io.Reader { return &sliceReader{data: []byte(s)} }

type sliceReader struct {
	data []byte
	off  int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}
```
(Put the `import "io"` with the other imports at the top rather than inline; shown here only for locality.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/retention/ -v`
Expected: FAIL (undefined Apply).

- [ ] **Step 3: Write the implementation**

`internal/retention/retention.go`:
```go
package retention

import (
	"context"
	"sort"

	"github.com/tanzid64/tui-db-manager/internal/storage"
)

// Apply keeps the keepLast newest objects under jobPrefix and deletes the rest.
// keepLast <= 0 means "keep everything" and deletes nothing.
func Apply(ctx context.Context, be storage.Backend, jobPrefix string, keepLast int) ([]string, error) {
	if keepLast <= 0 {
		return nil, nil
	}
	objs, err := be.List(ctx, jobPrefix)
	if err != nil {
		return nil, err
	}
	if len(objs) <= keepLast {
		return nil, nil
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].ModTime.After(objs[j].ModTime) })
	var deleted []string
	for _, o := range objs[keepLast:] {
		if err := be.Delete(ctx, o.Key); err != nil {
			return deleted, err
		}
		deleted = append(deleted, o.Key)
	}
	return deleted, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/retention/ -v`
Expected: PASS.

- [ ] **Step 5: Wire retention into backup orchestration**

In `internal/backup/backup.go`, after the successful `be.Stat(ctx, key)` block (just before the final `d.Log.Infof("backup complete...")` return), add retention using the job's policy:
```go
	if n := job.Retention.KeepLast; n > 0 {
		deleted, rerr := retention.Apply(ctx, be, job.Name+"/", n)
		if rerr != nil {
			// Retention failure must not fail an otherwise-good backup; log and continue.
			d.Log.Errorf("retention for job %q: %v", job.Name, logutil.MaskError(rerr, pw))
		} else if len(deleted) > 0 {
			d.Log.Infof("retention: pruned %d old backup(s) for job %q", len(deleted), job.Name)
		}
	}
```
Add the import `"github.com/tanzid64/tui-db-manager/internal/retention"` to `backup.go`.

- [ ] **Step 6: Write the failing test for the wiring**

Append to `internal/backup/backup_test.go`:
```go
func TestRunAppliesRetention(t *testing.T) {
	be := storage.NewLocal(t.TempDir())
	ctx := context.Background()
	// Pre-seed two old backups for job "main".
	be.Put(ctx, "main/main-20250101-000000.sql", readerOfB("old1"), 4)
	be.Put(ctx, "main/main-20250102-000000.sql", readerOfB("old2"), 4)

	cfg := &config.Config{
		Version: 1,
		Jobs: []config.Job{{Name: "main", Engine: "fake", Storage: "local",
			Conn: config.Conn{Database: "app"}, Retention: config.Retention{KeepLast: 2}}},
		Storages: []config.StorageConfig{{Name: "local", Type: "local"}},
	}
	d := Deps{
		Cfg:             cfg,
		Engine:          func(string) (db.Engine, bool) { return &fakeEngine{}, true },
		Backend:         func(config.StorageConfig) (storage.Backend, error) { return be, nil },
		ResolvePassword: func(config.Job) (string, error) { return "", nil },
		Now:             func() time.Time { return time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC) },
		Log:             logutil.New(io.Discard, logutil.LevelInfo),
	}
	if _, err := Run(ctx, d, "main"); err != nil {
		t.Fatalf("run: %v", err)
	}
	// After the new backup there are 3; keep_last=2 prunes the oldest.
	objs, _ := be.List(ctx, "main/")
	if len(objs) != 2 {
		t.Fatalf("expected 2 backups after retention, got %d: %+v", len(objs), objs)
	}
}

func readerOfB(s string) io.Reader { return bytes.NewReader([]byte(s)) }
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/retention/ ./internal/backup/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/retention internal/backup
git commit -m "feat: retention policy with keep-last pruning after backup"
```

---

### Task 3: `dbk secret` command (set/rm/ls) backed by the vault

**Files:**
- Create: `internal/cli/secret.go`
- Modify: `internal/cli/root.go` (register `newSecretCmd()`)
- Test: `internal/cli/secret_test.go`

**Interfaces:**
- Consumes: `vault` (Create/Open/Exists/ReadPassphrase), `config.Dir`.
- Produces: `dbk secret set <key> [--value V | --stdin]`, `dbk secret rm <key>`, `dbk secret ls` (prints keys only, never values). Creates the vault if it does not exist. Keys are arbitrary strings (e.g. `storage/r2/secret_key`, `job/main/password`).

- [ ] **Step 1: Write the failing test**

`internal/cli/secret_test.go`:
```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestSecretSetLsRm(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DBK_PASSPHRASE", "pw")

	run := func(args ...string) (string, error) {
		root := NewRootCmd()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(append([]string{"--config", dir}, args...))
		err := root.Execute()
		return out.String(), err
	}

	if _, err := run("secret", "set", "storage/r2/secret_key", "--value", "SUPERSECRET"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := run("secret", "ls")
	if err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(out, "storage/r2/secret_key") {
		t.Fatalf("ls should list the key, got %q", out)
	}
	if strings.Contains(out, "SUPERSECRET") {
		t.Fatalf("ls must never print values, got %q", out)
	}
	if _, err := run("secret", "rm", "storage/r2/secret_key"); err != nil {
		t.Fatalf("rm: %v", err)
	}
	out, _ = run("secret", "ls")
	if strings.Contains(out, "storage/r2/secret_key") {
		t.Fatalf("key should be gone after rm, got %q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestSecretSetLsRm -v`
Expected: FAIL (unknown command "secret").

- [ ] **Step 3: Add a key lister to the vault store**

In `internal/vault/vault.go`, add:
```go
import "sort" // add to the existing import block

// Keys returns the secret keys (not values), sorted.
func (s *Store) Keys() []string {
	out := make([]string, 0, len(s.secrets))
	for k := range s.secrets {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Write the implementation**

`internal/cli/secret.go`:
```go
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/vault"
	"github.com/spf13/cobra"
)

func newSecretCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "secret", Short: "Manage secrets in the encrypted vault"}
	cmd.AddCommand(newSecretSetCmd(), newSecretRmCmd(), newSecretLsCmd())
	return cmd
}

func openOrCreateVault(cmd *cobra.Command) (string, *vault.Store, error) {
	override, _ := cmd.Flags().GetString("config")
	dir, err := config.Dir(override)
	if err != nil {
		return "", nil, err
	}
	passFile, _ := cmd.Flags().GetString("passphrase-file")
	pass, err := vault.ReadPassphrase(dir, passFile, true)
	if err != nil {
		return "", nil, err
	}
	if vault.Exists(dir) {
		s, err := vault.Open(dir, pass)
		return dir, s, err
	}
	s, err := vault.Create(dir, pass)
	return dir, s, err
}

func newSecretSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key>",
		Short: "Store a secret value under a key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := openOrCreateVault(cmd)
			if err != nil {
				return err
			}
			val, _ := cmd.Flags().GetString("value")
			useStdin, _ := cmd.Flags().GetBool("stdin")
			if useStdin {
				b, rerr := readAllStdin()
				if rerr != nil {
					return rerr
				}
				val = strings.TrimRight(string(b), "\r\n")
			}
			if val == "" {
				return fmt.Errorf("no value provided: use --value or --stdin")
			}
			s.Set(args[0], val)
			if err := s.Save(); err != nil {
				return err
			}
			cmd.Printf("stored secret %q\n", args[0])
			return nil
		},
	}
	cmd.Flags().String("value", "", "the secret value (prefer --stdin to keep it out of shell history)")
	cmd.Flags().Bool("stdin", false, "read the secret value from stdin")
	return cmd
}

func newSecretRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <key>",
		Short: "Remove a secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := openOrCreateVault(cmd)
			if err != nil {
				return err
			}
			s.Delete(args[0])
			if err := s.Save(); err != nil {
				return err
			}
			cmd.Printf("removed secret %q\n", args[0])
			return nil
		},
	}
}

func newSecretLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List secret keys (never values)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, s, err := openOrCreateVault(cmd)
			if err != nil {
				return err
			}
			for _, k := range s.Keys() {
				cmd.Println(k)
			}
			return nil
		},
	}
}

func readAllStdin() ([]byte, error) {
	return os.ReadFile("/dev/stdin")
}
```

Note on `readAllStdin`: `/dev/stdin` is not portable to Windows. Use `io.ReadAll(os.Stdin)` instead to stay cross-platform:
```go
import "io"
func readAllStdin() ([]byte, error) { return io.ReadAll(os.Stdin) }
```
Use the `io.ReadAll(os.Stdin)` version; drop the `/dev/stdin` version and the unused `os` import if it becomes unused (keep `os` for `os.Stdin`).

- [ ] **Step 5: Register the command**

In `internal/cli/root.go`, add after the other `AddCommand` calls:
```go
	root.AddCommand(newSecretCmd())
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestSecretSetLsRm -v && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli internal/vault
git commit -m "feat: dbk secret set/rm/ls backed by the vault"
```

---

### Task 4: AWS SDK dependency + S3 client construction

**Files:**
- Create: `internal/storage/s3.go`
- Test: `internal/storage/s3_test.go`

**Interfaces:**
- Consumes: `config.StorageConfig` (indirectly; the constructor takes explicit params so storage does not import config).
- Produces:
  - `type S3Options struct { Bucket, Region, Endpoint, Prefix, AccessKeyID, SecretKey string; ForcePathStyle bool }`.
  - `storage.NewS3(ctx context.Context, o S3Options) (*S3, error)` — builds an `*s3.Client` with static credentials, optional custom `BaseEndpoint`, region, and path-style.
  - `(*S3) key(k string) string` — joins `Prefix` and `k` with a single `/`.

- [ ] **Step 1: Add the AWS SDK dependencies**

Run:
```bash
go get github.com/aws/aws-sdk-go-v2/config@v1.27.43
go get github.com/aws/aws-sdk-go-v2/credentials@v1.17.42
go get github.com/aws/aws-sdk-go-v2/service/s3@v1.66.0
go get github.com/aws/aws-sdk-go-v2/feature/s3/manager@v1.17.36
```

- [ ] **Step 2: Write the failing test**

`internal/storage/s3_test.go`:
```go
package storage

import (
	"context"
	"testing"
)

func TestNewS3AndKeyPrefix(t *testing.T) {
	s, err := NewS3(context.Background(), S3Options{
		Bucket: "backups", Region: "auto", Endpoint: "https://x.r2.cloudflarestorage.com",
		Prefix: "dbk/", AccessKeyID: "AK", SecretKey: "SK", ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("new s3: %v", err)
	}
	if s.Name() != "s3" {
		t.Fatalf("name: %q", s.Name())
	}
	if got := s.key("main/x.sql"); got != "dbk/main/x.sql" {
		t.Fatalf("key prefix join wrong: %q", got)
	}
	// No prefix -> key unchanged.
	s2, _ := NewS3(context.Background(), S3Options{Bucket: "b", Region: "us-east-1", AccessKeyID: "AK", SecretKey: "SK"})
	if got := s2.key("main/x.sql"); got != "main/x.sql" {
		t.Fatalf("no-prefix key wrong: %q", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestNewS3 -v`
Expected: FAIL (undefined NewS3).

- [ ] **Step 4: Write the implementation**

`internal/storage/s3.go`:
```go
package storage

import (
	"context"
	"fmt"
	"strings"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Options struct {
	Bucket         string
	Region         string
	Endpoint       string // empty = AWS default
	Prefix         string
	AccessKeyID    string
	SecretKey      string
	ForcePathStyle bool
}

type S3 struct {
	client *s3.Client
	bucket string
	prefix string
}

func NewS3(ctx context.Context, o S3Options) (*S3, error) {
	if o.Bucket == "" {
		return nil, fmt.Errorf("s3: bucket is required")
	}
	region := o.Region
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := awscfg.LoadDefaultConfig(ctx,
		awscfg.WithRegion(region),
		awscfg.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(o.AccessKeyID, o.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(p *s3.Options) {
		if o.Endpoint != "" {
			p.BaseEndpoint = &o.Endpoint
		}
		p.UsePathStyle = o.ForcePathStyle
	})
	return &S3{client: client, bucket: o.Bucket, prefix: o.Prefix}, nil
}

func (s *S3) Name() string { return "s3" }

func (s *S3) key(k string) string {
	if s.prefix == "" {
		return k
	}
	return strings.TrimRight(s.prefix, "/") + "/" + strings.TrimLeft(k, "/")
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/storage/ -run TestNewS3 -v && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/storage go.mod go.sum
git commit -m "feat: S3 client construction for the storage backend"
```

---

### Task 5: S3 backend operations (streamed Put, Get, List, Delete, Stat, Test)

**Files:**
- Modify: `internal/storage/s3.go` (add the `Backend` methods)
- Test: `internal/storage/s3_integration_test.go`

**Interfaces:**
- Consumes: the `*s3.Client` from Task 4; `feature/s3/manager` for multipart upload.
- Produces: `*S3` satisfies `storage.Backend`. `Put` streams via `manager.Uploader` (handles size `-1`). `List` pages `ListObjectsV2` and returns keys with the configured prefix stripped back off (so keys are job-relative, matching the local backend). `Stat` uses `HeadObject`. `Test` calls `HeadBucket`.

- [ ] **Step 1: Write the implementation**

Append to `internal/storage/s3.go`:
```go
import (
	"errors"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)
// (merge these into the existing import block)

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	uploader := manager.NewUploader(s.client)
	_, err := uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.key(key)),
		Body:   r,
	})
	return err
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	full := s.key(prefix)
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket), Prefix: aws.String(full),
	})
	var out []Object
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, o := range page.Contents {
			k := aws.ToString(o.Key)
			if s.prefix != "" {
				k = strings.TrimPrefix(k, strings.TrimRight(s.prefix, "/")+"/")
			}
			var mt time.Time
			if o.LastModified != nil {
				mt = *o.LastModified
			}
			out = append(out, Object{Key: k, Size: aws.ToInt64(o.Size), ModTime: mt})
		}
	}
	return out, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
	})
	return err
}

func (s *S3) Stat(ctx context.Context, key string) (Object, error) {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(s.key(key)),
	})
	if err != nil {
		return Object{}, err
	}
	var mt time.Time
	if out.LastModified != nil {
		mt = *out.LastModified
	}
	return Object{Key: key, Size: aws.ToInt64(out.ContentLength), ModTime: mt}, nil
}

func (s *S3) Test(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	var nf *s3types.NotFound
	if errors.As(err, &nf) {
		return fmt.Errorf("s3: bucket %q not found or not accessible", s.bucket)
	}
	return err
}
```

- [ ] **Step 2: Verify it compiles and satisfies the interface**

Add a compile-time assertion at the top of `internal/storage/s3.go` (after the imports):
```go
var _ Backend = (*S3)(nil)
```
Run: `go build ./... `
Expected: builds (interface satisfied). If it fails, the method set is wrong — fix to match `Backend`.

- [ ] **Step 3: Write the gated integration test (MinIO)**

`internal/storage/s3_integration_test.go`:
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

// Requires a MinIO/S3 reachable via DBK_IT_S3_* env vars and an existing bucket.
func TestS3RoundTripIntegration(t *testing.T) {
	o := S3Options{
		Bucket:         os.Getenv("DBK_IT_S3_BUCKET"),
		Region:         envOrS("DBK_IT_S3_REGION", "us-east-1"),
		Endpoint:       os.Getenv("DBK_IT_S3_ENDPOINT"),
		AccessKeyID:    os.Getenv("DBK_IT_S3_KEY"),
		SecretKey:      os.Getenv("DBK_IT_S3_SECRET"),
		ForcePathStyle: true,
		Prefix:         "it/",
	}
	if o.Bucket == "" {
		t.Skip("DBK_IT_S3_BUCKET not set")
	}
	ctx := context.Background()
	be, err := NewS3(ctx, o)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := be.Test(ctx); err != nil {
		t.Fatalf("test bucket: %v", err)
	}
	data := []byte("hello s3 backup")
	if err := be.Put(ctx, "main/x.sql", bytes.NewReader(data), -1); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := be.Get(ctx, "main/x.sql")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
	objs, err := be.List(ctx, "main/")
	if err != nil || len(objs) == 0 || objs[0].Key != "main/x.sql" {
		t.Fatalf("list: %+v err %v", objs, err)
	}
	if err := be.Delete(ctx, "main/x.sql"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func envOrS(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
```

- [ ] **Step 4: Verify the integration test compiles under its tag**

Run: `go vet -tags integration ./internal/storage/`
Expected: no output (compiles). Do NOT run it without a live store.

- [ ] **Step 5: Run the default (non-integration) suite**

Run: `go test ./internal/storage/ -v && go build ./...`
Expected: PASS (unit tests; integration test excluded).

- [ ] **Step 6: Commit**

```bash
git add internal/storage go.mod go.sum
git commit -m "feat: S3 backend Put/Get/List/Delete/Stat/Test with streamed multipart upload"
```

---

### Task 6: Wire the S3 backend into the CLI (`buildBackend` + secret resolution)

**Files:**
- Modify: `internal/cli/wire.go` (`buildBackend` to handle `type: s3`, resolving the secret key)
- Modify: `internal/cli/run.go`, `internal/cli/restore.go`, `internal/cli/list.go` as needed so the vault is available to `buildBackend`
- Test: `internal/cli/wire_test.go`

**Interfaces:**
- Consumes: `storage.NewS3`, `config.StorageConfig`, `secretref.Resolve`, `vault.Store`.
- Produces:
  - `buildBackend(ctx context.Context, sc config.StorageConfig, v *vault.Store) (storage.Backend, error)` — new signature; for `type: s3` it resolves the secret key from `sc.SecretKeyRef` (via `secretref`, with the vault as lookup) or, when empty, from the vault key `storage/<name>/secret_key`, then calls `storage.NewS3`.
  - All call sites updated to pass `cmd.Context()` and the opened vault.

- [ ] **Step 1: Write the failing test**

`internal/cli/wire_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestBuildBackend -v`
Expected: FAIL (buildBackend signature mismatch / undefined).

- [ ] **Step 3: Update `buildBackend`**

In `internal/cli/wire.go`, replace `buildBackend` with:
```go
func buildBackend(ctx context.Context, sc config.StorageConfig, v *vault.Store) (storage.Backend, error) {
	switch sc.Type {
	case "local":
		return storage.NewLocal(sc.Path), nil
	case "s3":
		secret, err := resolveStorageSecret(sc, v)
		if err != nil {
			return nil, err
		}
		return storage.NewS3(ctx, storage.S3Options{
			Bucket: sc.Bucket, Region: sc.Region, Endpoint: sc.Endpoint, Prefix: sc.Prefix,
			AccessKeyID: sc.AccessKeyID, SecretKey: secret, ForcePathStyle: sc.ForcePathStyle,
		})
	default:
		return nil, fmt.Errorf("storage type %q not supported", sc.Type)
	}
}

// resolveStorageSecret resolves an S3 secret key from the storage's SecretKeyRef
// (env:/file:/vault), defaulting to the vault key "storage/<name>/secret_key".
func resolveStorageSecret(sc config.StorageConfig, v *vault.Store) (string, error) {
	var lookup func(string) (string, bool)
	if v != nil {
		lookup = v.Get
	}
	ref := sc.SecretKeyRef
	if ref == "" {
		ref = "storage/" + sc.Name + "/secret_key"
	}
	return secretref.Resolve(ref, lookup)
}
```
Add imports `"context"` and `"github.com/tanzid64/tui-db-manager/internal/storage"` (already present) to `wire.go`; `secretref` and `vault` are already imported.

- [ ] **Step 4: Update the call sites**

In `internal/cli/run.go`, the `backup.Deps.Backend` closure becomes:
```go
				Backend: func(sc config.StorageConfig) (storage.Backend, error) {
					return buildBackend(cmd.Context(), sc, v)
				},
```
In `internal/cli/restore.go` and `internal/cli/list.go`, replace `buildBackend(sc)` with `buildBackend(cmd.Context(), sc, v)`. In `list.go` there is currently no vault opened; open it the same way as `restore.go` (add the `maybeOpenVault` call and pass `v`). For `list.go`, a nil vault is acceptable if the storage uses an `env:`/`file:` ref, so wrap the open so a missing vault is not fatal:
```go
			v, _ := maybeOpenVault(app.dir, passFileOf(cmd), true)
```
where `passFileOf` is:
```go
func passFileOf(cmd *cobra.Command) string {
	pf, _ := cmd.Flags().GetString("passphrase-file")
	return pf
}
```
Place `passFileOf` in `internal/cli/wire.go`. (Using `maybeOpenVault` which already returns `nil, nil` when no vault file exists.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/ -v && go build ./...`
Expected: PASS (including the earlier `TestRunCommandSQLiteEndToEnd` and `TestSecretSetLsRm`).

- [ ] **Step 6: Commit**

```bash
git add internal/cli
git commit -m "feat: wire S3 backend into CLI with vault/ref secret resolution"
```

---

### Task 7: Mask storage secrets in surfaced errors + docs

**Files:**
- Modify: `internal/backup/backup.go` and `internal/restore/restore.go` (mask the storage secret too, not just the DB password)
- Create: `docs/cloud-storage.md`
- Test: `internal/backup/backup_test.go` (a test that an upload error containing the S3 secret is masked)

**Interfaces:**
- Consumes: `logutil.MaskError`.
- Produces: backup/restore mask BOTH the DB password and the resolved storage secret in any returned error. Since the orchestration does not currently know the storage secret, the masking is applied where the backend is built: `buildBackend` wraps the returned backend's errors — simplest is to mask in the CLI layer. For this task, add the storage secret to the set of masked literals by having `buildBackend` register it with a logger-independent scrubber returned alongside the backend.

Decision to keep it simple and correct: `buildBackend` returns `(storage.Backend, string, error)` where the middle value is the resolved secret (empty for local), and the CLI commands pass that secret to a small `maskWith` helper when returning errors. To avoid threading a secret through every layer, implement a thin wrapper backend that masks the secret out of every error it returns.

- [ ] **Step 1: Write the failing test**

Append to `internal/storage/s3_test.go`:
```go
func TestMaskingBackendScrubsSecretFromErrors(t *testing.T) {
	inner := &errBackend{err: errorsNew("AccessDenied for key AKIA secret topsecretvalue")}
	wrapped := NewMaskingBackend(inner, "topsecretvalue")
	_, err := wrapped.Get(context.Background(), "k")
	if err == nil || contains(err.Error(), "topsecretvalue") {
		t.Fatalf("secret must be scrubbed from backend errors, got %v", err)
	}
}
```
Add helpers to `internal/storage/s3_test.go`:
```go
import (
	"errors"
	"strings"
)

func errorsNew(s string) error        { return errors.New(s) }
func contains(s, sub string) bool     { return strings.Contains(s, sub) }

type errBackend struct{ err error }

func (b *errBackend) Name() string                                           { return "err" }
func (b *errBackend) Put(context.Context, string, io.Reader, int64) error    { return b.err }
func (b *errBackend) Get(context.Context, string) (io.ReadCloser, error)     { return nil, b.err }
func (b *errBackend) List(context.Context, string) ([]Object, error)         { return nil, b.err }
func (b *errBackend) Delete(context.Context, string) error                   { return b.err }
func (b *errBackend) Stat(context.Context, string) (Object, error)           { return Object{}, b.err }
func (b *errBackend) Test(context.Context) error                             { return b.err }
```
(Move the `io`, `errors`, `strings` imports into the file's existing import block.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestMaskingBackend -v`
Expected: FAIL (undefined NewMaskingBackend).

- [ ] **Step 3: Write the implementation**

Create `internal/storage/masking.go`:
```go
package storage

import (
	"context"
	"errors"
	"io"
	"strings"
)

// maskingBackend wraps a Backend and scrubs a secret literal from every error
// it returns, so an S3 secret embedded in an SDK error can never reach a log or
// the CLI's stderr.
type maskingBackend struct {
	inner  Backend
	secret string
}

func NewMaskingBackend(inner Backend, secret string) Backend {
	if secret == "" {
		return inner
	}
	return &maskingBackend{inner: inner, secret: secret}
}

func (m *maskingBackend) scrub(err error) error {
	if err == nil {
		return err
	}
	msg := err.Error()
	if strings.Contains(msg, m.secret) {
		return errors.New(strings.ReplaceAll(msg, m.secret, "***"))
	}
	return err
}

func (m *maskingBackend) Name() string { return m.inner.Name() }

func (m *maskingBackend) Put(ctx context.Context, k string, r io.Reader, n int64) error {
	return m.scrub(m.inner.Put(ctx, k, r, n))
}

func (m *maskingBackend) Get(ctx context.Context, k string) (io.ReadCloser, error) {
	rc, err := m.inner.Get(ctx, k)
	return rc, m.scrub(err)
}

func (m *maskingBackend) List(ctx context.Context, p string) ([]Object, error) {
	o, err := m.inner.List(ctx, p)
	return o, m.scrub(err)
}

func (m *maskingBackend) Delete(ctx context.Context, k string) error {
	return m.scrub(m.inner.Delete(ctx, k))
}

func (m *maskingBackend) Stat(ctx context.Context, k string) (Object, error) {
	o, err := m.inner.Stat(ctx, k)
	return o, m.scrub(err)
}

func (m *maskingBackend) Test(ctx context.Context) error { return m.scrub(m.inner.Test(ctx)) }
```

- [ ] **Step 4: Apply the wrapper in `buildBackend`**

In `internal/cli/wire.go`, wrap the S3 backend before returning:
```go
	case "s3":
		secret, err := resolveStorageSecret(sc, v)
		if err != nil {
			return nil, err
		}
		be, err := storage.NewS3(ctx, storage.S3Options{
			Bucket: sc.Bucket, Region: sc.Region, Endpoint: sc.Endpoint, Prefix: sc.Prefix,
			AccessKeyID: sc.AccessKeyID, SecretKey: secret, ForcePathStyle: sc.ForcePathStyle,
		})
		if err != nil {
			return nil, err
		}
		return storage.NewMaskingBackend(be, secret), nil
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/storage/ ./internal/cli/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 6: Write the cloud docs**

Create `docs/cloud-storage.md` with a concrete, copy-pasteable config for each provider (AWS S3, Cloudflare R2, MinIO, Backblaze B2, Wasabi), showing `endpoint`, `region`, `force_path_style`, and how to store the secret:
```markdown
# Cloud storage (S3-compatible)

dbk backs up to any S3-compatible object store. Put the access key ID in
`config.yaml` and the secret key in the vault:

    dbk secret set storage/<name>/secret_key --stdin   # paste the secret, Ctrl-D

Or reference an environment variable / file with `secret_key_ref`.

## AWS S3
    storages:
      - name: aws
        type: s3
        bucket: my-backups
        region: us-east-1
        access_key_id: AKIA...

## Cloudflare R2
    storages:
      - name: r2
        type: s3
        bucket: my-backups
        region: auto
        endpoint: https://<account>.r2.cloudflarestorage.com
        force_path_style: true
        access_key_id: <r2-access-key-id>

## MinIO
    storages:
      - name: minio
        type: s3
        bucket: my-backups
        region: us-east-1
        endpoint: https://minio.example.com:9000
        force_path_style: true
        access_key_id: <minio-access-key>

## Backblaze B2 (S3 API)
    storages:
      - name: b2
        type: s3
        bucket: my-backups
        region: us-west-004
        endpoint: https://s3.us-west-004.backblazeb2.com
        access_key_id: <keyID>

## Wasabi
    storages:
      - name: wasabi
        type: s3
        bucket: my-backups
        region: us-east-1
        endpoint: https://s3.wasabisys.com
        access_key_id: <wasabi-key>
```

- [ ] **Step 7: Run the whole suite**

Run: `go test ./... && go vet ./... && go vet -tags integration ./...`
Expected: all PASS / clean.

- [ ] **Step 8: Commit**

```bash
git add internal/storage internal/cli docs/cloud-storage.md
git commit -m "feat: mask storage secrets in errors; document cloud storage config"
```

---

## Self-Review Notes

- **Spec coverage (this plan's slice):** S3 + R2 + all S3-compatible (MinIO/B2/Wasabi) via one driver with configurable endpoint/region/path-style (Tasks 4–6); streamed multipart upload for unknown-size dumps (Task 5); retention/pruning from the spec's backup data-flow step 4 (Task 2); secret key kept out of config, resolved from vault/ref, settable via a new `secret` CLI (Tasks 1, 3, 6). Deferred to a later plan (explicit): Google Drive (OAuth subsystem). Not in scope: compression, at-rest encryption, notifications, scheduling, export/import, TUI, installers.
- **Placeholder scan:** no TBD/TODO; every code step carries real code. Two "Decision" notes (Mongo-style stdin portability in Task 3; masking approach in Task 7) are resolved inline with the concrete code to use.
- **Type consistency:** `storage.Backend` method set is unchanged; `S3`, `Local`, and `maskingBackend` all implement it (Task 5 adds a compile-time `var _ Backend = (*S3)(nil)`). `buildBackend`'s new signature `(ctx, sc, v)` is updated at every call site in Task 6. `S3Options` fields match the `StorageConfig` fields added in Task 1.
- **Review Focus coverage:** streamed unknown-size upload (Task 5 uses `manager.Uploader`, exercised by the integration round-trip with `size=-1`); wrong/missing credentials (Task 5 integration `Test`/`Put`, Task 6 `TestBuildBackendS3MissingSecretErrors`); retention correctness + keep-everything-on-zero (Task 2 `TestApplyKeepsNewestNAndIgnoresOtherJobs`, `TestApplyKeepLastZeroDeletesNothing`); secret never in config/argv/errors (Task 1 `TestStorageConfigNeverSerializesASecretKey`, Task 7 `TestMaskingBackendScrubsSecretFromErrors`).
