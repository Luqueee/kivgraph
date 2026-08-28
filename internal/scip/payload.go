// Package scip turns a SCIP index into the semantic payload every external
// language producer shares.
//
// SCIP is one format with many producers -- scip-java, scip-python,
// scip-ruby, scip-dotnet, scip-clang, and `rust-analyzer scip` -- so the
// conversion is written once here rather than once per language. What a
// language still owns is running its indexer and naming its package; what it
// does not own is the graph model, which is facts.NormalizeSemantic's.
package scip

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
)

// Options is what the conversion cannot infer from the index itself.
type Options struct {
	// Language stamps every fact. It decides the provenance and the stable
	// key namespace, so it is required.
	Language facts.Language
	// Repository is the registered repository name the payload belongs to.
	Repository string
	// Package names the unit the symbols belong to. Empty falls back to the
	// repository name, which is what facts.NormalizeSemantic would do anyway.
	Package      string
	PackageRoot  string
	ManifestPath string
	// Analyzer and AnalyzerVersion identify the producer. They default to
	// the index's own metadata, which is what a producer writes there.
	Analyzer        string
	AnalyzerVersion string
	// Authoritative marks the references as type-checked. It is a statement
	// about the producer, not about SCIP: an index emitted by a syntax-only
	// tool is still a valid SCIP index and its edges are still candidates.
	Authoritative bool
	// ReadFile returns the bytes of a repository-relative path, for
	// converting the index's line/character positions into byte offsets. A
	// nil reader leaves offsets at zero, which is honest but costs every
	// consumer that ranks by position.
	ReadFile func(relativePath string) ([]byte, error)
	// IncludeFile decides whether a document enters the payload. A nil
	// filter includes every document the index carries.
	IncludeFile func(relativePath string) bool
	// Generated reports whether a document is generated code.
	Generated func(relativePath string) bool
	// LocalPackages are the SCIP package identities that belong to this
	// payload's repository. An occurrence of a symbol outside them is a
	// reference to something this repository does not declare, and becomes
	// an unresolved fact rather than an invented local symbol.
	LocalPackages map[string]bool
}

// Convert turns one SCIP index into one semantic payload.
//
// The shape it produces is the one facts.NormalizeSemantic consumes: files,
// symbols with a full declaration range, references attributed to the symbol
// that encloses them, and an unresolved row for every target this repository
// does not declare.
func Convert(index scipwire.Index, options Options) (facts.SemanticPayload, error) {
	if strings.TrimSpace(options.Repository) == "" {
		return facts.SemanticPayload{}, fmt.Errorf("scip: repository is required")
	}
	if options.Language == "" {
		return facts.SemanticPayload{}, fmt.Errorf("scip: language is required")
	}
	analyzer := strings.TrimSpace(options.Analyzer)
	if analyzer == "" {
		analyzer = index.ToolName
	}
	analyzerVersion := strings.TrimSpace(options.AnalyzerVersion)
	if analyzerVersion == "" {
		analyzerVersion = index.ToolVersion
	}
	packageName := strings.TrimSpace(options.Package)
	if packageName == "" {
		packageName = options.Repository
	}

	payload := facts.SemanticPayload{
		Version:         1,
		Authoritative:   options.Authoritative,
		Analyzer:        analyzer,
		AnalyzerVersion: analyzerVersion,
		Repository:      options.Repository,
		Language:        options.Language,
		Package: facts.SemanticPackage{
			Name:         packageName,
			RootPath:     options.PackageRoot,
			ManifestPath: options.ManifestPath,
		},
	}

	// A symbol is declared once and referenced from anywhere, so the
	// declaration table is built across every document before any reference
	// is attributed. Doing it per document made a use of a class declared in
	// another file of the same package look external.
	declared := map[string]bool{}
	// local is the set of SCIP packages this payload declares something in. It
	// separates two very different absences: a target in another package that
	// this graph does not hold, and a target in a package it does hold whose
	// declaration nobody observed. The second is not an import.
	local := map[string]bool{}
	for _, document := range index.Documents {
		if !options.includes(document.RelativePath) {
			continue
		}
		for _, occurrence := range document.Occurrences {
			if !occurrence.Definition() || !addressable(occurrence.Symbol) {
				continue
			}
			declared[occurrence.Symbol] = true
			if identity, err := parseSymbol(occurrence.Symbol); err == nil {
				local[identity.pkg] = true
			}
		}
	}
	for pkg := range options.LocalPackages {
		local[pkg] = true
	}

	for _, document := range index.Documents {
		path := filepath.ToSlash(filepath.Clean(document.RelativePath))
		if path == "." || path == "" || !options.includes(document.RelativePath) {
			continue
		}
		contents := options.read(document.RelativePath)
		offsets := newOffsetTable(contents, document.PositionEncoding)

		generated := false
		if options.Generated != nil {
			generated = options.Generated(document.RelativePath)
		}
		payload.Files = append(payload.Files, facts.SemanticFile{Path: path, Generated: generated})

		converted, err := convertDocument(document, path, offsets, declared, local, options)
		if err != nil {
			return facts.SemanticPayload{}, err
		}
		payload.Symbols = append(payload.Symbols, converted.symbols...)
		payload.References = append(payload.References, converted.references...)
		payload.Unresolved = append(payload.Unresolved, converted.unresolved...)
	}
	return payload, nil
}

