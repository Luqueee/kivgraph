package goloader

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

const typeRelationsFixture = "type-relations"

func loadTypeRelationsFixture(t *testing.T) Result {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "go", typeRelationsFixture))
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}
	result, err := Load(context.Background(), Options{Directory: root})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("load errors = %#v", result.Errors)
	}
	return result
}

func resolvedTypeRelations(t *testing.T) []TypeRelation {
	t.Helper()
	result := loadTypeRelationsFixture(t)
	relations, err := ResolveTypeRelations(context.Background(), result, TypeRelationOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ResolveTypeRelations() error = %v", err)
	}
	return relations
}

// findRelation returns the relation of kind from source to target. At most
// one relation exists per (kind, source, target) triple: IMPLEMENTS decides
// once per interface pair, and Go forbids embedding the same type twice.
func findRelation(relations []TypeRelation, kind TypeRelationKind, source, target string) (TypeRelation, bool) {
	for _, relation := range relations {
		if relation.Kind == kind && relation.SourceQualifiedName == source && relation.TargetQualifiedName == target {
			return relation, true
		}
	}
	return TypeRelation{}, false
}

func TestResolveTypeRelationsImplementsByValueAndByPointer(t *testing.T) {
	relations := resolvedTypeRelations(t)

	circle, found := findRelation(relations, RelationImplements, "Circle", "Shape")
	if !found {
		t.Fatalf("Circle IMPLEMENTS Shape not found in %#v", relations)
	}
	if circle.Pointer {
		t.Fatalf("Circle implements Shape by value: Pointer = %v, want false", circle.Pointer)
	}
	if circle.SourceKind != KindType || circle.TargetKind != KindType {
		t.Fatalf("Circle IMPLEMENTS Shape kinds = %#v", circle)
	}
	if circle.TargetPackagePath != "example.com/kivgraph-fixture/type-relations" {
		t.Fatalf("Shape target package = %q", circle.TargetPackagePath)
	}

	square, found := findRelation(relations, RelationImplements, "Square", "Shape")
	if !found {
		t.Fatalf("Square IMPLEMENTS Shape not found in %#v", relations)
	}
	if !square.Pointer {
		t.Fatalf("Square must be reported as pointer-only, never as a value implementation: %#v", square)
	}

	if _, found := findRelation(relations, RelationImplements, "Triangle", "Shape"); found {
		t.Fatalf("Triangle has no Area method: it must not implement Shape")
	}
}

func TestResolveTypeRelationsExcludesTheEmptyInterface(t *testing.T) {
	relations := resolvedTypeRelations(t)
	for _, relation := range relations {
		if relation.Kind == RelationImplements && relation.TargetQualifiedName == "Anything" {
			t.Fatalf("the empty interface must never be an IMPLEMENTS target: %#v", relation)
		}
	}
}

func TestResolveTypeRelationsDropsTargetsWithoutARepository(t *testing.T) {
	relations := resolvedTypeRelations(t)

	stringer, found := findRelation(relations, RelationImplements, "Circle", "Stringer")
	if !found {
		t.Fatalf("Circle IMPLEMENTS Stringer not found in %#v", relations)
	}
	if stringer.TargetModulePath != "" {
		t.Fatalf("fmt.Stringer must carry no module path: %#v", stringer)
	}
	if stringer.TargetPackagePath != "fmt" {
		t.Fatalf("target package = %q, want fmt", stringer.TargetPackagePath)
	}
	if stringer.TargetIsLocalPackage {
		t.Fatalf("fmt is not one of the fixture's root packages")
	}
}

func TestResolveTypeRelationsEmbedsByValueAndByPointer(t *testing.T) {
	relations := resolvedTypeRelations(t)

	circleBase, found := findRelation(relations, RelationEmbeds, "Circle", "Base")
	if !found {
		t.Fatalf("Circle EMBEDS Base not found in %#v", relations)
	}
	if circleBase.Pointer {
		t.Fatalf("Circle embeds Base by value: Pointer = %v, want false", circleBase.Pointer)
	}
	if circleBase.SourceKind != KindType || circleBase.TargetKind != KindType {
		t.Fatalf("Circle EMBEDS Base kinds = %#v", circleBase)
	}

	squareBase, found := findRelation(relations, RelationEmbeds, "Square", "Base")
	if !found {
		t.Fatalf("Square EMBEDS Base not found in %#v", relations)
	}
	if !squareBase.Pointer {
		t.Fatalf("Square embeds Base by pointer: Pointer = %v, want true", squareBase.Pointer)
	}

	solidShape, found := findRelation(relations, RelationEmbeds, "Solid", "Shape")
	if !found {
		t.Fatalf("Solid EMBEDS Shape not found in %#v", relations)
	}
	if solidShape.Pointer {
		t.Fatalf("interface embedding carries no pointer distinction: %#v", solidShape)
	}

	if _, found := findRelation(relations, RelationEmbeds, "Triangle", "Base"); found {
		t.Fatalf("Triangle embeds nothing: no EMBEDS relation must exist")
	}
}

