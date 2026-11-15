package goloader

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func loadedPackageIDs(result Result) []string {
	ids := make([]string, 0, len(result.Packages))
	for _, loaded := range result.Packages {
		ids = append(ids, loaded.ID)
	}
	sort.Strings(ids)
	return ids
}

// TestLoadWithTestsDropsTheMainThatGoTestGenerates guards the package that
// `go test` builds to run a package's tests. It is not source anybody wrote:
// its only file lives in the build cache, so every declaration in it was
// reported as a scope the index could not read, and one such scope per tested
// package made every answer about a repository a lower bound.
func TestLoadWithTestsDropsTheMainThatGoTestGenerates(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFiles(t, root, map[string]string{
		"go.mod": "module example.com/lib\n\ngo 1.24\n",
		"lib.go": "package lib\n\nfunc Add(a, b int) int { return a + b }\n",
		"lib_test.go": `package lib

import "testing"

func TestAdd(t *testing.T) { _ = Add(1, 2) }
`,
		"ext/ext_test.go": `package ext_test

import (
	"testing"

	"example.com/lib"
)

func TestExternal(t *testing.T) { _ = lib.Add(1, 1) }
`,
	})

	result, err := Load(context.Background(), Options{Directory: root, IncludeTests: true})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []string{
		"example.com/lib",
		"example.com/lib [example.com/lib.test]",
		"example.com/lib/ext",
		"example.com/lib/ext_test [example.com/lib/ext.test]",
	}
	if got := loadedPackageIDs(result); !reflect.DeepEqual(got, want) {
		t.Fatalf("package ids = %q, want %q", got, want)
	}

	// The tests themselves stay indexed: only the generated main goes.
	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "lib"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	declared := map[string]bool{}
	for _, definition := range definitions {
		if !strings.HasPrefix(definition.FileName, root+string(filepath.Separator)) {
			t.Errorf("definition %q is declared in %q, outside the repository", definition.QualifiedName, definition.FileName)
		}
		declared[definition.QualifiedName] = true
	}
	for _, name := range []string{"Add", "TestAdd", "TestExternal"} {
		if !declared[name] {
			t.Errorf("definitions miss %q: the test files must still be indexed", name)
		}
	}
}

// TestLoadWithTestsKeepsAPackageWhoseImportPathEndsInTest is the false positive
// the rule has to avoid: a real main package whose path ends in `.test` is
// source in the repository, and only the main generated *for it* -- the one
// named `<path>.test` for a package that is loaded -- is dropped.
func TestLoadWithTestsKeepsAPackageWhoseImportPathEndsInTest(t *testing.T) {
	root := testsupport.TempDir(t)
	writeFiles(t, root, map[string]string{
		"go.mod":  "module example.com/tool.test\n\ngo 1.24\n",
		"main.go": "package main\n\nfunc main() {}\n",
		"main_test.go": `package main

import "testing"

func TestMain(m *testing.M) { m.Run() }
`,
	})

	result, err := Load(context.Background(), Options{Directory: root, IncludeTests: true})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []string{
		"example.com/tool.test",
		"example.com/tool.test [example.com/tool.test.test]",
	}
	if got := loadedPackageIDs(result); !reflect.DeepEqual(got, want) {
		t.Fatalf("package ids = %q, want %q", got, want)
	}
}
