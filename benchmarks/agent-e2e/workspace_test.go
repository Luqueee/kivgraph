package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kivgraph runs under an isolated HOME, and Go looks for its module cache under
// $HOME/go, so the harness has to hand it the real one: without it a sweep
// indexed 9 packages instead of 97 and still reported success.
func TestKivgraphRunsUnderTheIsolatedHomeWithTheRealModuleCache(t *testing.T) {
	environment := indexer{Home: "/isolated", GoModCache: "/real/pkg/mod"}.kivgraphEnvironment()
	last := map[string]string{}
	for _, entry := range environment {
		if name, value, ok := strings.Cut(entry, "="); ok {
			last[name] = value
		}
	}
	if last["HOME"] != "/isolated" || last["GOMODCACHE"] != "/real/pkg/mod" {
		t.Fatalf("HOME=%q GOMODCACHE=%q, want the isolated home and the real module cache", last["HOME"], last["GOMODCACHE"])
	}
}

func TestResolveGoModCacheRefusesACacheThatIsNotThere(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-cache")
	t.Setenv("GOMODCACHE", missing)
	if _, err := resolveGoModCache(); err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("resolveGoModCache() error = %v, want one that names %s", err, missing)
	}

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMODCACHE", file)
	if _, err := resolveGoModCache(); err == nil {
		t.Fatal("a regular file was accepted as a module cache")
	}
}

func TestResolveGoModCacheUsesTheEnvironmentBeforeAsking(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("GOMODCACHE", cache)
	got, err := resolveGoModCache()
	if err != nil || got != cache {
		t.Fatalf("resolveGoModCache() = %q, %v, want %q", got, err, cache)
	}
}
