// Command unresolved-shape asks what a corpus's unresolved references are.
//
// Every counter this project publishes reports unresolved references as one
// number per language, and one number cannot tell a declared limitation from a
// hidden defect. `rust_unresolved` reads 1969 against 3063 Rust symbols on kena
// -- the highest ratio of the three languages -- and nothing had ever asked what
// those are.
//
// The question is answerable without reading code, because the contract in the
// root AGENTS.md says so: every unresolved reference keeps its reason, its
// repository and its language, and when there is a concrete occurrence it keeps
// the file and position too. This reads exactly those fields out of the
// published canonical graph and groups them.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
)

// group is one bucket of unresolved references.
type group struct {
	Language string `json:"language"`
	Reason   string `json:"reason"`
	Count    int    `json:"count"`
	// Share is the fraction of that language's unresolved references, which is
	// what decides whether a bucket is the story or a rounding error.
	Share float64 `json:"share_of_language"`
	// Examples are a few requested symbols, verbatim. A bucket without them
	// cannot be judged, and judging by reason alone is how a defect hides
	// behind a plausible word.
	Examples []string `json:"examples,omitempty"`
	// Files are where the occurrences sit, most first.
	Files []string `json:"top_files,omitempty"`
	// Details are the loader's own explanation, verbatim. A reason names a
	// class of failure and a detail names the instance, which is the
	// difference between "a package would not build" and knowing which one
	// and why.
	Details []string `json:"details,omitempty"`
}

type results struct {
	Benchmark string           `json:"benchmark"`
	Database  string           `json:"database"`
	Totals    map[string]int   `json:"unresolved_per_language"`
	Symbols   map[string]int   `json:"symbols_per_language"`
	Groups    []group          `json:"groups"`
	Withheld  map[string][]int `json:"-"`
}

func main() {
	var database string
	var language string
	var examples int
	flag.StringVar(&database, "database", "", "path to a published graph.db")
	flag.StringVar(&language, "language", "", "restrict the detail to one language")
	flag.IntVar(&examples, "examples", 6, "requested symbols to quote per group")
	flag.Parse()
	if database == "" {
		fmt.Fprintln(os.Stderr, "unresolved-shape: -database is required")
		os.Exit(2)
	}
	if err := run(context.Background(), database, language, examples); err != nil {
		fmt.Fprintf(os.Stderr, "unresolved-shape: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, database, only string, examples int) error {
	graph, err := ladybug.ScanCanonical(ctx, database)
	if err != nil {
		return fmt.Errorf("scan canonical: %w", err)
	}

	out := results{
		Benchmark: "unresolved-shape",
		Database:  database,
		Totals:    map[string]int{},
		Symbols:   map[string]int{},
	}
	for _, symbol := range graph.Symbols {
		out.Symbols[symbol.Language]++
	}

	type bucket struct {
		count    int
		examples map[string]int
		files    map[string]int
		details  map[string]int
	}
	buckets := map[string]*bucket{}
	for _, reference := range graph.Unresolved {
		out.Totals[reference.Language]++
		if only != "" && reference.Language != only {
			continue
		}
		key := reference.Language + "\x00" + reference.Reason
		entry, found := buckets[key]
		if !found {
			entry = &bucket{examples: map[string]int{}, files: map[string]int{}, details: map[string]int{}}
			buckets[key] = entry
		}
		entry.count++
		// The requested symbol is what a reader needs; the package is what
		// names an external crate, and both are kept when both exist.
		label := reference.RequestedSymbol
		if reference.RequestedPackage != "" {
			label = reference.RequestedPackage + "::" + label
		}
		if label != "" && label != "::" {
			entry.examples[label]++
		}
		if reference.FileKey != "" {
			entry.files[reference.FileKey]++
		}
		if detail := reference.Detail; detail != "" {
			entry.details[detail]++
		}
	}

	for key, entry := range buckets {
		parts := strings.SplitN(key, "\x00", 2)
		total := out.Totals[parts[0]]
		share := 0.0
		if total > 0 {
			share = float64(entry.count) / float64(total)
		}
		out.Groups = append(out.Groups, group{
			Language: parts[0], Reason: parts[1], Count: entry.count,
			Share:    share,
			Examples: topKeys(entry.examples, examples),
			Files:    topKeys(entry.files, 5),
			Details:  topKeys(entry.details, 3),
		})
	}
	sort.Slice(out.Groups, func(i, j int) bool {
		if out.Groups[i].Count != out.Groups[j].Count {
			return out.Groups[i].Count > out.Groups[j].Count
		}
		return out.Groups[i].Reason < out.Groups[j].Reason
	})

	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	fmt.Println(string(encoded))
	for language, count := range out.Totals {
		fmt.Fprintf(os.Stderr, "  %-12s %6d unresolved / %6d symbols\n", language, count, out.Symbols[language])
	}
	for _, entry := range out.Groups {
		fmt.Fprintf(os.Stderr, "  %-10s %-34s %6d  %5.1f %%  %s\n",
			entry.Language, entry.Reason, entry.Count, 100*entry.Share, strings.Join(entry.Examples, ", "))
	}
	return nil
}

// topKeys returns the most frequent keys, most first, capped.
func topKeys(counts map[string]int, limit int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return keys
}
