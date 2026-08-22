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

// TestNormalizeSemanticGivesADirectiveEdgeItsObservedSpan is the guard on a
// directive edge a reader can open. The producers always observed the span --
// both Python workers send the end of every import -- and the decoder had
// nowhere to put it, so `IMPORTS_SYMBOL`, `REEXPORTS` and `PART_OF` published
// with no evidence at all.
func TestNormalizeSemanticGivesADirectiveEdgeItsObservedSpan(t *testing.T) {
	repository := workspace.Repository{Name: "dart-app", Path: "/repos/dart-app", RealPath: "/repos/dart-app"}
	payload := SemanticPayload{
		Version: 1, Repository: "dart-app", Language: LanguageDart, Authoritative: true,
		Package: SemanticPackage{Name: "dart-app", RootPath: "/repos/dart-app"},
		Files:   []SemanticFile{{Path: "lib/main.dart"}, {Path: "lib/models.dart"}},
		Symbols: []SemanticSymbol{
			{ID: "module-main", File: "lib/main.dart", Name: "main.dart", QualifiedName: "lib.main", Kind: "module", Signature: "module lib.main"},
			{ID: "module-models", File: "lib/models.dart", Name: "models.dart", QualifiedName: "lib.models", Kind: "module", Signature: "module lib.models"},
		},
		Imports: []SemanticImport{{
			File: "lib/main.dart", RequestedPackage: "models.dart", TargetID: "module-models",
			StartLine: 1, StartColumn: 0, Start: 0,
			EndLine: 1, EndColumn: 23, End: 23,
			Detail: "'models.dart'",
		}},
	}
	set, err := NormalizeSemantic(context.Background(), repository, payload)
	if err != nil {
		t.Fatal(err)
	}
	var found Edge
	for _, edge := range set.Edges {
		if edge.Kind == ImportsSymbol {
			found = edge
			break
		}
	}
	if found.EvidenceKey == "" {
		t.Fatalf("import edge = %#v, want the span the producer observed", found)
	}
	var evidence Evidence
	for _, candidate := range set.Evidence {
		if candidate.Key == found.EvidenceKey {
			evidence = candidate
			break
		}
	}
	want := Evidence{
		Key: found.EvidenceKey, RepositoryKey: "repository:dart-app", FileKey: "file:dart-app:lib/main.dart",
		Start: Position{Line: 1, Column: 0, Offset: 0},
		End:   Position{Line: 1, Column: 23, Offset: 23},
		Text:  "'models.dart'",
	}
	if evidence != want {
		t.Fatalf("evidence = %#v, want %#v", evidence, want)
	}
}

// TestNormalizeSemanticKeepsOnePartEdgeWithEvidenceAtItsSource covers the
// relation Dart declares from both of its ends. Two directives, one
// relationship: the graph holds one edge, unlike two call sites, which are two
// events an agent has to visit. Its evidence is the directive in the part file,
// because the edge runs part -> library and every other edge here keeps its
// evidence at its source -- even when the library side was observed first.
func TestNormalizeSemanticKeepsOnePartEdgeWithEvidenceAtItsSource(t *testing.T) {
	repository := workspace.Repository{Name: "dart-app", Path: "/repos/dart-app", RealPath: "/repos/dart-app"}
	payload := SemanticPayload{
		Version: 1, Repository: "dart-app", Language: LanguageDart, Authoritative: true,
		Package: SemanticPackage{Name: "dart-app", RootPath: "/repos/dart-app"},
		Files:   []SemanticFile{{Path: "lib/library.dart"}, {Path: "lib/piece.dart"}},
		Symbols: []SemanticSymbol{
			{ID: "module-library", File: "lib/library.dart", Name: "library.dart", QualifiedName: "lib.library", Kind: "module", Signature: "module lib.library"},
			{ID: "module-piece", File: "lib/piece.dart", Name: "piece.dart", QualifiedName: "lib.piece", Kind: "module", Signature: "module lib.piece"},
		},
		Parts: []SemanticPart{
			// `part 'piece.dart';` observed in the library, first.
			{
				File: "lib/library.dart", LibraryFile: "lib/library.dart", PartFile: "lib/piece.dart",
				StartLine: 2, Start: 20, EndLine: 2, EndColumn: 19, End: 39, Detail: "piece.dart",
			},
			// `part of 'library.dart';` observed in the part, second.
			{
				File: "lib/piece.dart", LibraryFile: "lib/library.dart", PartFile: "lib/piece.dart",
				StartLine: 1, Start: 0, EndLine: 1, EndColumn: 23, End: 23, Detail: "library.dart",
			},
		},
	}
	set, err := NormalizeSemantic(context.Background(), repository, payload)
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]Edge, 0, 1)
	for _, edge := range set.Edges {
		if edge.Kind == PartOf {
			parts = append(parts, edge)
		}
	}
	if len(parts) != 1 {
		t.Fatalf("part edges = %#v, want one relation", parts)
	}
	for _, evidence := range set.Evidence {
		if evidence.Key != parts[0].EvidenceKey {
			continue
		}
		if evidence.FileKey != "file:dart-app:lib/piece.dart" {
			t.Fatalf("part evidence file = %q, want the part file: the edge's source", evidence.FileKey)
		}
		if evidence.Start.Offset != 0 || evidence.End.Offset != 23 {
			t.Fatalf("part evidence span = %d-%d, want the `part of` directive", evidence.Start.Offset, evidence.End.Offset)
		}
		return
	}
	t.Fatalf("part edge %#v names no evidence in %#v", parts[0], set.Evidence)
}

// TestNormalizeSemanticPublishesNoEvidenceWithoutASpan keeps the honest half.
// A producer that reports no end observed no span, and inventing one would
// collide: EvidenceKey is derived from the offsets, so every directive of one
// file would share a key and each would overwrite the last.
func TestNormalizeSemanticPublishesNoEvidenceWithoutASpan(t *testing.T) {
	repository := workspace.Repository{Name: "dart-app", Path: "/repos/dart-app", RealPath: "/repos/dart-app"}
	payload := SemanticPayload{
		Version: 1, Repository: "dart-app", Language: LanguageDart,
		Package: SemanticPackage{Name: "dart-app", RootPath: "/repos/dart-app"},
		Files:   []SemanticFile{{Path: "lib/main.dart"}, {Path: "lib/models.dart"}},
		Symbols: []SemanticSymbol{
			{ID: "module-main", File: "lib/main.dart", Name: "main.dart", QualifiedName: "lib.main", Kind: "module", Signature: "module lib.main"},
			{ID: "module-models", File: "lib/models.dart", Name: "models.dart", QualifiedName: "lib.models", Kind: "module", Signature: "module lib.models"},
		},
		Imports: []SemanticImport{
			{File: "lib/main.dart", RequestedPackage: "a.dart", TargetID: "module-models", StartLine: 1},
			{File: "lib/main.dart", RequestedPackage: "b.dart", TargetID: "module-models", StartLine: 2},
		},
	}
	set, err := NormalizeSemantic(context.Background(), repository, payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Evidence) != 0 {
		t.Fatalf("evidence = %#v, want none: no span was observed", set.Evidence)
	}
	for _, edge := range set.Edges {
		if edge.Kind == ImportsSymbol && edge.EvidenceKey != "" {
			t.Fatalf("import edge names evidence %q that does not exist", edge.EvidenceKey)
		}
	}
}
