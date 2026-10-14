package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/tanzid64/tui-db-manager/internal/vault"
	_ "modernc.org/sqlite"
)

const DBFileName = "dbk.db"

// DB represents a unified SQLite database backing dbk configuration, history, and encrypted secrets.
type DB struct {
	mu         sync.RWMutex
	db         *sql.DB
	dir        string
	path       string
	key        []byte // derived 32-byte key; nil if locked
	passphrase string
	kdf        vault.KDFParams
}

// Path returns the dbk.db file path for a config directory.
func Path(dir string) string {
	return filepath.Join(dir, DBFileName)
}

// Exists checks if the dbk.db SQLite database exists.
func Exists(dir string) bool {
	_, err := os.Stat(Path(dir))
	return err == nil
}

const schemaDDL = `
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS secrets (
    key        TEXT PRIMARY KEY,
    value      BLOB NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS jobs (
    name                TEXT PRIMARY KEY,
    engine              TEXT NOT NULL,
    conn_host           TEXT DEFAULT '',
    conn_port           INTEGER DEFAULT 0,
    conn_user           TEXT DEFAULT '',
    conn_database       TEXT DEFAULT '',
    conn_path           TEXT DEFAULT '',
    conn_tls            INTEGER DEFAULT 0,
    conn_password_ref   TEXT DEFAULT '',
    storage             TEXT NOT NULL,
    retention_keep_last INTEGER DEFAULT 0,
    compress            INTEGER DEFAULT 0,
    encrypt             INTEGER DEFAULT 0,
    schedule            TEXT DEFAULT '',
    encrypt_recipient   TEXT DEFAULT '',
    notifiers           TEXT DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS storages (
    name             TEXT PRIMARY KEY,
    type             TEXT NOT NULL,
    path             TEXT DEFAULT '',
    bucket           TEXT DEFAULT '',
    region           TEXT DEFAULT '',
    endpoint         TEXT DEFAULT '',
    prefix           TEXT DEFAULT '',
    access_key_id    TEXT DEFAULT '',
    force_path_style INTEGER DEFAULT 0,
    secret_key_ref   TEXT DEFAULT '',
    folder_id        TEXT DEFAULT '',
    credentials_ref  TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS notifiers (
    name          TEXT PRIMARY KEY,
    type          TEXT NOT NULL,
    bot_token_ref TEXT DEFAULT '',
    chat_id       TEXT DEFAULT '',
    host          TEXT DEFAULT '',
    port          INTEGER DEFAULT 0,
    from_addr     TEXT DEFAULT '',
    to_addrs      TEXT DEFAULT '[]',
    username      TEXT DEFAULT '',
    password_ref  TEXT DEFAULT '',
    tls           INTEGER DEFAULT 0,
    url           TEXT DEFAULT '',
    headers       TEXT DEFAULT '{}',
    token_ref     TEXT DEFAULT ''
);

CREATE TABLE IF NOT EXISTS history (
    id        TEXT PRIMARY KEY,
    job       TEXT NOT NULL,
    action    TEXT NOT NULL,
    status    TEXT NOT NULL,
    started   TEXT NOT NULL,
    duration  INTEGER DEFAULT 0,
    size      INTEGER DEFAULT 0,
    error     TEXT DEFAULT '',
    target    TEXT DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_history_job ON history(job);
CREATE INDEX IF NOT EXISTS idx_history_started ON history(started DESC);
`

