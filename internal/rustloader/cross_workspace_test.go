package rustloader

import "testing"

// A repository can hold two Cargo workspaces, one depending on a crate of the
// other by path. The use is resolved like one that crosses repositories: the
// key the other workspace publishes, and not a declared absence.
func TestResolveComposesATargetOfAnotherWorkspaceOfTheRepository(t *testing.T) {
	registry := &CrateRegistry{providers: map[string][]CrateProvider{
		"core": {{Repository: "mono", Name: "core", Version: "0.1.0", WorkspacePath: "/src/mono/Cargo.toml"}},
		"app":  {{Repository: "mono", Name: "app", Version: "0.1.0", WorkspacePath: "/src/mono/apps/app/Cargo.toml"}},
		"bare": {{Repository: "mono", Name: "bare", Version: "0.1.0"}},
		"far":  {{Repository: "other", Name: "far", Version: "0.1.0", WorkspacePath: "/src/other/Cargo.toml"}},
	}}
	resolver := &targetResolver{
		repository: "mono", registry: registry, workspace: "/src/mono/apps/app/./Cargo.toml",
		definitions: map[string]Definition{},
	}
	identity := func(crate string) SymbolIdentity {
		t.Helper()
		parsed, err := ParseSymbol("rust-analyzer cargo " + crate + " 0.1.0 lib/Thing#")
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	want, _, err := StableKeyFor("mono", identity("core"))
	if err != nil {
		t.Fatal(err)
	}

	elsewhere := resolver.resolve(identity("core"))
	if elsewhere.reason != "" || elsewhere.key != string(want) || elsewhere.repository != "mono" || elsewhere.qualifiedName == "" {
		t.Errorf("a crate of another workspace = %+v, want the key %s composed in mono", elsewhere, want)
	}
	// The same key the declaring workspace would give its own declaration.
	if far := resolver.resolve(identity("far")); far.reason != "" || far.repository != "other" {
		t.Errorf("a crate of another repository = %+v", far)
	}
	// This workspace's own crate, with no declaration in its index: absent.
	for _, crate := range []string{"app", "bare"} {
		if own := resolver.resolve(identity(crate)); own.reason != UnresolvedDefinitionNotIndexed || own.key != "" {
			t.Errorf("crate %s = %+v, want %s and no key", crate, own, UnresolvedDefinitionNotIndexed)
		}
	}
	// A resolver that was not told its workspace composes nothing it did not.
	resolver.workspace = ""
	if untold := resolver.resolve(identity("core")); untold.reason != UnresolvedDefinitionNotIndexed {
		t.Errorf("without a workspace = %+v", untold)
	}
}
