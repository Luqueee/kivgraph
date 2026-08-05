package syntax

import "testing"

func TestClassifyChangesUsesConservativePrecedence(t *testing.T) {
	ranges := []SyntaxRange{{StartByte: 1, EndByte: 2}}
	base := SyntaxInventory{
		Language: LanguageTypeScript,
		Candidates: []SyntaxCandidate{
			{Kind: CandidateDeclaration, NodeKind: "function_declaration", Name: "run", Signature: "function run()"},
			{Kind: CandidateImport, NodeKind: "import_statement", Name: "./dep", Signature: "import value from \"./dep\""},
			{Kind: CandidateExport, NodeKind: "export_statement", Name: "run", Signature: "export function run()"},
		},
	}

	tests := []struct {
		name    string
		current SyntaxInventory
		want    ChangeClass
	}{
		{
			name: "imports precede declarations",
			current: SyntaxInventory{Language: LanguageTypeScript, Candidates: []SyntaxCandidate{
				{Kind: CandidateDeclaration, NodeKind: "function_declaration", Name: "run", Signature: "function run(value)"},
				{Kind: CandidateImport, NodeKind: "import_statement", Name: "./other", Signature: "import value from \"./other\""},
				{Kind: CandidateExport, NodeKind: "export_statement", Name: "run", Signature: "export function run()"},
			}},
			want: ChangeImportsChanged,
		},
		{
			name: "exports precede signatures",
			current: SyntaxInventory{Language: LanguageTypeScript, Candidates: []SyntaxCandidate{
				{Kind: CandidateDeclaration, NodeKind: "function_declaration", Name: "run", Signature: "function run(value)"},
				{Kind: CandidateImport, NodeKind: "import_statement", Name: "./dep", Signature: "import value from \"./dep\""},
				{Kind: CandidateExport, NodeKind: "export_statement", Name: "run", Signature: "export function other()"},
			}},
			want: ChangeExportsChanged,
		},
		{
			name: "signature",
			current: SyntaxInventory{Language: LanguageTypeScript, Candidates: []SyntaxCandidate{
				{Kind: CandidateDeclaration, NodeKind: "function_declaration", Name: "run", Signature: "function run(value)"},
				{Kind: CandidateImport, NodeKind: "import_statement", Name: "./dep", Signature: "import value from \"./dep\""},
				{Kind: CandidateExport, NodeKind: "export_statement", Name: "run", Signature: "export function run()"},
			}},
			want: ChangeSignatureChanged,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ClassifyChanges(base, test.current, ranges)
			if got.Class != test.want {
				t.Fatalf("ClassifyChanges() = %#v, want %s", got, test.want)
			}
			if len(got.ChangedRanges) != 1 {
				t.Fatalf("changed ranges = %#v", got.ChangedRanges)
			}
		})
	}
}

func TestClassifyChangesDetectsDeclarationTransitionsAndBodyOnly(t *testing.T) {
	base := SyntaxInventory{Language: LanguageJavaScript, Candidates: []SyntaxCandidate{
		{Kind: CandidateDeclaration, NodeKind: "function_declaration", Name: "run", Signature: "function run()"},
	}}
	added := SyntaxInventory{Language: LanguageJavaScript, Candidates: append(base.Candidates, SyntaxCandidate{
		Kind: CandidateDeclaration, NodeKind: "function_declaration", Name: "parse", Signature: "function parse()",
	})}
	if got := ClassifyChanges(base, added, []SyntaxRange{{StartByte: 10, EndByte: 20}}); got.Class != ChangeDeclarationAdded {
		t.Fatalf("added declaration class = %s", got.Class)
	}
	if got := ClassifyChanges(added, base, []SyntaxRange{{StartByte: 10, EndByte: 20}}); got.Class != ChangeDeclarationRemoved {
		t.Fatalf("removed declaration class = %s", got.Class)
	}
	body := SyntaxInventory{Language: LanguageJavaScript, Candidates: []SyntaxCandidate{
		{Kind: CandidateDeclaration, NodeKind: "function_declaration", Name: "run", Signature: "function run()"},
		{Kind: CandidateCall, NodeKind: "call_expression", Name: "helper(2)", Signature: "helper(2)"},
	}}
	if got := ClassifyChanges(base, body, []SyntaxRange{{StartByte: 20, EndByte: 30}}); got.Class != ChangeBodyOnly {
		t.Fatalf("body-only class = %s", got.Class)
	}
}

func TestClassifyChangesReturnsUnknownWithoutEvidence(t *testing.T) {
	base := SyntaxInventory{Language: LanguageGo, Candidates: []SyntaxCandidate{
		{Kind: CandidateDeclaration, NodeKind: "function_declaration", Name: "run", Signature: "func run()"},
	}}
	if got := ClassifyChanges(base, base, nil); got.Class != ChangeUnknown {
		t.Fatalf("unchanged classification = %s", got.Class)
	}
	otherLanguage := base
	otherLanguage.Language = LanguageJavaScript
	if got := ClassifyChanges(base, otherLanguage, []SyntaxRange{{StartByte: 0, EndByte: 1}}); got.Class != ChangeUnknown {
		t.Fatalf("cross-language classification = %s", got.Class)
	}
}

func TestSortChangedRangesReturnsIndependentSourceOrder(t *testing.T) {
	input := []SyntaxRange{{StartByte: 10, EndByte: 20}, {StartByte: 2, EndByte: 4}}
	ordered := SortChangedRanges(input)
	if ordered[0].StartByte != 2 || ordered[1].StartByte != 10 {
		t.Fatalf("SortChangedRanges() = %#v", ordered)
	}
	ordered[0].StartByte = 99
	if input[1].StartByte == 99 {
		t.Fatal("SortChangedRanges() returned input storage")
	}
}
