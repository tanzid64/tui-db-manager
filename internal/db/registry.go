package db

import "sort"

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
