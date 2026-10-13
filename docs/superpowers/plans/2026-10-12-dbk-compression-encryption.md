# dbk Compression + At-Rest Encryption Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compress backups (gzip or zstd) and optionally encrypt them at rest with age (X25519) before they reach any storage backend, and reverse both transforms transparently on restore — so cloud copies are compact and unreadable without the private identity.

**Architecture:** A new `internal/pipeline` package provides streaming `NewWriter(dst, opts)` (compress → encrypt → dst) and `NewReader(src, opts)` (decrypt → decompress → data), plus `Ext(opts)` and `DetectExt(key)` so the applied transforms are encoded in and recovered from the stored object key (e.g. `main/main-<ts>.sql.gz.age`). `backup.Run` wraps its dump pipe with the writer and appends the extension to the key; `restore.Run` reads the transforms from the key and wraps the download with the reader, decrypting with the job's age identity (resolved from the vault/ref). Everything stays fully streamed — no whole-dump buffering. Encryption is asymmetric: the backup side needs only the public recipient; restore needs the secret identity.

**Tech Stack:** Go 1.26+, `compress/gzip` (stdlib), `github.com/klauspost/compress/zstd`, `filippo.io/age`, existing config/backup/restore/cli/secretref packages.

**Spec:** `docs/superpowers/specs/2026-10-12-dbk-design.md`

## Global Constraints

- Module path: `github.com/tanzid64/tui-db-manager`.
- Go version floor: `go 1.26` (raised by the Google/AWS/x deps; recorded in prior ledgers).
- Binary and root command name: `dbk`.
- The age **identity** (secret key) is never stored in `config.yaml`; only the public **recipient(s)** are. The identity is resolved at runtime from `identity_ref` (`env:`/`file:` or vault key `encryption/<job>/identity`).
- The age identity must never be passed as a process argument or written to logs; mask it in any surfaced error via `logutil.MaskError`.
- The pipeline is fully streaming: `NewWriter`/`NewReader` wrap `io.Writer`/`io.Reader` and never buffer the whole payload.
- Canonical transform order is fixed: dump → compress → encrypt (write side); decrypt → decompress → restore (read side). The stored key suffix records it.
- Integration tests needing live services stay gated behind `//go:build integration`; this plan's tests are all pure-Go unit tests (no external services).
- Each task ends with a green `go build ./...`, passing non-integration tests, and a git commit (author `tanzid64 <tanzid3@gmail.com>`). No `Co-Authored-By`/"Generated with Claude Code".

## Review Focus

- **Round-trip fidelity for every combination:** dump bytes must survive `NewWriter` → `NewReader` byte-for-byte for all of {none, gzip, zstd} × {no-encrypt, age} — pinned in Tasks 1 and 2.
- **Wrong/missing identity fails loudly on restore:** decrypting an age payload with the wrong identity, or none, must error clearly, never return garbage or partial plaintext — pinned in Task 2.
- **Transform metadata round-trips through the key:** `DetectExt(Ext(opts)+base)` must recover exactly the compression and encryption that `NewWriter` applied, so restore never guesses — pinned in Task 1 (compression) and Task 2 (encryption).
- **Identity never in config/argv/logs; recipient is not secret:** config serializes only recipients + `identity_ref`; the resolved identity is held in memory, masked from errors, never written to `config.yaml` — pinned in Tasks 3 and 5.
- **Streaming, not buffering:** a large dump must flow through compress+encrypt without the pipeline reading it all into memory; close order must flush/finalize correctly so no trailing bytes are lost — pinned in Task 1 (close-order test) and exercised by Task 4.

---

### Task 1: `pipeline` package — streaming compression + key-suffix metadata

**Files:**
- Create: `internal/pipeline/pipeline.go`
- Test: `internal/pipeline/pipeline_test.go`

**Interfaces:**
- Produces:
  - `type Options struct { Compression string; Recipients []string; Identities []string }` (Recipients/Identities unused until Task 2).
  - `pipeline.NewWriter(dst io.Writer, o Options) (io.WriteCloser, error)` — returns a writer the caller writes plaintext to; it compresses into `dst`. `Close` flushes/finalizes every layer (but never closes `dst`).
  - `pipeline.NewReader(src io.Reader, o Options) (io.ReadCloser, error)` — decompresses `src`.
  - `pipeline.Ext(o Options) string` — `""` / `".gz"` / `".zst"` (Task 2 adds `.age`).
  - `pipeline.DetectExt(key string) Options` — recovers `Compression` from the key suffix (Task 2 adds encryption).

