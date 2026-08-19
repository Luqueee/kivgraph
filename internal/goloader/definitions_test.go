package goloader

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

const definitionsSource = `package sample

import "fmt"

// Answer is a package constant.
const Answer = 42

// Registry is a package variable.
var Registry = map[string]int{}

// Shape is a defined type with fields.
type Shape struct {
	// Width is an exported field.
	Width int
	depth int
}

// Alias points at Shape.
type Alias = Shape

// Reader is an interface with one method.
type Reader interface {
	Read(count int) ([]byte, error)
}

// Area returns the area of the shape.
func (shape Shape) Area() int { return shape.Width * shape.depth }

// Scale mutates the shape through a pointer receiver.
func (shape *Shape) Scale(factor int) { shape.Width *= factor }

// Compute is an exported function.
func Compute(input int) int {
	local := input + Answer
	for index := 0; index < 1; index++ {
		local += index
	}
	return local
}

func unexported() { fmt.Println(Answer) }
`

func TestExtractDefinitionsCollectsAddressableSymbolsOnly(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": definitionsSource,
	})

	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v, want none", result.Errors)
	}

	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{
		Repository: "fixture",
	})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}

	byQualifiedName := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		if _, duplicate := byQualifiedName[definition.QualifiedName]; duplicate {
			t.Fatalf("duplicate definition %q", definition.QualifiedName)
		}
		byQualifiedName[definition.QualifiedName] = definition
	}

	wantKinds := map[string]DefinitionKind{
		"Answer":      KindConst,
		"Registry":    KindVar,
		"Shape":       KindType,
		"Alias":       KindAlias,
		"Reader":      KindType,
		"Shape.Width": KindField,
		"Shape.depth": KindField,
		"Shape.Area":  KindMethod,
		"Shape.Scale": KindMethod,
		"Reader.Read": KindMethod,
		"Compute":     KindFunc,
		"unexported":  KindFunc,
	}
	for name, kind := range wantKinds {
		definition, exists := byQualifiedName[name]
		if !exists {
			t.Fatalf("definition %q missing; got %v", name, QualifiedNames(definitions))
		}
		if definition.Kind != kind {
			t.Fatalf("definition %q kind = %q, want %q", name, definition.Kind, kind)
		}
	}
	if len(definitions) != len(wantKinds) {
		t.Fatalf("definitions = %v, want exactly the addressable symbols", QualifiedNames(definitions))
	}

	// Locals, parameters and loop variables are not graph symbols.
	for _, name := range []string{"local", "index", "input", "factor", "count", "shape"} {
		if _, exists := byQualifiedName[name]; exists {
			t.Fatalf("local %q was extracted as a definition", name)
		}
	}

	compute := byQualifiedName["Compute"]
	if compute.Repository != "fixture" || compute.ModulePath != "example.com/module" ||
		compute.PackagePath != "example.com/module/sample" || compute.PackageName != "sample" {
		t.Fatalf("Compute metadata = %#v", compute)
	}
	if compute.Signature != "func(input int) int" {
		t.Fatalf("Compute signature = %q", compute.Signature)
	}
	if !compute.Exported || compute.Owner != "" {
		t.Fatalf("Compute exported=%v owner=%q", compute.Exported, compute.Owner)
	}
	if compute.FileName != filepath.Join(module, "sample/sample.go") {
		t.Fatalf("Compute file = %q", compute.FileName)
	}
	if compute.StartLine != 33 || compute.StartColumn != 6 {
		t.Fatalf("Compute name position = %d:%d, want 33:6", compute.StartLine, compute.StartColumn)
	}
	if compute.DeclarationEndOffset <= compute.DeclarationStartOffset {
		t.Fatalf("Compute declaration span = %#v", compute)
	}
	if compute.Object() == nil || compute.Object().Name() != "Compute" {
		t.Fatalf("Compute object = %#v", compute.Object())
	}

	scale := byQualifiedName["Shape.Scale"]
	if scale.Owner != "Shape" || scale.Receiver != "*example.com/module/sample.Shape" {
		t.Fatalf("Scale owner=%q receiver=%q", scale.Owner, scale.Receiver)
	}
	read := byQualifiedName["Reader.Read"]
	if read.Owner != "Reader" || read.Kind != KindMethod {
		t.Fatalf("Reader.Read = %#v", read)
	}
	depth := byQualifiedName["Shape.depth"]
	if depth.Exported || depth.Owner != "Shape" {
		t.Fatalf("Shape.depth = %#v", depth)
	}
}

func TestExtractDefinitionsCanExcludeUnexported(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": definitionsSource,
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{
		Repository:        "fixture",
		ExcludeUnexported: true,
	})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	for _, definition := range definitions {
		if !definition.Exported {
			t.Fatalf("unexported definition %q was kept", definition.QualifiedName)
		}
	}
}

