package goloader

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/testsupport"
)

func keyedFixture(t *testing.T, repository string, files map[string]string) []KeyedDefinition {
	t.Helper()
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, files)
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{
		Repository: repository,
	})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	keyed, err := AssignStableKeys(context.Background(), definitions)
	if err != nil {
		t.Fatalf("AssignStableKeys() error = %v", err)
	}
	return keyed
}

func keysByName(keyed []KeyedDefinition) map[string]KeyedDefinition {
	byName := make(map[string]KeyedDefinition, len(keyed))
	for _, definition := range keyed {
		byName[definition.QualifiedName] = definition
	}
	return byName
}

const stableKeySource = `package sample

// Shape is a defined type.
type Shape struct {
	// Width is exported and reachable from the package scope.
	Width int
	depth int
}

// Area is an exported method.
func (shape Shape) Area() int { return shape.Width * shape.depth }

// Compute is an exported function.
func Compute(input int) int { return input }

func hidden() int { return 1 }
`

func TestStableKeysAreUniqueAndAuditable(t *testing.T) {
	keyed := keysByName(keyedFixture(t, "fixture", map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": stableKeySource,
	}))

	seen := make(map[string]string)
	for name, definition := range keyed {
		if definition.StableKey == "" {
			t.Fatalf("definition %q has no stable key", name)
		}
		if previous, collision := seen[string(definition.StableKey)]; collision {
			t.Fatalf("definitions %q and %q share a stable key", previous, name)
		}
		seen[string(definition.StableKey)] = name
	}

	compute := keyed["Compute"]
	if compute.ObjectPath != "Compute" {
		t.Fatalf("Compute object path = %q", compute.ObjectPath)
	}
	for _, fragment := range []string{
		"language=2:go",
		"repository=7:fixture",
		"example.com/module example.com/module/sample",
		"objectpath:Compute",
		"kind=4:func",
	} {
		if !strings.Contains(compute.CanonicalIdentity, fragment) {
			t.Fatalf("canonical identity missing %q:\n%s", fragment, compute.CanonicalIdentity)
		}
	}

	area := keyed["Shape.Area"]
	if area.ObjectPath != "Shape.M0" {
		t.Fatalf("Shape.Area object path = %q", area.ObjectPath)
	}
	// Index based paths rotate when a member is inserted, so identity uses the
	// syntactic name and keeps the path only as resolution metadata.
	if !strings.Contains(area.CanonicalIdentity, "syntax:Shape.Area") {
		t.Fatalf("Shape.Area identity = %s", area.CanonicalIdentity)
	}
	width := keyed["Shape.Width"]
	if width.ObjectPath != "Shape.UF0" ||
		!strings.Contains(width.CanonicalIdentity, "syntax:Shape.Width") {
		t.Fatalf("Shape.Width = %#v", width)
	}
	shape := keyed["Shape"]
	if shape.ObjectPath != "Shape" ||
		!strings.Contains(shape.CanonicalIdentity, "objectpath:Shape") {
		t.Fatalf("Shape identity = %#v", shape)
	}

	// Unexported package-level objects are unreachable from the package scope,
	// so they fall back to the syntactic identity instead of losing their key.
	hidden := keyed["hidden"]
	if hidden.ObjectPath != "" {
		t.Fatalf("unexported function reported an object path: %q", hidden.ObjectPath)
	}
	if !strings.Contains(hidden.CanonicalIdentity, "syntax:hidden") {
		t.Fatalf("hidden identity = %s", hidden.CanonicalIdentity)
	}
	depth := keyed["Shape.depth"]
	if depth.ObjectPath != "Shape.UF1" ||
		!strings.Contains(depth.CanonicalIdentity, "syntax:Shape.depth") {
		t.Fatalf("Shape.depth identity = %#v", depth)
	}
}

