package ladybug

import (
	"context"
	"fmt"
)

const (
	ReferenceKindReferences  = "REFERENCES"
	ReferenceKindCallsDirect = "CALLS_DIRECT"
)

// Writer owns the database's single logical write connection.
// Apply executes every operation in one transaction and rolls back on failure.
type Writer interface {
	Close() error
	Apply(context.Context, Delta) (MutationResult, error)
}

// Delta describes one atomic synthetic graph update.
type Delta struct {
	AddSymbols       []Symbol
	UpdateSymbols    []Symbol
	DeleteSymbolKeys []string
	AddReferences    []Reference
	DeleteReferences []ReferenceKey
	ReplaceOutgoing  []OutgoingReplacement
}

// ReferenceKey identifies every persisted relationship of one kind between two symbols.
type ReferenceKey struct {
	SourceKey string
	TargetKey string
	Kind      string
}

// OutgoingReplacement atomically replaces both semantic relationship kinds for one source.
type OutgoingReplacement struct {
	SourceKey  string
	References []Reference
}

// MutationResult reports committed record changes.
type MutationResult struct {
	AddedSymbols      int `json:"added_symbols"`
	UpdatedSymbols    int `json:"updated_symbols"`
	DeletedSymbols    int `json:"deleted_symbols"`
	AddedReferences   int `json:"added_references"`
	DeletedReferences int `json:"deleted_references"`
	ReplacedSources   int `json:"replaced_sources"`
}

