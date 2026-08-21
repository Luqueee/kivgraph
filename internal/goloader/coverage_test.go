package goloader

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCoverageFixtureSelectsGoVariantAndResolvesGenerics(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "go", "coverage"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Load(context.Background(), Options{
		Directory: root,
		GOOS:      "linux",
		GOARCH:    "amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if blocking := result.BlockingErrors(); len(blocking) != 0 {
		t.Fatalf("blocking errors = %#v", blocking)
	}

	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "coverage"})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, definition := range definitions {
		names[definition.Name] = true
		if definition.Name == "Platform" && filepath.Base(definition.FileName) != "platform_linux.go" {
			t.Fatalf("Platform file = %q", definition.FileName)
		}
	}
	for _, name := range []string{"Number", "Box", "Identity", "Runner", "implementation", "Run", "Use", "Platform"} {
		if !names[name] {
			t.Fatalf("definitions missing %q: %#v", name, names)
		}
	}
	if names["PlatformOther"] {
		t.Fatal("non-linux build variant was selected")
	}

	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "coverage"})
	if err != nil {
		t.Fatal(err)
	}
	references, err := ClassifyReferences(context.Background(), result, uses)
	if err != nil {
		t.Fatal(err)
	}
	seenCall := map[string]bool{}
	seenType := false
	for _, reference := range references {
		if reference.Kind == ReferenceCallsDirect {
			seenCall[reference.TargetQualifiedName] = true
		}
		if reference.Kind == ReferenceTypeUses && reference.TargetQualifiedName == "Box" {
			seenType = true
		}
	}
	if !seenCall["Identity"] || !seenCall["Runner.Run"] || !seenType {
		t.Fatalf("coverage references missing calls/types: calls=%v type=%v", seenCall, seenType)
	}
}
