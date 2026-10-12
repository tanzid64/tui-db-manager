//go:build integration

package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

// Requires a MinIO/S3 reachable via DBK_IT_S3_* env vars and an existing bucket.
func TestS3RoundTripIntegration(t *testing.T) {
	o := S3Options{
		Bucket:         os.Getenv("DBK_IT_S3_BUCKET"),
		Region:         envOrS("DBK_IT_S3_REGION", "us-east-1"),
		Endpoint:       os.Getenv("DBK_IT_S3_ENDPOINT"),
		AccessKeyID:    os.Getenv("DBK_IT_S3_KEY"),
		SecretKey:      os.Getenv("DBK_IT_S3_SECRET"),
		ForcePathStyle: true,
		Prefix:         "it/",
	}
	if o.Bucket == "" {
		t.Skip("DBK_IT_S3_BUCKET not set")
	}
	ctx := context.Background()
	be, err := NewS3(ctx, o)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := be.Test(ctx); err != nil {
		t.Fatalf("test bucket: %v", err)
	}
	data := []byte("hello s3 backup")
	if err := be.Put(ctx, "main/x.sql", bytes.NewReader(data), -1); err != nil {
		t.Fatalf("put: %v", err)
	}
	rc, err := be.Get(ctx, "main/x.sql")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("round-trip mismatch: %q", got)
	}
	objs, err := be.List(ctx, "main/")
	if err != nil || len(objs) == 0 || objs[0].Key != "main/x.sql" {
		t.Fatalf("list: %+v err %v", objs, err)
	}
	if err := be.Delete(ctx, "main/x.sql"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func envOrS(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