// Init creates a new SQLite database at dir/dbk.db with the given master passphrase.
func Init(dir, passphrase string) (*DB, error) {
	if passphrase == "" {
		return nil, errors.New("master passphrase cannot be empty")
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}

	dbPath := Path(dir)
	f, err := os.OpenFile(dbPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create sqlite db file: %w", err)
	}
	_ = f.Chmod(0o600)
	_ = f.Close()

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	if _, err := sqlDB.Exec(schemaDDL); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	_ = os.Chmod(dbPath, 0o600)

	// Check if already initialized with verify_token
	var count int
	_ = sqlDB.QueryRow("SELECT COUNT(*) FROM meta WHERE key = 'verify_token'").Scan(&count)
	if count > 0 {
		_ = sqlDB.Close()
		return nil, errors.New("database already initialized")
	}

	kdf := vault.DefaultKDFParams()
	derivedKey := vault.DeriveKey(passphrase, kdf)

	verifyBlob, err := encryptValue(derivedKey, []byte(verifySentinel))
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("encrypt verify token: %w", err)
	}

	tx, err := sqlDB.Begin()
	if err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	defer tx.Rollback()

	insertMeta := func(k string, v []byte) error {
		_, err := tx.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", k, v)
		return err
	}

	if err := insertMeta("schema_version", []byte("1")); err != nil {
		return nil, err
	}
	if err := insertMeta("kdf_salt", kdf.Salt); err != nil {
		return nil, err
	}
	if err := insertMeta("kdf_time", []byte(strconv.FormatUint(uint64(kdf.Time), 10))); err != nil {
		return nil, err
	}
	if err := insertMeta("kdf_memory", []byte(strconv.FormatUint(uint64(kdf.Memory), 10))); err != nil {
		return nil, err
	}
	if err := insertMeta("kdf_threads", []byte(strconv.FormatUint(uint64(kdf.Threads), 10))); err != nil {
		return nil, err
	}
	if err := insertMeta("verify_token", verifyBlob); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	return &DB{
		db:         sqlDB,
		dir:        dir,
		path:       dbPath,
		key:        derivedKey,
		passphrase: passphrase,
		kdf:        kdf,
	}, nil
}

// Open opens an existing SQLite database at dir/dbk.db.
// If passphrase is provided, it verifies the passphrase and unlocks secret access.
// If passphrase is empty, the database is opened locked (config and history readable, secrets inaccessible).
func Open(dir, passphrase string) (*DB, error) {
	dbPath := Path(dir)
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	// Apply file permissions 0600
	_ = os.Chmod(dbPath, 0o600)

	// Ensure tables exist in case of partial or older db
	if _, err := sqlDB.Exec(schemaDDL); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ensure schema: %w", err)
	}

	metaMap := make(map[string][]byte)
	rows, err := sqlDB.Query("SELECT key, value FROM meta")
	if err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("read meta: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
		metaMap[k] = v
	}

	salt, hasSalt := metaMap["kdf_salt"]
	token, hasToken := metaMap["verify_token"]
	_ = token
	if !hasSalt || !hasToken {
		_ = sqlDB.Close()
		return nil, errors.New("corrupt database: missing KDF parameters or verify token")
	}

	timeVal, _ := strconv.ParseUint(string(metaMap["kdf_time"]), 10, 32)
	memVal, _ := strconv.ParseUint(string(metaMap["kdf_memory"]), 10, 32)
	thrVal, _ := strconv.ParseUint(string(metaMap["kdf_threads"]), 10, 8)

	kdf := vault.KDFParams{
		Salt:    salt,
		Time:    uint32(timeVal),
		Memory:  uint32(memVal),
		Threads: uint8(thrVal),
	}

	d := &DB{
		db:   sqlDB,
		dir:  dir,
		path: dbPath,
		kdf:  kdf,
	}

	if passphrase != "" {
		if err := d.Unlock(passphrase); err != nil {
			_ = sqlDB.Close()
			return nil, err
		}
	}

	return d, nil
}

// Unlock attempts to unlock the database with the given master passphrase.
func (d *DB) Unlock(passphrase string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var verifyBlob []byte
	err := d.db.QueryRow("SELECT value FROM meta WHERE key = 'verify_token'").Scan(&verifyBlob)
	if err != nil {
		return fmt.Errorf("read verify token: %w", err)
	}

	derivedKey := vault.DeriveKey(passphrase, d.kdf)
	pt, err := decryptValue(derivedKey, verifyBlob)
	if err != nil || string(pt) != verifySentinel {
		return ErrBadPassphrase
	}

	d.key = derivedKey
	d.passphrase = passphrase
	return nil
}

// IsUnlocked returns true if the master key has been derived and verified.
func (d *DB) IsUnlocked() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.key) > 0
}

// Passphrase returns the master passphrase if unlocked.
func (d *DB) Passphrase() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.passphrase
}

// Dir returns the directory where dbk.db resides.
func (d *DB) Dir() string {
	return d.dir
}

// Close closes the underlying SQLite connection.
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}
