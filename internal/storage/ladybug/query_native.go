//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
)

const (
	getSymbolQuery = `MATCH (symbol:Symbol)
WHERE symbol.stable_key = $stable_key
RETURN symbol.stable_key, symbol.repository_key, symbol.file_key, symbol.name,
       symbol.qualified_name, symbol.kind, symbol.signature, symbol.start_line, symbol.end_line
LIMIT 1`
	incomingReferencesQuery = `MATCH (source:Symbol)-[edge:REFERENCES|:CALLS_DIRECT]->(target:Symbol)
WHERE target.stable_key = $stable_key
RETURN source.stable_key, target.stable_key, label(edge), edge.evidence_kind,
       edge.source_file_key, edge.target_file_key
ORDER BY source.stable_key, label(edge), edge.evidence_kind, edge.source_file_key, edge.target_file_key
LIMIT $limit`
	outgoingReferencesQuery = `MATCH (source:Symbol)-[edge:REFERENCES|:CALLS_DIRECT]->(target:Symbol)
WHERE source.stable_key = $stable_key
RETURN source.stable_key, target.stable_key, label(edge), edge.evidence_kind,
       edge.source_file_key, edge.target_file_key
ORDER BY target.stable_key, label(edge), edge.evidence_kind, edge.source_file_key, edge.target_file_key
LIMIT $limit`
	incomingByRepositoryQuery = `MATCH (source:Symbol)-[:REFERENCES|:CALLS_DIRECT]->(target:Symbol)
WHERE target.stable_key = $stable_key
RETURN source.repository_key, count(*)
ORDER BY source.repository_key`
)

type queryStatements struct {
	getSymbol            *lbug.PreparedStatement
	incomingReferences   *lbug.PreparedStatement
	outgoingReferences   *lbug.PreparedStatement
	incomingByRepository *lbug.PreparedStatement
	traversal            [MaxTraversalDepth + 1]*lbug.PreparedStatement
	shortestPath         [MaxTraversalDepth + 1]*lbug.PreparedStatement
}

type reader struct {
	parent     *database
	mu         sync.Mutex
	native     *lbug.Connection
	statements queryStatements
	closed     bool
}

func (db *database) OpenReader(ctx context.Context) (Reader, error) {
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: "open reader", Err: err}
	}

	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return nil, &Error{Op: "open reader", Err: ErrClosed}
	}
	native, err := lbug.OpenConnection(db.native)
	if err != nil {
		return nil, &Error{Op: "open reader", Err: err}
	}
	statements, err := prepareQueryStatements(native)
	if err != nil {
		native.Close()
		return nil, &Error{Op: "open reader", Err: err}
	}
	if err := ctx.Err(); err != nil {
		statements.close()
		native.Close()
		return nil, &Error{Op: "open reader", Err: err}
	}
	result := &reader{parent: db, native: native, statements: statements}
	if db.readers == nil {
		db.readers = make(map[*reader]struct{})
	}
	db.readers[result] = struct{}{}
	return result, nil
}

func prepareQueryStatements(connection *lbug.Connection) (queryStatements, error) {
	statements := queryStatements{}
	fixed := []struct {
		query  string
		target **lbug.PreparedStatement
	}{
		{getSymbolQuery, &statements.getSymbol},
		{incomingReferencesQuery, &statements.incomingReferences},
		{outgoingReferencesQuery, &statements.outgoingReferences},
		{incomingByRepositoryQuery, &statements.incomingByRepository},
	}
	for _, item := range fixed {
		prepared, err := connection.Prepare(item.query)
		if err != nil {
			statements.close()
			return queryStatements{}, err
		}
		*item.target = prepared
	}
	for depth := 1; depth <= MaxTraversalDepth; depth++ {
		traversalQuery := fmt.Sprintf(`MATCH (source:Symbol)-[path:REFERENCES|:CALLS_DIRECT* SHORTEST 1..%d]->(target:Symbol)
WHERE source.stable_key = $stable_key
RETURN target.stable_key, length(path)
ORDER BY length(path), target.stable_key
LIMIT $limit`, depth)
		prepared, err := connection.Prepare(traversalQuery)
		if err != nil {
			statements.close()
			return queryStatements{}, fmt.Errorf("prepare traversal depth %d: %w", depth, err)
		}
		statements.traversal[depth] = prepared

		shortestQuery := fmt.Sprintf(`MATCH path = (source:Symbol)-[:REFERENCES|:CALLS_DIRECT* SHORTEST 1..%d]->(target:Symbol)
WHERE source.stable_key = $source_key AND target.stable_key = $target_key
RETURN properties(nodes(path), 'stable_key'), length(path)
LIMIT 1`, depth)
		prepared, err = connection.Prepare(shortestQuery)
		if err != nil {
			statements.close()
			return queryStatements{}, fmt.Errorf("prepare shortest path depth %d: %w", depth, err)
		}
		statements.shortestPath[depth] = prepared
	}
	return statements, nil
}

