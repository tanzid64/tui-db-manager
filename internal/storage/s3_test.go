package storage

import (
	"context"
	"errors"
	"io"
	"strings"
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

func TestMaskingBackendScrubsSecretFromErrors(t *testing.T) {
	inner := &errBackend{err: errors.New("AccessDenied for key AKIA secret topsecretvalue")}
	wrapped := NewMaskingBackend(inner, "topsecretvalue")
	_, err := wrapped.Get(context.Background(), "k")
	if err == nil || strings.Contains(err.Error(), "topsecretvalue") {
		t.Fatalf("secret must be scrubbed from backend errors, got %v", err)
	}
}

type errBackend struct{ err error }

func (b *errBackend) Name() string                                        { return "err" }
func (b *errBackend) Put(context.Context, string, io.Reader, int64) error { return b.err }
func (b *errBackend) Get(context.Context, string) (io.ReadCloser, error)  { return nil, b.err }
func (b *errBackend) List(context.Context, string) ([]Object, error)      { return nil, b.err }
func (b *errBackend) Delete(context.Context, string) error                { return b.err }
func (b *errBackend) Stat(context.Context, string) (Object, error)        { return Object{}, b.err }
func (b *errBackend) Test(context.Context) error                          { return b.err }