- [ ] **Step 1: Add the zstd dependency**

Run: `go get github.com/klauspost/compress/zstd@latest`

- [ ] **Step 2: Write the failing test**

`internal/pipeline/pipeline_test.go`:
```go
package pipeline

import (
	"bytes"
	"io"
	"testing"
)

func roundTrip(t *testing.T, o Options, payload []byte) {
	t.Helper()
	var stored bytes.Buffer
	w, err := NewWriter(&stored, o)
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	r, err := NewReader(bytes.NewReader(stored.Bytes()), o)
	if err != nil {
		t.Fatalf("new reader: %v", err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	r.Close()
	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

func TestRoundTripCompression(t *testing.T) {
	payload := bytes.Repeat([]byte("the quick brown fox 1234567890\n"), 5000)
	for _, comp := range []string{"", "none", "gzip", "zstd"} {
		roundTrip(t, Options{Compression: comp}, payload)
	}
}

func TestCompressionActuallyShrinks(t *testing.T) {
	payload := bytes.Repeat([]byte("aaaaaaaaaaaaaaaa"), 10000)
	for _, comp := range []string{"gzip", "zstd"} {
		var stored bytes.Buffer
		w, _ := NewWriter(&stored, Options{Compression: comp})
		w.Write(payload)
		w.Close()
		if stored.Len() >= len(payload) {
			t.Fatalf("%s did not compress: %d >= %d", comp, stored.Len(), len(payload))
		}
	}
}

func TestExtAndDetectCompression(t *testing.T) {
	cases := map[string]string{"": "", "none": "", "gzip": ".gz", "zstd": ".zst"}
	for comp, wantExt := range cases {
		if got := Ext(Options{Compression: comp}); got != wantExt {
			t.Fatalf("Ext(%q)=%q want %q", comp, got, wantExt)
		}
		key := "main/main-20261012-030405.sql" + wantExt
		det := DetectExt(key)
		wantComp := comp
		if wantComp == "none" {
			wantComp = ""
		}
		if det.Compression != wantComp {
			t.Fatalf("DetectExt(%q).Compression=%q want %q", key, det.Compression, wantComp)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -v`
Expected: FAIL (undefined).

- [ ] **Step 4: Write the implementation**

`internal/pipeline/pipeline.go`:
```go
package pipeline

import (
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
)

type Options struct {
	Compression string   // "", "none", "gzip", "zstd"
	Recipients  []string // age recipients (encrypt side) — used from Task 2
	Identities  []string // age identities (decrypt side) — used from Task 2
}

func normComp(c string) string {
	if c == "none" {
		return ""
	}
	return c
}

// multiCloser closes a set of closers in order, returning the first error.
type multiCloser struct {
	io.Writer
	closers []io.Closer
}

func (m *multiCloser) Close() error {
	var first error
	for _, c := range m.closers {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// NewWriter returns a writer that compresses (Task 2: then encrypts) into dst.
// The caller writes plaintext; Close flushes every layer but never closes dst.
func NewWriter(dst io.Writer, o Options) (io.WriteCloser, error) {
	w := dst
	var closers []io.Closer // in data-flow order (innermost first)
	switch normComp(o.Compression) {
	case "":
		// no compression
	case "gzip":
		gw := gzip.NewWriter(w)
		w = gw
		closers = append([]io.Closer{gw}, closers...)
	case "zstd":
		zw, err := zstd.NewWriter(w)
		if err != nil {
			return nil, err
		}
		w = zw
		closers = append([]io.Closer{zw}, closers...)
	default:
		return nil, fmt.Errorf("pipeline: unknown compression %q", o.Compression)
	}
	return &multiCloser{Writer: w, closers: closers}, nil
}

// NewReader returns a reader that (Task 2: decrypts then) decompresses src.
func NewReader(src io.Reader, o Options) (io.ReadCloser, error) {
	r := src
	switch normComp(o.Compression) {
	case "":
		return io.NopCloser(r), nil
	case "gzip":
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		return gr, nil
	case "zstd":
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return zr.IOReadCloser(), nil
	default:
		return nil, fmt.Errorf("pipeline: unknown compression %q", o.Compression)
	}
}

func Ext(o Options) string {
	switch normComp(o.Compression) {
	case "gzip":
		return ".gz"
	case "zstd":
		return ".zst"
	default:
		return ""
	}
}

// DetectExt recovers the transforms recorded in a stored key's suffix.
func DetectExt(key string) Options {
	var o Options
	switch {
	case strings.HasSuffix(key, ".gz"):
		o.Compression = "gzip"
	case strings.HasSuffix(key, ".zst"):
		o.Compression = "zstd"
	}
	return o
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline go.mod go.sum
git commit -m "feat: streaming compression pipeline with key-suffix metadata"
```

