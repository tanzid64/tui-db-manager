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
