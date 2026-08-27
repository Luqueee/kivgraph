package dartloader

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		// An arrow body carries an `=` that is not an assignment: reading a
		// getter through `=>` was published as ASSIGNS_FUNCTION.
		{name: "arrow body reads a getter", source: "String asText() => value.toString();", target: "value", targetKind: "GETTER", want: "REFERENCES"},
		{name: "arrow body returns a function", source: "Runner build() => handler;", target: "handler", targetKind: "FUNCTION", want: "RETURNS_FUNCTION"},
		// The Analysis Server answers UNKNOWN for an enum, a mixin and an
		// extension type used as a type, so the position has to classify it.
		{name: "unknown kind annotating a parameter", source: "String describe(VehicleKind kind) {", target: "VehicleKind", targetKind: "UNKNOWN", want: "TYPE_USES"},
		{name: "unknown kind with generic arguments", source: "Result<int> value;", target: "Result", targetKind: "UNKNOWN", want: "TYPE_USES"},
		{name: "unknown kind reading a member", source: "final name = kind.name;", target: "kind", targetKind: "UNKNOWN", want: "REFERENCES"},
		// An argument list is opened by a callee, and a control-flow
		// parenthesis is not one. `if (other == handler)` has a `(` in the
		// prefix and a `)` in the suffix, which is the shape of an argument
		// and was classified as one.
		{name: "comparison inside an if", source: "if (other == handler) {", target: "handler", targetKind: "FUNCTION", want: "REFERENCES"},
		{name: "comparison inside a while", source: "while (queue == handler) {", target: "handler", targetKind: "FUNCTION", want: "REFERENCES"},
		// The same shape where the parenthesis *is* a call: the occurrence is
		// still an operand of the comparison and not the argument.
		{name: "comparison inside an argument list", source: "register(other == handler);", target: "handler", targetKind: "FUNCTION", want: "REFERENCES"},
		// A grouping parenthesis opened by nothing at all.
		{name: "comparison inside a grouping parenthesis", source: "final same = (other == handler);", target: "handler", targetKind: "FUNCTION", want: "REFERENCES"},
		// A control-flow parenthesis with no comparison in it: the subject of a
		// `switch` is not an argument either, and no comparison operator is
		// there to give it away.
		{name: "control flow subject", source: "switch (handler) {", target: "handler", targetKind: "FUNCTION", want: "REFERENCES"},
		// The positive case has to keep working, including when the callee is
		// reached through a member access.
		{name: "callback through a member access", source: "registry.add(handler);", target: "handler", targetKind: "FUNCTION", want: "PASSES_AS_CALLBACK"},
		{name: "callback in a trailing argument", source: "run(first, handler);", target: "handler", targetKind: "FUNCTION", want: "PASSES_AS_CALLBACK"},
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

