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

// TestPositionsCountFromOneLikeEveryOtherProducer pins the convention. SCIP
// counts lines from zero and the graph does not: a Java or C# row used to name
// the line above its declaration, in every tool, and `get_source` hid it by
// re-anchoring an unchanged file by one line.
func TestPositionsCountFromOneLikeEveryOtherProducer(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(fixtureRoot, "src/main/java/com/example/basic/Service.java"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(source), "\n")
	payload := convertRecorded(t)

	service, ok := findSymbol(payload, "com.example.basic.Service")
	if !ok {
		t.Fatal("the Service class is not in the payload")
	}
	if service.StartLine < 1 || !strings.Contains(lines[service.StartLine-1], "class Service") {
		t.Fatalf("Service starts at line %d, which reads %q", service.StartLine, lines[max(service.StartLine-1, 0)])
	}
	if service.StartColumn < 1 {
		t.Fatalf("Service starts at column %d, want a column counted from one", service.StartColumn)
	}
	for _, symbol := range payload.Symbols {
		if symbol.Kind == facts.ModuleSymbolKind && (symbol.StartLine != 1 || symbol.EndLine < 1) {
			t.Fatalf("module %s spans %d..%d, want it to start at line 1", symbol.File, symbol.StartLine, symbol.EndLine)
		}
	}
	checked := 0
	for _, reference := range payload.References {
		if reference.File != "src/main/java/com/example/basic/Service.java" || reference.End <= reference.Start {
			continue
		}
		text := string(source[reference.Start:reference.End])
		if !strings.Contains(lines[reference.StartLine-1], text) {
			t.Fatalf("a use of %q is reported on line %d, which reads %q", text, reference.StartLine, lines[reference.StartLine-1])
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no reference in Service.java was checked")
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
		if !strings.HasSuffix(reference.TargetID, "Service#model.") || reference.StartLine != 15 {
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

// TestAPartialTypeIsOneSymbolWithOneDefiningFile pins what a C# `partial` type
// does to the index. SCIP gives every part the same symbol, so the type arrived
// once per file and the fact set was refused -- "defined by two files" -- which
// took the whole index with it. The type is published once, from the first
// path, whatever order the producer wrote the documents in; a member of another
// part keeps its own symbol; and a use inside that part is still sourced at a
// symbol the payload declares.
func TestAPartialTypeIsOneSymbolWithOneDefiningFile(t *testing.T) {
	const guard = "scip-dotnet nuget Guard 1.0.0 Guard/Extensions#"
	span := func(line int32) scipwire.Range {
		return scipwire.Range{StartLine: line, EndLine: line + 4, EndCharacter: 1, Present: true}
	}
	name := func(line int32) scipwire.Range {
		return scipwire.Range{StartLine: line, StartCharacter: 4, EndLine: line, EndCharacter: 9, Present: true}
	}
	part := func(path, method string) scipwire.Document {
		return scipwire.Document{
			RelativePath: path,
			Occurrences: []scipwire.Occurrence{
				{Symbol: guard, Roles: scipwire.RoleDefinition, Range: name(0), EnclosingRange: span(0)},
				// The same part declared twice in one file is the same defect
				// in its other spelling: "file defines symbol twice".
				{Symbol: guard, Roles: scipwire.RoleDefinition, Range: name(10), EnclosingRange: span(10)},
				{Symbol: guard + method + "().", Roles: scipwire.RoleDefinition, Range: name(1), EnclosingRange: scipwire.Range{StartLine: 1, EndLine: 3, EndCharacter: 1, Present: true}},
				{Symbol: guard, Range: name(2)},
			},
		}
	}
	// Later path first: the choice must not follow the producer's order.
	index := scipwire.Index{Documents: []scipwire.Document{part("src/Zero.cs", "Zero"), part("src/Empty.cs", "Empty")}}
	payload, err := Convert(index, Options{Repository: "guard", Language: facts.LanguageCSharp})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}

	files := map[string][]string{}
	for _, symbol := range payload.Symbols {
		files[symbol.ID] = append(files[symbol.ID], symbol.File)
	}
	if got := files[guard]; len(got) != 1 || got[0] != "src/Empty.cs" {
		t.Fatalf("the partial type is published from %v, want once from src/Empty.cs", got)
	}
	for member, file := range map[string]string{guard + "Zero().": "src/Zero.cs", guard + "Empty().": "src/Empty.cs"} {
		if got := files[member]; len(got) != 1 || got[0] != file {
			t.Fatalf("member %q is published from %v, want %s", member, got, file)
		}
	}

	set, err := facts.NormalizeSemantic(t.Context(), workspace.Repository{Name: "guard", Path: t.TempDir()}, payload)
	if err != nil {
		t.Fatalf("NormalizeSemantic() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v: a partial type must not abort the index", err)
	}
	// The use in each part is kept, and each is owned by a symbol of the file
	// it is written in: the member that contains it.
	fileOf := map[string]string{}
	for _, symbol := range set.Symbols {
		fileOf[symbol.Key] = symbol.FileKey
	}
	evidenceFile := map[string]string{}
	for _, evidence := range set.Evidence {
		evidenceFile[evidence.Key] = evidence.FileKey
	}
	references := 0
	for _, edge := range set.Edges {
		if edge.Kind != facts.References {
			continue
		}
		references++
		if fileOf[edge.SourceKey] != evidenceFile[edge.EvidenceKey] {
			t.Fatalf("a use in %s is owned by a symbol of %s", evidenceFile[edge.EvidenceKey], fileOf[edge.SourceKey])
		}
	}
	if references != 2 {
		t.Fatalf("references = %d, want the use in each part", references)
	}
}

// TestAUseInThePartThatDoesNotPublishTheTypeFallsToItsFile is the half the
// members do not cover: a use written straight in the body of a part, outside
// any member. Its type lives in another file, and a use owned from another file
// is what the snapshot refuses ("source/file mismatch").
func TestAUseInThePartThatDoesNotPublishTheTypeFallsToItsFile(t *testing.T) {
	const guard = "scip-dotnet nuget Guard 1.0.0 Guard/Extensions#"
	const other = "scip-dotnet nuget Guard 1.0.0 Guard/Other#"
	body := scipwire.Range{EndLine: 9, EndCharacter: 1, Present: true}
	at := func(line int32) scipwire.Range {
		return scipwire.Range{StartLine: line, StartCharacter: 4, EndLine: line, EndCharacter: 9, Present: true}
	}
	part := func(path string, extra ...scipwire.Occurrence) scipwire.Document {
		return scipwire.Document{RelativePath: path, Occurrences: append([]scipwire.Occurrence{
			{Symbol: guard, Roles: scipwire.RoleDefinition, Range: at(0), EnclosingRange: body},
			{Symbol: other, Range: at(3)},
			{Symbol: "scip-dotnet nuget System.Runtime 8.0.0 System/ObsoleteAttribute#", Range: at(4)},
		}, extra...)}
	}
	index := scipwire.Index{Documents: []scipwire.Document{
		part("src/A.cs", scipwire.Occurrence{Symbol: other, Roles: scipwire.RoleDefinition, Range: at(20), EnclosingRange: scipwire.Range{StartLine: 20, EndLine: 22, EndCharacter: 1, Present: true}}),
		part("src/B.cs"),
	}}
	payload, err := Convert(index, Options{Repository: "guard", Language: facts.LanguageCSharp})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	owners := map[string]string{}
	for _, reference := range payload.References {
		owners[reference.File] = reference.SourceID
	}
	if owners["src/A.cs"] != guard {
		t.Fatalf("the use in the publishing part is owned by %q, want the type", owners["src/A.cs"])
	}
	if owners["src/B.cs"] != modulePseudoSymbol("src/B.cs") {
		t.Fatalf("the use in the other part is owned by %q, want the file's own scope", owners["src/B.cs"])
	}
	for _, unresolved := range payload.Unresolved {
		if unresolved.File == "src/B.cs" && unresolved.SourceID != modulePseudoSymbol("src/B.cs") {
			t.Fatalf("an unresolved use in src/B.cs is owned by %q", unresolved.SourceID)
		}
	}
}

// TestBuildingATypeIsAUseOfTheType pins `new T(...)`. Both producers record it
// as a use of the constructor and nothing else, so "who uses T" answered
// without any of the places that build one -- measured on jsoup, where a class
// only ever thrown was reported as used by nobody outside its file.
func TestBuildingATypeIsAUseOfTheType(t *testing.T) {
	at := func(line int32) scipwire.Range {
		return scipwire.Range{StartLine: line, StartCharacter: 4, EndLine: line, EndCharacter: 9, Present: true}
	}
	for name, constructor := range map[string]string{
		"java":            "semanticdb maven m 1.0 p/Thing#`<init>`().",
		"java overload":   "semanticdb maven m 1.0 p/Thing#`<init>`(+1).",
		"csharp":          "scip-dotnet nuget . . P/Thing#`.ctor`().",
		"csharp overload": "scip-dotnet nuget . . P/Thing#`.ctor`(+2).",
	} {
		t.Run(name, func(t *testing.T) {
			thing := constructor[:strings.LastIndexByte(constructor, '#')+1]
			index := scipwire.Index{Documents: []scipwire.Document{
				{RelativePath: "src/Thing", Occurrences: []scipwire.Occurrence{
					{Symbol: thing, Roles: scipwire.RoleDefinition, Range: at(0), EnclosingRange: scipwire.Range{EndLine: 5, EndCharacter: 1, Present: true}},
					{Symbol: constructor, Roles: scipwire.RoleDefinition, Range: at(1), EnclosingRange: scipwire.Range{StartLine: 1, EndLine: 2, EndCharacter: 1, Present: true}},
				}},
				{RelativePath: "src/User", Occurrences: []scipwire.Occurrence{{Symbol: constructor, Range: at(3)}}},
			}}
			payload, err := Convert(index, Options{Repository: "r", Language: facts.LanguageJava})
			if err != nil {
				t.Fatalf("Convert() error = %v", err)
			}
			targets := map[string]int{}
			for _, reference := range payload.References {
				if reference.File == "src/User" {
					targets[reference.TargetID]++
				}
			}
			if targets[thing] != 1 || targets[constructor] != 1 {
				t.Fatalf("the instantiation reaches %v, want the type and the constructor once each", targets)
			}
		})
	}
	// A method is not a constructor, and a type nobody here declares gets no
	// edge: that would be an EXACT claim about code outside the graph.
	for _, symbol := range []string{"semanticdb maven m 1.0 p/Thing#make().", "semanticdb maven m 1.0 p/Thing#", "local 3"} {
		if owner, constructs := constructedType(symbol); constructs {
			t.Fatalf("constructedType(%q) = %q, want none", symbol, owner)
		}
	}
	external := scipwire.Index{Documents: []scipwire.Document{
		{RelativePath: "src/User", Occurrences: []scipwire.Occurrence{{Symbol: "semanticdb maven jdk 21 java/util/ArrayList#`<init>`().", Range: at(3)}}},
	}}
	payload, err := Convert(external, Options{Repository: "r", Language: facts.LanguageJava})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if len(payload.References) != 0 {
		t.Fatalf("references = %#v, want none for a type outside the repository", payload.References)
	}
}
