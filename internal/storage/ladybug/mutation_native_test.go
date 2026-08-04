//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWriterAppliesEveryIncrementalMutation(t *testing.T) {
	database, reader := newQueryFixture(t)
	writer, err := database.OpenWriter(context.Background())
	if err != nil {
		t.Fatalf("OpenWriter() error = %v", err)
	}
	defer writer.Close()
	if _, err := database.OpenWriter(context.Background()); !errors.Is(err, ErrWriterOpen) {
		t.Fatalf("second OpenWriter() error = %v, want ErrWriterOpen", err)
	}

	added := mutationSymbol("s8", "r1", "f0")
	result, err := writer.Apply(context.Background(), Delta{AddSymbols: []Symbol{added}})
	if err != nil {
		t.Fatalf("Apply(add symbol) error = %v", err)
	}
	if result.AddedSymbols != 1 {
		t.Fatalf("Apply(add symbol) = %#v", result)
	}
	assertSymbol(t, reader, added)

	updated := added
	updated.Name = "renamed"
	updated.QualifiedName = "fixture.renamed"
	updated.Signature = "renamed(value string)"
	updated.StartLine = 40
	updated.EndLine = 50
	result, err = writer.Apply(context.Background(), Delta{UpdateSymbols: []Symbol{updated}})
	if err != nil {
		t.Fatalf("Apply(update symbol) error = %v", err)
	}
	if result.UpdatedSymbols != 1 {
		t.Fatalf("Apply(update symbol) = %#v", result)
	}
	assertSymbol(t, reader, updated)

	toHub := mutationReference("s8", "s0", ReferenceKindReferences)
	fromHub := mutationReference("s0", "s8", ReferenceKindCallsDirect)
	result, err = writer.Apply(context.Background(), Delta{AddReferences: []Reference{toHub, fromHub}})
	if err != nil {
		t.Fatalf("Apply(add references) error = %v", err)
	}
	if result.AddedReferences != 2 {
		t.Fatalf("Apply(add references) = %#v", result)
	}
	assertReferencePresent(t, reader, toHub)
	assertReferencePresent(t, reader, fromHub)

	result, err = writer.Apply(context.Background(), Delta{DeleteReferences: []ReferenceKey{toHub.key()}})
	if err != nil {
		t.Fatalf("Apply(delete reference) error = %v", err)
	}
	if result.DeletedReferences != 1 {
		t.Fatalf("Apply(delete reference) = %#v", result)
	}
	outgoing, err := reader.OutgoingReferences(context.Background(), "s8", 10)
	if err != nil {
		t.Fatalf("OutgoingReferences(s8) error = %v", err)
	}
	if len(outgoing) != 0 {
		t.Fatalf("OutgoingReferences(s8) = %#v, want empty", outgoing)
	}

	replacement := []Reference{
		mutationReference("s0", "s2", ReferenceKindCallsDirect),
		mutationReference("s0", "s8", ReferenceKindReferences),
	}
	result, err = writer.Apply(context.Background(), Delta{ReplaceOutgoing: []OutgoingReplacement{{SourceKey: "s0", References: replacement}}})
	if err != nil {
		t.Fatalf("Apply(replace outgoing) error = %v", err)
	}
	if result.ReplacedSources != 1 || result.AddedReferences != 2 || result.DeletedReferences != 2 {
		t.Fatalf("Apply(replace outgoing) = %#v", result)
	}
	outgoing, err = reader.OutgoingReferences(context.Background(), "s0", 10)
	if err != nil {
		t.Fatalf("OutgoingReferences(s0) error = %v", err)
	}
	wantOutgoing := []Reference{replacement[0], replacement[1]}
	if !reflect.DeepEqual(outgoing, wantOutgoing) {
		t.Fatalf("OutgoingReferences(s0) = %#v, want %#v", outgoing, wantOutgoing)
	}

	if _, err := writer.Apply(context.Background(), Delta{AddReferences: []Reference{replacement[0]}}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("Apply(duplicate reference) error = %v, want ErrAlreadyExists", err)
	}
	outgoing, err = reader.OutgoingReferences(context.Background(), "s0", 10)
	if err != nil || !reflect.DeepEqual(outgoing, wantOutgoing) {
		t.Fatalf("outgoing after duplicate = %#v, error=%v", outgoing, err)
	}

	result, err = writer.Apply(context.Background(), Delta{DeleteSymbolKeys: []string{"s8"}})
	if err != nil {
		t.Fatalf("Apply(delete symbol s8) error = %v", err)
	}
	if result.DeletedSymbols != 1 || result.DeletedReferences != 1 {
		t.Fatalf("Apply(delete symbol s8) = %#v", result)
	}
	if _, found, err := reader.GetSymbol(context.Background(), "s8"); err != nil || found {
		t.Fatalf("GetSymbol(s8) after delete found=%t error=%v", found, err)
	}
	outgoing, err = reader.OutgoingReferences(context.Background(), "s0", 10)
	if err != nil {
		t.Fatalf("OutgoingReferences(s0) after delete error = %v", err)
	}
	if !reflect.DeepEqual(outgoing, []Reference{replacement[0]}) {
		t.Fatalf("OutgoingReferences(s0) after delete = %#v", outgoing)
	}

	result, err = writer.Apply(context.Background(), Delta{DeleteSymbolKeys: []string{"s1"}})
	if err != nil {
		t.Fatalf("Apply(delete defined symbol s1) error = %v", err)
	}
	if result.DeletedSymbols != 1 || result.DeletedReferences != 1 {
		t.Fatalf("Apply(delete defined symbol s1) = %#v", result)
	}
	incoming, err := reader.IncomingReferences(context.Background(), "s2", 10)
	if err != nil {
		t.Fatalf("IncomingReferences(s2) error = %v", err)
	}
	for _, reference := range incoming {
		if reference.SourceKey == "s1" {
			t.Fatalf("ghost reference after deleting s1: %#v", reference)
		}
	}
}