// dartFixturePath renders a fixture path in the running platform's shape,
// volume included. Without the volume these are rooted paths that name no
// drive, which Windows resolves against the current one and filepath.IsAbs
// reports as relative -- so the fixture would be exercising the case the
// function now refuses rather than the case each assertion names.
func dartFixturePath(posix string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(`C:\`, filepath.FromSlash(strings.TrimPrefix(posix, "/")))
	}
	return filepath.FromSlash(posix)
}

func TestDartAnalyzerOutlinesIgnoreExternalConstructorInvocations(t *testing.T) {
	root := dartFixturePath("/workspace/app")
	external := dartFixturePath("/sdk/flutter/widgets.dart")
	internal := dartFixturePath("/workspace/app/lib/widgets.dart")
	if !isDartAnalyzerConstructorInvocation(analyzerOutline{Element: analyzerElement{Kind: "CONSTRUCTOR", Location: &analyzerLocation{File: external}}}, root) {
		t.Fatal("external constructor invocation should not become a declaration")
	}
	if !isDartAnalyzerConstructorInvocation(analyzerOutline{Element: analyzerElement{Kind: "CONSTRUCTOR_INVOCATION", Location: &analyzerLocation{File: internal}}}, root) {
		t.Fatal("constructor invocation should not become a declaration")
	}
	if isDartAnalyzerConstructorInvocation(analyzerOutline{Element: analyzerElement{Kind: "CONSTRUCTOR", Location: &analyzerLocation{File: internal}}}, root) {
		t.Fatal("constructor declaration in the repository should remain available")
	}
	// A rooted path with no volume is the shape the fix above refuses: on
	// Windows it names the current drive rather than anything under the
	// repository, so it cannot be a declaration this repository owns.
	if runtime.GOOS == "windows" {
		if !isDartAnalyzerConstructorInvocation(analyzerOutline{Element: analyzerElement{Kind: "CONSTRUCTOR", Location: &analyzerLocation{File: `\sdk\flutter\widgets.dart`}}}, root) {
			t.Fatal("a path rooted on the current drive was placed inside the repository")
		}
	}
	if isDartAnalyzerConstructorInvocation(analyzerOutline{Element: analyzerElement{Kind: "METHOD"}}, root) {
		t.Fatal("methods are declarations")
	}
}

// A declaration observed by the analyzer outline without an element location
// has to key on its identifier, the way the LSP outline does: keying on the
// start of the declaration published `Vehicle` twice, once per source.
func TestDeclarationNameOffsetFindsTheIdentifierInsideItsOwnSpan(t *testing.T) {
	data := []byte("class Vehicle {\n  String drive() => 'ready';\n}\n")
	if got := declarationNameOffset(data, 0, len(data), "Vehicle"); got != strings.Index(string(data), "Vehicle") {
		t.Fatalf("declarationNameOffset() = %d, want %d", got, strings.Index(string(data), "Vehicle"))
	}
	// A whole-word match, so a name that only occurs as a substring is absent.
	if got := declarationNameOffset([]byte("class PartValue {}"), 0, 18, "Value"); got != -1 {
		t.Fatalf("declarationNameOffset() = %d, want -1 for a substring match", got)
	}
	if got := declarationNameOffset(data, 0, len(data), "Missing"); got != -1 {
		t.Fatalf("declarationNameOffset() = %d, want -1 when the name is absent", got)
	}
}

// The Dart LSP reports an extension type as a Namespace. Publishing it as a
// module let it compete with its own file for the file's module identity, and a
// `part` directive then pointed at the declaration instead of the library.
func TestDartKindKeepsNamespaceOutOfTheModuleIdentity(t *testing.T) {
	if got := dartKind(3); got == "module" {
		t.Fatalf("dartKind(3) = %q, which competes with the file's module identity", got)
	}
	if got := dartKind(2); got != "module" {
		t.Fatalf("dartKind(2) = %q, want module", got)
	}
}

// Neither declaration source lists the representation field of an extension
// type, so every use of it pointed at a target outside the graph. A plain
// extension has no representation, and must not gain one.
func TestExtensionTypeHeaderNamesOnlyARepresentationField(t *testing.T) {
	cases := []struct {
		name, header, want string
	}{
		{name: "representation", header: "extension type UserId(int value) ", want: "value"},
		{name: "const representation", header: "extension type const Meters(double amount) ", want: "amount"},
		{name: "generic representation", header: "extension type Wrapper<T>(List<T> items) ", want: "items"},
		{name: "named constructor", header: "extension type UserId.from(int raw) ", want: "raw"},
		{name: "plain extension", header: "extension Helpers on String ", want: ""},
		{name: "class declaration", header: "class Vehicle ", want: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			match := extensionTypeHeader.FindStringSubmatch(testCase.header)
			got := ""
			if match != nil {
				got = match[1]
			}
			if got != testCase.want {
				t.Fatalf("extensionTypeHeader on %q named %q, want %q", testCase.header, got, testCase.want)
			}
		})
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

// A Windows path starts with its volume rather than a separator, and a URI
// path that does not start with a slash makes the drive the authority:
// `file://C:/x` says host "C:" and path "/x". The analysis server rejects it
// -- "URI does not contain an absolute file path (missing drive letter)" --
// and it rejected every request for the whole fixture that way.
//
// The spelling is asserted rather than the round trip alone, because the round
// trip would agree with itself on a URI no server accepts.
func TestFileURINamesTheVolumeInThePathAndNotTheAuthority(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("a volume is a Windows path shape")
	}
	path := filepath.Join(`C:\`, "dart project", "lib", "main.dart")

	got := fileURI(path)
	if got != "file:///C:/dart%20project/lib/main.dart" {
		t.Fatalf("fileURI(%q) = %q, want the volume inside the path", path, got)
	}
	back, err := uriPath(got)
	if err != nil {
		t.Fatalf("uriPath() error = %v", err)
	}
	if back != path {
		t.Fatalf("uriPath(fileURI(%q)) = %q, want the path back", path, back)
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