func (statements *queryStatements) close() {
	for _, statement := range []*lbug.PreparedStatement{
		statements.getSymbol,
		statements.incomingReferences,
		statements.outgoingReferences,
		statements.incomingByRepository,
	} {
		if statement != nil {
			statement.Close()
		}
	}
	for depth := 1; depth <= MaxTraversalDepth; depth++ {
		if statements.traversal[depth] != nil {
			statements.traversal[depth].Close()
		}
		if statements.shortestPath[depth] != nil {
			statements.shortestPath[depth].Close()
		}
	}
}

func (reader *reader) Close() error {
	reader.parent.mu.Lock()
	defer reader.parent.mu.Unlock()
	reader.mu.Lock()
	defer reader.mu.Unlock()
	delete(reader.parent.readers, reader)
	reader.closeNative()
	return nil
}

func (reader *reader) closeNative() {
	if reader.closed {
		return
	}
	reader.statements.close()
	reader.native.Close()
	reader.closed = true
}

func (reader *reader) GetSymbol(ctx context.Context, stableKey string) (Symbol, bool, error) {
	if err := validateStableKey(stableKey); err != nil {
		return Symbol{}, false, &Error{Op: "get symbol", Err: err}
	}
	result, err := reader.execute(ctx, "get symbol", reader.statements.getSymbol, map[string]any{"stable_key": stableKey})
	if err != nil {
		return Symbol{}, false, err
	}
	defer reader.finish(result)
	if !result.HasNext() {
		return Symbol{}, false, nil
	}
	tuple, err := nextTuple(result)
	if err != nil {
		return Symbol{}, false, &Error{Op: "get symbol", Err: err}
	}
	defer tuple.Close()
	symbol, err := decodeSymbol(tuple)
	if err != nil {
		return Symbol{}, false, &Error{Op: "get symbol", Err: err}
	}
	return symbol, true, nil
}

func (reader *reader) IncomingReferences(ctx context.Context, stableKey string, limit int) ([]Reference, error) {
	return reader.references(ctx, "incoming references", reader.statements.incomingReferences, stableKey, limit)
}

func (reader *reader) OutgoingReferences(ctx context.Context, stableKey string, limit int) ([]Reference, error) {
	return reader.references(ctx, "outgoing references", reader.statements.outgoingReferences, stableKey, limit)
}

func (reader *reader) references(ctx context.Context, operation string, statement *lbug.PreparedStatement, stableKey string, limit int) ([]Reference, error) {
	if err := validateStableKey(stableKey); err != nil {
		return nil, &Error{Op: operation, Err: err}
	}
	if err := validateReferenceLimit(limit); err != nil {
		return nil, &Error{Op: operation, Err: err}
	}
	result, err := reader.execute(ctx, operation, statement, map[string]any{"stable_key": stableKey, "limit": int64(limit)})
	if err != nil {
		return nil, err
	}
	defer reader.finish(result)
	references := make([]Reference, 0, min(limit, 16))
	for result.HasNext() {
		tuple, err := nextTuple(result)
		if err != nil {
			return nil, &Error{Op: operation, Err: err}
		}
		reference, decodeErr := decodeReference(tuple)
		tuple.Close()
		if decodeErr != nil {
			return nil, &Error{Op: operation, Err: decodeErr}
		}
		references = append(references, reference)
	}
	return references, nil
}

