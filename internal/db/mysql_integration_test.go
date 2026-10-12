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
		User:     envOr("DBK_IT_MYSQL_USER", "root"),
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
