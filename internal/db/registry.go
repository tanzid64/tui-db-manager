package db

import (
	"context"
	"fmt"
	"sort"
)

var registry = map[string]Engine{}

func Register(e Engine) { registry[e.Name()] = e }

// Get returns the named engine, wrapped so that connections with SSH settings
// are routed through an SSH tunnel.
func Get(name string) (Engine, bool) {
	e, ok := registry[name]
	if !ok {
		return nil, false
	}
	return tunneled{e}, true
}

func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ListDatabases queries the engine for available databases if supported.
func ListDatabases(ctx context.Context, engineName string, c Conn) ([]string, error) {
	eng, ok := Get(engineName)
	if !ok {
		return nil, fmt.Errorf("unknown database engine %q", engineName)
	}
	return eng.(DatabaseLister).ListDatabases(ctx, c)
}
