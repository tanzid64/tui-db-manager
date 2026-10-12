package storage

import (
	"context"
	"testing"
)

func TestNewS3AndKeyPrefix(t *testing.T) {
	s, err := NewS3(context.Background(), S3Options{
		Bucket: "backups", Region: "auto", Endpoint: "https://x.r2.cloudflarestorage.com",
		Prefix: "dbk/", AccessKeyID: "AK", SecretKey: "SK", ForcePathStyle: true,
	})
	if err != nil {
		t.Fatalf("new s3: %v", err)
	}
	if s.Name() != "s3" {
		t.Fatalf("name: %q", s.Name())
	}
	if got := s.key("main/x.sql"); got != "dbk/main/x.sql" {
		t.Fatalf("key prefix join wrong: %q", got)
	}
	s2, _ := NewS3(context.Background(), S3Options{Bucket: "b", Region: "us-east-1", AccessKeyID: "AK", SecretKey: "SK"})
	if got := s2.key("main/x.sql"); got != "main/x.sql" {
		t.Fatalf("no-prefix key wrong: %q", got)
	}
}
