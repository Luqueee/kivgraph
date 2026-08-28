package scip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// recordedIndex is a real scip-java index of testdata/java/basic, checked in so
// these tests need no JDK. TestRecordedIndexMatchesTheToolchain re-derives it
// where the toolchain exists, which is what keeps it from going stale.
const recordedIndex = "../../testdata/java/index/basic.scip"

const fixtureRoot = "../../testdata/java/basic"

func loadRecorded(t *testing.T) scipwire.Index {
	t.Helper()
	data, err := os.ReadFile(recordedIndex)
	if err != nil {
		t.Fatalf("read recorded index: %v", err)
	}
	index, err := scipwire.Decode(data)
	if err != nil {
		t.Fatalf("decode recorded index: %v", err)
	}
	return index
}

func convertRecorded(t *testing.T) facts.SemanticPayload {
	t.Helper()
	payload, err := Convert(loadRecorded(t), Options{
		Language:      facts.LanguageJava,
		Repository:    "basic",
		Package:       "basic",
		PackageRoot:   fixtureRoot,
		Authoritative: true,
		ReadFile: func(relative string) ([]byte, error) {
			return os.ReadFile(filepath.Join(fixtureRoot, filepath.FromSlash(relative)))
		},
		IncludeFile: func(relative string) bool {
			return strings.HasSuffix(relative, ".java")
		},
	})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return payload
}

