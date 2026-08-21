package dartloader

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestDartReferenceKindClassifiesResolvedUses(t *testing.T) {
	cases := []struct {
		name, source, target, targetKind, want string
	}{
		{name: "type", source: "Vehicle vehicle;", target: "Vehicle", targetKind: "CLASS", want: "TYPE_USES"},
		{name: "call", source: "Vehicle();", target: "Vehicle", targetKind: "CONSTRUCTOR", want: "CALLS_DIRECT"},
		{name: "callback", source: "run(handler);", target: "handler", targetKind: "FUNCTION", want: "PASSES_AS_CALLBACK"},
		{name: "assignment", source: "final value = handler;", target: "handler", targetKind: "FUNCTION", want: "ASSIGNS_FUNCTION"},
		{name: "return", source: "return handler;", target: "handler", targetKind: "FUNCTION", want: "RETURNS_FUNCTION"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			offset := strings.Index(testCase.source, testCase.target)
			if offset < 0 {
				t.Fatalf("target not found in %q", testCase.source)
			}
			got := dartReferenceKind([]byte(testCase.source), offset, len(testCase.target), testCase.targetKind)
			if got != testCase.want {
				t.Fatalf("dartReferenceKind() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestDartAnalyzerOutlinesIgnoreExternalConstructorInvocations(t *testing.T) {
	root := filepath.FromSlash("/workspace/app")
	if !isDartAnalyzerConstructorInvocation(analyzerOutline{Element: analyzerElement{Kind: "CONSTRUCTOR", Location: &analyzerLocation{File: "/sdk/flutter/widgets.dart"}}}, root) {
		t.Fatal("external constructor invocation should not become a declaration")
	}
	if !isDartAnalyzerConstructorInvocation(analyzerOutline{Element: analyzerElement{Kind: "CONSTRUCTOR_INVOCATION", Location: &analyzerLocation{File: "/workspace/app/lib/widgets.dart"}}}, root) {
		t.Fatal("constructor invocation should not become a declaration")
	}
	if isDartAnalyzerConstructorInvocation(analyzerOutline{Element: analyzerElement{Kind: "CONSTRUCTOR", Location: &analyzerLocation{File: "/workspace/app/lib/widgets.dart"}}}, root) {
		t.Fatal("constructor declaration in the repository should remain available")
	}
	if isDartAnalyzerConstructorInvocation(analyzerOutline{Element: analyzerElement{Kind: "METHOD"}}, root) {
		t.Fatal("methods are declarations")
	}
}

func TestRunAgainstConfiguredDartProject(t *testing.T) {
	root := os.Getenv("KIVGRAPH_DART_ROOT")
	if root == "" {
		t.Skip("set KIVGRAPH_DART_ROOT to run the external Dart analyzer acceptance test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	payload, err := Run(ctx, "dart", "dart", workspace.Repository{Name: "dart-fixture", Path: root, RealPath: root}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Language != facts.LanguageDart || len(payload.Files) == 0 || len(payload.Symbols) == 0 {
		t.Fatalf("payload = language %q, files=%d, symbols=%d", payload.Language, len(payload.Files), len(payload.Symbols))
	}
	if len(payload.References) == 0 {
		t.Fatal("Dart analyzer returned no resolved references")
	}
}

func TestFileURIKeepsAbsolutePathSegments(t *testing.T) {
	got := fileURI("/tmp/dart project/lib/main.dart")
	if got != "file:///tmp/dart%20project/lib/main.dart" {
		t.Fatalf("fileURI() = %q", got)
	}
}

func TestDartOffsetsUseUTF16ForAnalyzerAndLSPPositions(t *testing.T) {
	data := []byte("🚕 café\nnext")
	emojiEnd := len("🚕")
	if got := offsetAt(data, position{Line: 0, Character: 2}); got != emojiEnd {
		t.Fatalf("offsetAt() = %d, want %d", got, emojiEnd)
	}
	if got := positionAt(data, emojiEnd); got != (position{Line: 0, Character: 2}) {
		t.Fatalf("positionAt() = %#v, want line 0 column 2", got)
	}
	if got := analyzerOffset(data, 2); got != emojiEnd {
		t.Fatalf("analyzerOffset() = %d, want %d", got, emojiEnd)
	}
}

func TestDartFilesHonourTestAndGeneratedBoundaries(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"lib/main.dart", "test/main_test.dart", "lib/model.g.dart", "lib/gen/localizations.dart"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("class Fixture {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := dartFiles(root, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || relative(root, files[0]) != "lib/main.dart" {
		t.Fatalf("dartFiles() = %#v, want only lib/main.dart", files)
	}
	files, err = dartFiles(root, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("inclusive dartFiles() = %d, want 4", len(files))
	}
}

func TestRunFixtureResolvesDartDeclarationsAndCalls(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "dart", "basic"))
	if err != nil {
		t.Fatal(err)
	}
	repository := workspace.Repository{Name: "dart-basic", Path: root, RealPath: root}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	payload, err := Run(ctx, "dart", "dart", repository, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(payload.Files))
	}
	var hasVehicle, hasService, hasImport, hasCall, hasExtends, hasImplements, hasEmbeds bool
	for _, symbol := range payload.Symbols {
		hasVehicle = hasVehicle || symbol.Name == "Vehicle"
		hasService = hasService || symbol.Name == "Service"
	}
	for _, importFact := range payload.Imports {
		hasImport = hasImport || importFact.RequestedPackage == "models.dart"
	}
	for _, reference := range payload.References {
		hasCall = hasCall || reference.Kind == "CALLS_DIRECT"
		hasExtends = hasExtends || reference.Kind == "EXTENDS"
		hasImplements = hasImplements || reference.Kind == "IMPLEMENTS"
		hasEmbeds = hasEmbeds || reference.Kind == "EMBEDS"
	}
	if !hasVehicle || !hasService || !hasImport || !hasCall || !hasExtends || !hasImplements || !hasEmbeds {
		t.Fatalf("fixture facts: vehicle=%v service=%v import=%v call=%v extends=%v implements=%v embeds=%v references=%#v", hasVehicle, hasService, hasImport, hasCall, hasExtends, hasImplements, hasEmbeds, payload.References)
	}
	set, err := facts.NormalizeSemantic(context.Background(), repository, payload)
	if err != nil {
		t.Fatal(err)
	}
	hasImportEdge := false
	for _, edge := range set.Edges {
		if edge.Kind == facts.ImportsSymbol {
			hasImportEdge = true
		}
		if edge.Kind == facts.CallsDirect || edge.Kind == facts.References {
			if edge.Confidence != facts.ExactTypechecked {
				t.Fatalf("Dart edge confidence = %q, want EXACT_TYPECHECKED", edge.Confidence)
			}
		}
	}
	if !hasImportEdge {
		t.Fatal("Dart local import did not produce an IMPORTS_SYMBOL edge")
	}
}

func TestRunFixtureCapturesDartPartsAndExports(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "dart", "advanced"))
	if err != nil {
		t.Fatal(err)
	}
	repository := workspace.Repository{Name: "dart-advanced", Path: root, RealPath: root}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	payload, err := Run(ctx, "dart", "dart", repository, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Parts) != 2 {
		t.Fatalf("parts = %#v, want the library declaration and its part-of declaration", payload.Parts)
	}
	var hasExport bool
	var hasConditionalImport bool
	for _, importFact := range payload.Imports {
		if importFact.Kind == string(facts.Reexports) && importFact.RequestedSymbol == "models.dart" {
			hasExport = true
		}
		if importFact.Prefix == "models" && len(importFact.Alternatives) == 1 && importFact.Deferred == false {
			hasConditionalImport = true
		}
	}
	if !hasExport {
		t.Fatalf("exports = %#v", payload.Imports)
	}
	if !hasConditionalImport {
		t.Fatalf("conditional imports = %#v", payload.Imports)
	}
	wantedSymbols := map[string]bool{"VehicleKind": false, "Success": false, "Mapper": false, "UserId": false, "describe": false}
	for _, symbol := range payload.Symbols {
		if _, wanted := wantedSymbols[symbol.Name]; wanted {
			wantedSymbols[symbol.Name] = true
		}
	}
	for name, found := range wantedSymbols {
		if !found {
			t.Fatalf("Dart advanced symbol %q missing from %#v", name, payload.Symbols)
		}
	}
	set, err := facts.NormalizeSemantic(context.Background(), repository, payload)
	if err != nil {
		t.Fatal(err)
	}
	var hasPartEdge bool
	for _, edge := range set.Edges {
		if edge.Kind == facts.PartOf {
			hasPartEdge = true
			break
		}
	}
	if !hasPartEdge {
		t.Fatalf("normalized part edges = %#v", set.Edges)
	}
}

func TestDartPackageRootsReadsPubPackageConfig(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".dart_tool")
	external := filepath.Join(root, "external-package")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	config := `{"configVersion": 2, "packages": [{"name": "external", "rootUri": "../external-package/"}]}`
	if err := os.WriteFile(filepath.Join(configDir, "package_config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	roots := dartPackageRoots(root, "auto", true)
	if len(roots) != 1 || roots[0] != external {
		t.Fatalf("package roots = %#v, want [%q]", roots, external)
	}
	providers := ExternalPackageRepositories(root, "auto")
	if len(providers) != 1 || providers[0].Path != external || !strings.HasPrefix(providers[0].Name, "dart-package:external:") {
		t.Fatalf("external providers = %#v", providers)
	}
}

func TestSDKRootResolvesConfiguredDart(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	root, err := SDKRoot("dart")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "lib")); err != nil {
		t.Fatalf("SDK root %q has no lib directory: %v", root, err)
	}
}
