package goloader

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

const usesProviderSource = `package provider

// Answer is an exported constant.
const Answer = 42

// Shape carries an exported field.
type Shape struct {
	// Width is the exported field consumers read.
	Width int
}

// Area is a method consumers call.
func (shape *Shape) Area() int { return shape.Width }

// Compute is an exported function.
func Compute(input int) int { return input + Answer }
`

const usesConsumerSource = `package consumer

import (
	"example.com/module/provider"
)

// Total exercises every kind of use.
func Total(shape *provider.Shape) int {
	local := shape.Width
	area := shape.Area()
	method := (*provider.Shape).Area
	callback := provider.Compute
	return local + area + method(shape) + callback(provider.Answer)
}
`

func loadUses(t *testing.T) ([]Use, string) {
	t.Helper()
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":               "module example.com/module\n\ngo 1.24\n",
		"provider/provider.go": usesProviderSource,
		"consumer/consumer.go": usesConsumerSource,
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
	return uses, module
}

func consumerUses(uses []Use) []Use {
	filtered := make([]Use, 0, len(uses))
	for _, use := range uses {
		if use.PackagePath == "example.com/module/consumer" {
			filtered = append(filtered, use)
		}
	}
	return filtered
}

func TestExtractUsesResolvesTargetsThroughTheTypeChecker(t *testing.T) {
	uses, module := loadUses(t)
	filtered := consumerUses(uses)
	if len(filtered) == 0 {
		t.Fatalf("no uses extracted from the consumer package")
	}

	byTarget := make(map[string][]Use)
	for _, use := range filtered {
		byTarget[use.TargetPackagePath+"."+use.TargetQualifiedName] = append(
			byTarget[use.TargetPackagePath+"."+use.TargetQualifiedName], use)
	}

	compute := byTarget["example.com/module/provider.Compute"]
	if len(compute) != 1 {
		t.Fatalf("Compute uses = %#v", compute)
	}
	if compute[0].TargetKind != KindFunc || compute[0].TargetModulePath != "example.com/module" {
		t.Fatalf("Compute use = %#v", compute[0])
	}
	if !compute[0].TargetIsLocalPackage {
		t.Fatalf("Compute target should belong to a loaded root package")
	}
	if compute[0].SourceQualifiedName != "Total" || compute[0].SourceKind != KindFunc {
		t.Fatalf("Compute use source = %q/%q", compute[0].SourceQualifiedName, compute[0].SourceKind)
	}
	if compute[0].FileName != filepath.Join(module, "consumer/consumer.go") {
		t.Fatalf("Compute use file = %q", compute[0].FileName)
	}
	if compute[0].StartLine == 0 || compute[0].EndOffset <= compute[0].Offset {
		t.Fatalf("Compute use position = %#v", compute[0])
	}

	if answers := byTarget["example.com/module/provider.Answer"]; len(answers) != 1 ||
		answers[0].TargetKind != KindConst {
		t.Fatalf("Answer uses = %#v", answers)
	}
	if shapes := byTarget["example.com/module/provider.Shape"]; len(shapes) != 2 {
		t.Fatalf("Shape type uses = %d, want the parameter and the method expression", len(shapes))
	}
}

func TestExtractUsesRecordsSelections(t *testing.T) {
	uses, _ := loadUses(t)
	filtered := consumerUses(uses)

	var field, methodValue, methodExpression *Use
	for index, use := range filtered {
		switch {
		case use.TargetQualifiedName == "Shape.Width" && use.Selection == SelectionField:
			field = &filtered[index]
		case use.TargetQualifiedName == "Shape.Area" && use.Selection == SelectionMethodValue:
			methodValue = &filtered[index]
		case use.TargetQualifiedName == "Shape.Area" && use.Selection == SelectionMethodExpression:
			methodExpression = &filtered[index]
		}
	}

	if field == nil {
		t.Fatalf("field selection was not recorded: %#v", filtered)
	}
	if field.TargetKind != KindField || !field.IndirectReceiver {
		t.Fatalf("field selection = %#v, want an indirect field of a pointer receiver", field)
	}
	if field.ReceiverType != "*example.com/module/provider.Shape" {
		t.Fatalf("field receiver = %q", field.ReceiverType)
	}
	if methodValue == nil || methodValue.TargetKind != KindMethod {
		t.Fatalf("method value selection = %#v", methodValue)
	}
	if methodExpression == nil || methodExpression.ReceiverType != "*example.com/module/provider.Shape" {
		t.Fatalf("method expression selection = %#v", methodExpression)
	}
}

func TestExtractUsesOmitsLocalsAndPackageNames(t *testing.T) {
	uses, _ := loadUses(t)
	for _, use := range consumerUses(uses) {
		switch use.Name {
		case "local", "area", "method", "callback", "shape", "input", "provider":
			t.Fatalf("use of a local or package name was extracted: %#v", use)
		}
	}
}

func TestExtractUsesIsDeterministicAndCancellable(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":               "module example.com/module\n\ngo 1.24\n",
		"provider/provider.go": usesProviderSource,
		"consumer/consumer.go": usesConsumerSource,
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	first, err := ExtractUses(context.Background(), result, UseOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	second, err := ExtractUses(context.Background(), result, UseOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("extraction is not deterministic: %d vs %d", len(first), len(second))
	}
	for index := range first {
		if first[index].Offset != second[index].Offset ||
			first[index].TargetQualifiedName != second[index].TargetQualifiedName {
			t.Fatalf("use %d differs between runs", index)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExtractUses(ctx, result, UseOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ExtractUses() error = %v, want context.Canceled", err)
	}
}
