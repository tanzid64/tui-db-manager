package cli

import (
	"bytes"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got := out.String(); got == "" || got[0:3] != "dbk" {
		t.Fatalf("expected version output starting with 'dbk', got %q", got)
	}
}
