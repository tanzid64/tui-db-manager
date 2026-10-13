package db

import (
	"context"
	"fmt"
	"sort"
)

var registry = map[string]Engine{}

func Register(e Engine) { registry[e.Name()] = e }

func Get(name string) (Engine, bool) { e, ok := registry[name]; return e, ok }

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
	if lister, ok := eng.(DatabaseLister); ok {
		return lister.ListDatabases(ctx, c)
	}
	return nil, nil
}