func TestResolveTypeRelationsOverridesThePromotedMethod(t *testing.T) {
	relations := resolvedTypeRelations(t)

	override, found := findRelation(relations, RelationOverrides, "Circle.ID", "Base.ID")
	if !found {
		t.Fatalf("Circle.ID OVERRIDES Base.ID not found in %#v", relations)
	}
	if override.SourceKind != KindMethod || override.TargetKind != KindMethod {
		t.Fatalf("OVERRIDES kinds = %#v", override)
	}
	if override.PromotionDepth < 1 {
		t.Fatalf("PromotionDepth = %d, want at least 1", override.PromotionDepth)
	}

	// Square never declares its own ID: nothing shadows the one promoted
	// from its embedded *Base, so no OVERRIDES relation exists for it.
	if _, found := findRelation(relations, RelationOverrides, "Square.ID", "Base.ID"); found {
		t.Fatalf("Square declares no ID of its own: no OVERRIDES relation must exist")
	}
	// Area is declared once, directly, on Circle: it shadows nothing.
	if _, found := findRelation(relations, RelationOverrides, "Circle.Area", "Base.Area"); found {
		t.Fatalf("Base has no Area method: Circle.Area cannot override it")
	}
}

func TestResolveTypeRelationsIsDeterministicAndCancellable(t *testing.T) {
	result := loadTypeRelationsFixture(t)

	first, err := ResolveTypeRelations(context.Background(), result, TypeRelationOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ResolveTypeRelations() error = %v", err)
	}
	second, err := ResolveTypeRelations(context.Background(), result, TypeRelationOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ResolveTypeRelations() error = %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("resolution is not deterministic: %d vs %d", len(first), len(second))
	}
	for index := range first {
		if first[index].Kind != second[index].Kind ||
			first[index].SourceQualifiedName != second[index].SourceQualifiedName ||
			first[index].TargetQualifiedName != second[index].TargetQualifiedName ||
			first[index].Pointer != second[index].Pointer ||
			first[index].Offset != second[index].Offset {
			t.Fatalf("relation %d differs between runs:\n%#v\n%#v", index, first[index], second[index])
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ResolveTypeRelations(ctx, result, TypeRelationOptions{Repository: "fixture"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveTypeRelations() error = %v, want context.Canceled", err)
	}
}

// TestResolveTypeRelationsPairsInterfaceMethodsWithTheirImplementation is the
// property a dynamic call depends on. The type-level relation says Circle
// satisfies Shape; only the method-level one says which declaration a call
// through Shape.Area arrives at, and without it a reference question about the
// implementation answers that nothing calls it.
//
// Blob is the reason the pairing cannot be a name match: it declares Area too,
// spelled identically, and returns an int, so the method set does not match and
// the checker says no. A tool that paired on names would name Blob.Area as an
// implementation of Shape.Area, which no call can ever reach.
func TestResolveTypeRelationsPairsInterfaceMethodsWithTheirImplementation(t *testing.T) {
	relations := resolvedTypeRelations(t)

	byValue, found := findRelation(relations, RelationImplements, "Circle.Area", "Shape.Area")
	if !found {
		t.Fatalf("Circle.Area -> Shape.Area is missing from %d relations", len(relations))
	}
	if byValue.Pointer {
		t.Fatalf("Circle.Area pairs by value, got pointer = true: %#v", byValue)
	}
	if byValue.SourceKind != "method" || byValue.TargetKind != "method" {
		t.Fatalf("method pairing has non-method ends: source %q target %q", byValue.SourceKind, byValue.TargetKind)
	}

	// The pointer receiver is the one that satisfies Shape, and the pairing
	// says so on the method as well as on the type.
	byPointer, found := findRelation(relations, RelationImplements, "Square.Area", "Shape.Area")
	if !found {
		t.Fatal("Square.Area -> Shape.Area is missing")
	}
	if !byPointer.Pointer {
		t.Fatalf("Square.Area satisfies Shape only by pointer, got pointer = false: %#v", byPointer)
	}

	// An interface whose method lives in a dependency pairs the same way.
	if _, found := findRelation(relations, RelationImplements, "Circle.String", "Stringer.String"); !found {
		t.Fatal("Circle.String -> Stringer.String is missing")
	}

	// The trap: same name, different signature, no relation of either
	// granularity.
	if _, found := findRelation(relations, RelationImplements, "Blob.Area", "Shape.Area"); found {
		t.Fatal("Blob.Area was paired with Shape.Area, which a name match would do and the checker does not")
	}
	if _, found := findRelation(relations, RelationImplements, "Blob", "Shape"); found {
		t.Fatal("Blob was reported as implementing Shape")
	}

	// A type that declares no such method at all pairs nothing either.
	if _, found := findRelation(relations, RelationImplements, "Triangle.Area", "Shape.Area"); found {
		t.Fatal("Triangle.Area exists in no source file and was paired anyway")
	}
}
