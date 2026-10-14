# dbk Installation & Upgrade Guide

This guide covers fresh installation as well as upgrading from previous flat-file versions of `dbk` to the unified, encrypted SQLite vault architecture.

---

## 1. Fresh Installation Guide

Use this guide when installing `dbk` on a new server, workstation, VPS, or shared hosting environment (e.g., cPanel).

### Step 1: Install the Binary

**Using the installation script (Linux & macOS):**
```bash
curl -fsSL https://raw.githubusercontent.com/tanzid64/tui-db-manager/main/install.sh | sh
```

**Or using Go:**
```bash
go install github.com/tanzid64/tui-db-manager/cmd/dbk@latest
```

---

### Step 2: Initialize the Vault & SQLite Database

Run `dbk init` to set up the configuration directory and database:

#### Interactive Terminal:
```bash
dbk init
```
It will prompt you to set and confirm your **Master Password / Encryption Key**:
```
Enter Master Password or Encryption Key: ********
Confirm Master Password: ********
initialized dbk config at /home/user/.config/dbk
```

#### Automated / Headless / CI:
```bash
export DBK_MASTER_KEY="your-chosen-master-password"
dbk init
```

**What this creates:**
- `~/.config/dbk/dbk.db` (`0600` permissions) — SQLite database containing your jobs, storages, notifiers, history, and encrypted secrets.
- `~/.config/dbk/env` (`0600` permissions) — Contains `export DBK_MASTER_KEY="..."` for persistent CLI use.

---

### Step 3: Add Jobs, Storage, and Credentials

You can use the interactive terminal UI or the headless CLI.

#### Via Interactive UI (TUI):
```bash
dbk
```
- Select **Jobs → Add Job** (MySQL, PostgreSQL, MongoDB, or SQLite).
- Select **Storage → Add Storage** (Local, S3 / Cloudflare R2 / MinIO, or Google Drive).
- Passwords and secret keys are automatically encrypted before being saved into SQLite.

#### Via Headless CLI:
```bash
# Store database password:
dbk secret set job/production-postgres/password --stdin < /path/to/password.txt

# Store S3 secret access key:
dbk secret set storage/s3-backups/secret_key --stdin < /path/to/s3_secret.txt
```

---

### Step 4: Test & Run a Manual Backup

```bash
# Test connectivity:
dbk test production-postgres

# Trigger a backup run:
dbk run production-postgres

# View backup history (pulled directly from SQLite):
dbk list production-postgres
```

---

### Step 5: Install Automated Cron / Systemd Schedule

Run `dbk schedule install` from the CLI or navigate to **Settings → Install Schedule** in the TUI:

```bash
# Schedule daily backup at 3:00 AM:
dbk schedule install production-postgres --at "0 3 * * *"
```

> [!NOTE]
> `dbk` automatically generates a wrapper script at `~/.config/dbk/schedules/dbk-<job>.sh` with `0700` permissions. It bakes in `export DBK_MASTER_KEY='...'` and a complete `PATH`, ensuring unattended cron execution works without hassle—even in stripped cPanel cron environments.

---

## 2. Upgrade Guide (Migrating from Older Flat-File Version)

Use this guide if you are upgrading from an older version of `dbk` that used flat files (`secrets.json`, `config.yaml`, `history.jsonl`).

### What Changed in the New Architecture?

| Component | Previous Version | New Version |
|---|---|---|
| **Storage Engine** | Separate `config.yaml`, `secrets.json`, `history.jsonl` | Single unified SQLite database: `~/.config/dbk/dbk.db` (`0600`) |
| **Secrets Protection** | Plaintext JSON (`secrets.json`, file permission only) | Application-level **XChaCha20-Poly1305** encryption per secret |
| **Key Derivation** | None at rest | **Argon2id** derived 32-byte key from Master Key |
| **Audit Log & History** | Plain JSON Lines (`history.jsonl`) | Queryable `history` table inside SQLite |
| **Unattended Cron** | Required unencrypted file | Script wrapper exports `DBK_MASTER_KEY` (`0700`) |

---

### Step 1: Update the Binary

Pull and install the latest `dbk` release:
```bash
curl -fsSL https://raw.githubusercontent.com/tanzid64/tui-db-manager/main/install.sh | sh
```
Or with Go:
```bash
go install github.com/tanzid64/tui-db-manager/cmd/dbk@latest
```

---

### Step 2: Trigger Automatic Migration

Run `dbk init`:
```bash
dbk init
```

1. You will be prompted to enter a **Master Password / Encryption Key**:
   ```
   Enter Master Password or Encryption Key: ********
   Confirm Master Password: ********
   ```
2. `dbk` automatically detects your existing files and performs the migration:
   - Imports all jobs, storages, and notifiers from `config.yaml` into SQLite.
   - Encrypts each secret from `secrets.json` with your master key and stores it in the `secrets` table.
   - Imports execution logs from `history.jsonl` into the `history` table.
3. Your original files are safely renamed with a `.bak` suffix:
   - `config.yaml` → `config.yaml.bak`
   - `secrets.json` → `secrets.json.bak`
   - `history.jsonl` → `history.jsonl.bak`
4. The migration confirmation is printed:
   ```
   Migrated existing configuration, secrets, and history into SQLite database.
   initialized dbk config at /home/user/.config/dbk
   ```

---

### Step 3: Refresh Automated Schedules

Because the new version uses `DBK_MASTER_KEY` to unlock SQLite during automated runs, re-install your scheduled jobs to update their wrapper scripts:

```bash
# Re-install schedule (this regenerates ~/.config/dbk/schedules/dbk-<job>.sh with DBK_MASTER_KEY):
dbk schedule install <job-name> --at "0 3 * * *"
```

Verify the schedule status:
```bash
dbk schedule status <job-name>
```

---

### Step 4: Verify the Migration

Run a quick test to confirm database access, secrets decryption, and history tracking:

```bash
# 1. Test database connection:
dbk test <job-name>

# 2. Check secret keys:
dbk secret ls

# 3. Test a dry backup run:
dbk run <job-name>

# 4. Open TUI and check 'Logs' to see historical runs preserved:
dbk
```

Once you have verified that everything runs smoothly, you can archive or delete the `.bak` files.
