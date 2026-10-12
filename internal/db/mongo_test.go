package db

import (
	"os"
	"strings"
	"testing"
)

func TestMongoBaseArgsNoPassword(t *testing.T) {
	args := BuildMongoBaseArgs(Conn{Host: "h", Port: 27017, User: "u", Database: "app", Password: "pw"})
	j := strings.Join(args, " ")
	if strings.Contains(j, "pw") {
		t.Fatalf("password must not be in argv: %q", j)
	}
	for _, w := range []string{"--host=h", "--port=27017", "--username=u", "--db=app"} {
		if !strings.Contains(j, w) {
			t.Fatalf("expected %q in %q", w, j)
		}
	}
}

func TestWriteMongoConfigFilePerms(t *testing.T) {
	path, cleanup, err := writeMongoConfig("s3cr3t")
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	defer cleanup()
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config file must be 0600, got %o", info.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "s3cr3t") {
		t.Fatalf("password must be written to the config file")
	}
}
