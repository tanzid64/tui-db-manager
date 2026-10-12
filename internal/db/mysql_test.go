package db

import (
	"strings"
	"testing"
)

func TestMySQLEnvCarriesPasswordNotArgs(t *testing.T) {
	c := Conn{Host: "h", Port: 3306, User: "u", Password: `p@ss w"rd$x`, Database: "app"}
	env := MySQLEnv(c)
	found := false
	for _, e := range env {
		if e == `MYSQL_PWD=p@ss w"rd$x` {
			found = true
		}
	}
	if !found {
		t.Fatalf("password must be passed via MYSQL_PWD env, got %v", env)
	}
	for _, a := range BuildMySQLDumpArgs(c) {
		if strings.Contains(a, "p@ss") {
			t.Fatalf("password leaked into argv: %q", a)
		}
	}
}

func TestMySQLDumpArgsIncludeTarget(t *testing.T) {
	args := BuildMySQLDumpArgs(Conn{Host: "h", Port: 3306, User: "u", Database: "app"})
	joined := strings.Join(args, " ")
	for _, want := range []string{"--host=h", "--port=3306", "--user=u", "app"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q in args %q", want, joined)
		}
	}
}
