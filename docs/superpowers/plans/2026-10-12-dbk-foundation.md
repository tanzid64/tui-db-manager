# dbk Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A working `dbk` binary that initializes an encrypted config, connects to MySQL/MariaDB, PostgreSQL, MongoDB, and SQLite, and backs up and restores a database to/from the local filesystem via scriptable CLI subcommands.

**Architecture:** A single Go binary built on Cobra (CLI). Non-secret settings live in `config.yaml`; secrets live in an Argon2id + XChaCha20-Poly1305 encrypted `secrets.enc` vault. Database engines implement a common `db.Engine` interface and shell out to native client tools, passing passwords via environment (never argv). Backups are written through a `storage.Backend` interface whose first implementation is the local filesystem. This plan delivers phases 1–2 of the spec; later plans add cloud storage, compression/encryption, notifications, scheduling, export/import, the TUI, and installers.

**Tech Stack:** Go 1.23+, Cobra, `golang.org/x/crypto` (argon2, chacha20poly1305), `gopkg.in/yaml.v3`, native client tools (`mysqldump`/`mysql`, `pg_dump`/`pg_restore`, `mongodump`/`mongorestore`, `sqlite3`).

**Spec:** `docs/superpowers/specs/2026-10-12-dbk-design.md`

## Global Constraints

- Go version floor: `go 1.23`.
- Module path: `github.com/tanzid64/tui-db-manager`.
- Binary and root command name: `dbk`.
- Config directory default: `$XDG_CONFIG_HOME/dbk` or `$HOME/.config/dbk` (Unix), `%APPDATA%\dbk` (Windows); overridable with the global `--config <dir>` flag.
- Config directory is created `0700`; `config.yaml` and `secrets.enc` are written `0600`.
- Secrets are never passed as process arguments and never written to logs.
- TLS verification for DB connections defaults to on; disabling requires an explicit per-connection setting.
- Each task ends with a green `go build ./...`, passing non-integration tests, and a git commit using Conventional Commits.
- Integration tests that need real databases are gated behind `//go:build integration` and are not run by default `go test ./...`.
- Commit messages must NOT include any `Co-Authored-By` trailer or "Generated with Claude Code" footer (user global preference).

## Review Focus

- **Wrong vault passphrase:** opening the vault with the wrong passphrase must fail with a clear "invalid passphrase or corrupted vault" error, never a panic or a partial/garbage decrypt — pinned in Task 5.
- **Tampered vault file:** a flipped byte in `secrets.enc` must be rejected by AEAD authentication, not silently accepted — pinned in Task 4.
- **Missing native client tool:** running a backup when `mysqldump`/`pg_dump`/etc. is absent must produce an actionable "install X" message naming the tool, not an opaque `exec: not found` — pinned in Task 9.
- **Password containing shell metacharacters / spaces:** a DB password like `p@ss w"rd$x` must authenticate correctly because it is passed via environment, not a shell string — pinned in Task 8.
- **Restore overwriting the wrong target without consent:** `dbk restore` must require confirmation (or `--yes`) and must honor `--to` so a restore cannot silently clobber the configured production database — pinned in Task 15.

---

### Task 1: Project scaffold and `version` command

**Files:**
- Create: `go.mod`
- Create: `cmd/dbk/main.go`
- Create: `internal/cli/root.go`
- Create: `internal/cli/version.go`
- Create: `internal/buildinfo/buildinfo.go`
- Test: `internal/cli/version_test.go`

**Interfaces:**
- Produces: `cli.NewRootCmd() *cobra.Command`; `buildinfo.Version string` (var, set via ldflags, default `"dev"`).

- [ ] **Step 1: Initialize the module**

Run:
```bash
go mod init github.com/tanzid64/tui-db-manager
go get github.com/spf13/cobra@v1.8.1
```

- [ ] **Step 2: Write the failing test**

`internal/cli/version_test.go`:
```go
package cli

import (
	"bytes"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := out.String(); got == "" || got[0:3] != "dbk" {
		t.Fatalf("expected version output starting with 'dbk', got %q", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestVersionCommand -v`
Expected: FAIL (package/function not defined).

- [ ] **Step 4: Write the implementation**

`internal/buildinfo/buildinfo.go`:
```go
package buildinfo

// Version is overridden at release time via -ldflags "-X .../buildinfo.Version=...".
var Version = "dev"
```

`internal/cli/root.go`:
```go
package cli

import "github.com/spf13/cobra"

// NewRootCmd builds the dbk command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "dbk",
		Short:         "Secure database backup CLI/TUI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().String("config", "", "config directory (default: OS config dir + /dbk)")
	root.AddCommand(newVersionCmd())
	return root
}
```

`internal/cli/version.go`:
```go
package cli

import (
	"github.com/tanzid64/tui-db-manager/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the dbk version",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Printf("dbk %s\n", buildinfo.Version)
			return nil
		},
	}
}
```

`cmd/dbk/main.go`:
```go
package main

import (
	"fmt"
	"os"

	"github.com/tanzid64/tui-db-manager/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "dbk:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestVersionCommand -v && go build ./...`
