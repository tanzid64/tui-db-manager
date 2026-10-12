package storage

import (
	"context"
	"errors"
	"io"
	"strings"
)

// maskingBackend wraps a Backend and scrubs a secret literal from every error
// it returns, so an S3 secret embedded in an SDK error can never reach a log or
// the CLI's stderr.
type maskingBackend struct {
	inner  Backend
	secret string
}

func NewMaskingBackend(inner Backend, secret string) Backend {
	if secret == "" {
		return inner
	}
	return &maskingBackend{inner: inner, secret: secret}
}

func (m *maskingBackend) scrub(err error) error {
	if err == nil {
		return err
	}
	msg := err.Error()
	if strings.Contains(msg, m.secret) {
		return errors.New(strings.ReplaceAll(msg, m.secret, "***"))
	}
	return err
}

func (m *maskingBackend) Name() string { return m.inner.Name() }

func (m *maskingBackend) Put(ctx context.Context, k string, r io.Reader, n int64) error {
	return m.scrub(m.inner.Put(ctx, k, r, n))
}

func (m *maskingBackend) Get(ctx context.Context, k string) (io.ReadCloser, error) {
	rc, err := m.inner.Get(ctx, k)
	return rc, m.scrub(err)
}

func (m *maskingBackend) List(ctx context.Context, p string) ([]Object, error) {
	o, err := m.inner.List(ctx, p)
	return o, m.scrub(err)
}

func (m *maskingBackend) Delete(ctx context.Context, k string) error {
	return m.scrub(m.inner.Delete(ctx, k))
}

func (m *maskingBackend) Stat(ctx context.Context, k string) (Object, error) {
	o, err := m.inner.Stat(ctx, k)
	return o, m.scrub(err)
}

func (m *maskingBackend) Test(ctx context.Context) error { return m.scrub(m.inner.Test(ctx)) }
