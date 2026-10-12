package logutil

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRedactMasksSecrets(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelInfo)
	l.Redact("s3cr3t")
	l.Infof("connecting with password=%s", "s3cr3t")
	out := buf.String()
	if strings.Contains(out, "s3cr3t") {
		t.Fatalf("secret leaked into log: %q", out)
	}
	if !strings.Contains(out, "***") {
		t.Fatalf("expected masked marker, got %q", out)
	}
}

func TestMaskErrorScrubsSecret(t *testing.T) {
	err := errors.New("auth failed for user with password hunter2!")
	masked := MaskError(err, "hunter2!")
	if masked == nil || strings.Contains(masked.Error(), "hunter2!") {
		t.Fatalf("secret leaked in error: %v", masked)
	}
	if !strings.Contains(masked.Error(), "***") {
		t.Fatalf("expected masked marker, got %v", masked)
	}
}

func TestMaskErrorNilAndEmpty(t *testing.T) {
	if MaskError(nil, "x") != nil {
		t.Fatal("nil error must stay nil")
	}
	e := errors.New("plain")
	if MaskError(e, "") != e {
		t.Fatal("empty secret must return the same error unchanged")
	}
}

func TestDebugSuppressedAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelInfo)
	l.Debugf("hidden")
	if buf.Len() != 0 {
		t.Fatalf("debug should be suppressed at info level, got %q", buf.String())
	}
}
