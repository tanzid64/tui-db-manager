package db

import (
	"strings"
	"testing"
)

func TestPgEnvPassword(t *testing.T) {
	env := PgEnv(Conn{Password: "pw", TLS: true})
	j := strings.Join(env, " ")
	if !strings.Contains(j, "PGPASSWORD=pw") || !strings.Contains(j, "PGSSLMODE=require") {
		t.Fatalf("expected PGPASSWORD and PGSSLMODE, got %v", env)
	}
}

func TestPgDumpArgsCustomFormat(t *testing.T) {
	args := BuildPgDumpArgs(Conn{Host: "h", Port: 5432, User: "u", Database: "app"})
	j := strings.Join(args, " ")
	for _, w := range []string{"--host=h", "--port=5432", "--username=u", "--format=custom", "app"} {
		if !strings.Contains(j, w) {
			t.Fatalf("expected %q in %q", w, j)
		}
	}
}
