# dbk — Secure Database Backup CLI & TUI

[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8.svg)](https://golang.org)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/tanzid64/tui-db-manager?include_prereleases)](https://github.com/tanzid64/tui-db-manager/releases)
[![Documentation](https://img.shields.io/badge/docs-web%20guide-purple.svg)](docs/index.html)

`dbk` is a secure, automated database backup and restore manager. It pairs an interactive, keyboard-driven terminal dashboard (**Bubble Tea / Lipgloss**) with a scriptable, headless CLI designed for automated cron jobs, systemd timers, and CI/CD pipelines.

👉 **[View the Web Landing Page & Storage Setup Guide](docs/index.html)** (Google Drive, Cloudflare R2, AWS S3, MinIO, Backblaze B2).

---

## Key Highlights

- **Multi-Database Support:** Native streaming dump and restore drivers for **MySQL / MariaDB**, **PostgreSQL**, **MongoDB**, and **SQLite**.
- **Versatile Storage Destinations:** Stream backups directly to **Local Filesystems**, **AWS S3**, **Cloudflare R2**, **MinIO**, **Backblaze B2**, or **Google Drive**.
- **Streaming Compression & At-Rest Encryption:**
  - Lightweight streaming **Gzip** compression.
  - Zero-knowledge **age (X25519)** asymmetric encryption at rest—archives uploaded to cloud storage are cryptographically unreadable by the storage provider.
- **Automated Scheduling:** Native OS scheduling via `systemd user timers` (Linux), `crontab` (Linux/macOS), or `Task Scheduler` (Windows) with automatic environment detection.
- **Real-Time Notifications:** Instant alerts on backup success or failure via **Telegram Bot API**, **Email (SMTP with STARTTLS/TLS)**, or **Generic Webhooks**.
- **Hardened Security Model:**
  - Credentials stored in an encrypted vault (`secrets.enc`) locked with **Argon2id** key derivation and **XChaCha20-Poly1305** AEAD encryption.
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

---

## Quick Start

### 1. Interactive Terminal UI

Simply run `dbk` in any terminal:

```bash
dbk
```

When opened for the first time, `dbk` will prompt you to set a master passphrase for your encrypted secrets vault. From the main menu, you can:
- **Jobs:** Add, configure, test, and trigger database backups.
- **Storage:** Connect and verify local directories, S3 buckets, or Google Drive folders.
- **Notifications:** Set up Telegram bots, SMTP email alerts, or HTTP webhooks.
- **Restore:** Browse historical backups from storage and restore with safety confirmation guards.
- **Settings:** Change master passphrase, install system schedules, or export/import configuration bundles.
- **Logs / History:** Inspect recent runs, execution elapsed times, archive sizes, and error details.

### 2. Headless Automation (CLI)

#### Initialize Vault & Config
```bash
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
# Register daily 3:00 AM backup in systemd / crontab / Task Scheduler
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

1. **Vault Encryption:** Secrets (`secrets.enc`) are encrypted using `XChaCha20-Poly1305` AEAD with keys derived via `Argon2id` (memory: 64MB, iterations: 3, parallelism: 4).
2. **At-Rest Protection:** Dump streams can be encrypted on-the-fly using `age` (X25519) before hitting the network, ensuring zero exposure to third-party cloud providers.
3. **Safe Memory & Execution:** DB passwords are piped through standard input or injected via temporary scoped files with `0600` permissions. Process arguments never contain credentials.
4. **Log Redaction:** All logger sinks pass through a string scrubbing filter that automatically redacts known secrets and tokens.

---

## License

MIT License. See [LICENSE](LICENSE) for details.