Expected: PASS and clean build.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum cmd internal
git commit -m "feat: scaffold dbk binary with version command"
```

---

### Task 2: Config directory resolution and config file I/O

**Files:**
- Create: `internal/config/paths.go`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `config.Dir(override string) (string, error)` — resolves and creates (`0700`) the config dir.
  - Types `Config`, `Job`, `Conn`, `Retention`, `StorageConfig` (fields as below).
  - `config.Load(dir string) (*Config, error)` — returns an empty `&Config{Version: 1}` if the file is absent.
  - `config.Save(dir string, c *Config) error` — writes `config.yaml` with `0600`.
  - `(*Config).Job(name string) (Job, bool)` and `(*Config).Storage(name string) (StorageConfig, bool)`.

- [ ] **Step 1: Add the YAML dependency**

Run: `go get gopkg.in/yaml.v3@v3.0.1`

- [ ] **Step 2: Write the failing test**

`internal/config/config_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := &Config{
		Version: 1,
		Jobs: []Job{{
			Name:   "main",
			Engine: "mysql",
			Conn:   Conn{Host: "127.0.0.1", Port: 3306, User: "root", Database: "app"},
			Storage: "local",
			Retention: Retention{KeepLast: 7},
		}},
		Storages: []StorageConfig{{Name: "local", Type: "local", Path: "/var/backups/dbk"}},
	}
	if err := Save(dir, c); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected 0600, got %o", perm)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	j, ok := got.Job("main")
	if !ok || j.Engine != "mysql" || j.Conn.Port != 3306 {
		t.Fatalf("job round-trip mismatch: %+v ok=%v", j, ok)
	}
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if got.Version != 1 || len(got.Jobs) != 0 {
		t.Fatalf("expected empty config v1, got %+v", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/ -v`
Expected: FAIL (undefined types/functions).

- [ ] **Step 4: Write the implementation**

`internal/config/config.go`:
```go
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const CurrentVersion = 1

type Config struct {
	Version   int             `yaml:"version"`
	Jobs      []Job           `yaml:"jobs"`
	Storages  []StorageConfig `yaml:"storages"`
}

type Job struct {
	Name      string    `yaml:"name"`
	Engine    string    `yaml:"engine"` // mysql|postgres|mongodb|sqlite
	Conn      Conn      `yaml:"conn"`
	Storage   string    `yaml:"storage"`
	Retention Retention `yaml:"retention"`
}

type Conn struct {
	Host     string `yaml:"host,omitempty"`
	Port     int    `yaml:"port,omitempty"`
	User     string `yaml:"user,omitempty"`
	Database string `yaml:"database,omitempty"`
	Path     string `yaml:"path,omitempty"` // sqlite file path
	TLS      bool   `yaml:"tls,omitempty"`
	// PasswordRef optionally resolves the password from "env:VAR" or "file:/path"
	// instead of the vault. Empty means "look in the vault".
	PasswordRef string `yaml:"password_ref,omitempty"`
}

type Retention struct {
	KeepLast int `yaml:"keep_last,omitempty"`
}

type StorageConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"` // local (cloud types added in a later plan)
	Path string `yaml:"path,omitempty"`
}

func fileName(dir string) string { return filepath.Join(dir, "config.yaml") }

func Load(dir string) (*Config, error) {
	b, err := os.ReadFile(fileName(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{Version: CurrentVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	return &c, nil
}

func Save(dir string, c *Config) error {
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp := fileName(dir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, fileName(dir))
}

func (c *Config) Job(name string) (Job, bool) {
	for _, j := range c.Jobs {
		if j.Name == name {
			return j, true
		}
	}
	return Job{}, false
}

func (c *Config) Storage(name string) (StorageConfig, bool) {
	for _, s := range c.Storages {
		if s.Name == name {
			return s, true
		}
	}
	return StorageConfig{}, false
}
```

`internal/config/paths.go`:
```go
package config

import (
	"os"
	"path/filepath"
)

// Dir resolves the config directory (creating it 0700) using override if set,
// else the OS user config dir + "/dbk".
func Dir(override string) (string, error) {
	dir := override
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "dbk")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/config/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config go.mod go.sum
git commit -m "feat: config model with secure-permission file I/O"
```

---

### Task 3: Argon2id key derivation

**Files:**
- Create: `internal/vault/kdf.go`
- Test: `internal/vault/kdf_test.go`

**Interfaces:**
- Produces:
  - Type `KDFParams struct { Salt []byte; Time, Memory uint32; Threads uint8 }`.
  - `vault.DefaultKDFParams() KDFParams` — generates a fresh 16-byte random salt; Time=3, Memory=64*1024, Threads=4.
  - `vault.DeriveKey(passphrase string, p KDFParams) []byte` — returns a 32-byte key.

- [ ] **Step 1: Add the crypto dependency**

Run: `go get golang.org/x/crypto@v0.28.0`

- [ ] **Step 2: Write the failing test**

`internal/vault/kdf_test.go`:
```go
package vault

import (
	"bytes"
	"testing"
)

func TestDeriveKeyDeterministicForSameParams(t *testing.T) {
	p := DefaultKDFParams()
	k1 := DeriveKey("correct horse", p)
	k2 := DeriveKey("correct horse", p)
	if len(k1) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(k1))
	}
	if !bytes.Equal(k1, k2) {
		t.Fatal("same passphrase+params must derive the same key")
	}
}

func TestDeriveKeyDiffersByPassphrase(t *testing.T) {
	p := DefaultKDFParams()
	if bytes.Equal(DeriveKey("a", p), DeriveKey("b", p)) {
		t.Fatal("different passphrases must derive different keys")
	}
}

func TestDefaultParamsFreshSalt(t *testing.T) {
	if bytes.Equal(DefaultKDFParams().Salt, DefaultKDFParams().Salt) {
		t.Fatal("each DefaultKDFParams call must produce a fresh salt")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/vault/ -run TestDerive -v`
Expected: FAIL (undefined).

- [ ] **Step 4: Write the implementation**

`internal/vault/kdf.go`:
```go
package vault

import (
	"crypto/rand"

	"golang.org/x/crypto/argon2"
)

type KDFParams struct {
	Salt    []byte `json:"salt"`
	Time    uint32 `json:"time"`
	Memory  uint32 `json:"memory"`
	Threads uint8  `json:"threads"`
}

func DefaultKDFParams() KDFParams {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic("vault: cannot read random salt: " + err.Error())
	}
	return KDFParams{Salt: salt, Time: 3, Memory: 64 * 1024, Threads: 4}
}

// DeriveKey returns a 32-byte key for XChaCha20-Poly1305.
func DeriveKey(passphrase string, p KDFParams) []byte {
	return argon2.IDKey([]byte(passphrase), p.Salt, p.Time, p.Memory, p.Threads, 32)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/vault/ -run TestDerive -v && go test ./internal/vault/ -run TestDefaultParams -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/vault go.mod go.sum
git commit -m "feat: Argon2id key derivation for vault"
```

---

### Task 4: Encrypted vault file format (seal/open with tamper detection)

**Files:**
- Create: `internal/vault/file.go`
- Test: `internal/vault/file_test.go`

**Interfaces:**
- Consumes: `KDFParams`, `DeriveKey`, `DefaultKDFParams` from Task 3.
- Produces:
  - `vault.ErrBadPassphrase` (sentinel error).
  - `vault.seal(key, plaintext []byte, p KDFParams) ([]byte, error)` — returns a self-describing file blob (magic + version + KDF header + nonce + ciphertext).
  - `vault.open(key []byte, blob []byte) (plaintext []byte, p KDFParams, err error)` — returns `ErrBadPassphrase` on auth failure.

- [ ] **Step 1: Write the failing test**

`internal/vault/file_test.go`:
```go
package vault

import (
	"bytes"
	"errors"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	p := DefaultKDFParams()
	key := DeriveKey("pw", p)
	blob, err := seal(key, []byte(`{"k":"v"}`), p)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	pt, gotP, err := open(key, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(pt) != `{"k":"v"}` {
		t.Fatalf("plaintext mismatch: %s", pt)
	}
	if !bytes.Equal(gotP.Salt, p.Salt) {
		t.Fatal("KDF salt must survive the round trip")
	}
}

func TestOpenWrongKeyFails(t *testing.T) {
	p := DefaultKDFParams()
	blob, _ := seal(DeriveKey("right", p), []byte("secret"), p)
	_, _, err := open(DeriveKey("wrong", p), blob)
	if !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("expected ErrBadPassphrase, got %v", err)
	}
}

func TestOpenTamperedBlobFails(t *testing.T) {
	p := DefaultKDFParams()
	key := DeriveKey("pw", p)
	blob, _ := seal(key, []byte("secret"), p)
	blob[len(blob)-1] ^= 0xFF // flip a ciphertext byte
	if _, _, err := open(key, blob); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("tampered blob must fail auth, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vault/ -run TestSeal -v && go test ./internal/vault/ -run TestOpen -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Write the implementation**

Run: `go get golang.org/x/crypto/chacha20poly1305` (already vendored by the x/crypto module from Task 3).

`internal/vault/file.go`:
```go
package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

var ErrBadPassphrase = errors.New("invalid passphrase or corrupted vault")

var magic = []byte("DBKV")

const formatVersion = 1

// header is the authenticated, plaintext-readable file preamble.
type header struct {
	Version int       `json:"v"`
	KDF     KDFParams `json:"kdf"`
}

// seal layout: magic(4) | hlen(4, big-endian) | header-json | nonce(24) | ciphertext.
// The header JSON is used as AEAD additional data so KDF params cannot be swapped.
func seal(key, plaintext []byte, p KDFParams) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	hb, err := json.Marshal(header{Version: formatVersion, KDF: p})
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := aead.Seal(nil, nonce, plaintext, hb)

	var buf bytes.Buffer
	buf.Write(magic)
	buf.Write(uint32be(len(hb)))
	buf.Write(hb)
	buf.Write(nonce)
	buf.Write(ct)
	return buf.Bytes(), nil
}

func open(key, blob []byte) ([]byte, KDFParams, error) {
	if len(blob) < 8 || !bytes.Equal(blob[:4], magic) {
		return nil, KDFParams{}, fmt.Errorf("not a dbk vault file")
	}
	hlen := int(uint32(blob[4])<<24 | uint32(blob[5])<<16 | uint32(blob[6])<<8 | uint32(blob[7]))
	rest := blob[8:]
	if hlen < 0 || hlen > len(rest) {
		return nil, KDFParams{}, fmt.Errorf("corrupt vault header")
	}
	hb := rest[:hlen]
	var h header
	if err := json.Unmarshal(hb, &h); err != nil {
		return nil, KDFParams{}, fmt.Errorf("corrupt vault header: %w", err)
	}
	body := rest[hlen:]
	if len(body) < chacha20poly1305.NonceSizeX {
		return nil, KDFParams{}, fmt.Errorf("corrupt vault body")
	}
	nonce := body[:chacha20poly1305.NonceSizeX]
	ct := body[chacha20poly1305.NonceSizeX:]
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, KDFParams{}, err
	}
	pt, err := aead.Open(nil, nonce, ct, hb)
	if err != nil {
		return nil, KDFParams{}, ErrBadPassphrase
	}
	return pt, h.KDF, nil
}

func uint32be(n int) []byte {
	return []byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vault/ -v`
Expected: PASS (all vault tests).

- [ ] **Step 5: Commit**

```bash
git add internal/vault go.mod go.sum
git commit -m "feat: authenticated encrypted vault file format"
```

---

### Task 5: Vault store (load/save secrets, passphrase sources, wrong-passphrase handling)

**Files:**
- Create: `internal/vault/vault.go`
- Create: `internal/vault/passphrase.go`
- Test: `internal/vault/vault_test.go`

**Interfaces:**
- Consumes: `seal`, `open`, `DeriveKey`, `DefaultKDFParams`, `ErrBadPassphrase`.
- Produces:
  - `type Store struct { ... }` holding a `map[string]string` of secrets and the file path.
  - `vault.Path(dir string) string` → `filepath.Join(dir, "secrets.enc")`.
  - `vault.Exists(dir string) bool`.
  - `vault.Create(dir, passphrase string) (*Store, error)` — fresh empty vault, persisted.
  - `vault.Open(dir, passphrase string) (*Store, error)` — returns `ErrBadPassphrase` on wrong passphrase.
  - `(*Store) Get(key string) (string, bool)`, `(*Store) Set(key, val string)`, `(*Store) Delete(key string)`, `(*Store) Save() error`.
  - `vault.ReadPassphrase(dir string, passFile string, interactive bool) (string, error)` — order: `DBK_PASSPHRASE` env → `passFile` → interactive terminal prompt; error if none available and not interactive.

- [ ] **Step 1: Write the failing test**

`internal/vault/vault_test.go`:
```go
package vault

import (
	"errors"
	"testing"
)

func TestCreateSetSaveOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir, "pw")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	s.Set("job/main/password", "s3cr3t")
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	reopened, err := Open(dir, "pw")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, ok := reopened.Get("job/main/password")
	if !ok || got != "s3cr3t" {
		t.Fatalf("secret round-trip failed: %q ok=%v", got, ok)
	}
}

func TestOpenWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	s, _ := Create(dir, "right")
	_ = s.Save()
	if _, err := Open(dir, "wrong"); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("expected ErrBadPassphrase, got %v", err)
	}
}

func TestReadPassphraseFromEnv(t *testing.T) {
	t.Setenv("DBK_PASSPHRASE", "fromenv")
	got, err := ReadPassphrase(t.TempDir(), "", false)
	if err != nil || got != "fromenv" {
		t.Fatalf("env passphrase: got %q err %v", got, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vault/ -run 'TestCreate|TestOpenWrong|TestReadPassphrase' -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Write the implementation**

`internal/vault/vault.go`:
```go
package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Store struct {
	path    string
	key     []byte
	params  KDFParams
	secrets map[string]string
}

func Path(dir string) string { return filepath.Join(dir, "secrets.enc") }

func Exists(dir string) bool {
	_, err := os.Stat(Path(dir))
	return err == nil
}

func Create(dir, passphrase string) (*Store, error) {
	p := DefaultKDFParams()
	return &Store{
		path:    Path(dir),
		key:     DeriveKey(passphrase, p),
		params:  p,
		secrets: map[string]string{},
	}, nil
}

func Open(dir, passphrase string) (*Store, error) {
	blob, err := os.ReadFile(Path(dir))
	if err != nil {
		return nil, err
	}
	// Peek KDF params from the header by trying with a throwaway key fails;
	// instead derive after reading params. open() needs the key, and the key
	// needs the salt from the header — so read the header params first.
	p, err := peekParams(blob)
	if err != nil {
		return nil, err
	}
	key := DeriveKey(passphrase, p)
	pt, gotP, err := open(key, blob)
	if err != nil {
		return nil, err
	}
	var secrets map[string]string
	if err := json.Unmarshal(pt, &secrets); err != nil {
		return nil, err
	}
	return &Store{path: Path(dir), key: key, params: gotP, secrets: secrets}, nil
}

func (s *Store) Get(key string) (string, bool) { v, ok := s.secrets[key]; return v, ok }
func (s *Store) Set(key, val string)           { s.secrets[key] = val }
func (s *Store) Delete(key string)             { delete(s.secrets, key) }

func (s *Store) Save() error {
	pt, err := json.Marshal(s.secrets)
	if err != nil {
		return err
	}
	blob, err := seal(s.key, pt, s.params)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
```

Add `peekParams` to `internal/vault/file.go`:
```go
// peekParams reads the (authenticated-on-open) KDF header so the caller can
// derive the key. The values are not trusted until open() verifies the AEAD tag.
func peekParams(blob []byte) (KDFParams, error) {
	if len(blob) < 8 || !bytes.Equal(blob[:4], magic) {
		return KDFParams{}, fmt.Errorf("not a dbk vault file")
	}
	hlen := int(uint32(blob[4])<<24 | uint32(blob[5])<<16 | uint32(blob[6])<<8 | uint32(blob[7]))
	rest := blob[8:]
	if hlen < 0 || hlen > len(rest) {
		return KDFParams{}, fmt.Errorf("corrupt vault header")
	}
	var h header
	if err := json.Unmarshal(rest[:hlen], &h); err != nil {
		return KDFParams{}, fmt.Errorf("corrupt vault header: %w", err)
	}
	return h.KDF, nil
}
```

`internal/vault/passphrase.go`:
```go
package vault

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadPassphrase resolves the vault passphrase from, in order: the
// DBK_PASSPHRASE env var, a passphrase file, or an interactive TTY prompt.
func ReadPassphrase(dir, passFile string, interactive bool) (string, error) {
	if v := os.Getenv("DBK_PASSPHRASE"); v != "" {
		return v, nil
	}
	if passFile != "" {
		b, err := os.ReadFile(passFile)
		if err != nil {
			return "", fmt.Errorf("read passphrase file: %w", err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	if !interactive || !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", fmt.Errorf("no passphrase available: set DBK_PASSPHRASE or use --passphrase-file")
	}
	fmt.Fprint(os.Stderr, "Vault passphrase: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
```

Run: `go get golang.org/x/term@v0.25.0`

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vault/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vault go.mod go.sum
git commit -m "feat: encrypted secret store with passphrase sources"
```

---

### Task 6: Secret-redacting logger

**Files:**
- Create: `internal/logutil/logutil.go`
- Test: `internal/logutil/logutil_test.go`

**Interfaces:**
- Produces:
  - `logutil.New(w io.Writer, level Level) *Logger`.
  - `(*Logger) Redact(secret string)` — registers a literal string to mask in all future output.
  - `(*Logger) Infof(format string, args ...any)`, `Errorf(...)`, `Debugf(...)`.
  - Masked substrings are replaced with `***`.

- [ ] **Step 1: Write the failing test**

`internal/logutil/logutil_test.go`:
```go
package logutil

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedactMasksSecrets(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelInfo)
	l.Redact("s3cr3t")
	l.Infof("connecting with password=%s", "s3cr3t")
	out := buf.String()
	if strings.Contains(out, "s3cr3t") {
		t.Fatalf("secret leaked into log: %q", out)
	}
	if !strings.Contains(out, "***") {
		t.Fatalf("expected masked marker, got %q", out)
	}
}

func TestDebugSuppressedAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelInfo)
	l.Debugf("hidden")
	if buf.Len() != 0 {
		t.Fatalf("debug should be suppressed at info level, got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/logutil/ -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Write the implementation**

`internal/logutil/logutil.go`:
```go
package logutil

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelError
)

type Logger struct {
	mu      sync.Mutex
	w       io.Writer
	level   Level
	secrets []string
}

func New(w io.Writer, level Level) *Logger { return &Logger{w: w, level: level} }

// Redact registers a literal secret to be masked in all future output.
func (l *Logger) Redact(secret string) {
	if secret == "" {
		return
	}
	l.mu.Lock()
	l.secrets = append(l.secrets, secret)
	l.mu.Unlock()
}

func (l *Logger) mask(s string) string {
	for _, sec := range l.secrets {
		s = strings.ReplaceAll(s, sec, "***")
	}
	return s
}

func (l *Logger) logf(level Level, prefix, format string, args ...any) {
	if level < l.level {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.w, "%s %s\n", prefix, l.mask(fmt.Sprintf(format, args...)))
}

func (l *Logger) Debugf(f string, a ...any) { l.logf(LevelDebug, "[debug]", f, a...) }
func (l *Logger) Infof(f string, a ...any)  { l.logf(LevelInfo, "[info]", f, a...) }
func (l *Logger) Errorf(f string, a ...any) { l.logf(LevelError, "[error]", f, a...) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/logutil/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/logutil
git commit -m "feat: secret-redacting logger"
```

---

### Task 7: `db.Engine` interface, types, and registry

**Files:**
- Create: `internal/db/engine.go`
- Create: `internal/db/registry.go`
- Test: `internal/db/registry_test.go`

**Interfaces:**
- Produces:
  - `type Conn struct { Host string; Port int; User string; Password string; Database string; Path string; TLS bool }` (runtime connection with the resolved password).
  - `type Job struct { Name string; Database string; Conn Conn }`.
  - `type Engine interface { Name() string; DefaultPort() int; Detect() (string, error); Test(ctx, Conn) error; Dump(ctx, Conn, io.Writer) error; Restore(ctx, Conn, io.Reader) error }`.
  - `db.Register(e Engine)` and `db.Get(name string) (Engine, bool)` and `db.Names() []string`.

- [ ] **Step 1: Write the failing test**

`internal/db/registry_test.go`:
```go
package db

import (
	"context"
	"io"
	"testing"
)

type fakeEngine struct{ name string }

func (f fakeEngine) Name() string                                   { return f.name }
func (f fakeEngine) DefaultPort() int                               { return 1234 }
func (f fakeEngine) Detect() (string, error)                        { return "/bin/true", nil }
func (f fakeEngine) Test(context.Context, Conn) error               { return nil }
func (f fakeEngine) Dump(context.Context, Conn, io.Writer) error    { return nil }
func (f fakeEngine) Restore(context.Context, Conn, io.Reader) error { return nil }

func TestRegisterAndGet(t *testing.T) {
	Register(fakeEngine{name: "fake"})
	e, ok := Get("fake")
	if !ok || e.Name() != "fake" {
		t.Fatalf("expected registered engine, got %v ok=%v", e, ok)
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown engine should not be found")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Write the implementation**

`internal/db/engine.go`:
```go
package db

import (
	"context"
	"io"
)

// Conn is a resolved connection; Password is already pulled from the vault/refs.
type Conn struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
	Path     string // sqlite
	TLS      bool
}

type Job struct {
	Name     string
	Database string
	Conn     Conn
}

type Engine interface {
	Name() string
	DefaultPort() int
	Detect() (clientPath string, err error)
	Test(ctx context.Context, c Conn) error
	Dump(ctx context.Context, c Conn, w io.Writer) error
	Restore(ctx context.Context, c Conn, r io.Reader) error
}
```

`internal/db/registry.go`:
```go
package db

import "sort"

var registry = map[string]Engine{}

func Register(e Engine) { registry[e.Name()] = e }

func Get(name string) (Engine, bool) { e, ok := registry[name]; return e, ok }

func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/db/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db
git commit -m "feat: db engine interface and registry"
```

---

### Task 8: Client-tool command runner (passwords via env, not argv)

**Files:**
- Create: `internal/db/run.go`
- Test: `internal/db/run_test.go`

**Interfaces:**
- Consumes: nothing from prior db tasks beyond the package.
- Produces:
  - `type cmdSpec struct { Path string; Args []string; Env []string; Stdin io.Reader; Stdout io.Writer }`.
  - `db.runCmd(ctx context.Context, s cmdSpec) error` — runs the command, inherits the current environment plus `s.Env`, captures stderr and includes it in the returned error.
  - `db.lookTool(primary string, alternatives ...string) (string, error)` — returns the first tool found on PATH, or an error listing what to install.

- [ ] **Step 1: Write the failing test**

`internal/db/run_test.go`:
```go
package db

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestRunCmdCapturesStderrOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	err := runCmd(context.Background(), cmdSpec{
		Path: "/bin/sh",
		Args: []string{"-c", "echo boom 1>&2; exit 3"},
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error containing stderr 'boom', got %v", err)
	}
}

func TestRunCmdPassesEnvAndStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	var out bytes.Buffer
	err := runCmd(context.Background(), cmdSpec{
		Path:   "/bin/sh",
		Args:   []string{"-c", "printf %s \"$SECRET\""},
		Env:    []string{"SECRET=p@ss w\"rd$x"},
		Stdout: &out,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.String() != `p@ss w"rd$x` {
		t.Fatalf("env value not passed verbatim, got %q", out.String())
	}
}

func TestLookToolMissing(t *testing.T) {
	_, err := lookTool("definitely-not-a-real-tool-xyz")
	if err == nil || !strings.Contains(err.Error(), "definitely-not-a-real-tool-xyz") {
		t.Fatalf("expected missing-tool error naming the tool, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run 'TestRunCmd|TestLookTool' -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Write the implementation**

`internal/db/run.go`:
```go
package db

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type cmdSpec struct {
	Path   string
	Args   []string
	Env    []string // appended to the current environment
	Stdin  io.Reader
	Stdout io.Writer
}

func runCmd(ctx context.Context, s cmdSpec) error {
	cmd := exec.CommandContext(ctx, s.Path, s.Args...)
	cmd.Env = append(os.Environ(), s.Env...)
	cmd.Stdin = s.Stdin
	cmd.Stdout = s.Stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return fmt.Errorf("%s: %w", s.Path, err)
		}
		return fmt.Errorf("%s: %w: %s", s.Path, err, msg)
	}
	return nil
}

// lookTool returns the first of the candidates found on PATH.
func lookTool(primary string, alternatives ...string) (string, error) {
	for _, name := range append([]string{primary}, alternatives...) {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	all := strings.Join(append([]string{primary}, alternatives...), " or ")
	return "", fmt.Errorf("required client tool not found on PATH: install %s", all)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/db/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db
git commit -m "feat: client-tool runner with env-based secrets"
```

---

### Task 9: MySQL/MariaDB engine driver

**Files:**
- Create: `internal/db/mysql.go`
- Create: `internal/db/init.go`
- Test: `internal/db/mysql_test.go`
- Test: `internal/db/mysql_integration_test.go`

**Interfaces:**
- Consumes: `Engine`, `Conn`, `Register`, `cmdSpec`, `runCmd`, `lookTool`.
- Produces: `mysqlEngine` registered under `"mysql"`; exported helper `BuildMySQLDumpArgs(c Conn) []string` and `MySQLEnv(c Conn) []string` so command construction is unit-testable without a server.

- [ ] **Step 1: Write the failing test (unit, no server)**

`internal/db/mysql_test.go`:
```go
package db

import (
	"strings"
	"testing"
)

func TestMySQLEnvCarriesPasswordNotArgs(t *testing.T) {
	c := Conn{Host: "h", Port: 3306, User: "u", Password: `p@ss w"rd$x`, Database: "app"}
	env := MySQLEnv(c)
	found := false
	for _, e := range env {
		if e == `MYSQL_PWD=p@ss w"rd$x` {
			found = true
		}
	}
	if !found {
		t.Fatalf("password must be passed via MYSQL_PWD env, got %v", env)
	}
	for _, a := range BuildMySQLDumpArgs(c) {
		if strings.Contains(a, "p@ss") {
			t.Fatalf("password leaked into argv: %q", a)
		}
	}
}

func TestMySQLDumpArgsIncludeTarget(t *testing.T) {
	args := BuildMySQLDumpArgs(Conn{Host: "h", Port: 3306, User: "u", Database: "app"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"--host=h", "--port=3306", "--user=u", "app"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in args %q", want, joined)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestMySQL -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Write the implementation**

`internal/db/mysql.go`:
```go
package db

import (
	"context"
	"fmt"
	"io"
)

type mysqlEngine struct{}

func (mysqlEngine) Name() string     { return "mysql" }
func (mysqlEngine) DefaultPort() int { return 3306 }

func (mysqlEngine) Detect() (string, error) {
	return lookTool("mysqldump", "mariadb-dump")
}

func MySQLEnv(c Conn) []string {
	if c.Password == "" {
		return nil
	}
	return []string{"MYSQL_PWD=" + c.Password}
}

func BuildMySQLDumpArgs(c Conn) []string {
	args := []string{
		fmt.Sprintf("--host=%s", c.Host),
		fmt.Sprintf("--port=%d", c.Port),
		fmt.Sprintf("--user=%s", c.User),
		"--single-transaction", "--quick", "--routines", "--triggers", "--events",
	}
	if c.TLS {
		args = append(args, "--ssl-mode=REQUIRED")
	}
	args = append(args, c.Database)
	return args
}

func (e mysqlEngine) Test(ctx context.Context, c Conn) error {
	tool, err := lookTool("mysql", "mariadb")
	if err != nil {
		return err
	}
	return runCmd(ctx, cmdSpec{
		Path: tool,
		Args: []string{fmt.Sprintf("--host=%s", c.Host), fmt.Sprintf("--port=%d", c.Port),
			fmt.Sprintf("--user=%s", c.User), "--execute=SELECT 1", c.Database},
		Env: MySQLEnv(c),
	})
}

func (e mysqlEngine) Dump(ctx context.Context, c Conn, w io.Writer) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	return runCmd(ctx, cmdSpec{Path: tool, Args: BuildMySQLDumpArgs(c), Env: MySQLEnv(c), Stdout: w})
}

func (e mysqlEngine) Restore(ctx context.Context, c Conn, r io.Reader) error {
	tool, err := lookTool("mysql", "mariadb")
	if err != nil {
		return err
	}
	args := []string{fmt.Sprintf("--host=%s", c.Host), fmt.Sprintf("--port=%d", c.Port),
		fmt.Sprintf("--user=%s", c.User), c.Database}
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Env: MySQLEnv(c), Stdin: r})
}
```

`internal/db/init.go`:
```go
package db

func init() {
	Register(mysqlEngine{})
	Register(postgresEngine{})
	Register(mongoEngine{})
	Register(sqliteEngine{})
}
```

(Note: `postgresEngine`, `mongoEngine`, `sqliteEngine` are added in Tasks 10–12; until then, comment out the not-yet-defined registrations or implement Tasks 10–12 before building. To keep each task's build green, register each engine in its own file's `init()` instead — see Step 3b.)

- [ ] **Step 3b: Use per-file init to keep builds green**

Replace `internal/db/init.go` with a per-engine registration. In `mysql.go` add:
```go
func init() { Register(mysqlEngine{}) }
```
Delete `internal/db/init.go`. Each later engine task registers itself the same way.

- [ ] **Step 4: Run unit test to verify it passes**

Run: `go test ./internal/db/ -run TestMySQL -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Write the integration test (gated)**

`internal/db/mysql_integration_test.go`:
```go
//go:build integration

package db

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

// Requires a running MySQL reachable via DBK_IT_MYSQL_* env vars.
func TestMySQLDumpRestoreIntegration(t *testing.T) {
	c := Conn{
		Host: envOr("DBK_IT_MYSQL_HOST", "127.0.0.1"), Port: 3306,
		User: envOr("DBK_IT_MYSQL_USER", "root"),
		Password: os.Getenv("DBK_IT_MYSQL_PASSWORD"),
		Database: envOr("DBK_IT_MYSQL_DB", "dbk_it"),
	}
	e := mysqlEngine{}
	var dump bytes.Buffer
	if err := e.Dump(context.Background(), c, &dump); err != nil {
		t.Fatalf("dump: %v", err)
	}
	if !strings.Contains(dump.String(), "CREATE TABLE") && dump.Len() == 0 {
		t.Fatalf("expected non-empty dump")
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
```

- [ ] **Step 6: Commit**

```bash
git add internal/db
git commit -m "feat: mysql/mariadb engine driver"
```

---

### Task 10: PostgreSQL engine driver

**Files:**
- Create: `internal/db/postgres.go`
- Test: `internal/db/postgres_test.go`
- Test: `internal/db/postgres_integration_test.go`

**Interfaces:**
- Consumes: same package primitives.
- Produces: `postgresEngine` registered under `"postgres"`; `BuildPgDumpArgs(c Conn) []string`; `PgEnv(c Conn) []string` (sets `PGPASSWORD`, and `PGSSLMODE=require` when `c.TLS`).

- [ ] **Step 1: Write the failing test**

`internal/db/postgres_test.go`:
```go
package db

import (
	"strings"
	"testing"
)

func TestPgEnvPassword(t *testing.T) {
	env := PgEnv(Conn{Password: "pw", TLS: true})
	j := strings.Join(env, " ")
	if !strings.Contains(j, "PGPASSWORD=pw") || !strings.Contains(j, "PGSSLMODE=require") {
		t.Fatalf("expected PGPASSWORD and PGSSLMODE, got %v", env)
	}
}

func TestPgDumpArgsCustomFormat(t *testing.T) {
	args := BuildPgDumpArgs(Conn{Host: "h", Port: 5432, User: "u", Database: "app"})
	j := strings.Join(args, " ")
	for _, w := range []string{"--host=h", "--port=5432", "--username=u", "--format=custom", "app"} {
		if !strings.Contains(j, w) {
			t.Fatalf("expected %q in %q", w, j)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestPg -v`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

`internal/db/postgres.go`:
```go
package db

import (
	"context"
	"fmt"
	"io"
)

type postgresEngine struct{}

func init() { Register(postgresEngine{}) }

func (postgresEngine) Name() string     { return "postgres" }
func (postgresEngine) DefaultPort() int { return 5432 }

func (postgresEngine) Detect() (string, error) { return lookTool("pg_dump") }

func PgEnv(c Conn) []string {
	env := []string{}
	if c.Password != "" {
		env = append(env, "PGPASSWORD="+c.Password)
	}
	if c.TLS {
		env = append(env, "PGSSLMODE=require")
	}
	return env
}

func BuildPgDumpArgs(c Conn) []string {
	return []string{
		fmt.Sprintf("--host=%s", c.Host),
		fmt.Sprintf("--port=%d", c.Port),
		fmt.Sprintf("--username=%s", c.User),
		"--no-password", "--format=custom", c.Database,
	}
}

func (e postgresEngine) Test(ctx context.Context, c Conn) error {
	tool, err := lookTool("psql")
	if err != nil {
		return err
	}
	return runCmd(ctx, cmdSpec{Path: tool, Env: PgEnv(c),
		Args: []string{fmt.Sprintf("--host=%s", c.Host), fmt.Sprintf("--port=%d", c.Port),
			fmt.Sprintf("--username=%s", c.User), "--no-password", "-c", "SELECT 1", c.Database}})
}

func (e postgresEngine) Dump(ctx context.Context, c Conn, w io.Writer) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	return runCmd(ctx, cmdSpec{Path: tool, Args: BuildPgDumpArgs(c), Env: PgEnv(c), Stdout: w})
}

func (e postgresEngine) Restore(ctx context.Context, c Conn, r io.Reader) error {
	tool, err := lookTool("pg_restore")
	if err != nil {
		return err
	}
	args := []string{fmt.Sprintf("--host=%s", c.Host), fmt.Sprintf("--port=%d", c.Port),
		fmt.Sprintf("--username=%s", c.User), "--no-password", "--clean", "--if-exists",
		fmt.Sprintf("--dbname=%s", c.Database)}
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Env: PgEnv(c), Stdin: r})
}
```

`internal/db/postgres_integration_test.go`:
```go
//go:build integration

package db

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestPostgresDumpIntegration(t *testing.T) {
	c := Conn{Host: envOr("DBK_IT_PG_HOST", "127.0.0.1"), Port: 5432,
		User: envOr("DBK_IT_PG_USER", "postgres"), Password: os.Getenv("DBK_IT_PG_PASSWORD"),
		Database: envOr("DBK_IT_PG_DB", "dbk_it")}
	var buf bytes.Buffer
	if err := (postgresEngine{}).Dump(context.Background(), c, &buf); err != nil {
		t.Fatalf("dump: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty custom-format dump")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestPg -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db
git commit -m "feat: postgresql engine driver"
```

---

### Task 11: MongoDB engine driver

**Files:**
- Create: `internal/db/mongo.go`
- Test: `internal/db/mongo_test.go`
- Test: `internal/db/mongo_integration_test.go`

**Interfaces:**
- Produces: `mongoEngine` registered under `"mongodb"`; `BuildMongoURI(c Conn) string` (builds `mongodb://user@host:port/db` with password supplied separately via `--password` through stdin-safe env? Mongo tools accept `--uri` with password; to keep the password out of argv, use `MONGODUMP`-compatible approach: pass `--username`, `--host`, and the password via the `--password` prompt is interactive. Instead use the connection string with password in a file is not supported. Use env var is not supported by mongodump. Therefore pass password via `--password` is unavoidable on argv OR use `--uri` from an env-referenced value.)

Decision to avoid argv leakage: build the full URI including password and pass it through the `--config` YAML file that `mongodump`/`mongorestore` support (`--config=<file>` with `password: ...`), written to a `0600` temp file and removed after. `BuildMongoBaseArgs(c)` returns the non-secret args; `writeMongoConfig(password) (path string, cleanup func(), err error)` writes the secret file.

- [ ] **Step 1: Write the failing test**

`internal/db/mongo_test.go`:
```go
package db

import (
	"os"
	"strings"
	"testing"
)

func TestMongoBaseArgsNoPassword(t *testing.T) {
	args := BuildMongoBaseArgs(Conn{Host: "h", Port: 27017, User: "u", Database: "app", Password: "pw"})
	j := strings.Join(args, " ")
	if strings.Contains(j, "pw") {
		t.Fatalf("password must not be in argv: %q", j)
	}
	for _, w := range []string{"--host=h", "--port=27017", "--username=u", "--db=app"} {
		if !strings.Contains(j, w) {
			t.Fatalf("expected %q in %q", w, j)
		}
	}
}

func TestWriteMongoConfigFilePerms(t *testing.T) {
	path, cleanup, err := writeMongoConfig("s3cr3t")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	defer cleanup()
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config file must be 0600, got %o", info.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "s3cr3t") {
		t.Fatalf("password must be written to the config file")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run 'TestMongo|TestWriteMongo' -v`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

`internal/db/mongo.go`:
```go
package db

import (
	"context"
	"fmt"
	"io"
	"os"
)

type mongoEngine struct{}

func init() { Register(mongoEngine{}) }

func (mongoEngine) Name() string     { return "mongodb" }
func (mongoEngine) DefaultPort() int { return 27017 }

func (mongoEngine) Detect() (string, error) { return lookTool("mongodump") }

func BuildMongoBaseArgs(c Conn) []string {
	args := []string{
		fmt.Sprintf("--host=%s", c.Host),
		fmt.Sprintf("--port=%d", c.Port),
	}
	if c.User != "" {
		args = append(args, fmt.Sprintf("--username=%s", c.User))
	}
	if c.Database != "" {
		args = append(args, fmt.Sprintf("--db=%s", c.Database))
	}
	if c.TLS {
		args = append(args, "--tls")
	}
	return args
}

// writeMongoConfig writes a 0600 YAML config holding only the password, which
// mongodump/mongorestore accept via --config, keeping it off the argv/ps list.
func writeMongoConfig(password string) (string, func(), error) {
	f, err := os.CreateTemp("", "dbk-mongo-*.yaml")
	if err != nil {
		return "", func() {}, err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, err
	}
	if _, err := fmt.Fprintf(f, "password: %q\n", password); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, err
	}
	f.Close()
	return f.Name(), func() { os.Remove(f.Name()) }, nil
}

func (e mongoEngine) withConfig(c Conn, base []string) ([]string, func(), error) {
	if c.Password == "" {
		return base, func() {}, nil
	}
	path, cleanup, err := writeMongoConfig(c.Password)
	if err != nil {
		return nil, func() {}, err
	}
	return append(base, "--config="+path), cleanup, nil
}

func (e mongoEngine) Test(ctx context.Context, c Conn) error {
	tool, err := lookTool("mongosh", "mongo")
	if err != nil {
		return err
	}
	args, cleanup, err := e.withConfig(c, BuildMongoBaseArgs(c))
	if err != nil {
		return err
	}
	defer cleanup()
	return runCmd(ctx, cmdSpec{Path: tool, Args: append(args, "--eval", "db.runCommand({ping:1})")})
}

func (e mongoEngine) Dump(ctx context.Context, c Conn, w io.Writer) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	args, cleanup, err := e.withConfig(c, append(BuildMongoBaseArgs(c), "--archive"))
	if err != nil {
		return err
	}
	defer cleanup()
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Stdout: w})
}

func (e mongoEngine) Restore(ctx context.Context, c Conn, r io.Reader) error {
	tool, err := lookTool("mongorestore")
	if err != nil {
		return err
	}
	args, cleanup, err := e.withConfig(c, append(BuildMongoBaseArgs(c), "--archive", "--drop"))
	if err != nil {
		return err
	}
	defer cleanup()
	return runCmd(ctx, cmdSpec{Path: tool, Args: args, Stdin: r})
}
```

`internal/db/mongo_integration_test.go`:
```go
//go:build integration

package db

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func TestMongoDumpIntegration(t *testing.T) {
	c := Conn{Host: envOr("DBK_IT_MONGO_HOST", "127.0.0.1"), Port: 27017,
		User: os.Getenv("DBK_IT_MONGO_USER"), Password: os.Getenv("DBK_IT_MONGO_PASSWORD"),
		Database: envOr("DBK_IT_MONGO_DB", "dbk_it")}
	var buf bytes.Buffer
	if err := (mongoEngine{}).Dump(context.Background(), c, &buf); err != nil {
		t.Fatalf("dump: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("expected non-empty archive")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/db/ -run 'TestMongo|TestWriteMongo' -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db
git commit -m "feat: mongodb engine driver"
```

---

### Task 12: SQLite engine driver

**Files:**
- Create: `internal/db/sqlite.go`
- Test: `internal/db/sqlite_test.go`

**Interfaces:**
- Produces: `sqliteEngine` registered under `"sqlite"`. Dump runs `sqlite3 <path> .dump` to the writer; Restore pipes SQL into `sqlite3 <c.Path>`. Uses `c.Path` (the DB file), ignores host/port. This test is NOT gated because `sqlite3` is commonly present and operates on a temp file.

- [ ] **Step 1: Write the failing test**

`internal/db/sqlite_test.go`:
```go
package db

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteDumpRestore(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	// Seed a table via the restore path's tool.
	seed := exec.Command("sqlite3", src, "CREATE TABLE t(x); INSERT INTO t VALUES(42);")
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, out)
	}

	e := sqliteEngine{}
	var dump bytes.Buffer
	if err := e.Dump(context.Background(), Conn{Path: src}, &dump); err != nil {
		t.Fatalf("dump: %v", err)
	}
	if !strings.Contains(dump.String(), "INSERT INTO t VALUES(42)") {
		t.Fatalf("dump missing data: %q", dump.String())
	}

	dst := filepath.Join(dir, "dst.db")
	if err := e.Restore(context.Background(), Conn{Path: dst}, strings.NewReader(dump.String())); err != nil {
		t.Fatalf("restore: %v", err)
	}
	check := exec.Command("sqlite3", dst, "SELECT x FROM t;")
	out, err := check.Output()
	if err != nil || strings.TrimSpace(string(out)) != "42" {
		t.Fatalf("restored data mismatch: %q err %v", out, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/db/ -run TestSQLite -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Write the implementation**

`internal/db/sqlite.go`:
```go
package db

import (
	"context"
	"fmt"
	"io"
)

type sqliteEngine struct{}

func init() { Register(sqliteEngine{}) }

func (sqliteEngine) Name() string     { return "sqlite" }
func (sqliteEngine) DefaultPort() int { return 0 }

func (sqliteEngine) Detect() (string, error) { return lookTool("sqlite3") }

func (e sqliteEngine) Test(ctx context.Context, c Conn) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	if c.Path == "" {
		return fmt.Errorf("sqlite: conn.path (database file) is required")
	}
	return runCmd(ctx, cmdSpec{Path: tool, Args: []string{c.Path, "SELECT 1;"}})
}

func (e sqliteEngine) Dump(ctx context.Context, c Conn, w io.Writer) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	if c.Path == "" {
		return fmt.Errorf("sqlite: conn.path (database file) is required")
	}
	return runCmd(ctx, cmdSpec{Path: tool, Args: []string{c.Path, ".dump"}, Stdout: w})
}

func (e sqliteEngine) Restore(ctx context.Context, c Conn, r io.Reader) error {
	tool, err := e.Detect()
	if err != nil {
		return err
	}
	if c.Path == "" {
		return fmt.Errorf("sqlite: conn.path (database file) is required")
	}
	return runCmd(ctx, cmdSpec{Path: tool, Args: []string{c.Path}, Stdin: r})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/db/ -run TestSQLite -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/db
git commit -m "feat: sqlite engine driver"
```

---

### Task 13: `storage.Backend` interface and local filesystem backend

**Files:**
- Create: `internal/storage/backend.go`
- Create: `internal/storage/local.go`
- Test: `internal/storage/local_test.go`

**Interfaces:**
- Produces:
  - `type Object struct { Key string; Size int64; ModTime time.Time }`.
  - `type Backend interface { Name() string; Put(ctx, key string, r io.Reader, size int64) error; Get(ctx, key string) (io.ReadCloser, error); List(ctx, prefix string) ([]Object, error); Delete(ctx, key string) error; Stat(ctx, key string) (Object, error); Test(ctx) error }`.
  - `storage.NewLocal(root string) *Local`.

- [ ] **Step 1: Write the failing test**

`internal/storage/local_test.go`:
```go
package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestLocalPutGetListDelete(t *testing.T) {
	b := NewLocal(t.TempDir())
	ctx := context.Background()
	data := []byte("hello backup")
	if err := b.Put(ctx, "main/2026-10-12.sql", bytes.NewReader(data), int64(len(data))); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := b.Get(ctx, "main/2026-10-12.sql")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: %q", got)
	}
	objs, err := b.List(ctx, "main/")
	if err != nil || len(objs) != 1 || objs[0].Key != "main/2026-10-12.sql" {
		t.Fatalf("list mismatch: %+v err %v", objs, err)
	}
	if err := b.Delete(ctx, "main/2026-10-12.sql"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if objs, _ := b.List(ctx, "main/"); len(objs) != 0 {
		t.Fatalf("expected empty after delete, got %+v", objs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Write the implementation**

`internal/storage/backend.go`:
```go
package storage

import (
	"context"
	"io"
	"time"
)

type Object struct {
	Key     string
	Size    int64
	ModTime time.Time
}

type Backend interface {
	Name() string
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	List(ctx context.Context, prefix string) ([]Object, error)
	Delete(ctx context.Context, key string) error
	Stat(ctx context.Context, key string) (Object, error)
	Test(ctx context.Context) error
}
```

`internal/storage/local.go`:
```go
package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Local struct{ root string }

func NewLocal(root string) *Local { return &Local{root: root} }

func (l *Local) Name() string { return "local" }

func (l *Local) full(key string) string { return filepath.Join(l.root, filepath.FromSlash(key)) }

func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	dst := l.full(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return os.Open(l.full(key))
}

func (l *Local) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	err := filepath.Walk(l.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		rel, err := filepath.Rel(l.root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if strings.HasPrefix(key, prefix) {
			out = append(out, Object{Key: key, Size: info.Size(), ModTime: info.ModTime()})
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}

func (l *Local) Delete(ctx context.Context, key string) error {
	return os.Remove(l.full(key))
}

func (l *Local) Stat(ctx context.Context, key string) (Object, error) {
	info, err := os.Stat(l.full(key))
	if err != nil {
		return Object{}, err
	}
	return Object{Key: key, Size: info.Size(), ModTime: info.ModTime()}, nil
}

func (l *Local) Test(ctx context.Context) error {
	return os.MkdirAll(l.root, 0o700)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/storage
git commit -m "feat: storage backend interface and local filesystem driver"
```

---

### Task 14: Backup orchestration (resolve password, dump → storage, key naming)

**Files:**
- Create: `internal/backup/backup.go`
- Create: `internal/secretref/secretref.go`
- Test: `internal/backup/backup_test.go`
- Test: `internal/secretref/secretref_test.go`

**Interfaces:**
- Consumes: `config`, `vault.Store`, `db.Engine`/`db.Conn`, `storage.Backend`.
- Produces:
  - `secretref.Resolve(ref string, lookup func(key string) (string, bool)) (string, error)` — `env:VAR` → env; `file:/path` → file contents; otherwise treat `ref` as a vault key via `lookup`.
  - `backup.BuildKey(job string, t time.Time, ext string) string` → `"<job>/<job>-YYYYmmdd-HHMMSS.<ext>"`.
  - `backup.Run(ctx, Deps, jobName string) (storage.Object, error)` where `Deps` bundles `*config.Config`, `*vault.Store`, an engine resolver, a backend resolver, a clock, and `*logutil.Logger`.

- [ ] **Step 1: Write the failing test for secretref**

`internal/secretref/secretref_test.go`:
```go
package secretref

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEnv(t *testing.T) {
	t.Setenv("MYSECRET", "fromenv")
	got, err := Resolve("env:MYSECRET", nil)
	if err != nil || got != "fromenv" {
		t.Fatalf("env: %q %v", got, err)
	}
}

func TestResolveFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s")
	os.WriteFile(p, []byte("fromfile\n"), 0o600)
	got, err := Resolve("file:"+p, nil)
	if err != nil || got != "fromfile" {
		t.Fatalf("file: %q %v", got, err)
	}
}

func TestResolveVaultKey(t *testing.T) {
	lookup := func(k string) (string, bool) {
		if k == "job/main/password" {
			return "fromvault", true
		}
		return "", false
	}
	got, err := Resolve("job/main/password", lookup)
	if err != nil || got != "fromvault" {
		t.Fatalf("vault: %q %v", got, err)
	}
	if _, err := Resolve("missing/key", lookup); err == nil {
		t.Fatal("expected error for missing vault key")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/secretref/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement secretref**

`internal/secretref/secretref.go`:
```go
package secretref

import (
	"fmt"
	"os"
	"strings"
)

// Resolve turns a reference into a secret value.
//   - "env:NAME"   -> value of environment variable NAME
//   - "file:/path" -> trimmed contents of the file
//   - anything else -> looked up as a vault key via lookup
func Resolve(ref string, lookup func(key string) (string, bool)) (string, error) {
	switch {
	case strings.HasPrefix(ref, "env:"):
		name := strings.TrimPrefix(ref, "env:")
		v, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("env var %q not set", name)
		}
		return v, nil
	case strings.HasPrefix(ref, "file:"):
		b, err := os.ReadFile(strings.TrimPrefix(ref, "file:"))
		if err != nil {
			return "", fmt.Errorf("read secret file: %w", err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	default:
		if lookup == nil {
			return "", fmt.Errorf("no vault available to resolve %q", ref)
		}
		v, ok := lookup(ref)
		if !ok {
			return "", fmt.Errorf("secret %q not found in vault", ref)
		}
		return v, nil
	}
}
```

- [ ] **Step 4: Write the failing test for backup orchestration**

`internal/backup/backup_test.go`:
```go
package backup

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

type fakeEngine struct{ dumped string }

func (f *fakeEngine) Name() string                                   { return "fake" }
func (f *fakeEngine) DefaultPort() int                               { return 0 }
func (f *fakeEngine) Detect() (string, error)                        { return "x", nil }
func (f *fakeEngine) Test(context.Context, db.Conn) error            { return nil }
func (f *fakeEngine) Dump(_ context.Context, _ db.Conn, w io.Writer) error {
	_, err := io.WriteString(w, "DUMPDATA")
	return err
}
func (f *fakeEngine) Restore(context.Context, db.Conn, io.Reader) error { return nil }

func TestBuildKey(t *testing.T) {
	ts := time.Date(2026, 10, 12, 3, 4, 5, 0, time.UTC)
	if got := BuildKey("main", ts, "sql"); got != "main/main-20261012-030405.sql" {
		t.Fatalf("unexpected key: %q", got)
	}
}

func TestRunBackupWritesToBackend(t *testing.T) {
	cfg := &config.Config{
		Version:  1,
		Jobs:     []config.Job{{Name: "main", Engine: "fake", Storage: "local", Conn: config.Conn{Database: "app"}}},
		Storages: []config.StorageConfig{{Name: "local", Type: "local"}},
	}
	be := storage.NewLocal(t.TempDir())
	deps := Deps{
		Cfg:    cfg,
		Engine: func(string) (db.Engine, bool) { return &fakeEngine{}, true },
		Backend: func(config.StorageConfig) (storage.Backend, error) { return be, nil },
		ResolvePassword: func(config.Job) (string, error) { return "", nil },
		Now:    func() time.Time { return time.Date(2026, 10, 12, 3, 4, 5, 0, time.UTC) },
		Log:    logutil.New(io.Discard, logutil.LevelInfo),
	}
	obj, err := Run(context.Background(), deps, "main")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	rc, _ := be.Get(context.Background(), obj.Key)
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, []byte("DUMPDATA")) {
		t.Fatalf("backend content mismatch: %q", got)
	}
}
```

- [ ] **Step 5: Run test to verify it fails**

Run: `go test ./internal/backup/ -v`
Expected: FAIL (undefined).

- [ ] **Step 6: Implement backup orchestration**

`internal/backup/backup.go`:
```go
package backup

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

type Deps struct {
	Cfg             *config.Config
	Engine          func(name string) (db.Engine, bool)
	Backend         func(config.StorageConfig) (storage.Backend, error)
	ResolvePassword func(config.Job) (string, error)
	Now             func() time.Time
	Log             *logutil.Logger
}

func extFor(engine string) string {
	switch engine {
	case "postgres":
		return "pgdump"
	case "mongodb":
		return "archive"
	default:
		return "sql"
	}
}

func BuildKey(job string, t time.Time, ext string) string {
	return fmt.Sprintf("%s/%s-%s.%s", job, job, t.UTC().Format("20060102-150405"), ext)
}

func Run(ctx context.Context, d Deps, jobName string) (storage.Object, error) {
	job, ok := d.Cfg.Job(jobName)
	if !ok {
		return storage.Object{}, fmt.Errorf("job %q not found", jobName)
	}
	eng, ok := d.Engine(job.Engine)
	if !ok {
		return storage.Object{}, fmt.Errorf("unknown engine %q", job.Engine)
	}
	sc, ok := d.Cfg.Storage(job.Storage)
	if !ok {
		return storage.Object{}, fmt.Errorf("storage %q not found", job.Storage)
	}
	be, err := d.Backend(sc)
	if err != nil {
		return storage.Object{}, err
	}
	pw, err := d.ResolvePassword(job)
	if err != nil {
		return storage.Object{}, err
	}
	if pw != "" {
		d.Log.Redact(pw)
	}
	conn := db.Conn{
		Host: job.Conn.Host, Port: job.Conn.Port, User: job.Conn.User,
		Password: pw, Database: job.Conn.Database, Path: job.Conn.Path, TLS: job.Conn.TLS,
	}

	key := BuildKey(job.Name, d.Now(), extFor(job.Engine))
	d.Log.Infof("starting backup of job %q -> %s", job.Name, key)

	pr, pw2 := io.Pipe()
	go func() {
		err := eng.Dump(ctx, conn, pw2)
		pw2.CloseWithError(err)
	}()

	if err := be.Put(ctx, key, pr, -1); err != nil {
		return storage.Object{}, fmt.Errorf("upload: %w", err)
	}
	obj, err := be.Stat(ctx, key)
	if err != nil {
		return storage.Object{}, err
	}
	d.Log.Infof("backup complete: %s (%d bytes)", key, obj.Size)
	return obj, nil
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/secretref/ ./internal/backup/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/backup internal/secretref
git commit -m "feat: backup orchestration and secret reference resolution"
```

---

### Task 15: Restore orchestration with safe-target guard

**Files:**
- Create: `internal/restore/restore.go`
- Test: `internal/restore/restore_test.go`

**Interfaces:**
- Consumes: `config`, `db`, `storage`, `logutil`.
- Produces:
  - `restore.LatestKey(objs []storage.Object, jobPrefix string) (string, bool)` — newest by ModTime.
  - `restore.Run(ctx, Deps, jobName, atKey, toDatabase string, confirmed bool) error` — refuses without `confirmed`; when `toDatabase != ""`, restores into that database instead of the job's configured one.

- [ ] **Step 1: Write the failing test**

`internal/restore/restore_test.go`:
```go
package restore

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

type recordEngine struct{ restoredInto string }

func (e *recordEngine) Name() string                                { return "fake" }
func (e *recordEngine) DefaultPort() int                            { return 0 }
func (e *recordEngine) Detect() (string, error)                     { return "x", nil }
func (e *recordEngine) Test(context.Context, db.Conn) error         { return nil }
func (e *recordEngine) Dump(context.Context, db.Conn, io.Writer) error { return nil }
func (e *recordEngine) Restore(_ context.Context, c db.Conn, r io.Reader) error {
	io.ReadAll(r)
	e.restoredInto = c.Database
	return nil
}

func TestLatestKey(t *testing.T) {
	objs := []storage.Object{
		{Key: "main/main-20261001-000000.sql", ModTime: time.Unix(1000, 0)},
		{Key: "main/main-20261012-000000.sql", ModTime: time.Unix(2000, 0)},
	}
	got, ok := LatestKey(objs, "main/")
	if !ok || got != "main/main-20261012-000000.sql" {
		t.Fatalf("latest mismatch: %q ok=%v", got, ok)
	}
}

func TestRunRequiresConfirmation(t *testing.T) {
	d := baseDeps(t, &recordEngine{})
	err := Run(context.Background(), d, "main", "main/x.sql", "", false)
	if err == nil {
		t.Fatal("restore must refuse without confirmation")
	}
}

func TestRunHonorsToTarget(t *testing.T) {
	eng := &recordEngine{}
	d := baseDeps(t, eng)
	// seed a backup object
	d.Backend().Put(context.Background(), "main/x.sql", io.NopCloser(stringReader("DATA")), -1)
	if err := Run(context.Background(), d, "main", "main/x.sql", "otherdb", true); err != nil {
		t.Fatalf("run: %v", err)
	}
	if eng.restoredInto != "otherdb" {
		t.Fatalf("expected restore into otherdb, got %q", eng.restoredInto)
	}
}
```

Add helpers at the bottom of the test file:
```go
type stringReader string

func (s stringReader) Read(p []byte) (int, error) { return copy(p, s), io.EOF }

// baseDeps builds Deps around a single in-memory local backend and one job.
func baseDeps(t *testing.T, eng db.Engine) Deps {
	t.Helper()
	be := storage.NewLocal(t.TempDir())
	cfg := &config.Config{
		Version:  1,
		Jobs:     []config.Job{{Name: "main", Engine: "fake", Storage: "local", Conn: config.Conn{Database: "app"}}},
		Storages: []config.StorageConfig{{Name: "local", Type: "local"}},
	}
	return Deps{
		Cfg:             cfg,
		Engine:          func(string) (db.Engine, bool) { return eng, true },
		backend:         be,
		Backend:         func() storage.Backend { return be },
		ResolvePassword: func(config.Job) (string, error) { return "", nil },
		Log:             logutil.New(io.Discard, logutil.LevelInfo),
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/restore/ -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Write the implementation**

`internal/restore/restore.go`:
```go
package restore

import (
	"context"
	"fmt"
	"sort"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/tanzid64/tui-db-manager/internal/storage"
)

type Deps struct {
	Cfg             *config.Config
	Engine          func(name string) (db.Engine, bool)
	Backend         func() storage.Backend
	backend         storage.Backend // test convenience; unused in production path
	ResolvePassword func(config.Job) (string, error)
	Log             *logutil.Logger
}

func LatestKey(objs []storage.Object, jobPrefix string) (string, bool) {
	var filtered []storage.Object
	for _, o := range objs {
		if len(o.Key) >= len(jobPrefix) && o.Key[:len(jobPrefix)] == jobPrefix {
			filtered = append(filtered, o)
		}
	}
	if len(filtered) == 0 {
		return "", false
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].ModTime.After(filtered[j].ModTime) })
	return filtered[0].Key, true
}

func Run(ctx context.Context, d Deps, jobName, atKey, toDatabase string, confirmed bool) error {
	if !confirmed {
		return fmt.Errorf("restore not confirmed: re-run with --yes (this overwrites the target database)")
	}
	job, ok := d.Cfg.Job(jobName)
	if !ok {
		return fmt.Errorf("job %q not found", jobName)
	}
	eng, ok := d.Engine(job.Engine)
	if !ok {
		return fmt.Errorf("unknown engine %q", job.Engine)
	}
	be := d.Backend()
	if atKey == "" {
		objs, err := be.List(ctx, job.Name+"/")
		if err != nil {
			return err
		}
		k, ok := LatestKey(objs, job.Name+"/")
		if !ok {
			return fmt.Errorf("no backups found for job %q", job.Name)
		}
		atKey = k
	}
	pw, err := d.ResolvePassword(job)
	if err != nil {
		return err
	}
	if pw != "" {
		d.Log.Redact(pw)
	}
	target := job.Conn.Database
	if toDatabase != "" {
		target = toDatabase
	}
	conn := db.Conn{
		Host: job.Conn.Host, Port: job.Conn.Port, User: job.Conn.User,
		Password: pw, Database: target, Path: job.Conn.Path, TLS: job.Conn.TLS,
	}
	d.Log.Infof("restoring %s into database %q", atKey, target)
	rc, err := be.Get(ctx, atKey)
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := eng.Restore(ctx, conn, rc); err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	d.Log.Infof("restore complete")
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/restore/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/restore
git commit -m "feat: restore orchestration with safe-target guard"
```

---

### Task 16: Wire CLI subcommands (`init`, `run`, `restore`, `list`, `test`)

**Files:**
- Create: `internal/cli/context.go`
- Create: `internal/cli/init.go`
- Create: `internal/cli/run.go`
- Create: `internal/cli/restore.go`
- Create: `internal/cli/list.go`
- Create: `internal/cli/test.go`
- Create: `internal/cli/wire.go`
- Modify: `internal/cli/root.go` (register the new commands and global flags `--passphrase-file`, `--yes`, `--json`)
- Test: `internal/cli/run_test.go`

**Interfaces:**
- Consumes: `config`, `vault`, `db`, `storage`, `backup`, `restore`, `secretref`, `logutil`.
- Produces:
  - `cli.buildBackend(sc config.StorageConfig) (storage.Backend, error)` — maps `type: local` → `storage.NewLocal(sc.Path)`; errors on unknown/cloud types (added in the storage plan).
  - `cli.resolvePassword(v *vault.Store, j config.Job) func(config.Job) (string, error)` using `secretref`.
  - End-to-end `dbk run <job>` for a SQLite job writing to a local backend.

- [ ] **Step 1: Write the failing end-to-end test**

`internal/cli/run_test.go`:
```go
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Exercises dbk run end-to-end with a sqlite job and local storage.
func TestRunCommandSQLiteEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	cfgDir := t.TempDir()
	srcDB := filepath.Join(t.TempDir(), "app.db")
	backups := filepath.Join(t.TempDir(), "backups")
	seed := exec.Command("sqlite3", srcDB, "CREATE TABLE t(x); INSERT INTO t VALUES(7);")
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, out)
	}

	cfgYAML := "" +
		"version: 1\n" +
		"jobs:\n" +
		"  - name: main\n" +
		"    engine: sqlite\n" +
		"    storage: local\n" +
		"    conn:\n" +
		"      path: " + srcDB + "\n" +
		"storages:\n" +
		"  - name: local\n" +
		"    type: local\n" +
		"    path: " + backups + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfgYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DBK_PASSPHRASE", "pw")

	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"--config", cfgDir, "run", "main"})
	if err := root.Execute(); err != nil {
		t.Fatalf("run: %v (out=%s)", err, out.String())
	}

	entries, _ := os.ReadDir(filepath.Join(backups, "main"))
	if len(entries) == 0 {
		t.Fatalf("expected a backup file under %s/main", backups)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestRunCommandSQLite -v`
Expected: FAIL (commands not wired).

- [ ] **Step 3: Implement the shared context and wiring**

`internal/cli/context.go`:
```go
package cli

import (
	"os"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/logutil"
	"github.com/spf13/cobra"
)

type appCtx struct {
	dir string
	cfg *config.Config
	log *logutil.Logger
}

func loadApp(cmd *cobra.Command) (*appCtx, error) {
	override, _ := cmd.Flags().GetString("config")
	dir, err := config.Dir(override)
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	return &appCtx{dir: dir, cfg: cfg, log: logutil.New(os.Stderr, logutil.LevelInfo)}, nil
}
```

`internal/cli/wire.go`:
```go
package cli

import (
	"fmt"

	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/tanzid64/tui-db-manager/internal/secretref"
	"github.com/tanzid64/tui-db-manager/internal/storage"
	"github.com/tanzid64/tui-db-manager/internal/vault"
)

func buildBackend(sc config.StorageConfig) (storage.Backend, error) {
	switch sc.Type {
	case "local":
		return storage.NewLocal(sc.Path), nil
	default:
		return nil, fmt.Errorf("storage type %q not supported yet", sc.Type)
	}
}

func engineResolver() func(string) (db.Engine, bool) {
	return func(name string) (db.Engine, bool) { return db.Get(name) }
}

// resolvePassword returns a closure that resolves a job's DB password from a
// PasswordRef (env:/file:) or the vault key "job/<name>/password".
func resolvePassword(v *vault.Store) func(config.Job) (string, error) {
	return func(j config.Job) (string, error) {
		ref := j.Conn.PasswordRef
		var lookup func(string) (string, bool)
		if v != nil {
			lookup = v.Get
		}
		if ref == "" {
			// No explicit ref: look up the conventional vault key; empty is allowed
			// (e.g. sqlite or trust auth).
			if v == nil {
				return "", nil
			}
			if val, ok := v.Get("job/" + j.Name + "/password"); ok {
				return val, nil
			}
			return "", nil
		}
		return secretref.Resolve(ref, lookup)
	}
}

// maybeOpenVault opens the vault if it exists, else returns nil (jobs may have
// no secrets, e.g. sqlite). Passphrase comes from env/file/prompt.
func maybeOpenVault(dir, passFile string, interactive bool) (*vault.Store, error) {
	if !vault.Exists(dir) {
		return nil, nil
	}
	pass, err := vault.ReadPassphrase(dir, passFile, interactive)
	if err != nil {
		return nil, err
	}
	return vault.Open(dir, pass)
}
```

- [ ] **Step 4: Implement the commands**

`internal/cli/run.go`:
```go
package cli

import (
	"time"

	"github.com/tanzid64/tui-db-manager/internal/backup"
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/storage"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <job>",
		Short: "Run a backup job now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			passFile, _ := cmd.Flags().GetString("passphrase-file")
			v, err := maybeOpenVault(app.dir, passFile, true)
			if err != nil {
				return err
			}
			deps := backup.Deps{
				Cfg:             app.cfg,
				Engine:          engineResolver(),
				Backend:         func(sc config.StorageConfig) (storage.Backend, error) { return buildBackend(sc) },
				ResolvePassword: resolvePassword(v),
				Now:             time.Now,
				Log:             app.log,
			}
			obj, err := backup.Run(cmd.Context(), deps, args[0])
			if err != nil {
				return err
			}
			cmd.Printf("backup written: %s (%d bytes)\n", obj.Key, obj.Size)
			return nil
		},
	}
}
```

`internal/cli/restore.go`:
```go
package cli

import (
	"github.com/tanzid64/tui-db-manager/internal/restore"
	"github.com/tanzid64/tui-db-manager/internal/storage"
	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <job>",
		Short: "Restore a backup (requires --yes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			at, _ := cmd.Flags().GetString("at")
			to, _ := cmd.Flags().GetString("to")
			yes, _ := cmd.Flags().GetBool("yes")
			passFile, _ := cmd.Flags().GetString("passphrase-file")
			v, err := maybeOpenVault(app.dir, passFile, true)
			if err != nil {
				return err
			}
			job, ok := app.cfg.Job(args[0])
			if !ok {
				return errUnknownJob(args[0])
			}
			sc, ok := app.cfg.Storage(job.Storage)
			if !ok {
				return errUnknownStorage(job.Storage)
			}
			be, err := buildBackend(sc)
			if err != nil {
				return err
			}
			deps := restore.Deps{
				Cfg:             app.cfg,
				Engine:          engineResolver(),
				Backend:         func() storage.Backend { return be },
				ResolvePassword: resolvePassword(v),
				Log:             app.log,
			}
			return restore.Run(cmd.Context(), deps, args[0], at, to, yes)
		},
	}
	cmd.Flags().String("at", "", "backup key to restore (default: latest)")
	cmd.Flags().String("to", "", "restore into a different database")
	return cmd
}
```

`internal/cli/list.go`:
```go
package cli

import (
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <job>",
		Short: "List stored backups for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			job, ok := app.cfg.Job(args[0])
			if !ok {
				return errUnknownJob(args[0])
			}
			sc, ok := app.cfg.Storage(job.Storage)
			if !ok {
				return errUnknownStorage(job.Storage)
			}
			be, err := buildBackend(sc)
			if err != nil {
				return err
			}
			objs, err := be.List(cmd.Context(), job.Name+"/")
			if err != nil {
				return err
			}
			for _, o := range objs {
				cmd.Printf("%s\t%d\t%s\n", o.Key, o.Size, o.ModTime.Format("2006-01-02 15:04:05"))
			}
			return nil
		},
	}
}
```

`internal/cli/test.go`:
```go
package cli

import (
	"fmt"

	"github.com/tanzid64/tui-db-manager/internal/db"
	"github.com/spf13/cobra"
)

func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <job>",
		Short: "Test connectivity for a job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, err := loadApp(cmd)
			if err != nil {
				return err
			}
			job, ok := app.cfg.Job(args[0])
			if !ok {
				return errUnknownJob(args[0])
			}
			eng, ok := db.Get(job.Engine)
			if !ok {
				return fmt.Errorf("unknown engine %q", job.Engine)
			}
			passFile, _ := cmd.Flags().GetString("passphrase-file")
			v, err := maybeOpenVault(app.dir, passFile, true)
			if err != nil {
				return err
			}
			pw, err := resolvePassword(v)(job)
			if err != nil {
				return err
			}
			conn := db.Conn{Host: job.Conn.Host, Port: job.Conn.Port, User: job.Conn.User,
				Password: pw, Database: job.Conn.Database, Path: job.Conn.Path, TLS: job.Conn.TLS}
			if err := eng.Test(cmd.Context(), conn); err != nil {
				return err
			}
			cmd.Println("connection OK")
			return nil
		},
	}
}
```

`internal/cli/init.go`:
```go
package cli

import (
	"github.com/tanzid64/tui-db-manager/internal/config"
	"github.com/tanzid64/tui-db-manager/internal/vault"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the config directory and encrypted vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			override, _ := cmd.Flags().GetString("config")
			dir, err := config.Dir(override)
			if err != nil {
				return err
			}
			if _, err := config.Load(dir); err != nil {
				return err
			}
			if err := config.Save(dir, &config.Config{Version: config.CurrentVersion}); err != nil {
				return err
			}
			if !vault.Exists(dir) {
				passFile, _ := cmd.Flags().GetString("passphrase-file")
				pass, err := vault.ReadPassphrase(dir, passFile, true)
				if err != nil {
					return err
				}
				s, err := vault.Create(dir, pass)
				if err != nil {
					return err
				}
				if err := s.Save(); err != nil {
					return err
				}
			}
			cmd.Printf("initialized dbk config at %s\n", dir)
			return nil
		},
	}
}
```

Add shared error helpers to `internal/cli/wire.go`:
```go
func errUnknownJob(name string) error     { return fmt.Errorf("job %q not found", name) }
func errUnknownStorage(name string) error { return fmt.Errorf("storage %q not found", name) }
```
(Place these with the other `fmt` usage already imported in `wire.go`.)

- [ ] **Step 5: Register commands and flags**

Modify `internal/cli/root.go` to add global flags and subcommands:
```go
	root.PersistentFlags().String("passphrase-file", "", "file containing the vault passphrase")
	root.PersistentFlags().Bool("yes", false, "assume yes for confirmations")
	root.PersistentFlags().Bool("json", false, "machine-readable output where supported")
	root.AddCommand(newVersionCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newRestoreCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newTestCmd())
```

- [ ] **Step 6: Run the end-to-end test to verify it passes**

Run: `go test ./internal/cli/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 7: Run the whole test suite**

Run: `go test ./...`
Expected: all PASS (integration-tagged tests excluded by default).

- [ ] **Step 8: Commit**

```bash
git add internal/cli
git commit -m "feat: wire init/run/restore/list/test CLI commands"
```

---

## Self-Review Notes

- **Spec coverage (this plan's slice):** engines MySQL/Postgres/Mongo/SQLite (Tasks 9–12); local + remote TCP topology (Conn carries host/port/TLS; sqlite uses path); encrypted vault with env/file/prompt passphrase (Tasks 3–5, 16); secret injection refs (Task 14); local storage backend (Task 13); backup + restore with safe-target guard (Tasks 14–15); secret-redacting logs and env-not-argv passwords (Tasks 6, 8–12). Deferred to later plans (explicitly): cloud storage backends (S3/R2/GDrive), compression + at-rest encryption, notifications, scheduling, config export/import, the TUI, and installers.
- **Placeholder scan:** no TBD/TODO; every code step carries real code. The Task 9 note about `init.go` is resolved by Step 3b (per-file `init()`).
- **Type consistency:** `db.Conn` (runtime, has `Password`) vs `config.Conn` (persisted, has `PasswordRef`) are deliberately distinct and mapped in Tasks 14/16; `storage.Backend` method set is identical across Tasks 13/14/15/16; `backup.Deps.Backend` is `func(config.StorageConfig) (storage.Backend, error)` while `restore.Deps.Backend` is `func() storage.Backend` (restore resolves the single backend before constructing Deps) — intentional and consistent within each package.
- **Review Focus coverage:** wrong passphrase (Task 5 `TestOpenWrongPassphrase`), tampered vault (Task 4 `TestOpenTamperedBlobFails`), missing client tool (Task 8 `TestLookToolMissing`), password with metacharacters via env (Task 8 `TestRunCmdPassesEnvAndStdout`, Task 9 `TestMySQLEnvCarriesPasswordNotArgs`), restore target guard (Task 15 `TestRunRequiresConfirmation` + `TestRunHonorsToTarget`).
