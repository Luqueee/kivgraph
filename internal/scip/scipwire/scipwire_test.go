package scipwire

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixturePath is an index recorded from `rust-analyzer scip` over
// testdata/rust/workspace. It is the only proof that this decoder reads what
// the tool actually writes rather than what the schema allows.
func fixturePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "testdata", "protocol", "scip-v0.9", "engine.scip")
}

func decodeFixture(t *testing.T) Index {
	t.Helper()
	data, err := os.ReadFile(fixturePath(t))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	index, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	return index
}

func documentByPath(t *testing.T, index Index, path string) Document {
	t.Helper()
	for _, document := range index.Documents {
		if document.RelativePath == path {
			return document
		}
	}
	t.Fatalf("index has no document %q", path)
	return Document{}
}

func TestDecodeReadsARecordedRustAnalyzerIndex(t *testing.T) {
	index := decodeFixture(t)

	if index.ToolName != "rust-analyzer" || index.ToolVersion == "" {
		t.Fatalf("tool = %q %q", index.ToolName, index.ToolVersion)
	}
	if index.TextEncoding != EncodingUTF8 {
		t.Fatalf("text encoding = %d, want UTF-8", index.TextEncoding)
	}
	if !strings.HasPrefix(index.ProjectRoot, "file://") {
		t.Fatalf("project root = %q", index.ProjectRoot)
	}
	if len(index.Documents) != 3 {
		t.Fatalf("documents = %d, want the three sources of the fixture", len(index.Documents))
	}

	consumer := documentByPath(t, index, "crates/engine/src/lib.rs")
	if consumer.Language != "rust" || consumer.PositionEncoding != PositionUTF8 {
		t.Fatalf("document = %#v", consumer)
	}

	var run SymbolInformation
	var local SymbolInformation
	for _, symbol := range consumer.Symbols {
		switch symbol.DisplayName {
		case "run":
			run = symbol
		case "seed":
			local = symbol
		}
	}
	if run.Symbol != "rust-analyzer cargo engine 1.4.0 run()." {
		t.Fatalf("run symbol = %#v", run)
	}
	if run.Signature != "pub fn run(seed: i32) -> Value" {
		t.Fatalf("run signature = %q", run.Signature)
	}
	// A local carries no durable identity of its own; what it does carry is
	// the declaration that contains it.
	if !strings.HasPrefix(local.Symbol, "local ") || local.EnclosingSymbol != run.Symbol {
		t.Fatalf("local symbol = %#v", local)
	}
}

// TestDecodeKeepsDefinitionBodiesAndCrossCrateUses defends the two facts every
// Rust edge is built from: which span a definition owns, and that a use of
// another crate arrives with that crate's own symbol string.
func TestDecodeKeepsDefinitionBodiesAndCrossCrateUses(t *testing.T) {
	index := decodeFixture(t)
	consumer := documentByPath(t, index, "crates/engine/src/lib.rs")

	var definition Occurrence
	var crossCrate Occurrence
	for _, occurrence := range consumer.Occurrences {
		switch {
		case occurrence.Definition() && occurrence.Symbol == "rust-analyzer cargo engine 1.4.0 run().":
			definition = occurrence
		case occurrence.Symbol == "rust-analyzer cargo support 1.4.0 double()." && !occurrence.Definition():
			crossCrate = occurrence
		}
	}
	if !definition.Range.Present || !definition.EnclosingRange.Present {
		t.Fatalf("definition occurrence = %#v, want a body range", definition)
	}
	if !definition.EnclosingRange.Contains(definition.Range) {
		t.Fatalf("definition body %#v does not contain its name %#v", definition.EnclosingRange, definition.Range)
	}
	if definition.Roles&RoleDefinition == 0 {
		t.Fatalf("definition roles = %d", definition.Roles)
	}
	if !crossCrate.Range.Present || crossCrate.Definition() {
		t.Fatalf("cross-crate occurrence = %#v", crossCrate)
	}

	// The provider's own document defines exactly that symbol: this is the
	// identity a cross-repository edge is allowed to rely on.
	provider := documentByPath(t, index, "crates/support/src/lib.rs")
	defined := false
	for _, occurrence := range provider.Occurrences {
		if occurrence.Definition() && occurrence.Symbol == crossCrate.Symbol {
			defined = true
		}
	}
	if !defined {
		t.Fatalf("provider document does not define %q", crossCrate.Symbol)
	}
}

// TestDecodeReadsBothRangeShapes covers the two encodings rust-analyzer uses:
// three numbers when a span stays on one line, four when it does not.
func TestDecodeReadsBothRangeShapes(t *testing.T) {
	index := decodeFixture(t)
	provider := documentByPath(t, index, "crates/support/src/lib.rs")

	singleLine := false
	multiLine := false
	for _, occurrence := range provider.Occurrences {
		if !occurrence.Range.Present {
			continue
		}
		if occurrence.Range.StartLine == occurrence.Range.EndLine {
			singleLine = true
		} else {
			multiLine = true
		}
	}
	if !singleLine || !multiLine {
		t.Fatalf("fixture must exercise both range shapes: single=%t multi=%t", singleLine, multiLine)
	}
}