---

### Task 2: Add age encryption to the pipeline

**Files:**
- Modify: `internal/pipeline/pipeline.go`
- Test: `internal/pipeline/encrypt_test.go`

**Interfaces:**
- Consumes: `Options.Recipients`, `Options.Identities`.
- Produces: `NewWriter` also age-encrypts (outermost stored layer) when `Recipients` is non-empty; `NewReader` age-decrypts when `Identities` is non-empty; `Ext` appends `.age`; `DetectExt` sets a marker so the caller knows decryption is needed. Add `pipeline.Encrypted(key string) bool`.

- [ ] **Step 1: Write the failing test**

`internal/pipeline/encrypt_test.go`:
```go
package pipeline

import (
	"bytes"
	"io"
	"testing"

	"filippo.io/age"
)

func newKeypair(t *testing.T) (recipient, identity string) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return id.Recipient().String(), id.String()
}

func TestRoundTripCompressionAndEncryption(t *testing.T) {
	rec, id := newKeypair(t)
	payload := bytes.Repeat([]byte("secret backup data\n"), 3000)
	for _, comp := range []string{"", "gzip", "zstd"} {
		var stored bytes.Buffer
		w, err := NewWriter(&stored, Options{Compression: comp, Recipients: []string{rec}})
		if err != nil {
			t.Fatalf("writer: %v", err)
		}
		w.Write(payload)
		if err := w.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		// Stored bytes must not contain the plaintext.
		if bytes.Contains(stored.Bytes(), []byte("secret backup data")) {
			t.Fatalf("plaintext leaked into encrypted output (comp=%q)", comp)
		}
		r, err := NewReader(bytes.NewReader(stored.Bytes()), Options{Compression: comp, Identities: []string{id}})
		if err != nil {
			t.Fatalf("reader: %v", err)
		}
		got, _ := io.ReadAll(r)
		r.Close()
		if !bytes.Equal(got, payload) {
			t.Fatalf("round-trip mismatch (comp=%q)", comp)
		}
	}
}

func TestWrongIdentityFails(t *testing.T) {
	rec, _ := newKeypair(t)
	_, wrongID := newKeypair(t)
	var stored bytes.Buffer
	w, _ := NewWriter(&stored, Options{Recipients: []string{rec}})
	w.Write([]byte("topsecret"))
	w.Close()
	r, err := NewReader(bytes.NewReader(stored.Bytes()), Options{Identities: []string{wrongID}})
	if err == nil {
		// Some age errors surface on first Read rather than at construction.
		_, err = io.ReadAll(r)
	}
	if err == nil {
		t.Fatal("decrypting with the wrong identity must fail, not return garbage")
	}
}

func TestExtAndEncryptedMarker(t *testing.T) {
	rec, _ := newKeypair(t)
	o := Options{Compression: "gzip", Recipients: []string{rec}}
	if got := Ext(o); got != ".gz.age" {
		t.Fatalf("Ext=%q want .gz.age", got)
	}
	key := "main/main-20261012-030405.sql" + Ext(o)
	if !Encrypted(key) {
		t.Fatalf("Encrypted(%q) should be true", key)
	}
	if DetectExt(key).Compression != "gzip" {
		t.Fatalf("DetectExt should still see gzip under .age")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run 'Encrypt|WrongIdentity|EncryptedMarker|CompressionAndEncryption' -v`
Expected: FAIL (encryption not applied / undefined Encrypted).

- [ ] **Step 3: Add the age dependency**

Run: `go get filippo.io/age@latest`

- [ ] **Step 4: Write the implementation**

