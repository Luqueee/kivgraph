//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"fmt"
	"sync"

	lbug "github.com/LadybugDB/go-ladybug"
)

const (
	countSymbolsQuery = `UNWIND $keys AS key
MATCH (symbol:Symbol)
WHERE symbol.stable_key = key
RETURN count(*)`
	addSymbolsQuery = `UNWIND $rows AS row
CREATE (:Symbol {stable_key: row.stable_key, repository_key: row.repository_key,
 file_key: row.file_key, name: row.name, qualified_name: row.qualified_name,
 kind: row.kind, signature: row.signature, start_line: row.start_line, end_line: row.end_line})
RETURN count(*)`
	updateSymbolsQuery = `UNWIND $rows AS row
MATCH (symbol:Symbol)
WHERE symbol.stable_key = row.stable_key
SET symbol.repository_key = row.repository_key,
    symbol.file_key = row.file_key,
    symbol.name = row.name,
    symbol.qualified_name = row.qualified_name,
    symbol.kind = row.kind,
    symbol.signature = row.signature,
    symbol.start_line = row.start_line,
    symbol.end_line = row.end_line
RETURN count(*)`
	deleteSymbolsQuery = `UNWIND $keys AS key
MATCH (symbol:Symbol)
WHERE symbol.stable_key = key
DELETE symbol
RETURN count(*)`
	deleteDefinesQuery = `UNWIND $keys AS key
MATCH (:File)-[edge:DEFINES]->(symbol:Symbol)
WHERE symbol.stable_key = key
DELETE edge
RETURN count(*)`
)

type mutationStatements struct {
	countSymbols  *lbug.PreparedStatement
	addSymbols    *lbug.PreparedStatement
	updateSymbols *lbug.PreparedStatement
	deleteSymbols *lbug.PreparedStatement
	deleteDefines *lbug.PreparedStatement
	references    [2]referenceMutationStatements
}

type referenceMutationStatements struct {
	count          *lbug.PreparedStatement
	countBatch     *lbug.PreparedStatement
	add            *lbug.PreparedStatement
	delete         *lbug.PreparedStatement
	deleteOutgoing *lbug.PreparedStatement
	deleteIncoming *lbug.PreparedStatement
}

type writer struct {
	parent     *database
	mu         sync.Mutex
	native     *lbug.Connection
	statements mutationStatements
	closed     bool
}

func (db *database) OpenWriter(ctx context.Context) (Writer, error) {
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: "open writer", Err: err}
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return nil, &Error{Op: "open writer", Err: ErrClosed}
	}
	if db.readOnly {
		return nil, &Error{Op: "open writer", Err: ErrReadOnly}
	}
	if db.writer != nil {
		return nil, &Error{Op: "open writer", Err: ErrWriterOpen}
	}
	native, err := lbug.OpenConnection(db.native)
	if err != nil {
		return nil, &Error{Op: "open writer", Err: err}
	}
	statements, err := prepareMutationStatements(native)
	if err != nil {
		native.Close()
		return nil, &Error{Op: "open writer", Err: err}
	}
	if err := ctx.Err(); err != nil {
		statements.close()
		native.Close()
		return nil, &Error{Op: "open writer", Err: err}
	}
	result := &writer{parent: db, native: native, statements: statements}
	db.writer = result
	return result, nil
}