func tag(field, wire int) []byte {
	return binary.AppendUvarint(nil, uint64(field)<<3|uint64(wire))
}

func lengthDelimited(field int, payload []byte) []byte {
	out := tag(field, 2)
	out = binary.AppendUvarint(out, uint64(len(payload)))
	return append(out, payload...)
}

func varintField(field int, value uint64) []byte {
	return binary.AppendUvarint(tag(field, 0), value)
}

// TestDecodeSkipsUnknownFields is what lets a newer indexer stay readable: a
// field this decoder does not know must not turn into a failure.
func TestDecodeSkipsUnknownFields(t *testing.T) {
	metadata := append(varintField(99, 7), lengthDelimited(3, []byte("file:///tmp/x"))...)
	document := append(lengthDelimited(1, []byte("src/lib.rs")), varintField(31, 1)...)
	index := append(lengthDelimited(1, metadata), lengthDelimited(2, document)...)

	decoded, err := Decode(index)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.ProjectRoot != "file:///tmp/x" || len(decoded.Documents) != 1 {
		t.Fatalf("decoded = %#v", decoded)
	}
	if decoded.Documents[0].RelativePath != "src/lib.rs" {
		t.Fatalf("document = %#v", decoded.Documents[0])
	}
}

func TestDecodeRefusesMalformedIndexes(t *testing.T) {
	occurrenceWithBadRange := lengthDelimited(2, lengthDelimited(1, binary.AppendUvarint(nil, 3)))
	tests := map[string][]byte{
		"truncated tag":                 {0xff},
		"length beyond buffer":          append(tag(2, 2), binary.AppendUvarint(nil, 64)...),
		"string field sent as a varint": lengthDelimited(1, varintField(3, 5)),
		"range with two values": lengthDelimited(2, append(
			lengthDelimited(1, []byte("src/lib.rs")),
			lengthDelimited(2, lengthDelimited(1, append(
				binary.AppendUvarint(nil, 1), binary.AppendUvarint(nil, 2)...,
			)))...,
		)),
		"range with one value": lengthDelimited(2, append(
			lengthDelimited(1, []byte("src/lib.rs")),
			occurrenceWithBadRange...,
		)),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(data); err == nil {
				t.Fatalf("Decode() accepted %s", name)
			} else if !errors.Is(err, ErrMalformedIndex) {
				t.Fatalf("Decode() error = %v, want ErrMalformedIndex", err)
			}
		})
	}
}

// TestTheSameSourcesProduceTheSameIdentitiesOnEveryPlatform compares an index
// recorded on darwin/arm64 with one recorded on linux/amd64 over the same
// fixture.
//
// The two files are not byte identical -- the project root and the tool build
// differ -- and they must not need to be. What must match is everything an
// identity is built from: the documents, their symbols and the spans of every
// occurrence. If a platform changed any of those, the same repository would
// publish different stable keys depending on where it was indexed.
func TestTheSameSourcesProduceTheSameIdentitiesOnEveryPlatform(t *testing.T) {
	linuxPath := filepath.Join("..", "..", "..", "testdata", "protocol", "scip-v0.9", "engine-linux-amd64.scip")
	data, err := os.ReadFile(linuxPath)
	if err != nil {
		t.Fatalf("read the linux/amd64 index: %v", err)
	}
	linux, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	darwin := decodeFixture(t)

	if linux.ProjectRoot == darwin.ProjectRoot {
		t.Fatal("the two fixtures were recorded from the same root: one of them is not what it claims")
	}
	if len(linux.Documents) != len(darwin.Documents) {
		t.Fatalf("documents = %d on linux, %d on darwin", len(linux.Documents), len(darwin.Documents))
	}

	for _, document := range darwin.Documents {
		other := documentByPath(t, linux, document.RelativePath)
		if len(other.Occurrences) != len(document.Occurrences) {
			t.Fatalf("%s: %d occurrences on linux, %d on darwin",
				document.RelativePath, len(other.Occurrences), len(document.Occurrences))
		}
		for index, occurrence := range document.Occurrences {
			observed := other.Occurrences[index]
			if observed.Symbol != occurrence.Symbol {
				t.Fatalf("%s occurrence %d: symbol %q on linux, %q on darwin",
					document.RelativePath, index, observed.Symbol, occurrence.Symbol)
			}
			if observed.Range != occurrence.Range || observed.Roles != occurrence.Roles {
				t.Fatalf("%s occurrence %d: %#v on linux, %#v on darwin",
					document.RelativePath, index, observed, occurrence)
			}
		}
		for index, symbol := range document.Symbols {
			observed := other.Symbols[index]
			if observed.Symbol != symbol.Symbol || observed.Kind != symbol.Kind ||
				observed.DisplayName != symbol.DisplayName || observed.Signature != symbol.Signature {
				t.Fatalf("%s symbol %d: %#v on linux, %#v on darwin",
					document.RelativePath, index, observed, symbol)
			}
		}
	}
}