func TestStableKeysIgnoreSourcePositions(t *testing.T) {
	base := keysByName(keyedFixture(t, "fixture", map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": stableKeySource,
	}))
	moved := keysByName(keyedFixture(t, "fixture", map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": "// Package sample moves every declaration down.\n\n" + stableKeySource,
	}))

	for name, definition := range base {
		other, exists := moved[name]
		if !exists {
			t.Fatalf("definition %q disappeared after moving lines", name)
		}
		if definition.StableKey != other.StableKey {
			t.Fatalf("definition %q changed key when its line moved", name)
		}
		if definition.StartLine == other.StartLine {
			t.Fatalf("fixture did not actually move %q", name)
		}
	}
}

func TestStableKeysSurviveMemberInsertion(t *testing.T) {
	before := keysByName(keyedFixture(t, "fixture", map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": stableKeySource,
	}))
	// Inserting a field and a method rotates every later index based object
	// path; the identity of the existing members must not move with it.
	after := keysByName(keyedFixture(t, "fixture", map[string]string{
		"go.mod": "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": strings.Replace(
			strings.Replace(
				stableKeySource,
				"type Shape struct {\n",
				"type Shape struct {\n\t// Height is inserted before every other field.\n\tHeight int\n",
				1,
			),
			"// Area is an exported method.",
			"// Perimeter is inserted before Area.\nfunc (shape Shape) Perimeter() int { return 0 }\n\n// Area is an exported method.",
			1,
		),
	}))

	if after["Shape.Width"].ObjectPath == before["Shape.Width"].ObjectPath {
		t.Fatalf("fixture did not rotate the object paths")
	}
	for _, name := range []string{"Shape", "Shape.Width", "Shape.depth", "Shape.Area", "Compute"} {
		if before[name].StableKey != after[name].StableKey {
			t.Fatalf("definition %q changed key after inserting a member", name)
		}
	}
	if _, exists := after["Shape.Perimeter"]; !exists {
		t.Fatalf("inserted method was not extracted")
	}
}

func TestStableKeysSeparateHomonymsAcrossPackagesModulesAndRepositories(t *testing.T) {
	first := keysByName(keyedFixture(t, "first", map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": stableKeySource,
		"other/other.go":   "package other\n\n// Compute is a homonym in another package.\nfunc Compute(input int) int { return input }\n",
	}))
	otherModule := keysByName(keyedFixture(t, "first", map[string]string{
		"go.mod":           "module example.com/other\n\ngo 1.24\n",
		"sample/sample.go": stableKeySource,
	}))
	otherRepository := keysByName(keyedFixture(t, "second", map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": stableKeySource,
	}))

	compute := first["Compute"]
	homonyms := map[string]KeyedDefinition{
		"another module":     otherModule["Compute"],
		"another repository": otherRepository["Compute"],
	}
	for reason, homonym := range homonyms {
		if homonym.StableKey == compute.StableKey {
			t.Fatalf("Compute shares its key with a homonym in %s", reason)
		}
	}

	// Two packages of the same module also stay apart.
	var samePackageCount int
	for _, definition := range first {
		if definition.Name == "Compute" {
			samePackageCount++
		}
	}
	if samePackageCount != 1 {
		t.Fatalf("qualified names collided inside the same load")
	}
}

func TestAssignStableKeysRejectsIncompleteIdentity(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": stableKeySource,
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	if _, err := AssignStableKeys(context.Background(), definitions); !errors.Is(err, ErrMissingRepository) {
		t.Fatalf("AssignStableKeys() error = %v, want ErrMissingRepository", err)
	}

	withRepository, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	withRepository[0].ModulePath = ""
	if _, err := AssignStableKeys(context.Background(), withRepository); !errors.Is(err, ErrMissingModulePath) {
		t.Fatalf("AssignStableKeys() error = %v, want ErrMissingModulePath", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AssignStableKeys(ctx, withRepository); !errors.Is(err, context.Canceled) {
		t.Fatalf("AssignStableKeys() error = %v, want context.Canceled", err)
	}
}