Edit `internal/pipeline/pipeline.go`. Add imports `"filippo.io/age"`. In `NewWriter`, wrap `dst` with age FIRST (so encryption is the outermost stored layer), before compression:
```go
func NewWriter(dst io.Writer, o Options) (io.WriteCloser, error) {
	w := dst
	var closers []io.Closer // data-flow order (innermost first)

	if len(o.Recipients) > 0 {
		recs, err := parseRecipients(o.Recipients)
		if err != nil {
			return nil, err
		}
		aw, err := age.Encrypt(w, recs...)
		if err != nil {
			return nil, err
		}
		w = aw
		closers = append([]io.Closer{aw}, closers...)
	}

	switch normComp(o.Compression) {
	case "":
	case "gzip":
		gw := gzip.NewWriter(w)
		w = gw
		closers = append([]io.Closer{gw}, closers...)
	case "zstd":
		zw, err := zstd.NewWriter(w)
		if err != nil {
			return nil, err
		}
		w = zw
		closers = append([]io.Closer{zw}, closers...)
	default:
		return nil, fmt.Errorf("pipeline: unknown compression %q", o.Compression)
	}
	return &multiCloser{Writer: w, closers: closers}, nil
}
```
In `NewReader`, decrypt FIRST, then decompress:
```go
func NewReader(src io.Reader, o Options) (io.ReadCloser, error) {
	r := src
	if len(o.Identities) > 0 {
		ids, err := parseIdentities(o.Identities)
		if err != nil {
			return nil, err
		}
		dr, err := age.Decrypt(r, ids...)
		if err != nil {
			return nil, err
		}
		r = dr
	}
	switch normComp(o.Compression) {
	case "":
		return io.NopCloser(r), nil
	case "gzip":
		gr, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		return gr, nil
	case "zstd":
		zr, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return zr.IOReadCloser(), nil
	default:
		return nil, fmt.Errorf("pipeline: unknown compression %q", o.Compression)
	}
}
```
Add the parse helpers and update `Ext`/`DetectExt` plus `Encrypted`:
```go
func parseRecipients(ss []string) ([]age.Recipient, error) {
	var out []age.Recipient
	for _, s := range ss {
		r, err := age.ParseX25519Recipient(s)
		if err != nil {
			return nil, fmt.Errorf("pipeline: invalid age recipient: %w", err)
		}
		out = append(out, r)
	}
	return out, nil
}

func parseIdentities(ss []string) ([]age.Identity, error) {
	var out []age.Identity
	for _, s := range ss {
		id, err := age.ParseX25519Identity(s)
		if err != nil {
			return nil, fmt.Errorf("pipeline: invalid age identity: %w", err)
		}
		out = append(out, id)
	}
	return out, nil
}
```
Change `Ext` to append `.age` when `len(o.Recipients) > 0`:
```go
func Ext(o Options) string {
	ext := ""
	switch normComp(o.Compression) {
	case "gzip":
		ext = ".gz"
	case "zstd":
		ext = ".zst"
	}
	if len(o.Recipients) > 0 {
		ext += ".age"
	}
	return ext
}
```
Update `DetectExt` to strip a trailing `.age` before reading the compression suffix, and add `Encrypted`:
```go
func DetectExt(key string) Options {
	var o Options
	k := strings.TrimSuffix(key, ".age")
	switch {
	case strings.HasSuffix(k, ".gz"):
		o.Compression = "gzip"
	case strings.HasSuffix(k, ".zst"):
		o.Compression = "zstd"
	}
	return o
}

// Encrypted reports whether the stored key was age-encrypted.
func Encrypted(key string) bool { return strings.HasSuffix(key, ".age") }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -v && go build ./...`
Expected: PASS (all pipeline tests, compression + encryption).

- [ ] **Step 6: Commit**

```bash
git add internal/pipeline go.mod go.sum
git commit -m "feat: age encryption layer in the backup pipeline"
```

---

### Task 3: Config — per-job compression and encryption

**Files:**
- Modify: `internal/config/config.go` (`Job`)
- Test: `internal/config/pipeline_config_test.go`

**Interfaces:**
- Produces: `Job` gains `Compression string` (`yaml:"compression,omitempty"`) and `Encryption EncryptionConfig` (`yaml:"encryption,omitempty"`), where `type EncryptionConfig struct { Recipients []string yaml:"recipients,omitempty"; IdentityRef string yaml:"identity_ref,omitempty" }`. The secret identity is never a field.

- [ ] **Step 1: Write the failing test**