func TestConvertProducesAPayloadTheNormalizerAccepts(t *testing.T) {
	payload := convertRecorded(t)
	if payload.Language != facts.LanguageJava {
		t.Fatalf("language = %q, want java", payload.Language)
	}
	if len(payload.Files) != 2 {
		t.Fatalf("files = %d, want the two sources of the fixture", len(payload.Files))
	}
	// The whole point of the bridge: what it emits has to survive the shared
	// normaliser, which is where the graph model actually lives.
	set, err := facts.NormalizeSemantic(t.Context(), repositoryFixture(), payload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

// TestEveryEdgeCarriesAJavaProvenance is the defect this whole exercise exists
// to prevent: a language wired everywhere except the provenance table publishes
// a correct-looking graph stamped with another language's name.
func TestEveryEdgeCarriesAJavaProvenance(t *testing.T) {
	set, err := facts.NormalizeSemantic(t.Context(), repositoryFixture(), convertRecorded(t))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	seen := map[facts.Provenance]int{}
	for _, edge := range set.Edges {
		seen[edge.Provenance]++
	}
	for provenance := range seen {
		switch provenance {
		case facts.JavaScipDefinition, facts.JavaScipUse, facts.PackageManifest:
		default:
			t.Errorf("a Java edge carries provenance %q", provenance)
		}
	}
	if seen[facts.JavaScipDefinition] == 0 {
		t.Error("no definition carries JAVA_SCIP_DEF")
	}
	if seen[facts.JavaScipUse] == 0 {
		t.Error("no reference carries JAVA_SCIP_USE")
	}
}

func TestSymbolsSpanTheWholeDeclaration(t *testing.T) {
	payload := convertRecorded(t)
	service, ok := findSymbol(payload, "com.example.basic.Service")
	if !ok {
		t.Fatal("the Service class is not in the payload")
	}
	// The enclosing range, not the selection range. A symbol whose span is its
	// own name cannot contain a reference, so nothing would be attributed to
	// it and every use would be sourced at the file's module symbol.
	if service.EndLine <= service.StartLine {
		t.Fatalf("Service spans %d..%d, want the whole class body", service.StartLine, service.EndLine)
	}
	if service.End <= service.Start {
		t.Fatalf("Service byte range is %d..%d", service.Start, service.End)
	}
	if service.Kind != "class" {
		t.Errorf("Service kind = %q, want class", service.Kind)
	}
}

func TestReferencesAreAttributedToTheInnermostDeclaration(t *testing.T) {
	payload := convertRecorded(t)
	greet, ok := findSymbol(payload, "com.example.basic.Service.greet")
	if !ok {
		t.Fatal("Service.greet is not in the payload")
	}
	// `model.name()` is inside greet(), which is inside Service. The method
	// has to win: an edge sourced at the class would say the class calls
	// name(), which is true of the file and useless as an answer.
	found := false
	for _, reference := range payload.References {
		if strings.HasSuffix(reference.TargetID, "Models#name().") && reference.SourceID == greet.ID {
			found = true
		}
	}
	if !found {
		t.Error("the call to Models.name() is not sourced at Service.greet")
	}
}

// TestUnspecifiedEncodingCountsCodeUnits pins the measurement the offset table
// is built on. scip-java reports no position encoding, and its columns are
// UTF-16 code units, not UTF-8 bytes. The fixture puts `á` before a symbol on
// the same line so the two readings differ by one.
func TestUnspecifiedEncodingCountsCodeUnits(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(fixtureRoot,
		"src/main/java/com/example/basic/Service.java"))
	if err != nil {
		t.Fatal(err)
	}
	payload := convertRecorded(t)
	for _, reference := range payload.References {
		if !strings.HasSuffix(reference.TargetID, "Service#model.") || reference.StartLine != 14 {
			continue
		}
		got := string(source[reference.Start : reference.Start+len("model")])
		if got != "model" {
			t.Fatalf("byte offset %d lands on %q, want \"model\": the position encoding is being read wrong",
				reference.Start, got)
		}
		return
	}
	t.Fatal("the accented line carries no reference to Service.model")
}

func TestExternalTargetsBecomeUnresolvedRatherThanLocalSymbols(t *testing.T) {
	payload := convertRecorded(t)
	// java/lang/String is referenced and is not declared by this repository.
	// Inventing a local symbol for it would be an EXACT claim about code the
	// graph does not hold.
	for _, symbol := range payload.Symbols {
		if strings.Contains(symbol.QualifiedName, "java.lang.String") {
			t.Fatalf("the JDK's String was published as a local symbol: %+v", symbol)
		}
	}
	found := false
	for _, unresolved := range payload.Unresolved {
		if strings.Contains(unresolved.Detail, "java/lang/String#") {
			found = true
		}
	}
	if !found {
		t.Error("the reference to java.lang.String is not recorded as unresolved")
	}
}

func TestLocalsAndPackageQualifiersAreNotSymbols(t *testing.T) {
	for _, symbol := range []string{
		"local 0",
		"local 12",
		"semanticdb maven . . com/",
		"semanticdb maven . . com/example/",
	} {
		if addressable(symbol) {
			t.Errorf("%q is addressable, want it excluded", symbol)
		}
	}
	for _, symbol := range []string{
		"semanticdb maven maven/com.example/basic 1.0.0 com/example/basic/Service#",
		"semanticdb maven jdk 21 java/lang/String#",
	} {
		if !addressable(symbol) {
			t.Errorf("%q is not addressable, want it kept", symbol)
		}
	}
}

func TestQualifiedNameKeepsOverloadDisambiguators(t *testing.T) {
	for _, testCase := range []struct{ symbol, want string }{
		{"semanticdb maven p 1 com/example/Service#greet().", "com.example.Service.greet"},
		{"semanticdb maven p 1 com/example/Service#greet(+1).", "com.example.Service.greet(+1)"},
		{"semanticdb maven p 1 com/example/Service#model.", "com.example.Service.model"},
		{"semanticdb maven p 1 com/example/Models#Person#", "com.example.Models.Person"},
	} {
		identity, err := parseSymbol(testCase.symbol)
		if err != nil {
			t.Fatalf("parse %q: %v", testCase.symbol, err)
		}
		if got := identity.qualifiedName(); got != testCase.want {
			t.Errorf("qualifiedName(%q) = %q, want %q", testCase.symbol, got, testCase.want)
		}
	}
}

// TestOverloadsDoNotShareAStableKey is the identity decision of this language.
// Java scopes a declaration by its package, not by its file, so two files of
// one package never collide -- but two overloads in one class would, if the
// disambiguator were dropped from the qualified name or the signature.
func TestOverloadsDoNotShareAStableKey(t *testing.T) {
	first, err := facts.SemanticTargetKey(facts.LanguageJava, facts.SemanticTarget{
		Repository: "basic", Package: "basic", QualifiedName: "com.example.Service.greet",
		Kind: "method", Signature: "public String greet()",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := facts.SemanticTargetKey(facts.LanguageJava, facts.SemanticTarget{
		Repository: "basic", Package: "basic", QualifiedName: "com.example.Service.greet(+1)",
		Kind: "method", Signature: "public String greet(String name)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two overloads derived one stable key")
	}
}

func TestSignatureNeverCarriesANewline(t *testing.T) {
	payload := convertRecorded(t)
	// The signature is the stable key's discriminator. scip-java renders an
	// annotation on its own line -- `@Override\npublic String name()` -- and a
	// key that carried it would depend on the producer's formatting.
	for _, symbol := range payload.Symbols {
		if strings.ContainsAny(symbol.Signature, "\n\r") {
			t.Errorf("symbol %q has a multi-line signature %q", symbol.QualifiedName, symbol.Signature)
		}
		if strings.TrimSpace(symbol.Signature) == "" {
			t.Errorf("symbol %q has no signature, so its key has no discriminator", symbol.QualifiedName)
		}
	}
}

func TestConvertRefusesAPayloadItCannotStamp(t *testing.T) {
	if _, err := Convert(scipwire.Index{}, Options{Repository: "basic"}); err == nil {
		t.Fatal("a conversion with no language was accepted")
	}
	if _, err := Convert(scipwire.Index{}, Options{Language: facts.LanguageJava}); err == nil {
		t.Fatal("a conversion with no repository was accepted")
	}
}

func repositoryFixture() workspace.Repository {
	absolute, err := filepath.Abs(fixtureRoot)
	if err != nil {
		absolute = fixtureRoot
	}
	return workspace.Repository{
		Name: "basic", Path: absolute, RealPath: absolute, Languages: []string{"java"},
	}
}

func findSymbol(payload facts.SemanticPayload, qualified string) (facts.SemanticSymbol, bool) {
	for _, symbol := range payload.Symbols {
		if symbol.QualifiedName == qualified {
			return symbol, true
		}
	}
	return facts.SemanticSymbol{}, false
}
