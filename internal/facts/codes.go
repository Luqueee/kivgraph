package facts

import (
	"errors"
	"fmt"
)

// CodeFormatVersion is the version of the numbering below. The HotSnapshot
// stores kinds, confidences and provenances as bare uint8 codes and filters
// traversals by them, so a snapshot is only readable by code that agrees on
// this numbering. Reordering or reusing a code is a format break: bump this
// version instead, and never renumber an existing constant.
const CodeFormatVersion uint16 = 1

// ErrUnknownCode reports a code outside the canonical numbering.
var ErrUnknownCode = errors.New("unknown canonical code")

// Code zero is reserved on every axis: a zero valued PackedEdge field means
// "not set", which must never silently read as a legitimate value.
const (
	codeUnset uint8 = 0
)

// Edge kind codes. Frozen: append only.
const (
	CodeContainsPackage uint8 = iota + 1
	CodeContainsFile
	CodeDefines
	CodePackageDependsOn
	CodeModuleDependsOn
	CodeImportsSymbol
	CodeExports
	CodeReexports
	CodeReferences
	CodeCallsDirect
	CodePassesAsCallback
	CodeAssignsFunction
	CodeReturnsFunction
	CodeTypeUses
	CodeImplements
	CodeExtends
	CodeEmbeds
	CodeOverrides
)

// Confidence codes. Frozen: append only. The order matches the strength
// ordering of the model, strongest first, so a comparison on the code is a
// comparison on strength.
const (
	CodeExactTypechecked uint8 = iota + 1
	CodeExactDeclarationMapped
	CodeExactPackageMapped
	CodeStructuralCertain
	CodeCandidate
	CodeUnresolved
)

// Provenance codes. Frozen: append only.
const (
	CodeTypeScriptChecker uint8 = iota + 1
	CodeTypeScriptModuleResolution
	CodeTypeScriptDeclarationMap
	CodeTypeScriptProjectReference
	CodeGoTypesDefinition
	CodeGoTypesUse
	CodeGoTypesSelection
	CodeGoASTCall
	CodeGoASTCallback
	CodeGoObjectPath
	CodeTreeSitterSyntax
	CodePackageManifest
	// Appended for Rust. Never inserted above: a snapshot stores these
	// numbers, and renumbering one rewrites the meaning of every edge that
	// carries it.
	CodeRustAnalyzerDefinition
	CodeRustAnalyzerUse
	CodeRustAnalyzerMoniker
	CodeRustSyntaxCall
	CodeRustSyntaxType
	CodeRustSyntaxImplementation
	CodeRustSyntaxCallback
)

var edgeKindCodes = map[EdgeKind]uint8{
	ContainsPackage:  CodeContainsPackage,
	ContainsFile:     CodeContainsFile,
	Defines:          CodeDefines,
	PackageDependsOn: CodePackageDependsOn,
	ModuleDependsOn:  CodeModuleDependsOn,
	ImportsSymbol:    CodeImportsSymbol,
	Exports:          CodeExports,
	Reexports:        CodeReexports,
	References:       CodeReferences,
	CallsDirect:      CodeCallsDirect,
	PassesAsCallback: CodePassesAsCallback,
	AssignsFunction:  CodeAssignsFunction,
	ReturnsFunction:  CodeReturnsFunction,
	TypeUses:         CodeTypeUses,
	Implements:       CodeImplements,
	Extends:          CodeExtends,
	Embeds:           CodeEmbeds,
	Overrides:        CodeOverrides,
}

var confidenceCodes = map[Confidence]uint8{
	ExactTypechecked:       CodeExactTypechecked,
	ExactDeclarationMapped: CodeExactDeclarationMapped,
	ExactPackageMapped:     CodeExactPackageMapped,
	StructuralCertain:      CodeStructuralCertain,
	Candidate:              CodeCandidate,
	Unresolved:             CodeUnresolved,
}

var provenanceCodes = map[Provenance]uint8{
	TypeScriptChecker:          CodeTypeScriptChecker,
	TypeScriptModuleResolution: CodeTypeScriptModuleResolution,
	TypeScriptDeclarationMap:   CodeTypeScriptDeclarationMap,
	TypeScriptProjectReference: CodeTypeScriptProjectReference,
	GoTypesDefinition:          CodeGoTypesDefinition,
	GoTypesUse:                 CodeGoTypesUse,
	GoTypesSelection:           CodeGoTypesSelection,
	GoASTCall:                  CodeGoASTCall,
	GoASTCallback:              CodeGoASTCallback,
	GoObjectPath:               CodeGoObjectPath,
	TreeSitterSyntax:           CodeTreeSitterSyntax,
	PackageManifest:            CodePackageManifest,
	RustAnalyzerDefinition:     CodeRustAnalyzerDefinition,
	RustAnalyzerUse:            CodeRustAnalyzerUse,
	RustAnalyzerMoniker:        CodeRustAnalyzerMoniker,
	RustSyntaxCall:             CodeRustSyntaxCall,
	RustSyntaxType:             CodeRustSyntaxType,
	RustSyntaxImplementation:   CodeRustSyntaxImplementation,
	RustSyntaxCallback:         CodeRustSyntaxCallback,
}

// reverse builds the decoding table of a coding table. The tables are
// inverted once, at init, so a decode is a slice index and not a scan.
func reverse[T ~string](codes map[T]uint8) []T {
	highest := codeUnset
	for _, code := range codes {
		if code > highest {
			highest = code
		}
	}
	table := make([]T, highest+1)
	for value, code := range codes {
		table[code] = value
	}
	return table
}

var (
	edgeKindByCode   = reverse(edgeKindCodes)
	confidenceByCode = reverse(confidenceCodes)
	provenanceByCode = reverse(provenanceCodes)
)

// Code returns the frozen numeric code of the kind.
func (kind EdgeKind) Code() (uint8, error) {
	code, exists := edgeKindCodes[kind]
	if !exists {
		return codeUnset, fmt.Errorf("%w: edge kind %q", ErrUnknownCode, string(kind))
	}
	return code, nil
}

// Code returns the frozen numeric code of the confidence.
func (confidence Confidence) Code() (uint8, error) {
	code, exists := confidenceCodes[confidence]
	if !exists {
		return codeUnset, fmt.Errorf("%w: confidence %q", ErrUnknownCode, string(confidence))
	}
	return code, nil
}

// Code returns the frozen numeric code of the provenance.
func (provenance Provenance) Code() (uint8, error) {
	code, exists := provenanceCodes[provenance]
	if !exists {
		return codeUnset, fmt.Errorf("%w: provenance %q", ErrUnknownCode, string(provenance))
	}
	return code, nil
}

// EdgeKindFromCode decodes a stored edge kind.
func EdgeKindFromCode(code uint8) (EdgeKind, error) {
	return decode(code, edgeKindByCode, "edge kind")
}

// ConfidenceFromCode decodes a stored confidence.
func ConfidenceFromCode(code uint8) (Confidence, error) {
	return decode(code, confidenceByCode, "confidence")
}

// ProvenanceFromCode decodes a stored provenance.
func ProvenanceFromCode(code uint8) (Provenance, error) {
	return decode(code, provenanceByCode, "provenance")
}

func decode[T ~string](code uint8, table []T, axis string) (T, error) {
	var zero T
	if code == codeUnset || int(code) >= len(table) || table[code] == zero {
		return zero, fmt.Errorf("%w: %s code %d", ErrUnknownCode, axis, code)
	}
	return table[code], nil
}
