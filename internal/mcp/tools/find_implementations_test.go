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
		t.Fatalf("page: %#v", first)
	}
	if first.Results.Implementations[0].EdgeKind != "IMPLEMENTS" || first.Results.Implementations[0].Detection != "structural" {
		t.Fatalf("untyped result: %#v", first.Results)
	}
	args.Cursor = *first.NextCursor
	_, second, err := findImplementations(context.Background(), nil, args, store)
	if err != nil || second.Returned != 1 || second.NextCursor != nil {
		t.Fatalf("second page with %#v: %#v %v", args, second, err)
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
	_, concrete, err := findImplementations(context.Background(), nil, FindImplementationsInput{StableKey: "impl-sole"}, store)
	if err != nil || concrete.Total != 0 {
		t.Fatalf("dispatch calls leaked into implementations: %#v %v", concrete, err)
	}
	if concrete.Completeness == nil || concrete.Completeness.Verdict != VerdictLowerBound {
		t.Fatalf("legacy generation falsely attested complete coverage for stable_key %q", "impl-sole")
	}
	_, filtered, err := findImplementations(context.Background(), nil, FindImplementationsInput{StableKey: "iface-shared", Paths: []string{"disk.go"}}, store)
	if err != nil || filtered.Total != 1 || filtered.Results.Implementations[0].FilePath != "disk.go" {
		t.Fatalf("paths filter: %#v %v", filtered, err)
	}
	for _, args := range []FindImplementationsInput{{StableKey: "iface-sole", Detection: "guess"}, {StableKey: "iface-sole", Paths: []string{"../outside"}}} {
		if _, _, err := findImplementations(context.Background(), nil, args, store); err == nil {
			t.Fatalf("invalid arguments accepted: %#v", args)
		}
	}
}
