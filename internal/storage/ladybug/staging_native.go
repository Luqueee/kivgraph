//go:build ladybug && cgo

package ladybug

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strings"

	lbug "github.com/LadybugDB/go-ladybug"
)

const (
	stagedReferencesTable  = "STAGED_REFERENCES"
	stagedCallsDirectTable = "STAGED_CALLS_DIRECT"
)

func prepareStagingTables(ctx context.Context, connection *lbug.Connection) error {
	for _, table := range []string{stagedReferencesTable, stagedCallsDirectTable} {
		if err := queryWithDeadline(ctx, connection, fmt.Sprintf("CREATE REL TABLE IF NOT EXISTS %s(FROM Symbol TO Symbol)", table)); err != nil {
			return err
		}
	}
	return nil
}

func queryWithDeadline(ctx context.Context, connection *lbug.Connection, query string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := setQueryDeadline(connection, ctx); err != nil {
		return err
	}
	defer connection.SetTimeout(0)
	result, err := connection.Query(query)
	if result != nil {
		result.Close()
	}
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (writer *writer) copyReferences(ctx context.Context, kind string, references []Reference) error {
	if err := writer.requireSymbols(ctx, referenceEndpointKeys(references)); err != nil {
		return err
	}
	stagingPath, err := writeReferenceCSV(references, false)
	if err != nil {
		return err
	}
	defer os.Remove(stagingPath)
	referencesPath, err := writeReferenceCSV(references, true)
	if err != nil {
		return err
	}
	defer os.Remove(referencesPath)

	stagingTable := stagingTable(kind)
	if err := writer.transactionQuery(ctx, fmt.Sprintf("COPY %s FROM %s", stagingTable, cypherString(stagingPath))); err != nil {
		return err
	}
	count, err := writer.transactionCount(ctx, fmt.Sprintf(`MATCH (source:Symbol)-[:%s]->(target:Symbol)
MATCH (source)-[:%s]->(target)
RETURN count(*)`, stagingTable, kind))
	if err != nil {
		return err
	}
	if count != 0 {
		return fmt.Errorf("add %s references: %w: %d relationships already exist", kind, ErrAlreadyExists, count)
	}
	if err := writer.transactionQuery(ctx, fmt.Sprintf("MATCH ()-[edge:%s]->() DELETE edge", stagingTable)); err != nil {
		return err
	}
	return writer.transactionQuery(ctx, fmt.Sprintf("COPY %s FROM %s", kind, cypherString(referencesPath)))
}

func (writer *writer) transactionCount(ctx context.Context, query string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := setQueryDeadline(writer.native, ctx); err != nil {
		return 0, err
	}
	defer writer.native.SetTimeout(0)
	result, err := writer.native.Query(query)
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
	return tupleInt64(tuple, 0)
}

func writeReferenceCSV(references []Reference, properties bool) (string, error) {
	file, err := os.CreateTemp("", "ladygraph-ladybug-references-*.csv")
	if err != nil {
		return "", err
	}
	path := file.Name()
	writer := csv.NewWriter(file)
	for _, reference := range references {
		row := []string{reference.SourceKey, reference.TargetKey}
		if properties {
			row = append(row, reference.EvidenceKind, reference.SourceFileKey, reference.TargetFileKey)
		}
		if err := writer.Write(row); err != nil {
			_ = file.Close()
			_ = os.Remove(path)
			return "", err
		}
	}
	writer.Flush()
	if err := errors.Join(writer.Error(), file.Close()); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func referenceEndpointKeys(references []Reference) []string {
	keys := make([]string, 0, len(references)*2)
	seen := make(map[string]struct{}, len(references)*2)
	for _, reference := range references {
		for _, key := range [...]string{reference.SourceKey, reference.TargetKey} {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	return keys
}

func referenceAbsenceQuery(kind string, references []Reference) string {
	var query strings.Builder
	query.Grow(len(references) * 96)
	fmt.Fprintf(&query, "MATCH (source:Symbol)-[:%s]->(target:Symbol) WHERE ", kind)
	for index, reference := range references {
		if index > 0 {
			query.WriteString(" OR ")
		}
		fmt.Fprintf(&query, "(source.stable_key = %s AND target.stable_key = %s)", cypherString(reference.SourceKey), cypherString(reference.TargetKey))
	}
	query.WriteString(" RETURN count(*)")
	return query.String()
}

func stagingTable(kind string) string {
	if kind == ReferenceKindCallsDirect {
		return stagedCallsDirectTable
	}
	return stagedReferencesTable
}

func cypherString(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
