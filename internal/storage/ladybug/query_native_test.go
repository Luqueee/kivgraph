//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	lbug "github.com/LadybugDB/go-ladybug"
)

func TestReaderQueriesSyntheticGraph(t *testing.T) {
	database, reader := newQueryFixture(t)

	symbol, found, err := reader.GetSymbol(context.Background(), "s3")
	if err != nil {
		t.Fatalf("GetSymbol() error = %v", err)
	}
	if !found || symbol.StableKey != "s3" || symbol.RepositoryKey != "r0" || symbol.FileKey != "f3" || symbol.StartLine != 4 || symbol.EndLine != 8 {
		t.Fatalf("GetSymbol() = %#v, %t", symbol, found)
	}
	if _, found, err := reader.GetSymbol(context.Background(), "missing"); err != nil || found {
		t.Fatalf("GetSymbol(missing) found=%t error=%v", found, err)
	}

	incoming, err := reader.IncomingReferences(context.Background(), "s0", 10)
	if err != nil {
		t.Fatalf("IncomingReferences() error = %v", err)
	}
	wantIncoming := []Reference{
		{SourceKey: "s6", TargetKey: "s0", Kind: "REFERENCES", EvidenceKind: "fixture", SourceFileKey: "f6", TargetFileKey: "f0"},
		{SourceKey: "s7", TargetKey: "s0", Kind: "CALLS_DIRECT", EvidenceKind: "fixture", SourceFileKey: "f7", TargetFileKey: "f0"},
	}
	if !reflect.DeepEqual(incoming, wantIncoming) {
		t.Fatalf("IncomingReferences() = %#v, want %#v", incoming, wantIncoming)
	}
	outgoing, err := reader.OutgoingReferences(context.Background(), "s0", 10)
	if err != nil {
		t.Fatalf("OutgoingReferences() error = %v", err)
	}
	wantOutgoing := []Reference{{SourceKey: "s0", TargetKey: "s1", Kind: "REFERENCES", EvidenceKind: "fixture", SourceFileKey: "f0", TargetFileKey: "f1"}}
	if !reflect.DeepEqual(outgoing, wantOutgoing) {
		t.Fatalf("OutgoingReferences() = %#v, want %#v", outgoing, wantOutgoing)
	}

	depthThree, err := reader.TraverseOutgoing(context.Background(), "s0", 3, 100)
	if err != nil {
		t.Fatalf("TraverseOutgoing(depth=3) error = %v", err)
	}
	wantDepthThree := []TraversalNode{{StableKey: "s1", Depth: 1}, {StableKey: "s2", Depth: 2}, {StableKey: "s3", Depth: 3}}
	if !reflect.DeepEqual(depthThree, wantDepthThree) {
		t.Fatalf("TraverseOutgoing(depth=3) = %#v, want %#v", depthThree, wantDepthThree)
	}
	depthFive, err := reader.TraverseOutgoing(context.Background(), "s0", 5, 100)
	if err != nil {
		t.Fatalf("TraverseOutgoing(depth=5) error = %v", err)
	}
	wantDepthFive := []TraversalNode{{StableKey: "s1", Depth: 1}, {StableKey: "s2", Depth: 2}, {StableKey: "s3", Depth: 3}, {StableKey: "s4", Depth: 4}, {StableKey: "s5", Depth: 5}}
	if !reflect.DeepEqual(depthFive, wantDepthFive) {
		t.Fatalf("TraverseOutgoing(depth=5) = %#v, want %#v", depthFive, wantDepthFive)
	}

	if _, found, err := reader.ShortestPath(context.Background(), "s0", "s5", 4); err != nil || found {
		t.Fatalf("ShortestPath(maxDepth=4) found=%t error=%v", found, err)
	}
	path, found, err := reader.ShortestPath(context.Background(), "s0", "s5", 5)
	if err != nil {
		t.Fatalf("ShortestPath(maxDepth=5) error = %v", err)
	}
	wantPath := Path{StableKeys: []string{"s0", "s1", "s2", "s3", "s4", "s5"}, Length: 5}
	if !found || !reflect.DeepEqual(path, wantPath) {
		t.Fatalf("ShortestPath(maxDepth=5) = %#v, %t, want %#v", path, found, wantPath)
	}

	groups, err := reader.IncomingReferencesByRepository(context.Background(), "s0")
	if err != nil {
		t.Fatalf("IncomingReferencesByRepository() error = %v", err)
	}
	wantGroups := []RepositoryReferenceCount{{RepositoryKey: "r1", Count: 2}}
	if !reflect.DeepEqual(groups, wantGroups) {
		t.Fatalf("IncomingReferencesByRepository() = %#v, want %#v", groups, wantGroups)
	}

	if err := database.Close(); err != nil {
		t.Fatalf("Database.Close() error = %v", err)
	}
	if _, _, err := reader.GetSymbol(context.Background(), "s0"); !errors.Is(err, ErrClosed) {
		t.Fatalf("GetSymbol() after Database.Close() error = %v, want ErrClosed", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Reader.Close() after Database.Close() error = %v", err)
	}
}

func TestReaderValidatesInputsAndContext(t *testing.T) {
	_, reader := newQueryFixture(t)
	ctx := context.Background()

	if _, _, err := reader.GetSymbol(ctx, ""); !errors.Is(err, ErrInvalidStableKey) {
		t.Fatalf("GetSymbol(empty) error = %v, want ErrInvalidStableKey", err)
	}
	if _, err := reader.IncomingReferences(ctx, "s0", 0); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("IncomingReferences(limit=0) error = %v, want ErrInvalidLimit", err)
	}
	if _, err := reader.OutgoingReferences(ctx, "s0", MaxReferenceResults+1); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("OutgoingReferences(excessive limit) error = %v, want ErrInvalidLimit", err)
	}
	if _, err := reader.TraverseOutgoing(ctx, "s0", MaxTraversalDepth+1, 10); !errors.Is(err, ErrInvalidDepth) {
		t.Fatalf("TraverseOutgoing(excessive depth) error = %v, want ErrInvalidDepth", err)
	}
	if _, _, err := reader.ShortestPath(ctx, "s0", "s1", 0); !errors.Is(err, ErrInvalidDepth) {
		t.Fatalf("ShortestPath(depth=0) error = %v, want ErrInvalidDepth", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := reader.GetSymbol(canceled, "s0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetSymbol(canceled) error = %v, want context.Canceled", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("Reader.Close() error = %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("second Reader.Close() error = %v", err)
	}
}

func TestReaderSerializesConcurrentQueries(t *testing.T) {
	_, reader := newQueryFixture(t)
	const workers = 8
	failures := make(chan error, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for worker := range workers {
		go func() {
			defer wait.Done()
			for iteration := range 10 {
				key := fmt.Sprintf("s%d", (worker+iteration)%8)
				symbol, found, err := reader.GetSymbol(context.Background(), key)
				if err != nil {
					failures <- err
					return
				}
				if !found || symbol.StableKey != key {
					failures <- fmt.Errorf("GetSymbol(%s) = %#v, found=%t", key, symbol, found)
					return
				}
			}
		}()
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

func newQueryFixture(t *testing.T) (Database, Reader) {
	t.Helper()
	opened, err := Open(context.Background(), filepath.Join(t.TempDir(), "graph.db"), DefaultConfig())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	database := opened.(*database)
	t.Cleanup(func() { _ = database.Close() })
	connection, err := lbug.OpenConnection(database.native)
	if err != nil {
		t.Fatalf("OpenConnection() error = %v", err)
	}
	defer connection.Close()

	for _, query := range []string{
		"CREATE NODE TABLE File(stable_key STRING PRIMARY KEY, repository_key STRING, path STRING, content_hash STRING, language STRING)",
		"CREATE NODE TABLE Symbol(stable_key STRING PRIMARY KEY, repository_key STRING, file_key STRING, name STRING, qualified_name STRING, kind STRING, signature STRING, start_line INT64, end_line INT64)",
		"CREATE REL TABLE DEFINES(FROM File TO Symbol, relation_kind STRING)",
		"CREATE REL TABLE REFERENCES(FROM Symbol TO Symbol, evidence_kind STRING, source_file_key STRING, target_file_key STRING)",
		"CREATE REL TABLE CALLS_DIRECT(FROM Symbol TO Symbol, evidence_kind STRING, source_file_key STRING, target_file_key STRING)",
	} {
		mustExecuteQuery(t, connection, query)
	}

	createFile, err := connection.Prepare("CREATE (:File {stable_key: $stable_key, repository_key: $repository_key, path: $path, content_hash: $content_hash, language: $language})")
	if err != nil {
		t.Fatalf("prepare file: %v", err)
	}
	defer createFile.Close()
	createSymbol, err := connection.Prepare("CREATE (:Symbol {stable_key: $stable_key, repository_key: $repository_key, file_key: $file_key, name: $name, qualified_name: $qualified_name, kind: $kind, signature: $signature, start_line: $start_line, end_line: $end_line})")
	if err != nil {
		t.Fatalf("prepare symbol: %v", err)
	}
	defer createSymbol.Close()
	defines, err := connection.Prepare("MATCH (file:File {stable_key: $file_key}), (symbol:Symbol {stable_key: $symbol_key}) CREATE (file)-[:DEFINES {relation_kind: 'file_symbol'}]->(symbol)")
	if err != nil {
		t.Fatalf("prepare DEFINES: %v", err)
	}
	defer defines.Close()
	for index := range 8 {
		key := fmt.Sprintf("s%d", index)
		repository := "r0"
		if index >= 6 {
			repository = "r1"
		}
		mustExecutePrepared(t, connection, createFile, map[string]any{
			"stable_key": fmt.Sprintf("f%d", index), "repository_key": repository,
			"path": fmt.Sprintf("/fixture/f%d", index), "content_hash": fmt.Sprintf("hash-%d", index), "language": "go",
		})
		mustExecutePrepared(t, connection, createSymbol, map[string]any{
			"stable_key": key, "repository_key": repository, "file_key": fmt.Sprintf("f%d", index),
			"name": key, "qualified_name": "fixture." + key, "kind": "function", "signature": key + "()",
			"start_line": int64(index + 1), "end_line": int64(index + 5),
		})
		mustExecutePrepared(t, connection, defines, map[string]any{
			"file_key": fmt.Sprintf("f%d", index), "symbol_key": key,
		})
	}

	reference, err := connection.Prepare("MATCH (source:Symbol {stable_key: $from}), (target:Symbol {stable_key: $to}) CREATE (source)-[:REFERENCES {evidence_kind: 'fixture', source_file_key: $source_file_key, target_file_key: $target_file_key}]->(target)")
	if err != nil {
		t.Fatalf("prepare REFERENCES: %v", err)
	}
	defer reference.Close()
	callsDirect, err := connection.Prepare("MATCH (source:Symbol {stable_key: $from}), (target:Symbol {stable_key: $to}) CREATE (source)-[:CALLS_DIRECT {evidence_kind: 'fixture', source_file_key: $source_file_key, target_file_key: $target_file_key}]->(target)")
	if err != nil {
		t.Fatalf("prepare CALLS_DIRECT: %v", err)
	}
	defer callsDirect.Close()
	edges := []struct {
		from, to string
		calls    bool
	}{
		{"s0", "s1", false},
		{"s1", "s2", true},
		{"s2", "s3", false},
		{"s3", "s4", true},
		{"s4", "s5", false},
		{"s6", "s0", false},
		{"s7", "s0", true},
	}
	for _, edge := range edges {
		statement := reference
		if edge.calls {
			statement = callsDirect
		}
		mustExecutePrepared(t, connection, statement, map[string]any{
			"from": edge.from, "to": edge.to,
			"source_file_key": "f" + edge.from[1:], "target_file_key": "f" + edge.to[1:],
		})
	}

	queryReader, err := database.OpenReader(context.Background())
	if err != nil {
		t.Fatalf("OpenReader() error = %v", err)
	}
	t.Cleanup(func() { _ = queryReader.Close() })
	return database, queryReader
}

func mustExecuteQuery(t *testing.T, connection *lbug.Connection, query string) {
	t.Helper()
	result, err := connection.Query(query)
	if result != nil {
		result.Close()
	}
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
}

func mustExecutePrepared(t *testing.T, connection *lbug.Connection, statement *lbug.PreparedStatement, arguments map[string]any) {
	t.Helper()
	result, err := connection.Execute(statement, arguments)
	if result != nil {
		result.Close()
	}
	if err != nil {
		t.Fatalf("execute prepared statement: %v", err)
	}
}
