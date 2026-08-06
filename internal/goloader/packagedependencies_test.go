package goloader

import (
	"context"
	"errors"
	"testing"
)

func TestResolvePackageDependenciesGroupsUsesIntoOnePerPair(t *testing.T) {
	fixture := newCrossFixture(t)
	result, err := Load(context.Background(), Options{
		Directory: fixture.consumer,
		WorkFile:  fixture.workFile,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}

	dependencies, err := ResolvePackageDependencies(context.Background(), uses)
	if err != nil {
		t.Fatalf("ResolvePackageDependencies() error = %v", err)
	}
	if len(dependencies) != 1 {
		t.Fatalf("dependencies = %#v, want exactly one pair: main uses many symbols of api, "+
			"but they all belong to the same package dependency", dependencies)
	}
	dependency := dependencies[0]
	if dependency.PackagePath != "example.com/consumer" || dependency.TargetPackagePath != "example.com/provider/api" {
		t.Fatalf("dependency pair = %#v", dependency)
	}
	if dependency.ModulePath != "example.com/consumer" || dependency.TargetModulePath != "example.com/provider" {
		t.Fatalf("dependency modules = %#v", dependency)
	}
	if dependency.Repository != "consumer" {
		t.Fatalf("dependency.Repository = %q, want %q", dependency.Repository, "consumer")
	}

	// The witness must be the earliest use of the pair, by file then
	// position: recomputed independently here so the assertion does not
	// depend on the fixture's exact source layout.
	wantFile, wantOffset := "", -1
	for _, use := range uses {
		if use.TargetPackagePath != dependency.TargetPackagePath {
			continue
		}
		if wantOffset == -1 || use.FileName < wantFile || (use.FileName == wantFile && use.Offset < wantOffset) {
			wantFile, wantOffset = use.FileName, use.Offset
		}
	}
	if dependency.FileName != wantFile || dependency.Offset != wantOffset {
		t.Fatalf("witness = %s:%d, want the earliest use %s:%d", dependency.FileName, dependency.Offset, wantFile, wantOffset)
	}
}

// TestResolvePackageDependenciesExcludesSelfDependencies proves a package's
// own internal uses — a function calling another declared in the same file —
// never produce a dependency on itself.
func TestResolvePackageDependenciesExcludesSelfDependencies(t *testing.T) {
	fixture := newCrossFixture(t)
	result, err := Load(context.Background(), Options{Directory: fixture.provider})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "provider"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	if len(uses) == 0 {
		t.Fatalf("fixture produced no uses to exercise self-dependency exclusion")
	}

	dependencies, err := ResolvePackageDependencies(context.Background(), uses)
	if err != nil {
		t.Fatalf("ResolvePackageDependencies() error = %v", err)
	}
	if len(dependencies) != 0 {
		t.Fatalf("dependencies = %#v, want none: a package must not depend on itself", dependencies)
	}
}

// TestResolvePackageDependenciesChoosesTheEarliestWitnessRegardlessOfInputOrder
// proves the witness selection does not trust the order uses arrive in: it
// compares every candidate explicitly, by file name and then by position.
func TestResolvePackageDependenciesChoosesTheEarliestWitnessRegardlessOfInputOrder(t *testing.T) {
	later := Use{
		PackagePath: "example.com/consumer", TargetPackagePath: "example.com/provider/api",
		ModulePath: "example.com/consumer", TargetModulePath: "example.com/provider",
		FileName: "b.go", Offset: 5, Name: "Second",
	}
	earlier := Use{
		PackagePath: "example.com/consumer", TargetPackagePath: "example.com/provider/api",
		ModulePath: "example.com/consumer", TargetModulePath: "example.com/provider",
		FileName: "a.go", Offset: 50, Name: "First",
	}
	sameFileLater := Use{
		PackagePath: "example.com/consumer", TargetPackagePath: "example.com/provider/api",
		ModulePath: "example.com/consumer", TargetModulePath: "example.com/provider",
		FileName: "a.go", Offset: 90, Name: "Third",
	}

	// The earliest witness appears last in the slice; it must still win.
	dependencies, err := ResolvePackageDependencies(context.Background(), []Use{later, sameFileLater, earlier})
	if err != nil {
		t.Fatalf("ResolvePackageDependencies() error = %v", err)
	}
	if len(dependencies) != 1 {
		t.Fatalf("dependencies = %#v, want one pair", dependencies)
	}
	if dependencies[0].FileName != "a.go" || dependencies[0].Offset != 50 || dependencies[0].Name != "First" {
		t.Fatalf("witness = %#v, want the earliest file and position regardless of input order", dependencies[0])
	}
}

// TestResolvePackageDependenciesSortsDistinctPairs proves the result is
// ordered deterministically by (source, target) package path, independent of
// Go's randomised map iteration used internally to deduplicate.
func TestResolvePackageDependenciesSortsDistinctPairs(t *testing.T) {
	uses := []Use{
		{PackagePath: "a", TargetPackagePath: "z", FileName: "f.go", Offset: 1},
		{PackagePath: "a", TargetPackagePath: "b", FileName: "f.go", Offset: 2},
		{PackagePath: "c", TargetPackagePath: "a", FileName: "f.go", Offset: 3},
	}
	dependencies, err := ResolvePackageDependencies(context.Background(), uses)
	if err != nil {
		t.Fatalf("ResolvePackageDependencies() error = %v", err)
	}
	want := [][2]string{{"a", "b"}, {"a", "z"}, {"c", "a"}}
	if len(dependencies) != len(want) {
		t.Fatalf("dependencies = %#v, want %d pairs", dependencies, len(want))
	}
	for index, pair := range want {
		if dependencies[index].PackagePath != pair[0] || dependencies[index].TargetPackagePath != pair[1] {
			t.Fatalf("dependency %d = %#v, want (%s, %s)", index, dependencies[index], pair[0], pair[1])
		}
	}
}

func TestResolvePackageDependenciesIsDeterministicAndCancellable(t *testing.T) {
	fixture := newCrossFixture(t)
	result, err := Load(context.Background(), Options{
		Directory: fixture.consumer,
		WorkFile:  fixture.workFile,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}

	first, err := ResolvePackageDependencies(context.Background(), uses)
	if err != nil {
		t.Fatalf("ResolvePackageDependencies() error = %v", err)
	}
	second, err := ResolvePackageDependencies(context.Background(), uses)
	if err != nil {
		t.Fatalf("ResolvePackageDependencies() error = %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("resolution is not deterministic: %d vs %d", len(first), len(second))
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("dependency %d differs between runs:\n%#v\n%#v", index, first[index], second[index])
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ResolvePackageDependencies(ctx, uses); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolvePackageDependencies() error = %v, want context.Canceled", err)
	}
}
