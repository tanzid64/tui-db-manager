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