func TestWriterAddsOneThousandSymbolsWithoutDuplicates(t *testing.T) {
	database, reader := newQueryFixture(t)
	writer, err := database.OpenWriter(context.Background())
	if err != nil {
		t.Fatalf("OpenWriter() error = %v", err)
	}
	defer writer.Close()

	symbols := make([]Symbol, 1_000)
	for index := range symbols {
		symbols[index] = mutationSymbol(fmt.Sprintf("incremental-%04d", index), "r0", "f0")
	}
	result, err := writer.Apply(context.Background(), Delta{AddSymbols: symbols})
	if err != nil {
		t.Fatalf("Apply(1000 symbols) error = %v", err)
	}
	if result.AddedSymbols != len(symbols) {
		t.Fatalf("Apply(1000 symbols) = %#v", result)
	}
	assertSymbol(t, reader, symbols[0])
	assertSymbol(t, reader, symbols[len(symbols)-1])
	if _, err := writer.Apply(context.Background(), Delta{AddSymbols: symbols}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("Apply(duplicate batch) error = %v, want ErrAlreadyExists", err)
	}
	assertSymbol(t, reader, symbols[500])
}

func TestWriterDeletesReferencesInBatches(t *testing.T) {
	database, reader := newQueryFixture(t)
	writer, err := database.OpenWriter(context.Background())
	if err != nil {
		t.Fatalf("OpenWriter() error = %v", err)
	}
	defer writer.Close()

	const referenceCount = 100
	symbols := make([]Symbol, referenceCount+1)
	symbols[0] = mutationSymbol("delete-batch-source", "r0", "f0")
	for index := 0; index < referenceCount; index++ {
		symbols[index+1] = mutationSymbol(fmt.Sprintf("delete-batch-target-%03d", index), "r0", "f0")
	}
	if _, err := writer.Apply(context.Background(), Delta{AddSymbols: symbols}); err != nil {
		t.Fatalf("Apply(add symbols) error = %v", err)
	}

	references := make([]Reference, referenceCount)
	keys := make([]ReferenceKey, referenceCount)
	for index := range references {
		kind := ReferenceKindReferences
		if index%2 != 0 {
			kind = ReferenceKindCallsDirect
		}
		references[index] = mutationReference(symbols[0].StableKey, symbols[index+1].StableKey, kind)
		keys[index] = references[index].key()
	}
	if _, err := writer.Apply(context.Background(), Delta{AddReferences: references}); err != nil {
		t.Fatalf("Apply(add references) error = %v", err)
	}
	result, err := writer.Apply(context.Background(), Delta{DeleteReferences: keys})
	if err != nil {
		t.Fatalf("Apply(delete references) error = %v", err)
	}
	if result.DeletedReferences != referenceCount {
		t.Fatalf("Apply(delete references) = %#v", result)
	}
	outgoing, err := reader.OutgoingReferences(context.Background(), symbols[0].StableKey, referenceCount)
	if err != nil {
		t.Fatalf("OutgoingReferences() error = %v", err)
	}
	if len(outgoing) != 0 {
		t.Fatalf("OutgoingReferences() = %d references, want 0", len(outgoing))
	}
}

