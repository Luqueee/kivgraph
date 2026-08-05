package goloader

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

const definitionsSource = `package sample

import "fmt"

// Answer is a package constant.
const Answer = 42

// Registry is a package variable.
var Registry = map[string]int{}

// Shape is a defined type with fields.
type Shape struct {
	// Width is an exported field.
	Width int
	depth int
}

// Alias points at Shape.
type Alias = Shape

// Reader is an interface with one method.
type Reader interface {
	Read(count int) ([]byte, error)
}

// Area returns the area of the shape.
func (shape Shape) Area() int { return shape.Width * shape.depth }

// Scale mutates the shape through a pointer receiver.
func (shape *Shape) Scale(factor int) { shape.Width *= factor }

// Compute is an exported function.
func Compute(input int) int {
	local := input + Answer
	for index := 0; index < 1; index++ {
		local += index
	}
	return local
}

func unexported() { fmt.Println(Answer) }
`

func TestExtractDefinitionsCollectsAddressableSymbolsOnly(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": definitionsSource,
	})

	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v, want none", result.Errors)
	}

	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{
		Repository: "fixture",
	})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}

	byQualifiedName := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		if _, duplicate := byQualifiedName[definition.QualifiedName]; duplicate {
			t.Fatalf("duplicate definition %q", definition.QualifiedName)
		}
		byQualifiedName[definition.QualifiedName] = definition
	}

	wantKinds := map[string]DefinitionKind{
		"Answer":      KindConst,
		"Registry":    KindVar,
		"Shape":       KindType,
		"Alias":       KindAlias,
		"Reader":      KindType,
		"Shape.Width": KindField,
		"Shape.depth": KindField,
		"Shape.Area":  KindMethod,
		"Shape.Scale": KindMethod,
		"Reader.Read": KindMethod,
		"Compute":     KindFunc,
		"unexported":  KindFunc,
	}
	for name, kind := range wantKinds {
		definition, exists := byQualifiedName[name]
		if !exists {
			t.Fatalf("definition %q missing; got %v", name, QualifiedNames(definitions))
		}
		if definition.Kind != kind {
			t.Fatalf("definition %q kind = %q, want %q", name, definition.Kind, kind)
		}
	}
	if len(definitions) != len(wantKinds) {
		t.Fatalf("definitions = %v, want exactly the addressable symbols", QualifiedNames(definitions))
	}

	// Locals, parameters and loop variables are not graph symbols.
	for _, name := range []string{"local", "index", "input", "factor", "count", "shape"} {
		if _, exists := byQualifiedName[name]; exists {
			t.Fatalf("local %q was extracted as a definition", name)
		}
	}

	compute := byQualifiedName["Compute"]
	if compute.Repository != "fixture" || compute.ModulePath != "example.com/module" ||
		compute.PackagePath != "example.com/module/sample" || compute.PackageName != "sample" {
		t.Fatalf("Compute metadata = %#v", compute)
	}
	if compute.Signature != "func(input int) int" {
		t.Fatalf("Compute signature = %q", compute.Signature)
	}
	if !compute.Exported || compute.Owner != "" {
		t.Fatalf("Compute exported=%v owner=%q", compute.Exported, compute.Owner)
	}
	if compute.FileName != filepath.Join(module, "sample/sample.go") {
		t.Fatalf("Compute file = %q", compute.FileName)
	}
	if compute.StartLine != 33 || compute.StartColumn != 6 {
		t.Fatalf("Compute name position = %d:%d, want 33:6", compute.StartLine, compute.StartColumn)
	}
	if compute.DeclarationEndOffset <= compute.DeclarationStartOffset {
		t.Fatalf("Compute declaration span = %#v", compute)
	}
	if compute.Object() == nil || compute.Object().Name() != "Compute" {
		t.Fatalf("Compute object = %#v", compute.Object())
	}

	scale := byQualifiedName["Shape.Scale"]
	if scale.Owner != "Shape" || scale.Receiver != "*Shape" {
		t.Fatalf("Scale owner=%q receiver=%q", scale.Owner, scale.Receiver)
	}
	read := byQualifiedName["Reader.Read"]
	if read.Owner != "Reader" || read.Kind != KindMethod {
		t.Fatalf("Reader.Read = %#v", read)
	}
	depth := byQualifiedName["Shape.depth"]
	if depth.Exported || depth.Owner != "Shape" {
		t.Fatalf("Shape.depth = %#v", depth)
	}
}

func TestExtractDefinitionsCanExcludeUnexported(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": definitionsSource,
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{
		Repository:        "fixture",
		ExcludeUnexported: true,
	})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	for _, definition := range definitions {
		if !definition.Exported {
			t.Fatalf("unexported definition %q was kept", definition.QualifiedName)
		}
	}
}

func TestExtractDefinitionsIsDeterministicAndCancellable(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": definitionsSource,
		"other/other.go":   "package other\n\n// Value is a fact.\nconst Value = 1\n",
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	first, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	second, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	if !equalStringSlices(QualifiedNames(first), QualifiedNames(second)) {
		t.Fatalf("extraction is not deterministic")
	}
	grouped := PackageDefinitions(first)
	if len(grouped["example.com/module/other"]) != 1 {
		t.Fatalf("grouped = %#v", grouped)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExtractDefinitions(ctx, result, DefinitionOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ExtractDefinitions() error = %v, want context.Canceled", err)
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
