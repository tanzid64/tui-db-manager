# dbk — Secure Database Backup CLI/TUI — Design Spec

- **Date:** 2026-10-12
- **Status:** Approved for planning
- **Author:** tanzid64

## 1. Purpose & Scope

`dbk` is a self-hosted, cross-platform command-line tool for backing up and
restoring databases on cPanel hosts and VPS servers. It provides both an
interactive terminal UI (for setup and day-to-day operation) and scriptable
CLI subcommands (for unattended, cron-driven backups). It targets individuals
and small teams who want a single, secure, installable binary rather than a
Docker/web stack.

It is conceptually similar to Databasus (formerly Postgresus) in feature set —
scheduled backups, multiple storage destinations, restore, notifications — but
delivered as a portable CLI/TUI instead of a Docker container with a web UI, and
with multi-engine database support.

### In scope (v1)
- Database engines: MySQL/MariaDB, PostgreSQL, MongoDB, SQLite.
- Topology: tool runs on the DB server (local dumps via socket/localhost) **and**
  connects to remote DBs over TCP with TLS.
- Storage backends: Local filesystem, Amazon S3, Cloudflare R2, and any
  S3-compatible provider (MinIO, Backblaze B2 S3, Wasabi, etc.), Google Drive.
- Restore from any stored backup, with safe-target guards.
- Notifications: Telegram, Email (SMTP), generic Webhook.
- Security: encrypted secrets vault; optional backup-at-rest encryption;
  optional env/file secret injection.
- Scheduling via generated system entries (cron / systemd timers / Windows
  Task Scheduler).
- Config export/import for moving a setup between machines.
- Installation via `curl | sh` (Linux/macOS) and `irm | iex` (Windows).

### Out of scope (v1)
- SSH-tunnel topology (explicitly deferred; may be added later).
- Built-in long-lived daemon scheduler (system cron is used instead).
- Web UI.
- Point-in-time recovery / WAL streaming / incremental backups (logical dumps only).
- Backup verification/restore-testing agent (may be added later).

## 2. Tech Stack

- **Language:** Go 1.23+. Single static binary per OS/arch.
- **TUI:** Bubble Tea, Bubbles, Lipgloss (Charm).
- **CLI:** Cobra. Running the binary with no args launches the TUI; subcommands
  are used for automation.
- **Crypto:** `golang.org/x/crypto` (Argon2id, chacha20poly1305) for the vault;
  `filippo.io/age` for optional backup-at-rest encryption.
- **Cloud:** AWS SDK for Go v2 (S3 + all S3-compatible via custom endpoint);
  Google Drive API Go client (OAuth 2.0).
- **Release:** GoReleaser (cross-compiles all targets, emits checksums,
  publishes GitHub Releases).

### Project name
Working name `dbk` (binary, repo). Trivial to rename before first release.

### Module layout
```
cmd/dbk/            entrypoint; dispatches to TUI (no args) or Cobra subcommands
internal/config/    non-secret config model, load/save, schema versioning/migration
internal/vault/     Argon2id + XChaCha20-Poly1305 encrypted secret store
internal/db/        Engine interface + mysql, postgres, mongo, sqlite drivers
internal/storage/   Backend interface + local, s3, gdrive drivers (r2 = s3 preset)
internal/backup/    orchestration: dump -> compress -> (encrypt) -> upload -> retention
internal/restore/   download -> (decrypt) -> decompress -> restore
internal/notify/    Notifier interface + telegram, email, webhook
internal/schedule/  cron / systemd / Task Scheduler entry generation
internal/tui/       Bubble Tea models/views/screens and reusable wizard component
internal/logutil/   structured, secret-redacting logging
```

## 3. Core Abstractions

Three plugin-style interfaces keep engines, backends, and notifiers drop-in.

### 3.1 `db.Engine`
```go
type Engine interface {
    Name() string
    Detect() (clientPath string, err error)        // locate native client tool
    Test(ctx context.Context, c Conn) error         // connectivity check
    Dump(ctx context.Context, j Job, w io.Writer) error
    Restore(ctx context.Context, j Job, r io.Reader) error
}
```
- MySQL/MariaDB: `mysqldump` / `mysql` (or `mariadb-dump` / `mariadb`).
- PostgreSQL: `pg_dump` / `pg_restore` (or `psql` for plain dumps).
- MongoDB: `mongodump` / `mongorestore` (archive stream to stdout/stdin).
- SQLite: safe copy via the `sqlite3 .backup`/`.dump` mechanism (no external
  client required beyond `sqlite3`, with a pure-Go fallback for file copy with
  integrity check where feasible).