func TestWriterRollsBackAllChangesAfterLateFailure(t *testing.T) {
	database, reader := newQueryFixture(t)
	writer, err := database.OpenWriter(context.Background())
	if err != nil {
		t.Fatalf("OpenWriter() error = %v", err)
	}
	defer writer.Close()

	symbol := mutationSymbol("must-rollback", "r0", "f0")
	invalidEndpoint := mutationReference(symbol.StableKey, "missing-target", ReferenceKindReferences)
	_, err = writer.Apply(context.Background(), Delta{
		AddSymbols:    []Symbol{symbol},
		AddReferences: []Reference{invalidEndpoint},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Apply(late failure) error = %v, want ErrNotFound", err)
	}
	if _, found, lookupErr := reader.GetSymbol(context.Background(), symbol.StableKey); lookupErr != nil || found {
		t.Fatalf("rolled-back symbol found=%t error=%v", found, lookupErr)
	}
	if _, err := writer.Apply(context.Background(), Delta{AddSymbols: []Symbol{symbol}}); err != nil {
		t.Fatalf("Apply(symbol after rollback) error = %v", err)
	}
	assertSymbol(t, reader, symbol)
}

func TestOpenWriterRejectsReadOnlyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	database, err := Open(context.Background(), path, DefaultConfig())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	config := DefaultConfig()
	config.ReadOnly = true
	readOnly, err := Open(context.Background(), path, config)
	if err != nil {
		t.Fatalf("Open(read-only) error = %v", err)
	}
	defer readOnly.Close()
	if _, err := readOnly.OpenWriter(context.Background()); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("OpenWriter(read-only) error = %v, want ErrReadOnly", err)
	}
}

func mutationSymbol(stableKey, repositoryKey, fileKey string) Symbol {
	return Symbol{
		StableKey: stableKey, RepositoryKey: repositoryKey, FileKey: fileKey,
		Name: stableKey, QualifiedName: "fixture." + stableKey, Kind: "function",
		Signature: stableKey + "()", StartLine: 1, EndLine: 5,
	}
}

func mutationReference(sourceKey, targetKey, kind string) Reference {
	return Reference{
		SourceKey: sourceKey, TargetKey: targetKey, Kind: kind, EvidenceKind: "incremental",
		SourceFileKey: "f0", TargetFileKey: "f0",
	}
}

func assertSymbol(t *testing.T, reader Reader, want Symbol) {
	t.Helper()
	got, found, err := reader.GetSymbol(context.Background(), want.StableKey)
	if err != nil {
		t.Fatalf("GetSymbol(%s) error = %v", want.StableKey, err)
	}
	if !found || !reflect.DeepEqual(got, want) {
		t.Fatalf("GetSymbol(%s) = %#v, found=%t, want %#v", want.StableKey, got, found, want)
	}
}

func assertReferencePresent(t *testing.T, reader Reader, want Reference) {
	t.Helper()
	values, err := reader.OutgoingReferences(context.Background(), want.SourceKey, MaxReferenceResults)
	if err != nil {
		t.Fatalf("OutgoingReferences(%s) error = %v", want.SourceKey, err)
	}
	for _, value := range values {
		if reflect.DeepEqual(value, want) {
			return
		}
	}
	t.Fatalf("reference %#v not found in %#v", want, values)
}
