package cli

import (
	"bytes"
	"testing"
)

func TestUpdateCmdHelpAndCheck(t *testing.T) {
	root := NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"update", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("update --help failed: %v", err)
	}

	got := out.String()
	if got == "" {
		t.Fatalf("expected non-empty help output")
	}
}
