// Command incremental-cost measures what the delta route could save.
//
// The question this answers is not "how fast is a delta" -- nothing calls the
// delta route, so there is no such measurement to take. It is the prior
// question: the delta route skips exactly one step of a full pass, writing the
// canonical graph from scratch, and still pays every other one. So the ceiling
// on any saving is fixed by what it keeps, and what it keeps is measurable
// today against a real generated database.
//
// It reports the three costs applyDeltaRoute pays on every delta regardless of
// how small the edit was: reading the mutated table counts, refreshing the
// snapshot digest, and rebuilding the complete HotSnapshot. Each one scales with
// the corpus and not with the edit, which is what makes them a ceiling rather
// than an overhead.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
)

type phase struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	Detail  string  `json:"detail,omitempty"`
}

type results struct {
	Benchmark string  `json:"benchmark"`
	Database  string  `json:"database"`
	Bytes     int64   `json:"database_bytes"`
	Repeats   int     `json:"repeats"`
	Phases    []phase `json:"delta_fixed_costs"`
	Total     float64 `json:"delta_fixed_total_seconds"`
	Counts    map[string]int64
}

func main() {
	var database string
	var repeats int
	flag.StringVar(&database, "database", "", "path to a real generated graph.db")
	flag.IntVar(&repeats, "repeats", 3, "how many times to measure each step")
	flag.Parse()

	if database == "" {
		fmt.Fprintln(os.Stderr, "incremental-cost: -database is required")
		os.Exit(2)
	}
	if err := run(context.Background(), database, repeats); err != nil {
		fmt.Fprintf(os.Stderr, "incremental-cost: %v\n", err)
		os.Exit(1)
	}
}

// best keeps the fastest of several runs. A ceiling argued from a slow run is
// not an argument: the question is how cheap this step can be, and the fastest
// observation is the closest honest answer to that.
func best(repeats int, step func() error) (float64, error) {
	fastest := -1.0
	for i := 0; i < repeats; i++ {
		start := time.Now()
		if err := step(); err != nil {
			return 0, err
		}
		elapsed := time.Since(start).Seconds()
		if fastest < 0 || elapsed < fastest {
			fastest = elapsed
		}
	}
	return fastest, nil
}

func run(ctx context.Context, database string, repeats int) error {
	info, err := os.Stat(database)
	if err != nil {
		return fmt.Errorf("stat database: %w", err)
	}
	out := results{
		Benchmark: "delta-route-fixed-costs",
		Database:  database,
		Bytes:     info.Size(),
		Repeats:   repeats,
	}

	var tables map[string]int64
	seconds, err := best(repeats, func() error {
		tables, err = ladybug.CanonicalTableCounts(ctx, database)
		return err
	})
	if err != nil {
		return fmt.Errorf("canonical table counts: %w", err)
	}
	out.Counts = tables
	out.Phases = append(out.Phases, phase{Name: "CanonicalTableCounts", Seconds: seconds})

	generation := filepath.Dir(database)
	seconds, err = best(repeats, func() error {
		_, digestErr := rebuild.RefreshSnapshotDigest(generation, tables)
		return digestErr
	})
	if err != nil {
		return fmt.Errorf("refresh snapshot digest: %w", err)
	}
	out.Phases = append(out.Phases, phase{Name: "RefreshSnapshotDigest", Seconds: seconds})

	var rows string
	seconds, err = best(repeats, func() error {
		_, report, buildErr := rebuild.BuildSnapshot(ctx, rebuild.BuildSnapshotOptions{
			DatabasePath: database,
			SnapshotID:   1,
		})
		if buildErr != nil {
			return buildErr
		}
		if !report.Passed {
			return fmt.Errorf("snapshot build did not pass")
		}
		rows = fmt.Sprintf("%+v", report.Stats)
		return nil
	})
	if err != nil {
		return fmt.Errorf("build snapshot: %w", err)
	}
	out.Phases = append(out.Phases, phase{Name: "BuildSnapshot", Seconds: seconds, Detail: rows})

	for _, entry := range out.Phases {
		out.Total += entry.Seconds
	}

	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	fmt.Println(string(encoded))
	for _, entry := range out.Phases {
		fmt.Fprintf(os.Stderr, "  %-24s %6.3f s  %s\n", entry.Name, entry.Seconds, entry.Detail)
	}
	fmt.Fprintf(os.Stderr, "  %-24s %6.3f s\n", "TOTAL fijo por delta", out.Total)
	return nil
}