`internal/config/pipeline_config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobCompressionEncryptionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := &Config{Version: 1, Jobs: []Job{{
		Name: "main", Engine: "postgres", Storage: "r2",
		Compression: "zstd",
		Encryption:  EncryptionConfig{Recipients: []string{"age1examplerecipient"}, IdentityRef: ""},
	}}}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	j, ok := got.Job("main")
	if !ok || j.Compression != "zstd" || len(j.Encryption.Recipients) != 1 {
		t.Fatalf("round-trip mismatch: %+v ok=%v", j, ok)
	}
}

func TestEncryptionConfigHasNoIdentityField(t *testing.T) {
	dir := t.TempDir()
	c := &Config{Version: 1, Jobs: []Job{{
		Name: "main", Engine: "sqlite",
		Encryption: EncryptionConfig{Recipients: []string{"age1x"}, IdentityRef: "env:DBK_AGE_ID"},
	}}}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if strings.Contains(string(b), "AGE-SECRET-KEY") {
		t.Fatalf("config must never contain an age secret key: %s", b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'TestJobCompression|TestEncryptionConfig' -v`
Expected: FAIL (unknown fields).

- [ ] **Step 3: Write the implementation**

In `internal/config/config.go`, add to the `Job` struct:
```go
	Compression string           `yaml:"compression,omitempty"` // "", "none", "gzip", "zstd"
	Encryption  EncryptionConfig `yaml:"encryption,omitempty"`
```
And add the type:
```go
type EncryptionConfig struct {
	// Recipients are age public keys ("age1..."); safe to store in config.
	Recipients []string `yaml:"recipients,omitempty"`
	// IdentityRef is "env:NAME", "file:/path", or empty (meaning the vault key
	// "encryption/<job>/identity"). The raw age secret key is never stored here.
	IdentityRef string `yaml:"identity_ref,omitempty"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat: per-job compression and age encryption config"
```

---

### Task 4: Wire the pipeline into backup and restore

**Files:**
- Modify: `internal/backup/backup.go`
- Modify: `internal/restore/restore.go`
- Modify: `internal/cli/run.go`, `internal/cli/restore.go` (provide the identity resolver)
- Test: `internal/backup/pipeline_wire_test.go`, `internal/restore/pipeline_wire_test.go`

**Interfaces:**
- Consumes: `pipeline.NewWriter/NewReader/Ext/DetectExt`, `config.Job.Compression`, `config.Job.Encryption`.
- Produces:
  - `backup.Run` builds `pipeline.Options{Compression: job.Compression, Recipients: job.Encryption.Recipients}`, appends `pipeline.Ext(opts)` to the key, and wraps the dump with `pipeline.NewWriter`.
  - `restore.Deps` gains `ResolveIdentities func(config.Job) ([]string, error)`; `restore.Run` builds options from the stored key via `pipeline.DetectExt` (+ `Encrypted`), attaches the resolved identities when encrypted, and wraps the download with `pipeline.NewReader`.
  - CLI wires `ResolveIdentities` from `job.Encryption.IdentityRef` (or vault key `encryption/<job>/identity`) via `secretref`.

- [ ] **Step 1: Write the failing test for backup**

`internal/backup/pipeline_wire_test.go`:
```go
package backup

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

