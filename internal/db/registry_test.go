package db

import (
	"context"
	"io"
	"testing"
)

type fakeEngine struct{ name string }

func (f fakeEngine) Name() string                                   { return f.name }
func (f fakeEngine) DefaultPort() int                               { return 1234 }
func (f fakeEngine) Detect() (string, error)                        { return "/bin/true", nil }
func (f fakeEngine) Test(context.Context, Conn) error               { return nil }
func (f fakeEngine) Dump(context.Context, Conn, io.Writer) error    { return nil }
func (f fakeEngine) Restore(context.Context, Conn, io.Reader) error { return nil }

func TestRegisterAndGet(t *testing.T) {
	Register(fakeEngine{name: "fake"})
	e, ok := Get("fake")
	if !ok || e.Name() != "fake" {
		t.Fatalf("expected registered engine, got %v ok=%v", e, ok)
	}
	if _, ok := Get("nope"); ok {
		t.Fatal("unknown engine should not be found")
	}
}
