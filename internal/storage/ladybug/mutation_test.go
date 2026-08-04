package ladybug

import (
	"errors"
	"testing"
)

func TestValidateDeltaAcceptsAtomicReplacement(t *testing.T) {
	delta := Delta{
		UpdateSymbols: []Symbol{validMutationSymbol("source")},
		DeleteReferences: []ReferenceKey{{
			SourceKey: "other", TargetKey: "source", Kind: ReferenceKindReferences,
		}},
		ReplaceOutgoing: []OutgoingReplacement{{
			SourceKey:  "source",
			References: []Reference{validMutationReference("source", "target", ReferenceKindCallsDirect)},
		}},
	}
	if err := validateDelta(delta); err != nil {
		t.Fatalf("validateDelta() error = %v", err)
	}
}

func TestValidateDeltaRejectsAmbiguousOrMalformedMutations(t *testing.T) {
	validSymbol := validMutationSymbol("symbol")
	validReference := validMutationReference("source", "target", ReferenceKindReferences)
	tests := map[string]Delta{
		"empty": {},
		"duplicate symbol": {
			AddSymbols: []Symbol{validSymbol, validSymbol},
		},
		"conflicting symbol actions": {
			AddSymbols:       []Symbol{validSymbol},
			DeleteSymbolKeys: []string{validSymbol.StableKey},
		},
		"invalid line range": {
			AddSymbols: []Symbol{func() Symbol {
				value := validSymbol
				value.StartLine = 10
				value.EndLine = 2
				return value
			}()},
		},
		"invalid reference kind": {
			AddReferences: []Reference{validMutationReference("source", "target", "TEXT_MATCH")},
		},
		"duplicate reference": {
			AddReferences: []Reference{validReference, validReference},
		},
		"replacement source mismatch": {
			ReplaceOutgoing: []OutgoingReplacement{{
				SourceKey:  "source",
				References: []Reference{validMutationReference("different", "target", ReferenceKindReferences)},
			}},
		},
		"replacement and explicit add": {
			AddReferences: []Reference{validReference},
			ReplaceOutgoing: []OutgoingReplacement{{
				SourceKey: "source",
			}},
		},
	}
	for name, delta := range tests {
		t.Run(name, func(t *testing.T) {
			if err := validateDelta(delta); !errors.Is(err, ErrInvalidMutation) {
				t.Fatalf("validateDelta() error = %v, want ErrInvalidMutation", err)
			}
		})
	}
}

func validMutationSymbol(stableKey string) Symbol {
	return Symbol{
		StableKey: stableKey, RepositoryKey: "repository", FileKey: "file",
		Name: stableKey, QualifiedName: "fixture." + stableKey, Kind: "function",
		Signature: stableKey + "()", StartLine: 1, EndLine: 2,
	}
}

func validMutationReference(sourceKey, targetKey, kind string) Reference {
	return Reference{
		SourceKey: sourceKey, TargetKey: targetKey, Kind: kind,
		EvidenceKind: "exact", SourceFileKey: "source-file", TargetFileKey: "target-file",
	}
}