func TestRunCompressesAndNamesKeyWithExt(t *testing.T) {
	be := storage.NewLocal(t.TempDir())
	cfg := &config.Config{
		Version: 1,
		Jobs: []config.Job{{Name: "main", Engine: "fake", Storage: "local",
			Conn: config.Conn{Database: "app"}, Compression: "gzip"}},
		Storages: []config.StorageConfig{{Name: "local", Type: "local"}},
	}
	d := Deps{
		Cfg:             cfg,
		Engine:          func(string) (db.Engine, bool) { return &fakeEngine{}, true },
		Backend:         func(config.StorageConfig) (storage.Backend, error) { return be, nil },
		ResolvePassword: func(config.Job) (string, error) { return "", nil },
		Now:             func() time.Time { return time.Date(2026, 10, 12, 3, 4, 5, 0, time.UTC) },
		Log:             logutil.New(io.Discard, logutil.LevelInfo),
	}
	obj, err := Run(context.Background(), d, "main")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.HasSuffix(obj.Key, ".sql.gz") {
		t.Fatalf("key should carry the .gz ext, got %q", obj.Key)
	}
	// Stored bytes are valid gzip wrapping the fake dump "DUMPDATA".
	rc, _ := be.Get(context.Background(), obj.Key)
	gz, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatalf("stored object is not gzip: %v", err)
	}
	got, _ := io.ReadAll(gz)
	rc.Close()
	if !bytes.Equal(got, []byte("DUMPDATA")) {
		t.Fatalf("decompressed content mismatch: %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -run TestRunCompresses -v`
Expected: FAIL (key has no `.gz`; stored bytes are raw, not gzip).

- [ ] **Step 3: Implement the backup wiring**

In `internal/backup/backup.go`, add import `"github.com/tanzid64/tui-db-manager/internal/pipeline"`. Build options and extend the key before the pipe, and wrap the dump:
```go
	opts := pipeline.Options{Compression: job.Compression, Recipients: job.Encryption.Recipients}
	key := BuildKey(job.Name, d.Now(), extFor(job.Engine)) + pipeline.Ext(opts)
	d.Log.Infof("starting backup of job %q -> %s", job.Name, key)

	pr, pw2 := io.Pipe()
	dumpErrCh := make(chan error, 1)
	go func() {
		pw, perr := pipeline.NewWriter(pw2, opts)
		if perr != nil {
			pw2.CloseWithError(perr)
			dumpErrCh <- perr
			return
		}
		derr := eng.Dump(ctx, conn, pw)
		cerr := pw.Close() // flush/finalize compress+encrypt layers
		err := derr
		if err == nil {
			err = cerr
		}
		pw2.CloseWithError(err)
		dumpErrCh <- err
	}()
```
(Delete the old `key := BuildKey(...)` line and the old goroutine that called `eng.Dump` directly; the `putErr`/`dumpErr` switch below stays unchanged.)

- [ ] **Step 4: Write the failing test for restore**

`internal/restore/pipeline_wire_test.go`:
```go
package restore

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"testing"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

// captureEngine records what bytes Restore receives.
type captureEngine struct{ got []byte }

func (e *captureEngine) Name() string                                { return "fake" }
func (e *captureEngine) DefaultPort() int                            { return 0 }
func (e *captureEngine) Detect() (string, error)                     { return "x", nil }
func (e *captureEngine) Test(context.Context, db.Conn) error         { return nil }
func (e *captureEngine) Dump(context.Context, db.Conn, io.Writer) error { return nil }
func (e *captureEngine) Restore(_ context.Context, _ db.Conn, r io.Reader) error {
	b, _ := io.ReadAll(r)
	e.got = b
	return nil
}

func TestRunDecompressesGzipBackup(t *testing.T) {
	be := storage.NewLocal(t.TempDir())
	ctx := context.Background()
	// Seed a gzip-compressed backup under a .gz key.
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	gw.Write([]byte("RESTORED-PAYLOAD"))
	gw.Close()
	be.Put(ctx, "main/main-20261012-030405.sql.gz", bytes.NewReader(gzBuf.Bytes()), -1)

	eng := &captureEngine{}
	cfg := &config.Config{
		Version:  1,
		Jobs:     []config.Job{{Name: "main", Engine: "fake", Storage: "local", Conn: config.Conn{Database: "app"}}},
		Storages: []config.StorageConfig{{Name: "local", Type: "local"}},
	}
	d := Deps{
		Cfg:               cfg,
		Engine:            func(string) (db.Engine, bool) { return eng, true },
		Backend:           func() storage.Backend { return be },
		ResolvePassword:   func(config.Job) (string, error) { return "", nil },
		ResolveIdentities: func(config.Job) ([]string, error) { return nil, nil },
		Log:               logutil.New(io.Discard, logutil.LevelInfo),
	}
	if err := Run(ctx, d, "main", "main/main-20261012-030405.sql.gz", "", true); err != nil {
		t.Fatalf("run: %v", err)
	}
	if string(eng.got) != "RESTORED-PAYLOAD" {
		t.Fatalf("engine should receive decompressed bytes, got %q", eng.got)
	}
}
```

- [ ] **Step 5: Run test to verify it fails**

Run: `go test ./internal/restore/ -run TestRunDecompresses -v`
Expected: FAIL (undefined `ResolveIdentities`; engine gets raw gzip bytes).

- [ ] **Step 6: Implement the restore wiring**

In `internal/restore/restore.go`: add `ResolveIdentities func(config.Job) ([]string, error)` to `Deps`, import `pipeline`, and wrap the download. Replace the `eng.Restore(ctx, conn, rc)` block:
```go
	rc, err := be.Get(ctx, atKey)
	if err != nil {
		return err
	}
	defer rc.Close()

	opts := pipeline.DetectExt(atKey)
	if pipeline.Encrypted(atKey) {
		ids, ierr := d.ResolveIdentities(job)
		if ierr != nil {
			return ierr
		}
		opts.Identities = ids
	}
	pr, perr := pipeline.NewReader(rc, opts)
	if perr != nil {
		return perr
	}
	defer pr.Close()

	if err := eng.Restore(ctx, conn, pr); err != nil {
		return fmt.Errorf("restore: %w", logutil.MaskError(err, pw))
	}
```

- [ ] **Step 7: Wire the identity resolver in the CLI**

In `internal/cli/wire.go`, add:
```go
// resolveIdentities returns a job's age identities (secret keys) from the
// encryption IdentityRef, defaulting to the vault key "encryption/<job>/identity".
func resolveIdentities(v *vault.Store) func(config.Job) ([]string, error) {
	return func(j config.Job) ([]string, error) {
		ref := j.Encryption.IdentityRef
		if ref == "" {
			ref = "encryption/" + j.Name + "/identity"
		}
		var lookup func(string) (string, bool)
		if v != nil {
			lookup = v.Get
		}
		id, err := secretref.Resolve(ref, lookup)
		if err != nil {
			return nil, err
		}
		if id == "" {
			return nil, nil
		}
		return []string{id}, nil
	}
}
```
In `internal/cli/restore.go`, set `ResolveIdentities: resolveIdentities(v)` in the `restore.Deps` literal. (`backup.Run` needs no new resolver — it uses the public recipients straight from config.)

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/backup/ ./internal/restore/ ./internal/cli/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/backup internal/restore internal/cli
git commit -m "feat: apply compression+encryption in backup and reverse on restore"
```

---

### Task 5: `dbk encryption keygen` + docs

**Files:**
- Create: `internal/cli/encryption.go`
- Modify: `internal/cli/root.go` (register `newEncryptionCmd()`)
- Modify: `docs/cloud-storage.md` (or create `docs/encryption.md`)
- Test: `internal/cli/encryption_test.go`

**Interfaces:**
- Produces: `dbk encryption keygen [--job <name>] [--print]`. Generates an age X25519 identity; stores the secret identity in the vault under `encryption/<job>/identity` (or `encryption/identity` if no job); prints the public recipient line (`age1...`) to stdout for pasting into `config.yaml`. With `--print` it also prints the secret to stdout instead of storing it (for `env:`/`file:` workflows).

- [ ] **Step 1: Write the failing test**

`internal/cli/encryption_test.go`:
```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestEncryptionKeygenStoresIdentityAndPrintsRecipient(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DBK_PASSPHRASE", "pw")

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", dir, "encryption", "keygen", "--job", "main"})
	if err := root.Execute(); err != nil {
		t.Fatalf("keygen: %v", err)
	}
	if !strings.Contains(out.String(), "age1") {
		t.Fatalf("keygen must print the public recipient, got %q", out.String())
	}
	if strings.Contains(out.String(), "AGE-SECRET-KEY") {
		t.Fatalf("keygen must NOT print the secret identity by default, got %q", out.String())
	}

	// The secret identity was stored in the vault (listed by key, not value).
	var ls bytes.Buffer
	r2 := NewRootCmd()
	r2.SetOut(&ls)
	r2.SetArgs([]string{"--config", dir, "secret", "ls"})
	if err := r2.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}
	if !strings.Contains(ls.String(), "encryption/main/identity") {
		t.Fatalf("identity should be stored under encryption/main/identity, got %q", ls.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestEncryptionKeygen -v`
Expected: FAIL (unknown command "encryption").

- [ ] **Step 3: Write the implementation**

`internal/cli/encryption.go`:
```go
package cli

import (
	"filippo.io/age"
	"github.com/spf13/cobra"
)

func newEncryptionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "encryption", Short: "Manage at-rest encryption keys"}
	cmd.AddCommand(newEncryptionKeygenCmd())
	return cmd
}

func newEncryptionKeygenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Generate an age key; store the identity in the vault, print the recipient",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			job, _ := cmd.Flags().GetString("job")
			printSecret, _ := cmd.Flags().GetBool("print")

			id, err := age.GenerateX25519Identity()
			if err != nil {
				return err
			}
			cmd.Printf("recipient (put in config.yaml under encryption.recipients):\n%s\n", id.Recipient().String())

			if printSecret {
				cmd.Printf("\nidentity (store securely; referenced via identity_ref):\n%s\n", id.String())
				return nil
			}

			key := "encryption/identity"
			if job != "" {
				key = "encryption/" + job + "/identity"
			}
			_, s, err := openOrCreateVault(cmd)
			if err != nil {
				return err
			}
			s.Set(key, id.String())
			if err := s.Save(); err != nil {
				return err
			}
			cmd.Printf("\nstored identity in the vault under %q\n", key)
			return nil
		},
	}
	cmd.Flags().String("job", "", "store the identity under encryption/<job>/identity")
	cmd.Flags().Bool("print", false, "print the secret identity instead of storing it in the vault")
	return cmd
}
```
Register in `internal/cli/root.go`:
```go
	root.AddCommand(newEncryptionCmd())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestEncryptionKeygen -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Add docs**

