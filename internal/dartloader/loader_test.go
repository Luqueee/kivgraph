package dartloader

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
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
	providers := ExternalPackageRepositories(root, "auto")
	if len(providers) != 1 || providers[0].Path != external || !strings.HasPrefix(providers[0].Name, "dart-package:external:") {
		t.Fatalf("external providers = %#v", providers)
	}
}

// `dart.include_external_packages` used to send the analysis server `packageRoots` as
// a list; the protocol defines a map, so every SDK refused the request and the whole
// pass failed. The server finds the packages through the package config on its own.
func TestRunResolvesAPackageDependencyWhenExternalPackagesAreIncluded(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	base := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		path = filepath.Join(base, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("provider/pubspec.yaml", "name: provider_pkg\nenvironment:\n  sdk: ^3.0.0\n")
	write("provider/lib/provider.dart", "int addOne(int value) => value + 1;\n")
	write("consumer/pubspec.yaml", "name: consumer_pkg\nenvironment:\n  sdk: ^3.0.0\n")
	write("consumer/lib/consumer.dart", "import 'package:provider_pkg/provider.dart';\n\nint run() => addOne(1);\n")
	write("consumer/.dart_tool/package_config.json", `{"configVersion": 2, "packages": [
		{"name": "provider_pkg", "rootUri": "../../provider", "packageUri": "lib/"},
		{"name": "consumer_pkg", "rootUri": "../", "packageUri": "lib/"}
	]}`)
	root := filepath.Join(base, "consumer")
	repository := workspace.Repository{Name: "consumer", Path: root, RealPath: root}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, includeExternal := range []bool{false, true} {
		payload, err := RunWithOptions(ctx, Options{
			Command: "dart", SDKPath: "dart", Repository: repository, IncludeExternal: includeExternal,
			Providers: ExternalPackageRepositories(root, "auto"),
		})
		if err != nil {
			t.Fatalf("include_external_packages=%v: %v", includeExternal, err)
		}
		var calls bool
		for _, reference := range payload.References {
			calls = calls || (reference.Kind == "CALLS_DIRECT" && reference.Target != nil)
		}
		if !calls {
			t.Fatalf("include_external_packages=%v: the call into the package has no target: %#v", includeExternal, payload.References)
		}
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

// The roots of the packages a repository reads besides itself, from its package
// config: what the analysis server can open, so what a repository can depend on.
func TestPackageRootsListsTheOtherPackagesThatExist(t *testing.T) {
	base := t.TempDir()
	for _, directory := range []string{"consumer", "provider", "other"} {
		if err := os.MkdirAll(filepath.Join(base, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	consumer := filepath.Join(base, "consumer")
	writeConfig := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Itself, one that exists twice, one that does not exist and one by file URI.
	automatic := `{"configVersion": 2, "packages": [
		{"name": "consumer", "rootUri": "../"},
		{"name": "provider", "rootUri": "../../provider"},
		{"name": "provider_again", "rootUri": "../../provider/"},
		{"name": "missing", "rootUri": "../../missing"},
		{"name": "other", "rootUri": "` + (&url.URL{Scheme: "file", Path: filepath.Join(base, "other")}).String() + `"}
	]}`
	writeConfig(filepath.Join(consumer, ".dart_tool", "package_config.json"), automatic)

	want := []string{filepath.Join(base, "other"), filepath.Join(base, "provider")}
	for _, configured := range []string{"", "auto"} {
		if got := PackageRoots(consumer, configured); !slices.Equal(got, want) {
			t.Errorf("PackageRoots(configured=%q) = %q, want %q", configured, got, want)
		}
	}

	// A configured path is relative to the repository, and one that is not a package
	// config names no packages.
	writeConfig(filepath.Join(consumer, "custom", "config.json"), `{"packages": [{"name": "provider", "rootUri": "../../provider"}]}`)
	if got, want := PackageRoots(consumer, filepath.Join("custom", "config.json")), []string{filepath.Join(base, "provider")}; !slices.Equal(got, want) {
		t.Errorf("PackageRoots(custom config) = %q, want %q", got, want)
	}
	writeConfig(filepath.Join(consumer, "broken.json"), "not json")
	for name, configured := range map[string]string{"malformed": "broken.json", "absent": "nothing.json"} {
		if got := PackageRoots(consumer, configured); len(got) != 0 {
			t.Errorf("PackageRoots(%s config) = %q, want none", name, got)
		}
	}
}

func TestUTF16LengthCountsCodeUnitsAndNotBytes(t *testing.T) {
	for _, testCase := range []struct {
		name string
		data string
		want int
	}{
		{name: "empty", data: "", want: 0},
		{name: "ascii", data: "abc", want: 3},
		{name: "two byte rune is one unit", data: "á—", want: 2},
		{name: "astral rune is a surrogate pair", data: "a😀", want: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := utf16Length([]byte(testCase.data)); got != testCase.want {
				t.Fatalf("utf16Length(%q) = %d, want %d", testCase.data, got, testCase.want)
			}
		})
	}
}

// TestRunResolvesUsesInAFileWithNonASCIIText pins the request the Analysis
// Server answers with nothing when its length runs past the end of the file:
// the length is in UTF-16 code units, and a comment with an accent made the byte
// count larger than that, so every use in the file went missing without an
// error.
func TestRunResolvesUsesInAFileWithNonASCIIText(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	root := t.TempDir()
	for name, content := range map[string]string{
		"pubspec.yaml":     "name: accents\npublish_to: none\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\n",
		"lib/accents.dart": "// Una nota con acentos: áéíóú — y un emoji 😀\nconst int _limit = 3;\nint doubled(int value) => value * _limit;\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repository := workspace.Repository{Name: "accents", Path: root, RealPath: root}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	payload, err := Run(ctx, "dart", "dart", repository, true, false)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, symbol := range payload.Symbols {
		names[symbol.ID] = symbol.Name
	}
	var found []string
	for _, reference := range payload.References {
		found = append(found, names[reference.SourceID]+" -> "+names[reference.TargetID])
	}
	if !slices.Contains(found, "doubled -> _limit") {
		t.Fatalf("references = %v, want doubled -> _limit", found)
	}
}

// TestRunAttributesAnInstanceToItsClass pins that building an object is a use
// of its class. A class with an explicit constructor gets the call resolved to
// the constructor, which left the class itself with no reference from the file
// that instantiates it.
func TestRunAttributesAnInstanceToItsClass(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	root := t.TempDir()
	for name, content := range map[string]string{
		"pubspec.yaml":   "name: boxes\npublish_to: none\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\n",
		"lib/box.dart":   "class Box {\n  Box(this.side);\n  Box.square(int side) : this(side);\n  final int side;\n}\n\nclass Plain {}\n",
		"lib/build.dart": "import 'box.dart';\n\nBox makeBox() => Box(2);\nBox makeSquare() => Box.square(3);\nPlain makePlain() => Plain();\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repository := workspace.Repository{Name: "boxes", Path: root, RealPath: root}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	payload, err := Run(ctx, "dart", "dart", repository, true, false)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, symbol := range payload.Symbols {
		names[symbol.ID] = symbol.QualifiedName + " " + symbol.Kind
	}
	found := map[string]int{}
	for _, reference := range payload.References {
		if reference.Kind == "CALLS_DIRECT" {
			found[names[reference.SourceID]+" -> "+names[reference.TargetID]]++
		}
	}
	// The constructor edges stay, the class gains its own, an implicit
	// constructor is still one edge and not two, and a constructor does not
	// make its own class a callee of itself.
	for edge, want := range map[string]int{
		"makeBox function -> Box.Box constructor":           1,
		"makeBox function -> Box class":                     1,
		"makeSquare function -> Box class":                  1,
		"makePlain function -> Plain class":                 1,
		"Box.Box.square constructor -> Box.Box constructor": 1,
		"Box.Box.square constructor -> Box class":           0,
	} {
		if found[edge] != want {
			t.Errorf("%q = %d, want %d; calls = %v", edge, found[edge], want, found)
		}
	}
}

// TestRunLeavesDocCommentLinksOutOfTheGraph pins that a `[name]` in a comment is
// not a use of the declaration. The Analysis Server navigates doc links like
// code, and one written "compatible with [hash]" was published as a module
// EMBEDS the function, because the keyword scan read the sentence.
func TestRunLeavesDocCommentLinksOutOfTheGraph(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	root := t.TempDir()
	for name, content := range map[string]string{
		"pubspec.yaml":  "name: docs\npublish_to: none\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\n",
		"lib/docs.dart": "int hash(String value) => value.length;\n\n/// A length that is compatible with [hash] and extends [hash].\nint other(String value) => value.length;\n\nint real(String value) => hash(value); // see [hash]\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repository := workspace.Repository{Name: "docs", Path: root, RealPath: root}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	payload, err := Run(ctx, "dart", "dart", repository, true, false)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, symbol := range payload.Symbols {
		names[symbol.ID] = symbol.QualifiedName
	}
	var found []string
	for _, reference := range payload.References {
		found = append(found, names[reference.SourceID]+" -"+reference.Kind+"-> "+names[reference.TargetID])
	}
	if want := []string{"real -CALLS_DIRECT-> hash"}; !slices.Equal(found, want) {
		t.Fatalf("references = %v, want %v", found, want)
	}
}

// `exclusions` is a key of the repository, and the Dart loader read it with a
// matcher of its own: a bare name at any depth, and `./x` or an absolute path
// not at all. One list excluded different directories for Dart than for the Go
// and TypeScript files beside them.
func TestDartFilesReadExclusionsAsTheRegistryDoes(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"lib/main.dart", "gen/a.dart", "lib/gen/b.dart", "lib/x/gen/c.dart", "tool/d.dart"} {
		path := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("class Fixture {}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	all := []string{"gen/a.dart", "lib/gen/b.dart", "lib/main.dart", "lib/x/gen/c.dart", "tool/d.dart"}
	for name, test := range map[string]struct {
		exclusions []string
		want       []string
	}{
		"none":                    {nil, all},
		"a bare name is the root": {[]string{"gen"}, []string{"lib/gen/b.dart", "lib/main.dart", "lib/x/gen/c.dart", "tool/d.dart"}},
		"any depth is spelled":    {[]string{"**/gen"}, []string{"lib/main.dart", "tool/d.dart"}},
		"relative to the root":    {[]string{"./gen", "./tool"}, []string{"lib/gen/b.dart", "lib/main.dart", "lib/x/gen/c.dart"}},
		"absolute":                {[]string{filepath.Join(root, "lib", "gen")}, []string{"gen/a.dart", "lib/main.dart", "lib/x/gen/c.dart", "tool/d.dart"}},
		"through a wildcard":      {[]string{"lib/*/gen"}, []string{"gen/a.dart", "lib/gen/b.dart", "lib/main.dart", "tool/d.dart"}},
	} {
		t.Run(name, func(t *testing.T) {
			files, err := dartFilesForRepository(root, nil, test.exclusions, true, true)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, file := range files {
				got = append(got, relative(root, file))
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("files = %q, want %q", got, test.want)
			}
		})
	}
	if _, err := dartFilesForRepository(root, nil, []string{"[unterminated"}, true, true); err == nil {
		t.Error("a pattern the registry refuses was read as matching nothing")
	}
}

func TestExtensionOverrideFindsTheNameBeforeTheReceiver(t *testing.T) {
	for source, want := range map[string]string{
		"return IterableExtension(this).isSorted(compare);": "IterableExtension",
		"IterableExtension<T>(this).isSorted(compare)":      "IterableExtension",
		"Ext<Map<K, V>> (a.b(c), (d))\n    .isSorted()":     "Ext",
		"p.$Ext(x).isSorted":                                "$Ext",
		// Not an override: a member of a value, of a call's result with no
		// name, of nothing.
		"values.isSorted(compare)": "",
		"(a + b).isSorted":         "",
		"isSorted(compare)":        "",
		"x) .isSorted":             "",
		"Ext<T(x).isSorted":        "Ext<T"[len("Ext<"):],
		"> (x).isSorted":           "",
	} {
		data := []byte(source)
		start, end, found := extensionOverride(data, strings.Index(source, "isSorted"))
		got := ""
		if found {
			got = source[start:end]
		}
		if got != want {
			t.Errorf("extensionOverride(%q) = %q, want %q", source, got, want)
		}
	}
	if _, _, found := extensionOverride(nil, 5); found {
		t.Error("an offset beyond the file named an extension")
	}
}

// The one place code names an extension is `Extension(receiver).member`, and
// the Analysis Server navigates the member and not the name: an answer about
// the extension was COMPLETE and missed the file.
func TestRunRecordsAnExplicitExtensionApplicationAsAUseOfTheExtension(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	root := t.TempDir()
	for name, content := range map[string]string{
		"pubspec.yaml":  "name: sorted\npublish_to: none\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\n",
		"lib/any.dart":  "extension AnyList<T> on List<T> {\n  bool isSorted(int Function(T, T) compare) => true;\n}\n\nextension Other<T> on List<T> {\n  bool isOther() => false;\n}\n",
		"lib/uses.dart": "import 'any.dart';\n\nbool applied(List<int> values) => AnyList(values).isSorted((a, b) => a - b);\nbool implicit(List<int> values) => values.isSorted((a, b) => a - b);\nbool Other(List<int> values) => values.isOther();\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repository := workspace.Repository{Name: "sorted", Path: root, RealPath: root}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	payload, err := Run(ctx, "dart", "dart", repository, true, false)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, symbol := range payload.Symbols {
		names[symbol.ID] = symbol.QualifiedName + " " + symbol.Kind
	}
	found := map[string]int{}
	for _, reference := range payload.References {
		if strings.HasSuffix(names[reference.TargetID], " extension") {
			found[names[reference.SourceID]+" -> "+names[reference.TargetID]+" "+reference.Text[reference.StartColumn:reference.EndColumn]]++
		}
	}
	// The explicit application names the extension; a member reached through a
	// value names nothing.
	if want := map[string]int{"applied function -> AnyList extension AnyList": 1}; !reflect.DeepEqual(found, want) {
		t.Errorf("uses of an extension = %v, want %v", found, want)
	}
}

func TestDartLibraryOfNamesADependencyTheWayAnImportDoes(t *testing.T) {
	for path, want := range map[string]string{
		"/opt/flutter/bin/cache/dart-sdk/lib/core/string.dart":              "dart:core",
		"/opt/flutter/bin/cache/dart-sdk/lib/async/future.dart":             "dart:async",
		"/opt/dart-sdk/lib/html.dart":                                       "dart:html",
		"/home/u/.pub-cache/hosted/pub.dev/collection-1.19.0/lib/a.dart":    "package:collection",
		"/home/u/.pub-cache/hosted/pub.dev/go_router-14.2.0/lib/src/x.dart": "package:go_router",
		"/opt/flutter/packages/flutter/lib/src/widgets/framework.dart":      "package:flutter",
		// A package whose own name ends in a word and not a version.
		"/work/my-app/lib/main.dart": "package:my-app",
		// The innermost `lib` is the package's.
		"/work/lib/vendored/thing/lib/thing.dart": "package:thing",
		// Not a library of a package: nothing to name.
		"/work/tool/build.dart": "",
		"/lib/x.dart":           "",
		"/work/pkg/lib/":        "",
		"":                      "",
	} {
		got, named := dartLibraryOf(path)
		if got != want || named != (want != "") {
			t.Errorf("dartLibraryOf(%q) = %q, %t, want %q", path, got, named, want)
		}
	}
}

// An SDK is not always in a directory called `dart-sdk`: a hosted runner
// unpacks it as `<version>/x64`. It is told by what it holds, and a package
// that happens to have a `lib/core` is still a package.
func TestDartLibraryOfNamesAnSDKByWhatItHolds(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"x64/lib/core/core.dart", "x64/lib/core/string.dart",
		"pkg/lib/core/core.dart", "pkg/pubspec.yaml",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{
		"x64/lib/core/string.dart":  "dart:core",
		"x64/lib/async/future.dart": "dart:async",
		"pkg/lib/core/core.dart":    "package:pkg",
	} {
		if got, _ := dartLibraryOf(filepath.Join(root, filepath.FromSlash(name))); got != want {
			t.Errorf("dartLibraryOf(%s) = %q, want %q", name, got, want)
		}
	}
}

// A dependency nobody registered is declared once for each declaration that
// uses it and each kind of thing used, not once for each use, and by the name
// an import gives it and not by a path of this machine.
func TestRunDeclaresAnUnregisteredLibraryOncePerDeclaration(t *testing.T) {
	if _, err := exec.LookPath("dart"); err != nil {
		t.Skip("dart SDK is not installed")
	}
	root := t.TempDir()
	for name, content := range map[string]string{
		"pubspec.yaml":   "name: words\npublish_to: none\nenvironment:\n  sdk: \">=3.0.0 <4.0.0\"\n",
		"lib/words.dart": "String join(String a, String b, String c) {\n  String all = a + b;\n  return all + c;\n}\n\nString other(String a) => a;\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	payload, err := Run(ctx, "dart", "dart", workspace.Repository{Name: "words", Path: root, RealPath: root}, true, false)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, symbol := range payload.Symbols {
		names[symbol.ID] = symbol.QualifiedName
	}
	classes := map[string]int{}
	for _, unresolved := range payload.Unresolved {
		// `a`, `b`, `c` and `all` are used in the function that declares them:
		// that is no failure.
		if unresolved.RequestedSymbol == "PARAMETER" || unresolved.RequestedSymbol == "LOCAL_VARIABLE" {
			t.Errorf("a use of a name scoped to its declaration was recorded as a failure: %#v", unresolved)
		}
		if unresolved.Reason != "DART_TARGET_NOT_INDEXED" || unresolved.RequestedSymbol != "CLASS" {
			continue
		}
		if strings.Contains(unresolved.RequestedPackage, string(filepath.Separator)) {
			t.Errorf("a failure names a path of this machine: %q", unresolved.RequestedPackage)
		}
		classes[names[unresolved.SourceID]+" "+unresolved.RequestedPackage]++
	}
	// `join` writes `String` five times and `other` twice.
	if want := map[string]int{"join dart:core": 1, "other dart:core": 1}; !reflect.DeepEqual(classes, want) {
		t.Errorf("uses of a class of an unregistered library = %v, want %v", classes, want)
	}
}
