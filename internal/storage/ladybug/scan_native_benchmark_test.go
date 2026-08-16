//go:build ladybug && cgo

package ladybug

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
)

func BenchmarkReaderScanAll(b *testing.B) {
	databasePath := os.Getenv("KIVGRAPH_LADYBUG_SCAN_DB")
	if databasePath == "" {
		b.Skip("set KIVGRAPH_LADYBUG_SCAN_DB to a populated synthetic LadybugDB database")
	}
	ctx := context.Background()
	database, err := Open(ctx, databasePath, Config{EnableCompression: true, ReadOnly: true})
	if err != nil {
		b.Fatal(err)
	}
	defer database.Close()
	reader, err := database.OpenReader(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer reader.Close()

	b.ReportAllocs()
	b.ResetTimer()
	var rows ScanRows
	for b.Loop() {
		rows, err = reader.ScanAll(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if err := validateScanBenchmarkCounts(rows); err != nil {
		b.Fatal(err)
	}
	if err := validateScanBenchmarkOrdering(rows); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(len(rows.Repositories)+len(rows.Files)+len(rows.Symbols)+len(rows.Edges)), "records")
}

func validateScanBenchmarkCounts(rows ScanRows) error {
	expected := map[string]int{
		"repositories": scanBenchmarkCount("KIVGRAPH_LADYBUG_SCAN_REPOSITORIES", 40),
		"files":        scanBenchmarkCount("KIVGRAPH_LADYBUG_SCAN_FILES", 100000),
		"symbols":      scanBenchmarkCount("KIVGRAPH_LADYBUG_SCAN_SYMBOLS", 100000),
		"edges":        scanBenchmarkCount("KIVGRAPH_LADYBUG_SCAN_EDGES", 1000000),
	}
	actual := map[string]int{
		"repositories": len(rows.Repositories),
		"files":        len(rows.Files),
		"symbols":      len(rows.Symbols),
		"edges":        len(rows.Edges),
	}
	for name, want := range expected {
		if actual[name] != want {
			return &scanCountError{name: name, want: want, got: actual[name]}
		}
	}
	return nil
}

func validateScanBenchmarkOrdering(rows ScanRows) error {
	for index := 1; index < len(rows.Repositories); index++ {
		if rows.Repositories[index].StableKey < rows.Repositories[index-1].StableKey {
			return fmt.Errorf("repositories are not ordered at index %d", index)
		}
	}
	for index := 1; index < len(rows.Files); index++ {
		if rows.Files[index].StableKey < rows.Files[index-1].StableKey {
			return fmt.Errorf("files are not ordered at index %d", index)
		}
	}
	for index := 1; index < len(rows.Symbols); index++ {
		if rows.Symbols[index].StableKey < rows.Symbols[index-1].StableKey {
			return fmt.Errorf("symbols are not ordered at index %d", index)
		}
	}
	for index := 1; index < len(rows.Edges); index++ {
		if scanEdgeLess(rows.Edges[index], rows.Edges[index-1]) {
			return fmt.Errorf("edges are not ordered at index %d", index)
		}
	}
	return nil
}

type scanCountError struct {
	name string
	want int
	got  int
}

func (err *scanCountError) Error() string {
	return err.name + " count = " + strconv.Itoa(err.got) + ", want " + strconv.Itoa(err.want)
}

func scanBenchmarkCount(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return -1
	}
	return count
}