func prepareMutationStatements(connection *lbug.Connection) (mutationStatements, error) {
	statements := mutationStatements{}
	fixed := []struct {
		name   string
		query  string
		target **lbug.PreparedStatement
	}{
		{"count symbols", countSymbolsQuery, &statements.countSymbols},
		{"add symbols", addSymbolsQuery, &statements.addSymbols},
		{"update symbols", updateSymbolsQuery, &statements.updateSymbols},
		{"delete symbols", deleteSymbolsQuery, &statements.deleteSymbols},
		{"delete defines", deleteDefinesQuery, &statements.deleteDefines},
	}
	for _, item := range fixed {
		prepared, err := connection.Prepare(item.query)
		if err != nil {
			statements.close()
			return mutationStatements{}, fmt.Errorf("prepare %s: %w", item.name, err)
		}
		*item.target = prepared
	}
	for index, kind := range []string{ReferenceKindReferences, ReferenceKindCallsDirect} {
		queries := []struct {
			name   string
			query  string
			target **lbug.PreparedStatement
		}{
			{"count", fmt.Sprintf(`MATCH (source:Symbol)-[edge:%s]->(target:Symbol)
WHERE source.stable_key = $source_key AND target.stable_key = $target_key
RETURN count(*)`, kind), &statements.references[index].count},
			{"count batch", fmt.Sprintf(`UNWIND $rows AS row
MATCH (source:Symbol)-[edge:%s]->(target:Symbol)
WHERE source.stable_key = row.source_key AND target.stable_key = row.target_key
RETURN count(*)`, kind), &statements.references[index].countBatch},
			{"add", fmt.Sprintf(`UNWIND $rows AS row
MATCH (source:Symbol), (target:Symbol)
WHERE source.stable_key = row.source_key AND target.stable_key = row.target_key
CREATE (source)-[:%s {evidence_kind: row.evidence_kind,
 source_file_key: row.source_file_key, target_file_key: row.target_file_key}]->(target)
RETURN count(*)`, kind), &statements.references[index].add},
			{"delete", fmt.Sprintf(`MATCH (source:Symbol)-[edge:%s]->(target:Symbol)
WHERE source.stable_key = $source_key AND target.stable_key = $target_key
DELETE edge
RETURN count(*)`, kind), &statements.references[index].delete},
			{"delete outgoing", fmt.Sprintf(`UNWIND $keys AS key
MATCH (source:Symbol)-[edge:%s]->()
WHERE source.stable_key = key
DELETE edge
RETURN count(*)`, kind), &statements.references[index].deleteOutgoing},
			{"delete incoming", fmt.Sprintf(`UNWIND $keys AS key
MATCH ()-[edge:%s]->(target:Symbol)
WHERE target.stable_key = key
DELETE edge
RETURN count(*)`, kind), &statements.references[index].deleteIncoming},
		}
		for _, item := range queries {
			prepared, err := connection.Prepare(item.query)
			if err != nil {
				statements.close()
				return mutationStatements{}, fmt.Errorf("prepare %s %s: %w", kind, item.name, err)
			}
			*item.target = prepared
		}
	}
	return statements, nil
}

func (statements *mutationStatements) close() {
	for _, statement := range []*lbug.PreparedStatement{
		statements.countSymbols,
		statements.addSymbols,
		statements.updateSymbols,
		statements.deleteSymbols,
		statements.deleteDefines,
	} {
		if statement != nil {
			statement.Close()
		}
	}
	for index := range statements.references {
		reference := &statements.references[index]
		for _, statement := range []*lbug.PreparedStatement{reference.count, reference.countBatch, reference.add, reference.delete, reference.deleteOutgoing, reference.deleteIncoming} {
			if statement != nil {
				statement.Close()
			}
		}
	}
}

func (writer *writer) Close() error {
	writer.parent.mu.Lock()
	defer writer.parent.mu.Unlock()
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.parent.writer == writer {
		writer.parent.writer = nil
	}
	writer.closeNative()
	return nil
}

func (writer *writer) closeNative() {
	if writer.closed {
		return
	}
	writer.statements.close()
	writer.native.Close()
	writer.closed = true
}

