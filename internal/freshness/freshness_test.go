package freshness

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestInventoryRejectsMissingRootsAndCancellation(t *testing.T) {
	if _, err := Capture(t.Context(), []workspace.Repository{{Name: "missing", Path: filepath.Join(t.TempDir(), "absent")}}); err == nil {
		t.Fatal("missing root accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Capture(ctx, []workspace.Repository{{Name: "x", Path: t.TempDir()}}); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestInventoryTracksEditsAdditionsDeletionsAndExclusions(t *testing.T) {
	root := t.TempDir()
	repos := []workspace.Repository{{Name: "test", Path: root, Exclusions: []string{"build"}}}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	capture := func() string {
		t.Helper()
		value, err := Capture(t.Context(), repos)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	write("main.go", "package main")
	initial := capture()
	write("main.go", "package changed")
	if capture() == initial {
		t.Fatal("edit invisible")
	}
	write("main.go", "package main")
	if capture() != initial {
		t.Fatal("restored content differs")
	}
	write("new.go", "package main")
	if capture() == initial {
		t.Fatal("new file invisible")
	}
	if err := os.Remove(filepath.Join(root, "new.go")); err != nil {
		t.Fatal(err)
	}
	if capture() != initial {
		t.Fatal("deletion not tracked")
	}
	if err := os.Mkdir(filepath.Join(root, "build"), 0700); err != nil {
		t.Fatal(err)
	}
	write("build/generated.go", "ignored")
	if capture() != initial {
		t.Fatal("excluded output changed inventory")
	}
	repos[0].Exclusions = nil
	if capture() == initial {
		t.Fatal("registry exclusions not fingerprinted")
	}
}

func TestInventoryIncludesRegisteredScopeAndExplicitManifests(t *testing.T) {
	root := t.TempDir()
	explicit := filepath.Join(root, "project.settings")
	externalRoot := t.TempDir()
	external := filepath.Join(externalRoot, "shared.settings")
	if err := os.WriteFile(explicit, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := workspace.Repository{
		Name:      "test",
		Path:      root,
		Roots:     []string{filepath.Join(root, "src")},
		Manifests: []string{explicit},
	}
	capture := func() string {
		t.Helper()
		digest, err := Capture(t.Context(), []workspace.Repository{repository})
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}

	initial := capture()
	repository.Roots[0] = filepath.Join(root, "other")
	if capture() == initial {
		t.Fatalf("registered root identity %q did not invalidate the inventory", repository.Roots[0])
	}
	initial = capture()
	repository.Manifests[0] = filepath.Join(root, "other.settings")
	if capture() == initial {
		t.Fatalf("registered manifest identity %q did not invalidate the inventory", repository.Manifests[0])
	}
	repository.Manifests[0] = explicit
	initial = capture()
	if err := os.WriteFile(explicit, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if capture() == initial {
		t.Fatalf("explicit manifest %q outside the extension set was not fingerprinted", explicit)
	}
	repository.Manifests[0] = external
	initial = capture()
	if err := os.WriteFile(external, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if capture() == initial {
		t.Fatalf("explicit manifest %q outside the repository walk was not fingerprinted", external)
	}
}

func TestInventoryCanonicalizesEffectiveRepositoryConfiguration(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"main.go", "package.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	defaults := workspace.Repository{
		Name: "test", Path: root,
		Roots:      []string{"z", "a"},
		Manifests:  []string{"package.json", "missing.json"},
		Exclusions: []string{"vendor", "dist"},
	}
	explicit := defaults
	explicit.Languages = slices.Clone(config.SupportedLanguages())
	slices.Reverse(explicit.Languages)
	explicit.Roots = []string{"a", "z"}
	explicit.Manifests = []string{"missing.json", "package.json"}
	explicit.Exclusions = []string{"dist", "vendor"}

	defaultDigest, err := Capture(t.Context(), []workspace.Repository{defaults})
	if err != nil {
		t.Fatal(err)
	}
	explicitDigest, err := Capture(t.Context(), []workspace.Repository{explicit})
	if err != nil {
		t.Fatal(err)
	}
	if defaultDigest != explicitDigest {
		t.Fatalf("equivalent repository configuration produced %q and %q", defaultDigest, explicitDigest)
	}
}

func TestInventoryIncludesAnalyzerBuildConfiguration(t *testing.T) {
	for _, name := range []string{
		"build.gradle", "settings.gradle", "gradle.properties", "build.sbt",
		"Pipfile", "requirements-dev.txt",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("before\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			repository := workspace.Repository{Name: "test", Path: root}
			before, err := Capture(t.Context(), []workspace.Repository{repository})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("after\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			after, err := Capture(t.Context(), []workspace.Repository{repository})
			if err != nil {
				t.Fatal(err)
			}
			if after == before {
				t.Fatalf("editing analyzer configuration %q did not change the inventory", name)
			}
		})
	}
}

func TestInventoryUsesRecursiveWorkspaceExclusions(t *testing.T) {
	root := t.TempDir()
	excluded := filepath.Join(root, "nested", "deeper", "benchmarks", "generated.go")
	if err := os.MkdirAll(filepath.Dir(excluded), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(excluded, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := workspace.Repository{
		Name:       "test",
		Path:       root,
		Languages:  []string{"go"},
		Exclusions: []string{"**/benchmarks"},
	}
	before, err := Capture(t.Context(), []workspace.Repository{repository})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(excluded, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := Capture(t.Context(), []workspace.Repository{repository})
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("editing a deeply nested recursively excluded source changed the inventory")
	}
}

func TestInventoryMatchesWorkspaceExclusionCorpus(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		path       string
		exclusions []string
		absolute   bool
		wantExcl   bool
	}{
		{name: "root exact", path: "benchmarks/main.go", exclusions: []string{"benchmarks"}, wantExcl: true},
		{name: "nested recursive", path: "services/api/benchmarks/main.go", exclusions: []string{"**/benchmarks"}, wantExcl: true},
		{name: "nested generated", path: "services/api/generated/main.go", exclusions: []string{"**/generated"}, wantExcl: true},
		{name: "wildcard segment", path: "services/api/testdata/main.go", exclusions: []string{"**/test*"}, wantExcl: true},
		{name: "relative path", path: "services/api/benchmarks/main.go", exclusions: []string{"./services/api/benchmarks"}, wantExcl: true},
		{name: "absolute path", path: "services/api/benchmarks/main.go", absolute: true, wantExcl: true},
		{name: "not excluded", path: "services/api/main.go", exclusions: []string{"**/benchmarks", "**/generated"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, filepath.FromSlash(testCase.path))
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("before\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			repository := workspace.Repository{
				Name: "test", Path: root, Languages: []string{"go"},
			}
			repository.Exclusions = testCase.exclusions
			if testCase.absolute {
				repository.Exclusions = []string{filepath.Join(root, "services", "api", "benchmarks")}
			}
			excluded, err := workspace.MatchesExclusion(root, path, repository.Exclusions)
			if err != nil {
				t.Fatal(err)
			}
			if excluded != testCase.wantExcl {
				t.Fatalf("workspace matcher path=%q exclusions=%q excluded=%t, want %t", testCase.path, repository.Exclusions, excluded, testCase.wantExcl)
			}
			before, err := Capture(t.Context(), []workspace.Repository{repository})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("after\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			after, err := Capture(t.Context(), []workspace.Repository{repository})
			if err != nil {
				t.Fatal(err)
			}
			changed := after != before
			if changed == excluded {
				t.Fatalf("freshness decision path=%q exclusions=%q changed=%t disagrees with workspace matcher excluded=%t", testCase.path, repository.Exclusions, changed, excluded)
			}
		})
	}
}

func TestFreshnessMonitorInvalidatesOnlyRegisteredInputs(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "main.go")
	ignored := filepath.Join(root, "benchmarks", "main.go")
	if err := os.MkdirAll(filepath.Dir(ignored), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{source, ignored} {
		if err := os.WriteFile(path, []byte("before\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cache := NewCache(Status{Generation: 9, State: "fresh"})
	monitor, err := NewMonitor(t.Context(), []workspace.Repository{{
		Name: "test", Path: root, Languages: []string{"go"}, Exclusions: []string{"**/benchmarks"},
	}}, cache)
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	<-monitor.ready
	if err := os.WriteFile(ignored, []byte("ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFreshnessState(t, cache, "stale", source)
	if got := cache.Load(); !strings.Contains(got.Detail, source) {
		t.Fatalf("source input %q did not produce the invalidation: %+v", source, got)
	}
	if got := cache.Load(); strings.Contains(got.Detail, ignored) {
		t.Fatalf("excluded input %q produced the invalidation: %+v", ignored, got)
	}
}

func waitForFreshnessState(t *testing.T, cache *Cache, want, input string) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if got := cache.Load(); got.State == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("input %q left cache state = %+v, want %q", input, cache.Load(), want)
		case <-ticker.C:
		}
	}
}

func TestFreshnessMonitorInvalidatesRepositoryRegistryChanges(t *testing.T) {
	directory := t.TempDir()
	registryPath := filepath.Join(directory, "repositories.yaml")
	if err := os.WriteFile(registryPath, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	registryWatch, resolved, err := newRegistryWatcher(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	cache := NewCache(Status{Generation: 9, State: "fresh"})
	monitor, err := newMonitor(t.Context(), nil, cache, registryWatch, resolved)
	if err != nil {
		_ = registryWatch.Close()
		t.Fatal(err)
	}
	defer monitor.Close()
	<-monitor.ready
	if err := os.WriteFile(registryPath, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForFreshnessState(t, cache, "stale", registryPath)
}

func TestAttestationIsGenerationBoundAndFailsClosed(t *testing.T) {
	root := t.TempDir()
	source := t.TempDir()
	repos := []workspace.Repository{{Name: "test", Path: source}}
	if got := Check(t.Context(), root, 1, repos); got.State != "unverified" {
		t.Fatal(got)
	}
	digest, err := Capture(t.Context(), repos)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(t.Context(), root, 1, digest); err != nil {
		t.Fatal(err)
	}
	if got := Check(t.Context(), root, 1, repos); got.State != "fresh" {
		t.Fatal(got)
	}
	if got := Check(t.Context(), root, 2, repos); got.State != "unverified" {
		t.Fatal(got)
	}
	if err := os.WriteFile(filepath.Join(source, "new.go"), []byte("package main"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Check(t.Context(), root, 1, repos); got.State != "stale" {
		t.Fatal(got)
	}
	repos[0].Path = filepath.Join(source, "missing")
	if got := Check(t.Context(), root, 1, repos); got.State != "unavailable" {
		t.Fatal(got)
	}
	if err := os.WriteFile(recordPath(root, 1), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Check(t.Context(), root, 1, repos); got.State != "unverified" {
		t.Fatalf("empty record body {} = %+v, want unverified", got)
	}
	if err := os.WriteFile(recordPath(root, 1), []byte(`{"version":1,"generation":1,"digest":"`+strings.Repeat("z", 64)+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Check(t.Context(), root, 1, repos); got.State != "unverified" {
		t.Fatalf("invalid hexadecimal digest = %+v", got)
	}
}

func TestSaveHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Save(ctx, t.TempDir(), 1, strings.Repeat("0", 64)); err == nil {
		t.Fatal("Save() ignored cancellation")
	}
}

func TestCancelledInitialAttestationDoesNotReplaceCache(t *testing.T) {
	root := t.TempDir()
	digest, err := Capture(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := Save(t.Context(), root, 9, digest); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	want := Status{Generation: 9, State: "unverified", Detail: "waiting for initial attestation"}
	cache := NewCache(want)
	monitor := &Monitor{ctx: ctx, cache: cache}
	monitor.verify.Add(1)
	monitor.verifyInitial(root, want.Generation, nil)
	monitor.verify.Wait()
	if got := cache.Load(); got != want {
		t.Fatalf("cancelled initial attestation = %+v, want %+v", got, want)
	}
}

func TestSourceSymlinksAreNotFollowed(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.go"), filepath.Join(root, "link.go")); err != nil {
		t.Skip(err)
	}
	if _, err := Capture(t.Context(), []workspace.Repository{{Name: "test", Path: root}}); err == nil {
		t.Fatal("source symlink accepted")
	}
}

// Remaining filesystem-error branches require failed reads/writes/renames after
// successful opens; no production fault-injection seam is added for these.
