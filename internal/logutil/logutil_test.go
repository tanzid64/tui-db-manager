package logutil

import (
	"bytes"
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

func TestDebugSuppressedAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, LevelInfo)
	l.Debugf("hidden")
	if buf.Len() != 0 {
		t.Fatalf("debug should be suppressed at info level, got %q", buf.String())
	}
}