func (writer *writer) Apply(ctx context.Context, delta Delta) (mutationResult MutationResult, returnErr error) {
	if err := validateDelta(delta); err != nil {
		return MutationResult{}, &Error{Op: "apply delta", Err: err}
	}
	if err := ctx.Err(); err != nil {
		return MutationResult{}, &Error{Op: "apply delta", Err: err}
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.closed {
		return MutationResult{}, &Error{Op: "apply delta", Err: ErrClosed}
	}
	if err := writer.transactionQuery(ctx, "BEGIN TRANSACTION"); err != nil {
		return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("begin: %w", err)}
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := writer.rollback(); rollbackErr != nil {
			returnErr = errors.Join(returnErr, &Error{Op: "apply delta rollback", Err: rollbackErr})
		}
	}()

	deleted, err := writer.deleteReferences(ctx, delta.DeleteReferences)
	if err != nil {
		return MutationResult{}, &Error{Op: "apply delta", Err: err}
	}
	mutationResult.DeletedReferences += deleted

	if len(delta.ReplaceOutgoing) > 0 {
		keys := replacementKeys(delta.ReplaceOutgoing)
		if err := writer.requireSymbols(ctx, keys); err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("replace outgoing: %w", err)}
		}
		deleted, err := writer.deleteAttachedReferences(ctx, keys, true, false)
		if err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("replace outgoing: %w", err)}
		}
		mutationResult.DeletedReferences += deleted
		mutationResult.ReplacedSources = len(keys)
	}

	if len(delta.DeleteSymbolKeys) > 0 {
		if err := writer.requireSymbols(ctx, delta.DeleteSymbolKeys); err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("delete symbols: %w", err)}
		}
		deleted, err := writer.deleteAttachedReferences(ctx, delta.DeleteSymbolKeys, true, true)
		if err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("delete symbol references: %w", err)}
		}
		mutationResult.DeletedReferences += deleted
		if _, err := writer.executeCount(ctx, writer.statements.deleteDefines, map[string]any{"keys": delta.DeleteSymbolKeys}); err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("delete symbol definitions: %w", err)}
		}
	}

	if len(delta.UpdateSymbols) > 0 {
		count, err := writer.executeCount(ctx, writer.statements.updateSymbols, map[string]any{"rows": mutationSymbolRows(delta.UpdateSymbols)})
		if err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("update symbols: %w", err)}
		}
		if count != int64(len(delta.UpdateSymbols)) {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("update symbols: %w: updated %d of %d", ErrNotFound, count, len(delta.UpdateSymbols))}
		}
		mutationResult.UpdatedSymbols = int(count)
	}

	if len(delta.AddSymbols) > 0 {
		keys := symbolKeys(delta.AddSymbols)
		count, err := writer.countSymbols(ctx, keys)
		if err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("check added symbols: %w", err)}
		}
		if count != 0 {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("add symbols: %w: %d keys already exist", ErrAlreadyExists, count)}
		}
		count, err = writer.executeCount(ctx, writer.statements.addSymbols, map[string]any{"rows": mutationSymbolRows(delta.AddSymbols)})
		if err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("add symbols: %w", err)}
		}
		if count != int64(len(delta.AddSymbols)) {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("add symbols: inserted %d of %d", count, len(delta.AddSymbols))}
		}
		mutationResult.AddedSymbols = int(count)
	}

	addedReferences := appendReferences(delta)
	if len(addedReferences) > 0 {
		if err := writer.requireReferencesAbsent(ctx, addedReferences); err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: err}
		}
		count, err := writer.addReferences(ctx, addedReferences)
		if err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: err}
		}
		mutationResult.AddedReferences = count
	}

	if len(delta.DeleteSymbolKeys) > 0 {
		count, err := writer.executeCount(ctx, writer.statements.deleteSymbols, map[string]any{"keys": delta.DeleteSymbolKeys})
		if err != nil {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("delete symbols: %w", err)}
		}
		if count != int64(len(delta.DeleteSymbolKeys)) {
			return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("delete symbols: deleted %d of %d", count, len(delta.DeleteSymbolKeys))}
		}
		mutationResult.DeletedSymbols = int(count)
	}

	if err := writer.transactionQuery(ctx, "COMMIT"); err != nil {
		return MutationResult{}, &Error{Op: "apply delta", Err: fmt.Errorf("commit: %w", err)}
	}
	committed = true
	return mutationResult, nil
}

func (writer *writer) deleteReferences(ctx context.Context, references []ReferenceKey) (int, error) {
	deleted := 0
	for _, reference := range references {
		statements := writer.referenceStatements(reference.Kind)
		count, err := writer.executeCount(ctx, statements.count, map[string]any{"source_key": reference.SourceKey, "target_key": reference.TargetKey})
		if err != nil {
			return 0, fmt.Errorf("check deleted reference %#v: %w", reference, err)
		}
		if count == 0 {
			return 0, fmt.Errorf("delete reference %#v: %w", reference, ErrNotFound)
		}
		count, err = writer.executeCount(ctx, statements.delete, map[string]any{"source_key": reference.SourceKey, "target_key": reference.TargetKey})
		if err != nil {
			return 0, fmt.Errorf("delete reference %#v: %w", reference, err)
		}
		deleted += int(count)
	}
	return deleted, nil
}

func (writer *writer) deleteAttachedReferences(ctx context.Context, keys []string, outgoing, incoming bool) (int, error) {
	deleted := 0
	for index := range writer.statements.references {
		statements := &writer.statements.references[index]
		if outgoing {
			count, err := writer.executeCount(ctx, statements.deleteOutgoing, map[string]any{"keys": keys})
			if err != nil {
				return 0, err
			}
			deleted += int(count)
		}
		if incoming {
			count, err := writer.executeCount(ctx, statements.deleteIncoming, map[string]any{"keys": keys})
			if err != nil {
				return 0, err
			}
			deleted += int(count)
		}
	}
	return deleted, nil
}

func (writer *writer) requireSymbols(ctx context.Context, keys []string) error {
	count, err := writer.countSymbols(ctx, keys)
	if err != nil {
		return err
	}
	if count != int64(len(keys)) {
		return fmt.Errorf("%w: found %d of %d symbols", ErrNotFound, count, len(keys))
	}
	return nil
}

func (writer *writer) countSymbols(ctx context.Context, keys []string) (int64, error) {
	return writer.executeCount(ctx, writer.statements.countSymbols, map[string]any{"keys": keys})
}

