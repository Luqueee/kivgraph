package facts

import (
	"context"
	"testing"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestNormalizeSemanticUsesProviderIdentityForExternalTarget(t *testing.T) {
	repository := workspace.Repository{Name: "consumer", Path: "/repos/consumer", RealPath: "/repos/consumer"}
	payload := SemanticPayload{
		Version:       1,
		Authoritative: true,
		Repository:    "consumer",
		Language:      LanguageDart,
		Package:       SemanticPackage{Name: "consumer_app", RootPath: "/repos/consumer"},
		Files:         []SemanticFile{{Path: "lib/main.dart"}},
		Symbols: []SemanticSymbol{{
			ID: "main", File: "lib/main.dart", Name: "main", QualifiedName: "main", Kind: "function", Signature: "void main()",
			StartLine: 1, EndLine: 1, End: 10,
		}},
		References: []SemanticReference{{
			File: "lib/main.dart", SourceID: "main", Kind: string(CallsDirect), StartLine: 1, Start: 1, EndLine: 1, End: 7,
			Target: &SemanticTarget{Repository: "provider", Package: "shared_widgets", File: "lib/widget.dart", QualifiedName: "Widget.create", Kind: "method", Signature: "Widget create()", Source: "PROVIDER_SOURCE"},
		}},
	}
	set, err := NormalizeSemantic(context.Background(), repository, payload)
	if err != nil {
		t.Fatal(err)
	}
	targetKey, err := SemanticTargetKey(LanguageDart, *payload.References[0].Target)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Edges) != 4 {
		t.Fatalf("edges = %#v, want package/file containment, definition and external call", set.Edges)
	}
	for _, edge := range set.Edges {
		if edge.Kind == CallsDirect {
			if edge.TargetKey != targetKey || edge.Confidence != ExactPackageMapped {
				t.Fatalf("external edge = %#v, want target %q and package-mapped confidence", edge, targetKey)
			}
		}
	}
}

func TestNormalizeSemanticSeparatesDartFileScopedDeclarations(t *testing.T) {
	repository := workspace.Repository{Name: "dart-app", Path: "/repos/dart-app", RealPath: "/repos/dart-app"}
	payload := SemanticPayload{
		Version: 1, Repository: "dart-app", Language: LanguageDart,
		Package: SemanticPackage{Name: "dart-app", RootPath: "/repos/dart-app"},
		Files:   []SemanticFile{{Path: "lib/first.dart"}, {Path: "lib/second.dart"}},
		Symbols: []SemanticSymbol{
			{ID: "first", File: "lib/first.dart", Name: "_phoneRe", QualifiedName: "_phoneRe", Kind: "variable", Signature: "RegExp _phoneRe"},
			{ID: "second", File: "lib/second.dart", Name: "_phoneRe", QualifiedName: "_phoneRe", Kind: "variable", Signature: "RegExp _phoneRe"},
		},
	}
	set, err := NormalizeSemantic(context.Background(), repository, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(set.Symbols) != 2 {
		t.Fatalf("symbols = %d, want 2", len(set.Symbols))
	}
	if set.Symbols[0].Key == set.Symbols[1].Key {
		t.Fatalf("file-scoped Dart declarations share key %q", set.Symbols[0].Key)
	}
}

func TestNormalizeSemanticSeparatesDartSameFileLocalDeclarations(t *testing.T) {
	repository := workspace.Repository{Name: "dart-app", Path: "/repos/dart-app", RealPath: "/repos/dart-app"}
	payload := SemanticPayload{
		Version: 1, Repository: "dart-app", Language: LanguageDart,
		Package: SemanticPackage{Name: "dart-app", RootPath: "/repos/dart-app"},
		Files:   []SemanticFile{{Path: "lib/widgets.dart"}},
		Symbols: []SemanticSymbol{
			{ID: "first", File: "lib/widgets.dart", Name: "cardRect", QualifiedName: "build.cardRect", Kind: "function", Signature: "Rect? cardRect()", Start: 100},
			{ID: "second", File: "lib/widgets.dart", Name: "cardRect", QualifiedName: "build.cardRect", Kind: "function", Signature: "Rect? cardRect()", Start: 400},
		},
	}
	set, err := NormalizeSemantic(context.Background(), repository, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(set.Symbols) != 2 || set.Symbols[0].Key == set.Symbols[1].Key {
		t.Fatalf("same-file Dart locals were not separated: %#v", set.Symbols)
	}
}

func TestNormalizeSemanticUsesDartModuleForFileImports(t *testing.T) {
	repository := workspace.Repository{Name: "dart-app", Path: "/repos/dart-app", RealPath: "/repos/dart-app"}
	payload := SemanticPayload{
		Version: 1, Repository: "dart-app", Language: LanguageDart,
		Package: SemanticPackage{Name: "dart-app", RootPath: "/repos/dart-app"},
		Files:   []SemanticFile{{Path: "lib/main.dart"}, {Path: "lib/models.dart"}},
		Symbols: []SemanticSymbol{
			{ID: "module-main", File: "lib/main.dart", Name: "main.dart", QualifiedName: "lib.main", Kind: "module", Signature: "module lib.main"},
			{ID: "module-models", File: "lib/models.dart", Name: "models.dart", QualifiedName: "lib.models", Kind: "module", Signature: "module lib.models"},
		},
		Imports: []SemanticImport{{File: "lib/main.dart", RequestedPackage: "models.dart", TargetID: "module-models"}},
	}
	set, err := NormalizeSemantic(context.Background(), repository, payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range set.Edges {
		if edge.Kind == ImportsSymbol {
			if edge.SourceKey == "file:dart-app:lib/main.dart" {
				t.Fatal("Dart file import used a File as its symbol-relation source")
			}
			return
		}
	}
	t.Fatal("Dart import edge not found")
}