func validateDelta(delta Delta) error {
	if len(delta.AddSymbols) == 0 && len(delta.UpdateSymbols) == 0 && len(delta.DeleteSymbolKeys) == 0 && len(delta.AddReferences) == 0 && len(delta.DeleteReferences) == 0 && len(delta.ReplaceOutgoing) == 0 {
		return fmt.Errorf("%w: delta is empty", ErrInvalidMutation)
	}

	symbolActions := make(map[string]string, len(delta.AddSymbols)+len(delta.UpdateSymbols)+len(delta.DeleteSymbolKeys))
	registerSymbol := func(stableKey, action string) error {
		if err := validateStableKey(stableKey); err != nil {
			return fmt.Errorf("%w: %s symbol: %v", ErrInvalidMutation, action, err)
		}
		if previous, exists := symbolActions[stableKey]; exists {
			return fmt.Errorf("%w: symbol %s appears in both %s and %s", ErrInvalidMutation, stableKey, previous, action)
		}
		symbolActions[stableKey] = action
		return nil
	}
	for _, symbol := range delta.AddSymbols {
		if err := validateMutationSymbol(symbol); err != nil {
			return err
		}
		if err := registerSymbol(symbol.StableKey, "add"); err != nil {
			return err
		}
	}
	for _, symbol := range delta.UpdateSymbols {
		if err := validateMutationSymbol(symbol); err != nil {
			return err
		}
		if err := registerSymbol(symbol.StableKey, "update"); err != nil {
			return err
		}
	}
	deleted := make(map[string]struct{}, len(delta.DeleteSymbolKeys))
	for _, stableKey := range delta.DeleteSymbolKeys {
		if err := registerSymbol(stableKey, "delete"); err != nil {
			return err
		}
		deleted[stableKey] = struct{}{}
	}

	deleteKeys := make(map[ReferenceKey]struct{}, len(delta.DeleteReferences))
	for _, reference := range delta.DeleteReferences {
		if err := validateReferenceKey(reference); err != nil {
			return err
		}
		if _, exists := deleteKeys[reference]; exists {
			return fmt.Errorf("%w: duplicate deleted reference %#v", ErrInvalidMutation, reference)
		}
		deleteKeys[reference] = struct{}{}
	}

	replacedSources := make(map[string]struct{}, len(delta.ReplaceOutgoing))
	for _, replacement := range delta.ReplaceOutgoing {
		if err := validateStableKey(replacement.SourceKey); err != nil {
			return fmt.Errorf("%w: replacement source: %v", ErrInvalidMutation, err)
		}
		if _, exists := deleted[replacement.SourceKey]; exists {
			return fmt.Errorf("%w: replacement source %s is deleted", ErrInvalidMutation, replacement.SourceKey)
		}
		if _, exists := replacedSources[replacement.SourceKey]; exists {
			return fmt.Errorf("%w: source %s has multiple outgoing replacements", ErrInvalidMutation, replacement.SourceKey)
		}
		replacedSources[replacement.SourceKey] = struct{}{}
	}

	addKeys := make(map[ReferenceKey]struct{}, len(delta.AddReferences))
	registerAddedReference := func(reference Reference, replacementSource string) error {
		if err := validateMutationReference(reference); err != nil {
			return err
		}
		if replacementSource != "" && reference.SourceKey != replacementSource {
			return fmt.Errorf("%w: replacement for %s contains source %s", ErrInvalidMutation, replacementSource, reference.SourceKey)
		}
		if _, exists := deleted[reference.SourceKey]; exists {
			return fmt.Errorf("%w: added reference source %s is deleted", ErrInvalidMutation, reference.SourceKey)
		}
		if _, exists := deleted[reference.TargetKey]; exists {
			return fmt.Errorf("%w: added reference target %s is deleted", ErrInvalidMutation, reference.TargetKey)
		}
		key := reference.key()
		if _, exists := addKeys[key]; exists {
			return fmt.Errorf("%w: duplicate added reference %#v", ErrInvalidMutation, key)
		}
		addKeys[key] = struct{}{}
		return nil
	}
	for _, reference := range delta.AddReferences {
		if _, replaced := replacedSources[reference.SourceKey]; replaced {
			return fmt.Errorf("%w: source %s has both added references and an outgoing replacement", ErrInvalidMutation, reference.SourceKey)
		}
		if err := registerAddedReference(reference, ""); err != nil {
			return err
		}
	}
	for _, replacement := range delta.ReplaceOutgoing {
		for _, reference := range replacement.References {
			if err := registerAddedReference(reference, replacement.SourceKey); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateMutationSymbol(symbol Symbol) error {
	if err := validateStableKey(symbol.StableKey); err != nil {
		return fmt.Errorf("%w: add or update symbol: %v", ErrInvalidMutation, err)
	}
	if symbol.RepositoryKey == "" || symbol.FileKey == "" || symbol.Name == "" || symbol.QualifiedName == "" || symbol.Kind == "" {
		return fmt.Errorf("%w: symbol %s has an empty required property", ErrInvalidMutation, symbol.StableKey)
	}
	if symbol.StartLine < 1 || symbol.EndLine < symbol.StartLine {
		return fmt.Errorf("%w: symbol %s has invalid line range %d..%d", ErrInvalidMutation, symbol.StableKey, symbol.StartLine, symbol.EndLine)
	}
	return nil
}

func validateMutationReference(reference Reference) error {
	if err := validateReferenceKey(reference.key()); err != nil {
		return err
	}
	if reference.EvidenceKind == "" || reference.SourceFileKey == "" || reference.TargetFileKey == "" {
		return fmt.Errorf("%w: reference %s -> %s has an empty required property", ErrInvalidMutation, reference.SourceKey, reference.TargetKey)
	}
	return nil
}

func validateReferenceKey(reference ReferenceKey) error {
	if err := validateStableKey(reference.SourceKey); err != nil {
		return fmt.Errorf("%w: reference source: %v", ErrInvalidMutation, err)
	}
	if err := validateStableKey(reference.TargetKey); err != nil {
		return fmt.Errorf("%w: reference target: %v", ErrInvalidMutation, err)
	}
	if reference.Kind != ReferenceKindReferences && reference.Kind != ReferenceKindCallsDirect {
		return fmt.Errorf("%w: unsupported reference kind %q", ErrInvalidMutation, reference.Kind)
	}
	return nil
}

func (reference Reference) key() ReferenceKey {
	return ReferenceKey{SourceKey: reference.SourceKey, TargetKey: reference.TargetKey, Kind: reference.Kind}
}
