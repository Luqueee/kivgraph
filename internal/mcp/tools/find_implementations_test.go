package tools

import (
	"context"
	"testing"
)

func TestImplementationsPageContainsTypedRelationsOnly(t *testing.T) {
	store := dispatchSnapshot(t, 200, 2)
	args := FindImplementationsInput{StableKey: "iface-shared", Limit: 1}
	_, first, err := findImplementations(context.Background(), nil, args, store)
	if err != nil {
		t.Fatalf("find implementations with %#v: %v", args, err)
	}
	if first.Total != 2 || first.Returned != 1 || first.NextCursor == nil {
		t.Fatalf("page for %#v: %#v", args, first)
	}
	if first.Results.Implementations[0].EdgeKind != "IMPLEMENTS" || first.Results.Implementations[0].Detection != "structural" {
		t.Fatalf("untyped result for %#v: %#v", args, first.Results)
	}
	args.Cursor = *first.NextCursor
	_, second, err := findImplementations(context.Background(), nil, args, store)
	if err != nil || second.Returned != 1 || second.NextCursor != nil {
		t.Fatalf("second page with %#v: %#v %v", args, second, err)
	}
	if second.Results.Implementations[0].EdgeKind != "IMPLEMENTS" || second.Results.Implementations[0].Detection != "structural" {
		t.Fatalf("untyped second result for %#v: %#v", args, second.Results)
	}
	if first.Results.Implementations[0].StableKey == second.Results.Implementations[0].StableKey {
		t.Fatalf("duplicate page for %#v: first=%q second=%q", args, first.Results.Implementations[0].StableKey, second.Results.Implementations[0].StableKey)
	}
	args.Detection = "declared"
	if _, _, err := findImplementations(context.Background(), nil, args, store); err == nil {
		t.Fatalf("cursor accepted changed filters: %#v", args)
	}
	args.Detection = ""
	if _, _, err := findImplementations(context.Background(), nil, args, dispatchSnapshot(t, 201, 2)); err == nil {
		t.Fatalf("cursor crossed generations: %#v", args)
	}
	concreteArgs := FindImplementationsInput{StableKey: "impl-sole"}
	_, concrete, err := findImplementations(context.Background(), nil, concreteArgs, store)
	if err != nil || concrete.Total != 0 {
		t.Fatalf("dispatch calls leaked for %#v: %#v %v", concreteArgs, concrete, err)
	}
	if concrete.Completeness == nil || concrete.Completeness.Verdict != VerdictLowerBound {
		t.Fatalf("legacy generation falsely attested complete coverage for %#v", concreteArgs)
	}
	filteredArgs := FindImplementationsInput{StableKey: "iface-shared", Paths: []string{"disk.go"}}
	_, filtered, err := findImplementations(context.Background(), nil, filteredArgs, store)
	if err != nil || filtered.Total != 1 || filtered.Results.Implementations[0].FilePath != "disk.go" {
		t.Fatalf("paths filter for %#v: %#v %v", filteredArgs, filtered, err)
	}
	for _, args := range []FindImplementationsInput{{StableKey: "iface-sole", Detection: "guess"}, {StableKey: "iface-sole", Paths: []string{"../outside"}}} {
		if _, _, err := findImplementations(context.Background(), nil, args, store); err == nil {
			t.Fatalf("invalid arguments accepted: %#v", args)
		}
	}
}
