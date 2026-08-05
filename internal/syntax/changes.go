package syntax

import (
	"sort"
)

// ChangeClass is the conservative syntactic impact classification.
type ChangeClass string

const (
	ChangeBodyOnly           ChangeClass = "BODY_ONLY"
	ChangeSignatureChanged   ChangeClass = "SIGNATURE_CHANGED"
	ChangeImportsChanged     ChangeClass = "IMPORTS_CHANGED"
	ChangeExportsChanged     ChangeClass = "EXPORTS_CHANGED"
	ChangeDeclarationAdded   ChangeClass = "DECLARATION_ADDED"
	ChangeDeclarationRemoved ChangeClass = "DECLARATION_REMOVED"
	ChangeUnknown            ChangeClass = "UNKNOWN"
)

// ChangeClassification combines one conservative class with the ranges that
// caused the comparison. It contains no semantic relationship or edge.
type ChangeClassification struct {
	Class         ChangeClass
	ChangedRanges []SyntaxRange
}

// ClassifyChanges compares two syntax inventories. Imports and exports take
// precedence; declaration additions/removals precede signature changes, and
// a change with no structural candidate difference is BODY_ONLY only when
// changed ranges provide evidence.
func ClassifyChanges(previous, current SyntaxInventory, changedRanges []SyntaxRange) ChangeClassification {
	result := ChangeClassification{
		Class:         ChangeUnknown,
		ChangedRanges: append([]SyntaxRange(nil), changedRanges...),
	}
	if previous.Language == "" || current.Language == "" || previous.Language != current.Language {
		return result
	}
	if candidateCategoryChanged(previous.Candidates, current.Candidates, CandidateImport) {
		result.Class = ChangeImportsChanged
		return result
	}
	if candidateCategoryChanged(previous.Candidates, current.Candidates, CandidateExport) {
		result.Class = ChangeExportsChanged
		return result
	}

	previousDeclarations := candidateMultiset(previous.Candidates, CandidateDeclaration)
	currentDeclarations := candidateMultiset(current.Candidates, CandidateDeclaration)
	added, removed := multisetDifference(currentDeclarations, previousDeclarations), multisetDifference(previousDeclarations, currentDeclarations)
	if len(added) != 0 && len(removed) == 0 {
		result.Class = ChangeDeclarationAdded
		return result
	}
	if len(removed) != 0 && len(added) == 0 {
		result.Class = ChangeDeclarationRemoved
		return result
	}
	if len(added) != 0 || len(removed) != 0 {
		return result
	}

	if signatureChanged(previous.Candidates, current.Candidates) {
		result.Class = ChangeSignatureChanged
		return result
	}
	if len(changedRanges) != 0 {
		result.Class = ChangeBodyOnly
	}
	return result
}

func candidateCategoryChanged(previous, current []SyntaxCandidate, category CandidateKind) bool {
	return !equalStringMultisets(candidateSignatures(previous, category), candidateSignatures(current, category))
}

func signatureChanged(previous, current []SyntaxCandidate) bool {
	for _, category := range []CandidateKind{CandidateDeclaration, CandidateClass, CandidateInterface, CandidateMethod} {
		before := candidateSignatures(previous, category)
		after := candidateSignatures(current, category)
		if !equalStringMultisets(before, after) {
			return true
		}
	}
	return false
}

func candidateMultiset(candidates []SyntaxCandidate, category CandidateKind) map[string]int {
	result := make(map[string]int)
	for _, candidate := range candidates {
		if candidate.Kind != category {
			continue
		}
		result[candidateIdentity(candidate)]++
	}
	return result
}

func candidateSignatures(candidates []SyntaxCandidate, category CandidateKind) map[string]int {
	result := make(map[string]int)
	for _, candidate := range candidates {
		if candidate.Kind != category {
			continue
		}
		result[candidateIdentity(candidate)+"\x00"+candidate.Signature]++
	}
	return result
}

func candidateIdentity(candidate SyntaxCandidate) string {
	return string(candidate.Kind) + "\x00" + candidate.NodeKind + "\x00" + candidate.Name
}

func multisetDifference(left, right map[string]int) map[string]int {
	result := make(map[string]int)
	for key, count := range left {
		if difference := count - right[key]; difference > 0 {
			result[key] = difference
		}
	}
	return result
}

func equalCandidateMultisets(left, right map[string]int) bool {
	return equalStringMultisets(left, right)
}

func equalStringMultisets(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, count := range left {
		if right[key] != count {
			return false
		}
	}
	return true
}

// SortChangedRanges returns an independent source-order copy for downstream
// invalidation code.
func SortChangedRanges(ranges []SyntaxRange) []SyntaxRange {
	result := append([]SyntaxRange(nil), ranges...)
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].StartByte != result[right].StartByte {
			return result[left].StartByte < result[right].StartByte
		}
		return result[left].EndByte < result[right].EndByte
	})
	return result
}
