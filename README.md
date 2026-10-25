# dbk — Secure Database Backup CLI & TUI

[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/tanzid64/tui-db-manager?include_prereleases)](https://github.com/tanzid64/tui-db-manager/releases)
[![Documentation](https://img.shields.io/badge/docs-dbk.tanzid.dev-purple.svg)](https://dbk.tanzid.dev)

`dbk` is a secure, automated database backup and restore manager. It pairs an interactive, keyboard-driven terminal dashboard (**Bubble Tea / Lipgloss**) with a scriptable, headless CLI designed for automated cron jobs, systemd timers, and CI/CD pipelines.

👉 **[View the Web Landing Page & Storage Setup Guide](https://dbk.tanzid.dev)** (Google Drive, Cloudflare R2, AWS S3, MinIO, Backblaze B2).

---

## Key Highlights

- **Multi-Database Support:** Native streaming dump and restore drivers for **MySQL / MariaDB**, **PostgreSQL**, **MongoDB**, and **SQLite**.
- **Versatile Storage Destinations:** Stream backups directly to **Local Filesystems**, **AWS S3**, **Cloudflare R2**, **MinIO**, **Backblaze B2**, or **Google Drive**.
- **Streaming Compression & At-Rest Encryption:**
  - Lightweight streaming **Gzip** compression.
  - Zero-knowledge **age (X25519)** asymmetric encryption at rest—archives uploaded to cloud storage are cryptographically unreadable by the storage provider.
- **Automated Scheduling:** Uses `crontab` whenever it exists (cPanel and VPS alike, keeps running after you log out), falling back to `systemd` user timers. Each scheduled run appends its output to `~/.config/dbk/logs/<job>.log`.
- **Ready Out of the Box:** Installing creates a default `local` storage at `~/dbk-backups`, so you can add a database and back it up immediately.
- **Real-Time Notifications:** Instant alerts on backup success or failure via **Telegram Bot API**, **Email (SMTP with STARTTLS/TLS)**, or **Generic Webhooks**.
- **Hardened Security Model:**
  - Credentials stored in a dedicated local secrets vault (`secrets.json`) with restricted `0600` file permissions, decoupled from `config.yaml`.
  - Zero password leakage: credentials are never passed via command-line arguments (preventing `ps` leakage) and automatically redacted from logs.
  - Support for decoupled secret references (`env:VAR_NAME` and `file:/path/to/secret`).
- **Portable Config Bundles:** Single-file YAML/JSON configuration export and import with transfer-passphrase re-encryption for safe transmission across untrusted networks.
- **Interactive Terminal UI:** Modern terminal dashboard with multi-step setup wizards, live status indicators, historical backup browser, and destructive restore safety guards.

---

## Installation

### Linux & macOS

```bash
curl -fsSL https://raw.githubusercontent.com/tanzid64/tui-db-manager/main/install.sh | sh
```

### Using Go

```bash
go install github.com/tanzid64/tui-db-manager/cmd/dbk@latest
```

📖 **Detailed Documentation:** See the [Installation & Upgrade Guide](docs/install-and-upgrade.md) for step-by-step setup and flat-file migration instructions.

### Clean Uninstallation

To cleanly wipe all background schedules, configuration, secrets, and SQLite database:

```bash
# Via CLI:
dbk uninstall --yes --purge-data

# Via install script:
curl -fsSL https://raw.githubusercontent.com/tanzid64/tui-db-manager/main/install.sh | sh -s -- --uninstall

# Or inside TUI: Settings → Clean Uninstall & Purge Data
```

---

## Quick Start

### 1. Interactive Terminal UI

Simply run `dbk` in any terminal:

```bash
dbk
```

When opened, `dbk` launches into the interactive dashboard. If your vault is locked, it will prompt for your Master Key (or read `DBK_MASTER_KEY` from your environment). From the main menu, you can:
- **Backup Jobs:** Add a database, test its connection (`t`), and back it up now (`Enter`).
- **Schedules:** Pick a frequency (daily, every 6/12 hours, hourly, weekly, or custom cron) for each job.
- **Restore:** Pick a backup (newest first) and restore it after typing `RESTORE` to confirm.
- **Storage:** The default `local` folder, or an S3-compatible bucket (AWS S3, Cloudflare R2, MinIO, Backblaze B2, Wasabi) or Google Drive. Every storage is tested with a real write when you save it.
- **History:** The last 20 backups and restores, including scheduled runs, with errors.
- **Settings:** Notifications (Telegram, email, webhook), export/import, updates, and uninstall.

> **cPanel tip:** use the database user cPanel created (e.g. `account_dbuser`) with host `localhost` or `127.0.0.1`. dbk passes `--no-tablespaces`, so the dump works without the global PROCESS privilege that shared hosting never grants.

### 2. Headless Automation (CLI)

#### Initialize Vault & SQLite Database
```bash
# Interactive setup (press Enter to generate a random master key; creates ~/.config/dbk/dbk.db):
dbk init

# Or unattended setup via environment variable:
export DBK_MASTER_KEY="your-secure-master-key"
dbk init
```

#### Run a Backup
```bash
dbk run production-postgres
```

Output with `--json` for automation:
```bash
dbk run production-postgres --json
```

#### List Backups for a Job
```bash
dbk list production-postgres
```

#### Restore a Backup
```bash
# Restore latest backup (requires --yes confirmation)
dbk restore production-postgres --yes

# Restore a specific timestamped archive into a staging database
dbk restore production-postgres --at production-postgres/production-postgres-20261013-143000.pgdump.gz.age --to staging_db --yes
```

#### Manage Vault Secrets
```bash
# Store a database password
dbk secret set job/production-postgres/password --stdin < secret.txt

# List secret keys (values are never exposed)
dbk secret ls

# Remove a secret
dbk secret rm job/production-postgres/password
```

#### Install Automated Schedules
```bash
# Register a daily 3:00 AM backup (crontab when available, otherwise systemd)
dbk schedule install production-postgres --at "0 3 * * *"

# Check scheduler status
dbk schedule status production-postgres

# Remove scheduled timer
dbk schedule remove production-postgres
```

#### Portable Config Export & Import
```bash
# Export configuration structure only (no secrets)
dbk config export backup-config.yaml

# Export configuration with re-encrypted secrets under a transfer passphrase
dbk config export backup-bundle.yaml --include-secrets

# Import and merge bundle into another machine
dbk config import backup-bundle.yaml --yes
```

---

## Configuration Reference (`config.yaml`)

Configuration is stored in `~/.config/dbk/config.yaml` (or `%APPDATA%\dbk\config.yaml` on Windows).

```yaml
version: 1

jobs:
  - name: production-postgres
    engine: postgres
    conn:
      host: 127.0.0.1
      port: 5432
      user: postgres
      database: app_prod
      # password stored in vault under "job/production-postgres/password"
      # or explicitly referenced: password_ref: "env:DB_PASSWORD"
    storage: s3-backups
    retention:
      keep_last: 14
    compress: true
    encrypt: true
    encrypt_recipient: "age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p"
    notifiers:
      - telegram-ops

storages:
  - name: s3-backups
    type: s3
    bucket: company-db-backups
    region: us-east-1
    access_key_id: AKIAEXAMPLE
    # secret_key stored in vault under "storage/s3-backups/secret_key"
    prefix: daily/

notifiers:
  - name: telegram-ops
    type: telegram
    chat_id: "-100123456789"
    # bot_token stored in vault under "notifier/telegram-ops/bot_token"
```

---

## Security Model

1. **Unified SQLite Vault & At-Rest Encryption:** Configuration, history audit trails, and encrypted credentials are stored in a dedicated SQLite database (`dbk.db`) with strict `0600` file permissions. Sensitive secret values are encrypted at the application level using XChaCha20-Poly1305 with an Argon2id key derived from the master key.
2. **Random Master Key:** Every install gets its own random 256-bit master key, saved in `~/.config/dbk/env` (mode `0600`). Installs from versions that used the old fixed `dbk-default` key are moved to a random key automatically the next time you open dbk or run the installer. Schedule wrapper scripts (`0700`) do not contain the key; scheduled runs read it from the env file.
3. **Encrypted Backups by Default:** New jobs encrypt every backup with `age` (X25519) before it leaves the server, so S3, R2 or Google Drive only ever store unreadable files. The private key lives in the vault: keep a copy (**Settings → Export**, include secrets) or encrypted backups cannot be restored on a new server. Jobs sending unencrypted backups to cloud storage are marked `⚠ not encrypted`.
4. **Safe Memory & Execution:** DB passwords are piped through standard input or injected via temporary scoped files with `0600` permissions. Process arguments never contain credentials.
5. **Log Redaction:** All logger sinks pass through a string scrubbing filter that automatically redacts known secrets and tokens.

---

## License

MIT License. See [LICENSE](LICENSE) for details.