func (writer *writer) requireReferencesAbsent(ctx context.Context, references []Reference) error {
	for index, values := range referencesByKind(references) {
		if len(values) == 0 {
			continue
		}
		count, err := writer.executeCount(ctx, writer.statements.references[index].countBatch, map[string]any{"rows": mutationReferenceRows(values)})
		if err != nil {
			return fmt.Errorf("check added %s references: %w", values[0].Kind, err)
		}
		if count != 0 {
			return fmt.Errorf("add %s references: %w: %d relationships already exist", values[0].Kind, ErrAlreadyExists, count)
		}
	}
	return nil
}

func (writer *writer) addReferences(ctx context.Context, references []Reference) (int, error) {
	byKind := referencesByKind(references)
	added := 0
	for index, values := range byKind {
		if len(values) == 0 {
			continue
		}
		count, err := writer.executeCount(ctx, writer.statements.references[index].add, map[string]any{"rows": mutationReferenceRows(values)})
		if err != nil {
			return 0, fmt.Errorf("add %s references: %w", values[0].Kind, err)
		}
		if count != int64(len(values)) {
			return 0, fmt.Errorf("add %s references: %w: inserted %d of %d", values[0].Kind, ErrNotFound, count, len(values))
		}
		added += int(count)
	}
	return added, nil
}

func referencesByKind(references []Reference) [2][]Reference {
	grouped := [2][]Reference{}
	for _, reference := range references {
		index := referenceKindIndex(reference.Kind)
		grouped[index] = append(grouped[index], reference)
	}
	return grouped
}

func (writer *writer) referenceStatements(kind string) *referenceMutationStatements {
	return &writer.statements.references[referenceKindIndex(kind)]
}

func referenceKindIndex(kind string) int {
	if kind == ReferenceKindCallsDirect {
		return 1
	}
	return 0
}

func (writer *writer) executeCount(ctx context.Context, statement *lbug.PreparedStatement, arguments map[string]any) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := setQueryDeadline(writer.native, ctx); err != nil {
		return 0, err
	}
	defer writer.native.SetTimeout(0)
	result, err := writer.native.Execute(statement, arguments)
	if err != nil {
		if result != nil {
			result.Close()
		}
		return 0, err
	}
	defer result.Close()
	if !result.HasNext() {
		return 0, errors.New("mutation query returned no count")
	}
	tuple, err := nextTuple(result)
	if err != nil {
		return 0, err
	}
	defer tuple.Close()
	count, err := tupleInt64(tuple, 0)
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func (writer *writer) transactionQuery(ctx context.Context, query string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := setQueryDeadline(writer.native, ctx); err != nil {
		return err
	}
	defer writer.native.SetTimeout(0)
	result, err := writer.native.Query(query)
	if result != nil {
		result.Close()
	}
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (writer *writer) rollback() error {
	writer.native.SetTimeout(0)
	result, err := writer.native.Query("ROLLBACK")
	if result != nil {
		result.Close()
	}
	return err
}

func symbolKeys(symbols []Symbol) []string {
	keys := make([]string, len(symbols))
	for index, symbol := range symbols {
		keys[index] = symbol.StableKey
	}
	return keys
}

func replacementKeys(replacements []OutgoingReplacement) []string {
	keys := make([]string, len(replacements))
	for index, replacement := range replacements {
		keys[index] = replacement.SourceKey
	}
	return keys
}

func mutationSymbolRows(symbols []Symbol) []any {
	rows := make([]any, len(symbols))
	for index, symbol := range symbols {
		rows[index] = map[string]any{
			"stable_key": symbol.StableKey, "repository_key": symbol.RepositoryKey,
			"file_key": symbol.FileKey, "name": symbol.Name,
			"qualified_name": symbol.QualifiedName, "kind": symbol.Kind,
			"signature": symbol.Signature, "start_line": symbol.StartLine, "end_line": symbol.EndLine,
		}
	}
	return rows
}

func mutationReferenceRows(references []Reference) []any {
	rows := make([]any, len(references))
	for index, reference := range references {
		rows[index] = map[string]any{
			"source_key": reference.SourceKey, "target_key": reference.TargetKey,
			"evidence_kind": reference.EvidenceKind, "source_file_key": reference.SourceFileKey,
			"target_file_key": reference.TargetFileKey,
		}
	}
	return rows
}

func appendReferences(delta Delta) []Reference {
	count := len(delta.AddReferences)
	for _, replacement := range delta.ReplaceOutgoing {
		count += len(replacement.References)
	}
	references := make([]Reference, 0, count)
	references = append(references, delta.AddReferences...)
	for _, replacement := range delta.ReplaceOutgoing {
		references = append(references, replacement.References...)
	}
	return references
}
