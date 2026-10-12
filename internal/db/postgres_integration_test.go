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