func (reader *reader) TraverseOutgoing(ctx context.Context, stableKey string, depth, limit int) ([]TraversalNode, error) {
	if err := validateStableKey(stableKey); err != nil {
		return nil, &Error{Op: "traverse outgoing", Err: err}
	}
	if err := validateTraversal(depth, limit); err != nil {
		return nil, &Error{Op: "traverse outgoing", Err: err}
	}
	result, err := reader.execute(ctx, "traverse outgoing", reader.statements.traversal[depth], map[string]any{"stable_key": stableKey, "limit": int64(limit)})
	if err != nil {
		return nil, err
	}
	defer reader.finish(result)
	nodes := make([]TraversalNode, 0, min(limit, 32))
	for result.HasNext() {
		tuple, err := nextTuple(result)
		if err != nil {
			return nil, &Error{Op: "traverse outgoing", Err: err}
		}
		stableKey, keyErr := tupleString(tuple, 0)
		depthValue, depthErr := tupleInt64(tuple, 1)
		tuple.Close()
		if err := errors.Join(keyErr, depthErr); err != nil {
			return nil, &Error{Op: "traverse outgoing", Err: err}
		}
		nodes = append(nodes, TraversalNode{StableKey: stableKey, Depth: int(depthValue)})
	}
	return nodes, nil
}

func (reader *reader) ShortestPath(ctx context.Context, sourceKey, targetKey string, maxDepth int) (Path, bool, error) {
	if err := validatePathQuery(sourceKey, targetKey, maxDepth); err != nil {
		return Path{}, false, &Error{Op: "shortest path", Err: err}
	}
	result, err := reader.execute(ctx, "shortest path", reader.statements.shortestPath[maxDepth], map[string]any{
		"source_key": sourceKey,
		"target_key": targetKey,
	})
	if err != nil {
		return Path{}, false, err
	}
	defer reader.finish(result)
	if !result.HasNext() {
		return Path{}, false, nil
	}
	tuple, err := nextTuple(result)
	if err != nil {
		return Path{}, false, &Error{Op: "shortest path", Err: err}
	}
	defer tuple.Close()
	values, err := tupleStrings(tuple, 0)
	if err != nil {
		return Path{}, false, &Error{Op: "shortest path", Err: err}
	}
	length, err := tupleInt64(tuple, 1)
	if err != nil {
		return Path{}, false, &Error{Op: "shortest path", Err: err}
	}
	if len(values) != int(length)+1 {
		return Path{}, false, &Error{Op: "shortest path", Err: fmt.Errorf("path has %d keys for length %d", len(values), length)}
	}
	return Path{StableKeys: values, Length: int(length)}, true, nil
}

func (reader *reader) IncomingReferencesByRepository(ctx context.Context, stableKey string) ([]RepositoryReferenceCount, error) {
	if err := validateStableKey(stableKey); err != nil {
		return nil, &Error{Op: "group incoming references", Err: err}
	}
	result, err := reader.execute(ctx, "group incoming references", reader.statements.incomingByRepository, map[string]any{"stable_key": stableKey})
	if err != nil {
		return nil, err
	}
	defer reader.finish(result)
	groups := make([]RepositoryReferenceCount, 0, 8)
	for result.HasNext() {
		tuple, err := nextTuple(result)
		if err != nil {
			return nil, &Error{Op: "group incoming references", Err: err}
		}
		repositoryKey, keyErr := tupleString(tuple, 0)
		count, countErr := tupleInt64(tuple, 1)
		tuple.Close()
		if err := errors.Join(keyErr, countErr); err != nil {
			return nil, &Error{Op: "group incoming references", Err: err}
		}
		groups = append(groups, RepositoryReferenceCount{RepositoryKey: repositoryKey, Count: count})
	}
	return groups, nil
}

