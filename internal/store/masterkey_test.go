package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeDefaultKey(t *testing.T) {
	t.Setenv(MasterKeyEnvVar, "")
	dir := t.TempDir()
	db, err := Init(dir, DefaultMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := SaveMasterKeyToEnv(dir, DefaultMasterKey); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSecret("job/a/password", "s3cret"); err != nil {
		t.Fatal(err)
	}
	wrapDir := filepath.Join(dir, "schedules")
	_ = os.MkdirAll(wrapDir, 0o700)
	wrapper := filepath.Join(wrapDir, "dbk-a.sh")
	_ = os.WriteFile(wrapper, []byte("#!/bin/sh\nexport PATH=\"/bin\"\nexport DBK_MASTER_KEY=\"dbk-default\"\nexec dbk run a\n"), 0o700)

	changed, err := UpgradeDefaultKey(dir, db)
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	newKey := MasterKeyFromFile(dir)
	if newKey == "" || newKey == DefaultMasterKey {
		t.Fatalf("env file still has %q", newKey)
	}
	if got, ok := db.GetSecret("job/a/password"); !ok || got != "s3cret" {
		t.Fatalf("secret after rekey = %q, %v", got, ok)
	}
	data, _ := os.ReadFile(wrapper)
	if strings.Contains(string(data), "DBK_MASTER_KEY") || !strings.Contains(string(data), "exec dbk run a") {
		t.Fatalf("wrapper not cleaned: %s", data)
	}

	// The old key no longer opens it; the new one does.
	db.Close()
	if _, err := Open(dir, DefaultMasterKey); err == nil {
		t.Fatal("default key still opens the database")
	}
	db2, err := Open(dir, newKey)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if got, _ := db2.GetSecret("job/a/password"); got != "s3cret" {
		t.Fatalf("secret after reopen = %q", got)
	}
	if changed, _ := UpgradeDefaultKey(dir, db2); changed {
		t.Fatal("upgraded a key that is not the default")
	}
}

func TestUpgradeKeepsUserKey(t *testing.T) {
	t.Setenv(MasterKeyEnvVar, "")
	dir := t.TempDir()
	db, err := Init(dir, "my-own-key")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_ = SaveMasterKeyToEnv(dir, "my-own-key")
	if changed, err := UpgradeDefaultKey(dir, db); changed || err != nil {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if MasterKeyFromFile(dir) != "my-own-key" {
		t.Fatal("user key was replaced")
	}
}

func TestKeyForSchedule(t *testing.T) {
	dir := t.TempDir()
	_ = SaveMasterKeyToEnv(dir, "k1")
	if got := KeyForSchedule(dir, "k1"); got != "" {
		t.Fatalf("key in env file should not be copied, got %q", got)
	}
	if got := KeyForSchedule(dir, "only-in-env-var"); got != "only-in-env-var" {
		t.Fatalf("got %q", got)
	}
}

func TestGenerateMasterKeyRandom(t *testing.T) {
	a, b := GenerateMasterKey(), GenerateMasterKey()
	if a == b || len(a) < 40 {
		t.Fatalf("weak keys %q %q", a, b)
	}
}