Create `docs/encryption.md`:
```markdown
# Compression & at-rest encryption

## Compression
Set `compression` on a job: `none` (default), `gzip`, or `zstd`. The stored
backup's name gains `.gz` / `.zst`, and restore decompresses automatically.

    jobs:
      - name: main
        engine: postgres
        storage: r2
        compression: zstd

## Encryption (age)
Backups are encrypted with a public age recipient; only the matching secret
identity (kept in the vault) can restore them. Generate a key:

    dbk encryption keygen --job main
    # prints: age1....  (the recipient)
    # stores the secret identity in the vault under encryption/main/identity

Put the recipient in the job config:

    jobs:
      - name: main
        engine: postgres
        storage: r2
        compression: zstd
        encryption:
          recipients:
            - age1....

Now `dbk run main` writes `main/main-<ts>.pgdump.zst.age`, unreadable without
the identity. `dbk restore main --yes` resolves the identity from the vault and
reverses encryption + compression automatically. To keep the identity off the
server, use `--print` and reference it with `identity_ref: file:/path` or
`env:NAME` instead of storing it in the vault.
```

- [ ] **Step 6: Run the whole suite and commit**

Run: `go test ./... && go vet ./... && go vet -tags integration ./...`
Expected: all PASS / clean.

```bash
git add internal/cli docs/encryption.md
git commit -m "feat: dbk encryption keygen; document compression and encryption"
```

