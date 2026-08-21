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
	if len(set.Edges) != 3 {
		t.Fatalf("edges = %#v, want containment, definition and external call", set.Edges)
	}
	for _, edge := range set.Edges {
		if edge.Kind == CallsDirect {
			if edge.TargetKey != targetKey || edge.Confidence != ExactPackageMapped {
				t.Fatalf("external edge = %#v, want target %q and package-mapped confidence", edge, targetKey)
			}
		}
	}
}
