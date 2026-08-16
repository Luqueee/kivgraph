package goloader

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

const methodsProviderSource = `package provider

// Base declares a method that Wrapper promotes.
type Base struct {
	// ID identifies the base.
	ID int
}

// Describe is declared on Base.
func (base Base) Describe() int { return base.ID }

// Inner is embedded through a pointer.
type Inner struct{}

// Deep is declared on a pointer receiver.
func (inner *Inner) Deep() int { return 1 }

// Wrapper promotes Describe and Deep.
type Wrapper struct {
	Base
	*Inner
	// Name is a plain field.
	Name string
}

// Reader is an interface implemented by File.
type Reader interface {
	// Read is the interface method.
	Read() int
}

// File implements Reader with a pointer receiver.
type File struct{}

// Read is declared on *File.
func (file *File) Read() int { return 0 }
`

const methodsConsumerSource = `package consumer

import "example.com/module/provider"

// Run exercises promotion, interfaces and method expressions.
func Run(wrapper provider.Wrapper, file *provider.File) int {
	promoted := wrapper.Describe()
	deep := wrapper.Deep()
	direct := file.Read()

	var reader provider.Reader = file
	through := reader.Read()

	expression := provider.Base.Describe
	return promoted + deep + direct + through + expression(wrapper.Base)
}
`

func resolvedMethods(t *testing.T) map[string][]MethodResolution {
	t.Helper()
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":               "module example.com/module\n\ngo 1.24\n",
		"provider/provider.go": methodsProviderSource,
		"consumer/consumer.go": methodsConsumerSource,
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
	resolutions, err := ResolveMethods(context.Background(), result, uses)
	if err != nil {
		t.Fatalf("ResolveMethods() error = %v", err)
	}
	byName := make(map[string][]MethodResolution)
	for _, resolution := range resolutions {
		if resolution.PackagePath != "example.com/module/consumer" {
			continue
		}
		byName[resolution.TargetQualifiedName] = append(byName[resolution.TargetQualifiedName], resolution)
	}
	return byName
}

func TestResolveMethodsAttributesPromotedMethodsToTheirDeclaringType(t *testing.T) {
	byName := resolvedMethods(t)

	describe := byName["Base.Describe"]
	if len(describe) != 2 {
		t.Fatalf("Base.Describe resolutions = %d, want the promoted call and the method expression", len(describe))
	}

	var promoted, expression *MethodResolution
	for index, resolution := range describe {
		switch resolution.Selection {
		case SelectionMethodValue:
			promoted = &describe[index]
		case SelectionMethodExpression:
			expression = &describe[index]
		}
	}
	if promoted == nil || expression == nil {
		t.Fatalf("Base.Describe selections = %#v", describe)
	}

	if promoted.ReceiverTypeName != "Wrapper" {
		t.Fatalf("promoted receiver = %q, want the type that exposes the method", promoted.ReceiverTypeName)
	}
	if promoted.DeclaringTypeName != "Base" {
		t.Fatalf("promoted declaring type = %q, want the type that declares it", promoted.DeclaringTypeName)
	}
	if !promoted.Promoted || len(promoted.EmbeddedPath) != 1 || promoted.EmbeddedPath[0] != "Base" {
		t.Fatalf("promotion path = %#v", promoted.EmbeddedPath)
	}
	if promoted.ReceiverPackagePath != "example.com/module/provider" ||
		promoted.DeclaringPackagePath != "example.com/module/provider" {
		t.Fatalf("promoted packages = %#v", promoted)
	}
	if promoted.DeclaringReceiverIsPointer {
		t.Fatalf("Describe is declared on a value receiver")
	}

	if expression.ReceiverTypeName != "Base" || expression.Promoted {
		t.Fatalf("method expression = %#v", expression)
	}
}

func TestResolveMethodsSeparatesPointerEmbeddingAndInterfaces(t *testing.T) {
	byName := resolvedMethods(t)

	deep := byName["Inner.Deep"]
	if len(deep) != 1 {
		t.Fatalf("Inner.Deep resolutions = %#v", deep)
	}
	if !deep[0].Promoted || len(deep[0].EmbeddedPath) != 1 || deep[0].EmbeddedPath[0] != "Inner" {
		t.Fatalf("Inner.Deep promotion = %#v", deep[0])
	}
	if deep[0].DeclaringTypeName != "Inner" || !deep[0].DeclaringReceiverIsPointer {
		t.Fatalf("Inner.Deep declaring type = %#v", deep[0])
	}
	if !deep[0].IndirectReceiver {
		t.Fatalf("promotion through an embedded pointer must be indirect")
	}

	fileRead := byName["File.Read"]
	if len(fileRead) != 1 {
		t.Fatalf("File.Read resolutions = %#v", fileRead)
	}
	if fileRead[0].ReceiverTypeName != "File" || !fileRead[0].ReceiverIsPointer {
		t.Fatalf("File.Read receiver = %#v", fileRead[0])
	}
	if fileRead[0].ReceiverIsInterface {
		t.Fatalf("a concrete call must not be reported as an interface call")
	}
	if !fileRead[0].DeclaringReceiverIsPointer {
		t.Fatalf("File.Read is declared on a pointer receiver")
	}

	interfaceRead := byName["Reader.Read"]
	if len(interfaceRead) != 1 {
		t.Fatalf("Reader.Read resolutions = %#v", interfaceRead)
	}
	if !interfaceRead[0].ReceiverIsInterface {
		t.Fatalf("call through an interface value = %#v", interfaceRead[0])
	}
	if interfaceRead[0].DeclaringTypeName != "Reader" {
		t.Fatalf("interface method declaring type = %q, want the interface", interfaceRead[0].DeclaringTypeName)
	}
	if interfaceRead[0].Promoted {
		t.Fatalf("an interface method is not promoted")
	}
}

func TestResolveMethodsIsDeterministicAndCancellable(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":               "module example.com/module\n\ngo 1.24\n",
		"provider/provider.go": methodsProviderSource,
		"consumer/consumer.go": methodsConsumerSource,
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}

	first, err := ResolveMethods(context.Background(), result, uses)
	if err != nil {
		t.Fatalf("ResolveMethods() error = %v", err)
	}
	second, err := ResolveMethods(context.Background(), result, uses)
	if err != nil {
		t.Fatalf("ResolveMethods() error = %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("resolution is not deterministic")
	}
	for index := range first {
		if first[index].Offset != second[index].Offset ||
			first[index].DeclaringTypeName != second[index].DeclaringTypeName {
			t.Fatalf("resolution %d differs between runs", index)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ResolveMethods(ctx, result, uses); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveMethods() error = %v, want context.Canceled", err)
	}
}