---

## Self-Review Notes

- **Spec coverage (this plan's slice):** compression (gzip/zstd) and optional at-rest encryption (age X25519) inserted in the canonical dump→compress→encrypt pipeline and reversed on restore (Tasks 1, 2, 4), satisfying the spec's data-flow steps and the "backup-at-rest encryption (optional)" security feature; transform metadata recorded in the key suffix per the spec's restore-correctness risk note (Tasks 1, 2); identity kept out of config, resolved from vault/ref, with a keygen helper (Tasks 3, 5). Not in scope: notifications, scheduling, config export/import, TUI, installers.
- **Placeholder scan:** no TBD/TODO; every code step carries real code.
- **Type consistency:** `pipeline.Options` is the single shape passed on both write and read sides; `Ext`/`DetectExt`/`Encrypted` agree on `.gz`/`.zst`/`.age`; `restore.Deps.ResolveIdentities` added in Task 4 is wired in Task 4 Step 7 and used by the keygen-stored vault key convention in Task 5 (`encryption/<job>/identity`). `backup.Run`'s existing putErr/dumpErr switch is preserved; only the goroutine body and key construction change.
- **Review Focus coverage:** full-matrix round-trip (Task 1 `TestRoundTripCompression`, Task 2 `TestRoundTripCompressionAndEncryption`); wrong/missing identity (Task 2 `TestWrongIdentityFails`); metadata through the key (Task 1 `TestExtAndDetectCompression`, Task 2 `TestExtAndEncryptedMarker`, Task 4 backend/restore wiring tests); identity never in config (Task 3 `TestEncryptionConfigHasNoIdentityField`, Task 5 keygen does-not-print-secret test); streaming + close-order flush (Task 1/2 round-trips through `io.Pipe`-style buffers exercise Close finalization; Task 4 drives it through the real backup pipe).
```