func TestExtractDefinitionsIsDeterministicAndCancellable(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": definitionsSource,
		"other/other.go":   "package other\n\n// Value is a fact.\nconst Value = 1\n",
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	first, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	second, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	if !equalStringSlices(QualifiedNames(first), QualifiedNames(second)) {
		t.Fatalf("extraction is not deterministic")
	}
	grouped := PackageDefinitions(first)
	if len(grouped["example.com/module/other"]) != 1 {
		t.Fatalf("grouped = %#v", grouped)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExtractDefinitions(ctx, result, DefinitionOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ExtractDefinitions() error = %v, want context.Canceled", err)
	}
}

// Fields of anonymous structs declared inside functions are not addressable
// from the package scope, so nothing but their syntactic container separates
// them. Two functions of one package routinely declare the same request shape;
// each field must keep its own identity and its own stable key.
func TestExtractDefinitionsQualifiesFieldsOfLocalAnonymousStructs(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod": "module example.com/module\n\ngo 1.24\n",
		"sample/first.go": `package sample

func ParseFirst() string {
	var raw struct {
		GuildID string
	}
	return raw.GuildID
}
`,
		"sample/second.go": `package sample

type Store struct{}

func (store Store) ParseSecond() string {
	var raw struct {
		GuildID string
	}
	return raw.GuildID
}
`,
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}

	fields := make(map[string]Definition)
	for _, definition := range definitions {
		if definition.Kind == KindField && definition.Name == "GuildID" {
			fields[definition.QualifiedName] = definition
		}
	}
	if len(fields) != 2 {
		names := make([]string, 0, len(fields))
		for name := range fields {
			names = append(names, name)
		}
		t.Fatalf("GuildID fields = %v, want two distinct qualified names", names)
	}
	for _, want := range []string{"ParseFirst.raw.GuildID", "Store.ParseSecond.raw.GuildID"} {
		if _, found := fields[want]; !found {
			t.Fatalf("missing qualified name %q; got %#v", want, fields)
		}
	}

	keyed, err := AssignStableKeys(context.Background(), definitions)
	if err != nil {
		t.Fatalf("AssignStableKeys() error = %v", err)
	}
	keys := make(map[hotsnapshot.StableKey]string)
	for _, definition := range keyed {
		if previous, clash := keys[definition.StableKey]; clash {
			t.Fatalf("%q and %q share a stable key", previous, definition.QualifiedName)
		}
		keys[definition.StableKey] = definition.QualifiedName
	}
}

// A nested anonymous struct inside a function is the case that failed on a real
// corpus: `var env struct{ Errors []struct{ Message string } }` has an
// intermediate field name, so the owner path looked answerable -- `Errors` --
// while being rooted at nothing. Every file of a package that unmarshals into
// that shape then declared one `Errors.Message`, and two test files were enough
// to make indexing fail at publish time on the DEFINES multiplicity.
//
// The identity has to carry what actually separates the two: the function and the
// variable.
func TestExtractDefinitionsQualifiesFieldsOfNestedLocalAnonymousStructs(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod": "module example.com/module\n\ngo 1.24\n",
		"sample/first.go": `package sample

func ParseFirst() string {
	var env struct {
		Errors []struct {
			Message string
		}
	}
	if len(env.Errors) == 0 {
		return ""
	}
	return env.Errors[0].Message
}
`,
		"sample/second.go": `package sample

func ParseSecond() string {
	var env struct {
		Errors []struct {
			Message string
		}
	}
	if len(env.Errors) == 0 {
		return ""
	}
	return env.Errors[0].Message
}
`,
	})
	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}

	names := make(map[string]struct{})
	for _, definition := range definitions {
		if definition.Kind == KindField && definition.Name == "Message" {
			names[definition.QualifiedName] = struct{}{}
		}
	}
	for _, want := range []string{"ParseFirst.env.Errors.Message", "ParseSecond.env.Errors.Message"} {
		if _, found := names[want]; !found {
			t.Fatalf("missing qualified name %q; got %v", want, sortedNames(names))
		}
	}
	if len(names) != 2 {
		t.Fatalf("Message fields = %v, want exactly the two qualified names", sortedNames(names))
	}

	keyed, err := AssignStableKeys(context.Background(), definitions)
	if err != nil {
		t.Fatalf("AssignStableKeys() error = %v", err)
	}
	keys := make(map[hotsnapshot.StableKey]string)
	for _, definition := range keyed {
		if previous, clash := keys[definition.StableKey]; clash {
			t.Fatalf("%q and %q share a stable key", previous, definition.QualifiedName)
		}
		keys[definition.StableKey] = definition.QualifiedName
	}
}