type documentFacts struct {
	symbols    []facts.SemanticSymbol
	references []facts.SemanticReference
	unresolved []facts.SemanticUnresolved
}

// declaration pairs a definition occurrence with the metadata SCIP carries in
// a separate table. The occurrence has the position and the enclosing range;
// SymbolInformation has the kind, the display name and the signature.
type declaration struct {
	symbol    string
	selection scipwire.Range
	enclosing scipwire.Range
	info      scipwire.SymbolInformation
}

func convertDocument(
	document scipwire.Document,
	path string,
	offsets *offsetTable,
	declared map[string]bool,
	local map[string]bool,
	options Options,
) (documentFacts, error) {
	var result documentFacts

	information := make(map[string]scipwire.SymbolInformation, len(document.Symbols))
	for _, info := range document.Symbols {
		information[info.Symbol] = info
	}

	// The module symbol stands for the file's own top level scope. Every
	// reference that no declaration encloses is sourced here rather than
	// dropped -- an import, an annotation on the file, a use in a static
	// initialiser. facts.ModuleSymbolKind is the convention it must carry.
	moduleID := modulePseudoSymbol(path)
	result.symbols = append(result.symbols, facts.SemanticSymbol{
		ID:            moduleID,
		File:          path,
		Name:          filepath.Base(path),
		QualifiedName: moduleQualifiedName(path),
		Kind:          facts.ModuleSymbolKind,
		Exported:      true,
		Signature:     facts.ModuleSymbolKind + " " + path,
		EndLine:       offsets.lastLine(),
		End:           offsets.size(),
	})

	declarations := make([]declaration, 0, len(document.Occurrences))
	for _, occurrence := range document.Occurrences {
		if !occurrence.Definition() || !addressable(occurrence.Symbol) {
			continue
		}
		declarations = append(declarations, declaration{
			symbol:    occurrence.Symbol,
			selection: occurrence.Range,
			enclosing: occurrence.EnclosingRange,
			info:      information[occurrence.Symbol],
		})
	}

	for _, entry := range declarations {
		identity, err := parseSymbol(entry.symbol)
		if err != nil {
			continue
		}
		// The enclosing range is the whole declaration; the selection range
		// is only its name. A symbol whose range is its name alone cannot
		// contain anything, so nothing would ever be attributed to it.
		span := entry.enclosing
		if !span.Present {
			span = entry.selection
		}
		start := offsets.position(span.StartLine, span.StartCharacter)
		end := offsets.position(span.EndLine, span.EndCharacter)
		name := strings.TrimSpace(entry.info.DisplayName)
		if name == "" {
			name = identity.name()
		}
		result.symbols = append(result.symbols, facts.SemanticSymbol{
			ID:            entry.symbol,
			File:          path,
			Name:          name,
			QualifiedName: identity.qualifiedName(),
			Kind:          kindName(entry.info.Kind, identity),
			Exported:      exported(entry.info, identity),
			Signature:     signature(entry.info, identity),
			StartLine:     int(span.StartLine),
			StartColumn:   int(span.StartCharacter),
			Start:         start,
			EndLine:       int(span.EndLine),
			EndColumn:     int(span.EndCharacter),
			End:           end,
		})
	}

	// Innermost first, so attributing a use walks from the tightest scope
	// outwards and a method wins over the class that contains it.
	enclosing := append([]declaration(nil), declarations...)
	sort.SliceStable(enclosing, func(left, right int) bool {
		return narrower(enclosing[left], enclosing[right])
	})

	seenUnresolved := map[string]bool{}
	for _, occurrence := range document.Occurrences {
		if occurrence.Definition() || !addressable(occurrence.Symbol) {
			continue
		}
		sourceID := moduleID
		for _, candidate := range enclosing {
			if candidate.enclosing.Present && candidate.enclosing.Contains(occurrence.Range) {
				sourceID = candidate.symbol
				break
			}
		}
		start := offsets.position(occurrence.Range.StartLine, occurrence.Range.StartCharacter)
		end := offsets.position(occurrence.Range.EndLine, occurrence.Range.EndCharacter)

		if !declared[occurrence.Symbol] {
			// A target this repository does not declare. It is never turned
			// into a local symbol: an edge to a declaration nobody observed
			// would be an EXACT claim about code that is not in the graph.
			identity, err := parseSymbol(occurrence.Symbol)
			if err != nil {
				continue
			}
			key := identity.pkg + "\x00" + identity.descriptors
			if seenUnresolved[key] {
				continue
			}
			seenUnresolved[key] = true
			// Only a target outside every package this payload declares is an
			// import. Inside one, the declaration exists and was not observed
			// -- a compiler-synthesised member is the ordinary case: a Java
			// record's accessor `Point#x()` is referenced and never defined in
			// any document. Calling that an unresolved import would also feed
			// the package-dependency rule with a package depending on itself.
			reason := "IMPORT_NOT_RESOLVED"
			if local[identity.pkg] {
				reason = "DEFINITION_NOT_INDEXED"
			}
			result.unresolved = append(result.unresolved, facts.SemanticUnresolved{
				File:             path,
				SourceID:         sourceID,
				RequestedPackage: identity.pkg,
				RequestedSymbol:  identity.qualifiedName(),
				Reason:           reason,
				Detail:           occurrence.Symbol,
				StartLine:        int(occurrence.Range.StartLine),
				StartColumn:      int(occurrence.Range.StartCharacter),
				Start:            start,
			})
			continue
		}
		result.references = append(result.references, facts.SemanticReference{
			File:        path,
			SourceID:    sourceID,
			TargetID:    occurrence.Symbol,
			Kind:        string(referenceKind(occurrence.Symbol)),
			StartLine:   int(occurrence.Range.StartLine),
			StartColumn: int(occurrence.Range.StartCharacter),
			Start:       start,
			EndLine:     int(occurrence.Range.EndLine),
			EndColumn:   int(occurrence.Range.EndCharacter),
			End:         end,
		})
	}
	return result, nil
}