func (reader *reader) execute(ctx context.Context, operation string, statement *lbug.PreparedStatement, arguments map[string]any) (*lbug.QueryResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: operation, Err: err}
	}
	reader.mu.Lock()
	if reader.closed {
		reader.mu.Unlock()
		return nil, &Error{Op: operation, Err: ErrClosed}
	}
	if err := setQueryDeadline(reader.native, ctx); err != nil {
		reader.mu.Unlock()
		return nil, &Error{Op: operation, Err: err}
	}
	result, err := reader.native.Execute(statement, arguments)
	if err != nil {
		if result != nil {
			result.Close()
		}
		reader.native.SetTimeout(0)
		reader.mu.Unlock()
		return nil, &Error{Op: operation, Err: err}
	}
	if err := ctx.Err(); err != nil {
		result.Close()
		reader.native.SetTimeout(0)
		reader.mu.Unlock()
		return nil, &Error{Op: operation, Err: err}
	}
	return result, nil
}

func (reader *reader) finish(result *lbug.QueryResult) {
	result.Close()
	reader.native.SetTimeout(0)
	reader.mu.Unlock()
}

func setQueryDeadline(connection *lbug.Connection, ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		connection.SetTimeout(0)
		return nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	milliseconds := (remaining + time.Millisecond - 1) / time.Millisecond
	connection.SetTimeout(uint64(milliseconds))
	return nil
}

func decodeSymbol(tuple *lbug.FlatTuple) (Symbol, error) {
	values := [7]string{}
	var decodeErrors []error
	for index := range values {
		value, err := tupleString(tuple, uint64(index))
		values[index] = value
		if err != nil {
			decodeErrors = append(decodeErrors, err)
		}
	}
	startLine, err := tupleInt64(tuple, 7)
	if err != nil {
		decodeErrors = append(decodeErrors, err)
	}
	endLine, err := tupleInt64(tuple, 8)
	if err != nil {
		decodeErrors = append(decodeErrors, err)
	}
	if err := errors.Join(decodeErrors...); err != nil {
		return Symbol{}, err
	}
	return Symbol{
		StableKey:     values[0],
		RepositoryKey: values[1],
		FileKey:       values[2],
		Name:          values[3],
		QualifiedName: values[4],
		Kind:          values[5],
		Signature:     values[6],
		StartLine:     startLine,
		EndLine:       endLine,
	}, nil
}

func decodeReference(tuple *lbug.FlatTuple) (Reference, error) {
	values := [6]string{}
	var decodeErrors []error
	for index := range values {
		value, err := tupleString(tuple, uint64(index))
		values[index] = value
		if err != nil {
			decodeErrors = append(decodeErrors, err)
		}
	}
	if err := errors.Join(decodeErrors...); err != nil {
		return Reference{}, err
	}
	return Reference{
		SourceKey:     values[0],
		TargetKey:     values[1],
		Kind:          values[2],
		EvidenceKind:  values[3],
		SourceFileKey: values[4],
		TargetFileKey: values[5],
	}, nil
}

func nextTuple(result *lbug.QueryResult) (*lbug.FlatTuple, error) {
	tuple, err := result.Next()
	if err != nil && tuple != nil {
		tuple.Close()
	}
	return tuple, err
}

func tupleString(tuple *lbug.FlatTuple, index uint64) (string, error) {
	value, err := tuple.GetValue(index)
	if err != nil {
		return "", err
	}
	result, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("column %d has type %T, want string", index, value)
	}
	return result, nil
}

func tupleInt64(tuple *lbug.FlatTuple, index uint64) (int64, error) {
	value, err := tuple.GetValue(index)
	if err != nil {
		return 0, err
	}
	result, ok := value.(int64)
	if !ok {
		return 0, fmt.Errorf("column %d has type %T, want int64", index, value)
	}
	return result, nil
}

func tupleStrings(tuple *lbug.FlatTuple, index uint64) ([]string, error) {
	value, err := tuple.GetValue(index)
	if err != nil {
		return nil, err
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("column %d has type %T, want list", index, value)
	}
	result := make([]string, len(items))
	for itemIndex, item := range items {
		stringValue, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("column %d item %d has type %T, want string", index, itemIndex, item)
		}
		result[itemIndex] = stringValue
	}
	return result, nil
}