func sortedNames(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// anonymousSource declares the same method and the same field twice, each time
// on a type with no name. Nothing can name either of them, and every field the
// identity is built from -- package, qualified name, kind and signature -- is
// equal across the pair.
const anonymousSource = `package sample

import "io"

type Closer interface {
	CloseWithError(error) error
}

type Box struct {
	Payload string
}

type body struct{}

func (b *body) Read(p []byte) (int, error)      { return 0, io.EOF }
func (b *body) CloseWithError(err error) error  { return err }

var _ interface {
	io.Reader
	CloseWithError(error) error
} = (*body)(nil)

func assertBody(value any) bool {
	_, ok := value.(interface{ CloseWithError(error) error })
	return ok
}

var first = struct{ Payload string }{}
var second = struct{ Payload string }{}
`

// TestExtractDefinitionsGivesEveryDeclarationItsOwnKey is the guard for the
// DEFINES multiplicity constraint: a Symbol has exactly one declaring File, so
// two declarations that derive one key make the canonical graph unloadable.
func TestExtractDefinitionsGivesEveryDeclarationItsOwnKey(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": anonymousSource,
	})

	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	keyed, err := AssignStableKeys(context.Background(), definitions)
	if err != nil {
		t.Fatalf("AssignStableKeys() error = %v", err)
	}

	owners := make(map[string]KeyedDefinition, len(keyed))
	for _, definition := range keyed {
		key := string(definition.StableKey)
		if previous, taken := owners[key]; taken {
			t.Fatalf("%q at %s:%d and %q at %s:%d share the stable key %s",
				previous.QualifiedName, filepath.Base(previous.FileName), previous.StartLine,
				definition.QualifiedName, filepath.Base(definition.FileName), definition.StartLine,
				definition.CanonicalIdentity)
		}
		owners[key] = definition
	}

	// The named declarations stay: they are the ones a consumer can reach.
	present := make(map[string]struct{}, len(keyed))
	for _, definition := range keyed {
		present[definition.QualifiedName] = struct{}{}
	}
	for _, wanted := range []string{"Closer.CloseWithError", "Box.Payload", "body.CloseWithError"} {
		if _, found := present[wanted]; !found {
			t.Errorf("declaration %q is missing", wanted)
		}
	}
}

// inlineLiteralSource is the shape that made this repository unindexable: two
// anonymous structs marshalled inline inside one method, an embedded field that
// contributes no name, and two `Release` fields at different depths of one
// named type.
const inlineLiteralSource = `package sample

import "encoding/json"

type header struct {
	Repository string
}

type Payload struct {
	Release string
	Tools   struct {
		Release      string
		RustAnalyzer struct {
			Release string
		}
	}
}

type View struct {
	Compact bool
}

func (view View) MarshalJSON() ([]byte, error) {
	if view.Compact {
		return json.Marshal(struct {
			header
			Files []string ` + "`json:\"files\"`" + `
		}{header: header{Repository: "a"}, Files: nil})
	}
	return json.Marshal(struct {
		header
		Files []string ` + "`json:\"files\"`" + `
	}{header: header{Repository: "b"}, Files: nil})
}
`

// TestExtractDefinitionsSkipsFieldsOfInlineLiteralsAndQualifiesNestedOnes is the
// regression guard for the two identity collapses that reached LadybugDB as a
// node offset: sibling inline literals in one function, and two fields of the
// same name at different depths of one named type.
func TestExtractDefinitionsSkipsFieldsOfInlineLiteralsAndQualifiesNestedOnes(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"sample/sample.go": inlineLiteralSource,
	})

	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	definitions, err := ExtractDefinitions(context.Background(), result, DefinitionOptions{Repository: "fixture"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}

	identities := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		identity := definition.QualifiedName + "|" + string(definition.Kind)
		if previous, taken := identities[identity]; taken {
			t.Fatalf("%q at line %d and line %d share one identity",
				identity, previous.StartLine, definition.StartLine)
		}
		identities[identity] = definition
	}

	// The inline literals contribute no field: nothing names them, so nothing
	// separates the two of them either.
	for identity := range identities {
		if strings.HasSuffix(identity, "Files|field") {
			t.Fatalf("a field of an inline literal reached the graph: %q", identity)
		}
	}
	// Depth is part of the path, so the three `Release` fields are three symbols.
	for _, wanted := range []string{
		"Payload.Release|field",
		"Payload.Tools.Release|field",
		"Payload.Tools.RustAnalyzer.Release|field",
		"header.Repository|field",
		"View.MarshalJSON|method",
	} {
		if _, found := identities[wanted]; !found {
			t.Fatalf("declaration %q is missing; got %d identities", wanted, len(identities))
		}
	}
}