// narrower reports whether one declaration's enclosing range is contained in
// the other's, so the innermost declaration is found first.
func narrower(left, right declaration) bool {
	if !left.enclosing.Present {
		return false
	}
	if !right.enclosing.Present {
		return true
	}
	leftLines := left.enclosing.EndLine - left.enclosing.StartLine
	rightLines := right.enclosing.EndLine - right.enclosing.StartLine
	if leftLines != rightLines {
		return leftLines < rightLines
	}
	return left.enclosing.StartLine > right.enclosing.StartLine
}

// referenceKind is the canonical edge kind of a use.
//
// SCIP says where a symbol is used, not what the use was: a call, a type
// position and a field read are the same occurrence with the same roles. So
// every use is REFERENCES, which is the kind that claims the least. Producing
// CALLS_DIRECT from the descriptor suffix would be a guess -- `f()` in a type
// position is not a call -- and the model forbids inventing an edge kind from
// a name.
func referenceKind(string) facts.EdgeKind { return facts.References }

func (options Options) includes(path string) bool {
	if options.IncludeFile == nil {
		return true
	}
	return options.IncludeFile(path)
}

func (options Options) read(path string) []byte {
	if options.ReadFile == nil {
		return nil
	}
	data, err := options.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

// modulePseudoSymbol is the payload-local identifier of a file's module
// symbol. It is not a SCIP symbol and never collides with one: SCIP symbols
// carry a scheme and this carries a sentinel prefix no scheme uses.
func modulePseudoSymbol(path string) string { return "kivgraph-module\x00" + path }

func moduleQualifiedName(path string) string {
	trimmed := strings.TrimSuffix(path, filepath.Ext(path))
	return strings.ReplaceAll(trimmed, "/", ".")
}