- Each driver verifies its client binary exists and emits a clear,
  OS-specific install hint when missing.

### 3.2 `storage.Backend`
```go
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
- Local FS (atomic writes, configurable root).
- S3 via AWS SDK v2; R2 and other S3-compatible providers are the same driver
  with a configured endpoint/region and path-style option.
- Google Drive via OAuth 2.0 (device/loopback flow suitable for servers;
  refresh token stored in the vault).

### 3.3 `notify.Notifier`
```go
type Notifier interface {
    Name() string
    Send(ctx context.Context, ev Event) error
    Test(ctx context.Context) error
}
type Event struct {
    Type    EventType // BackupSucceeded, BackupFailed, RestoreDone, Test
    Job     string
    Message string
    Size    int64
    Started time.Time
    Elapsed time.Duration
    Err     string
}
```
- Telegram (Bot API; bot token + chat id).
- Email (SMTP with STARTTLS/implicit TLS; configurable server/port/auth).
- Generic Webhook (JSON POST; for Slack/Discord/Teams-style incoming hooks).

## 4. Data Flow

### Backup (`dbk run <job>`)
1. Load `config.yaml`; unlock vault (passphrase prompt / env / file).
2. Resolve the job (engine, connection, storage target(s), options).
3. `Engine.Dump` streams to a pipeline:
   `dump -> compress (gzip or zstd) -> optional encrypt (age) -> Backend.Put`.
   Fully streamed (io.Pipe); large databases never fully buffer to RAM.
4. Apply retention policy (keep last N and/or keep-for-duration), pruning the
   remote store.
5. Emit notification (success with size/elapsed, or failure with error).

### Restore (`dbk restore <job> [--at <id>] [--to <target>]`)
1. Resolve backup (explicit id, or `latest`, or interactive browser).
2. `Backend.Get -> decrypt -> decompress -> Engine.Restore`.
3. Safety guards: explicit confirmation required; `--to` allows restoring into a
   different database/host to avoid clobbering production; dry-run shows what
   would happen.

## 5. Security Model

- **Secrets vault:** two files in the config dir.
  - `config.yaml` — non-secret settings (hostnames, ports, schedules, retention,
    storage endpoints, which notifier). Safe to read/share/commit.
  - `secrets.enc` — XChaCha20-Poly1305 ciphertext. Encryption key derived from a
    master passphrase via Argon2id (tunable params stored in the file header:
    salt, memory, iterations, parallelism). AEAD tamper detection.
  - Passphrase sources: interactive prompt (default), `DBK_PASSPHRASE` env var,
    or `--passphrase-file`. Config dir and both files forced to `0600`/`0700`.
- **Backup-at-rest encryption (optional, default-on recommended):** dump stream
  encrypted with `age` (X25519 recipient) before upload, so cloud copies are
  unreadable by the provider. Recipient public key in config; identity (private
  key) in the vault. Restore requires the identity.
- **Secret injection (optional):** any secret value may be a reference
  `env:VAR_NAME` or `file:/path`, resolved at runtime instead of living in the
  vault — so CI/cron can supply a single credential without the vault passphrase.
- **Hardening:**
  - Secrets never written to logs (redaction layer around logutil).
  - DB passwords never passed as argv (would show in `ps`); supplied via env
    (e.g. `MYSQL_PWD`, `PGPASSWORD`) or stdin/temp credential files with `0600`.
  - TLS verification on by default for remote DB connections and S3/R2; opt-out
    requires an explicit per-connection flag.
  - Config/permission sanity checks on startup with warnings.

## 6. TUI Structure (Bubble Tea)

Main menu, each opening a focused screen:
- **Jobs** — list; add/edit (wizard); run now; view run history/last status.
- **Storage** — list/add/test backends.
- **Notifications** — list/add/test channels.
- **Restore** — browse backups per job (from `Backend.List`), restore with guards.
- **Settings** — vault (change passphrase), schedule install/remove, config
  export/import.
- **Logs/History** — recent runs and outcomes.

Conventions: a reusable multi-step wizard component powers all add-flows;
consistent keybindings (arrows/jk, enter, esc, q, ? for help);
honors `NO_COLOR`; when stdout is not a TTY, the binary refuses the TUI and
prints CLI usage instead.

## 7. CLI Surface (automation)

```
dbk                              launch TUI
dbk init                         create config dir + vault (first-run wizard)
dbk run <job>                    perform a backup now
dbk restore <job> [--at id] [--to target] [--dry-run]
dbk list <job>                   list stored backups for a job
dbk test <job|storage|notifier>  connectivity/credential test
dbk schedule install <job> [--at "0 3 * * *"]   write cron/systemd/Task entry
dbk schedule remove <job>
dbk config export [--include-secrets] <file>
dbk config import <file>
dbk vault change-passphrase
dbk version
```
Global flags: `--config <dir>`, `--passphrase-file <f>`, `--json` (machine output
for automation), `--quiet`, `--yes` (non-interactive confirmations).

## 8. Config Export / Import

- Export produces a single portable bundle derived from `config.yaml`.
- By default secrets are excluded (structure only).
- With `--include-secrets`, secrets are re-encrypted under a user-supplied
  **transfer passphrase** (independent of the local vault passphrase), so an
  entire setup can be moved between servers over an untrusted channel.
- Import validates schema version, migrates if needed, and merges (with
  conflict prompts in TUI / `--yes` to overwrite in CLI).

## 9. Scheduling

`dbk schedule install <job>` detects the platform and writes the appropriate
unattended entry that calls `dbk run <job> --quiet --json`:
- **Linux:** systemd user/system timer when systemd is present, else a crontab line.
- **macOS:** crontab line (launchd plist optional later).
- **Windows:** a Scheduled Task via `schtasks`.
Unattended runs obtain the passphrase from `DBK_PASSPHRASE`/`--passphrase-file`,
or rely on secret-injection references, so no interactive prompt is needed.

## 10. Installation

- **Linux/macOS:** `curl -fsSL https://<host>/install.sh | sh`
  - Detects OS/arch, downloads the matching binary from GitHub Releases,
    verifies the SHA-256 checksum, installs to `/usr/local/bin` (fallback
    `~/.local/bin`), makes it executable.
