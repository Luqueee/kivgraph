package goloader

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

const callsProviderSource = `package provider

// Answer is a constant.
const Answer = 42

// Handler is a function type.
type Handler func(int) int

// Shape carries a method and a field.
type Shape struct {
	// Width is a field.
	Width int
}

// Area is a method.
func (shape *Shape) Area() int { return shape.Width }

// Compute is a function.
func Compute(input int) int { return input + Answer }

// Hook is a package level variable holding a function.
var Hook = Compute
`

const callsConsumerSource = `package consumer

import "example.com/module/provider"

// Run exercises calls, conversions and plain reads.
func Run(shape *provider.Shape) int {
	direct := provider.Compute(1)
	method := shape.Area()
	expression := (*provider.Shape).Area(shape)
	throughVariable := provider.Hook(2)
	converted := provider.Handler(provider.Compute)
	read := provider.Answer + shape.Width
	return direct + method + expression + throughVariable + converted(3) + read
}
`

func classifiedReferences(t *testing.T) []Reference {
	t.Helper()
	root := t.TempDir()
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":               "module example.com/module\n\ngo 1.24\n",
		"provider/provider.go": callsProviderSource,
		"consumer/consumer.go": callsConsumerSource,
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	references, err := ClassifyReferences(context.Background(), result, uses)
	if err != nil {
		t.Fatalf("ClassifyReferences() error = %v", err)
	}
	filtered := make([]Reference, 0, len(references))
	for _, reference := range references {
		if reference.PackagePath == "example.com/module/consumer" {
			filtered = append(filtered, reference)
		}
	}
	return filtered
}

func kindsByTarget(references []Reference) map[string][]ReferenceKind {
	kinds := make(map[string][]ReferenceKind)
	for _, reference := range references {
		kinds[reference.TargetQualifiedName] = append(kinds[reference.TargetQualifiedName], reference.Kind)
	}
	return kinds
}

func TestClassifyReferencesMarksDirectCalls(t *testing.T) {
	references := classifiedReferences(t)
	kinds := kindsByTarget(references)

	// provider.Compute is called once and passed once as a conversion argument.
	if got := countKind(kinds["Compute"], ReferenceCallsDirect); got != 1 {
		t.Fatalf("Compute direct calls = %d, want 1 (kinds=%v)", got, kinds["Compute"])
	}
	if got := countKind(kinds["Compute"], ReferenceRead); got != 1 {
		t.Fatalf("Compute plain reads = %d, want 1 (kinds=%v)", got, kinds["Compute"])
	}

	// The method value call and the method expression call are both direct.
	if got := countKind(kinds["Shape.Area"], ReferenceCallsDirect); got != 2 {
		t.Fatalf("Shape.Area direct calls = %d, want 2 (kinds=%v)", got, kinds["Shape.Area"])
	}
}

func TestClassifyReferencesKeepsIndirectAndNonCallableUsesApart(t *testing.T) {
	references := classifiedReferences(t)
	kinds := kindsByTarget(references)

	// Calling a variable that holds a function is not a call to a function
	// symbol: the exact callee is unknown at this layer.
	for _, kind := range kinds["Hook"] {
		if kind == ReferenceCallsDirect {
			t.Fatalf("call through a variable was reported as a direct call")
		}
	}
	if countKind(kinds["Hook"], ReferenceRead) != 1 {
		t.Fatalf("Hook kinds = %v, want a plain reference", kinds["Hook"])
	}

	// A conversion names a type, never a call.
	for _, kind := range kinds["Handler"] {
		if kind != ReferenceTypeUses {
			t.Fatalf("Handler kinds = %v, want only type uses", kinds["Handler"])
		}
	}
	if len(kinds["Shape"]) == 0 {
		t.Fatalf("the type of the parameter was not classified")
	}
	for _, kind := range kinds["Shape"] {
		if kind != ReferenceTypeUses {
			t.Fatalf("Shape kinds = %v, want only type uses", kinds["Shape"])
		}
	}

	// Fields and constants stay plain references.
	if countKind(kinds["Shape.Width"], ReferenceRead) != 1 {
		t.Fatalf("Shape.Width kinds = %v", kinds["Shape.Width"])
	}
	if countKind(kinds["Answer"], ReferenceRead) != 1 {
		t.Fatalf("Answer kinds = %v", kinds["Answer"])
	}
}

func TestClassifyReferencesIsDeterministicAndCancellable(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":               "module example.com/module\n\ngo 1.24\n",
		"provider/provider.go": callsProviderSource,
		"consumer/consumer.go": callsConsumerSource,
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}

	first, err := ClassifyReferences(context.Background(), result, uses)
	if err != nil {
		t.Fatalf("ClassifyReferences() error = %v", err)
	}
	second, err := ClassifyReferences(context.Background(), result, uses)
	if err != nil {
		t.Fatalf("ClassifyReferences() error = %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("classification is not deterministic")
	}
	for index := range first {
		if first[index].Kind != second[index].Kind || first[index].Offset != second[index].Offset {
			t.Fatalf("reference %d differs between runs", index)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ClassifyReferences(ctx, result, uses); !errors.Is(err, context.Canceled) {
		t.Fatalf("ClassifyReferences() error = %v, want context.Canceled", err)
	}
}

func countKind(kinds []ReferenceKind, wanted ReferenceKind) int {
	total := 0
	for _, kind := range kinds {
		if kind == wanted {
			total++
		}
	}
	return total
}