- **Windows:** `irm https://<host>/install.ps1 | iex`
  - PowerShell equivalent; downloads, verifies checksum, installs to a dir on
    PATH (`%LOCALAPPDATA%\Programs\dbk`).
- **Releases:** GoReleaser builds linux/darwin/windows for amd64/arm64 on tag,
  attaches archives + `checksums.txt`.

## 11. Build Phases (git commit per feature)

1. Scaffold (go.mod, Cobra, module layout) + `config` + `vault` + `logutil`.
2. `db.Engine` + MySQL/Postgres/Mongo/SQLite drivers + local backup & restore.
3. `storage.Backend` + Local/S3/R2/GDrive + streamed upload + retention.
4. Compression + optional at-rest encryption in the pipeline.
5. Restore flows (CLI + interactive browser).
6. Notifications (Telegram/email/webhook).
7. Scheduling (cron/systemd/Task Scheduler generation).
8. Config export/import.
9. TUI wiring across all features.
10. GoReleaser + install.sh/install.ps1 + README/docs.

Each phase ends with a green build, passing tests for that phase, and a git commit.

## 12. Testing Strategy

- Interface-level unit tests with fakes (fake Engine/Backend/Notifier) for the
  backup/restore orchestration.
- Vault/crypto: round-trip, wrong-passphrase, and tamper-detection tests.
- Storage: S3 driver tested against a local MinIO instance; Local driver with a
  temp dir.
- DB drivers: integration tests against disposable Docker database instances,
  gated behind a build tag (`//go:build integration`) so the default `go test`
  stays fast and dependency-free.
- Config export/import: round-trip including the `--include-secrets` transfer path.
- Installers: shellcheck on `install.sh`; a smoke test that parses the script's
  OS/arch detection.

## 13. Key Risks & Decisions

- **Native client-tool dependency:** dumps rely on `mysqldump`, `pg_dump`,
  `mongodump`, `sqlite3`. Mitigation: detection + clear install guidance; document
  prerequisites. (Pure-Go dumping is out of scope for v1.)
- **Google Drive OAuth on headless servers:** use device/loopback flow and store
  the refresh token in the vault; document the one-time interactive authorization.
- **Cross-platform scheduling differences:** handled per-OS in `internal/schedule`
  with a common interface; systemd-vs-cron detection on Linux.
- **Streaming vs. restore correctness:** some engines (e.g. Postgres custom
  format) interact with compression/encryption ordering; the pipeline fixes a
  single canonical order (dump -> compress -> encrypt) and records format metadata
  in the object key/sidecar so restore reverses it deterministically.
