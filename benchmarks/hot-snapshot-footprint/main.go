// Command hot-snapshot-footprint answers where the resident bytes of a
// published HotSnapshot go, component by component.
//
// LUQUE-2001. A client launches `kivgraph serve` itself, so there is one server
// per client and each one rebuilds the whole graph in its own private heap. The
// phase that wants to change that has to pick a file format, and picking one
// without knowing what each component costs is picking blind: an ordered index
// and a hash table are a preference until somebody weighs them.
//
// The breakdown is measured two ways on purpose, because neither alone is
// honest. Flat tables and the two CSR arrays are computed analytically --
// unsafe.Sizeof times the element count is exact and needs no instrumentation.
// The four exact indexes and the string arena are observed on the heap, by
// building each one on its own with a forced GC on both sides, because a Go map
// costs what the runtime decides it costs and no arithmetic here can predict it.
//
// The report closes its own budget: the sum of the parts against HeapAlloc with
// the snapshot alive, with the remainder named rather than spread across the
// components that happen to be nearby. A breakdown that does not close is not a
// basis for designing a file.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"time"
	"unsafe"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/rebuild"
)

const defaultDirectory = "benchmarks/hot-snapshot-footprint"

type config struct {
	Graph     string
	Directory string
	// Generation is the generation the caller says it is measuring. The run
	// refuses to publish when the database disagrees, because a footprint
	// labelled with the wrong generation is worse than no footprint.
	Generation string
	Repeat     int
	// HeapProfile names where to write a live-heap profile. The breakdown
	// closes to a remainder and a remainder needs an owner: a profile names
	// the allocation site, which arithmetic over exported types cannot.
	HeapProfile string
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Graph, "graph", "", "path to a published graph.db")
	flag.StringVar(&cfg.Directory, "dir", defaultDirectory, "directory to write results into")
	flag.StringVar(&cfg.Generation, "generation", "", "the generation the caller expects to measure")
	flag.IntVar(&cfg.Repeat, "repeat", 2, "how many passes to run; two is what proves stability")
	flag.StringVar(&cfg.HeapProfile, "heap-profile", "", "write a live-heap pprof profile here, to attribute the remainder")
	flag.Parse()

	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "hot-snapshot-footprint: %v\n", err)
		os.Exit(1)
	}
}

// component is one measured part of the resident snapshot.
type component struct {
	Name string `json:"name"`
	// Method is "analytic" or "heap": which of the two ways this number was
	// obtained, because they carry different error and a reader has to know
	// which one they are trusting.
	Method  string  `json:"method"`
	Bytes   int64   `json:"bytes"`
	Entries int64   `json:"entries"`
	PerUnit float64 `json:"bytes_per_entry"`
}

type pass struct {
	Index         int         `json:"pass"`
	ResidentBytes int64       `json:"resident_bytes"`
	Components    []component `json:"components"`
	AccountedFor  int64       `json:"accounted_for_bytes"`
	Remainder     int64       `json:"remainder_bytes"`
	CoveragePct   float64     `json:"coverage_percent"`
	// PinningBytes is the stable key characters, which are not a component
	// but are what keeps the remainder reachable.
	PinningBytes int64 `json:"stable_key_characters_bytes"`
	ElapsedMS    int64 `json:"elapsed_ms"`
}

type results struct {
	Benchmark   string            `json:"benchmark"`
	Task        string            `json:"task"`
	Date        string            `json:"date"`
	Commit      string            `json:"commit"`
	Generation  string            `json:"generation"`
	GraphPath   string            `json:"graph_path"`
	Environment map[string]string `json:"environment"`
	Counts      map[string]uint64 `json:"counts"`
	Passes      []pass            `json:"passes"`
	// StabilityPct is the largest relative difference between passes of the
	// resident total. The acceptance criterion is one percent.
	StabilityPct float64  `json:"stability_percent"`
	Dominant     []string `json:"dominant_components"`
	Limitations  []string `json:"limitations"`
}

func run(cfg config) error {
	if strings.TrimSpace(cfg.Graph) == "" {
		return fmt.Errorf("--graph is required: point it at a published graph.db")
	}
	if cfg.Repeat < 2 {
		return fmt.Errorf("--repeat must be at least 2: one pass cannot show stability")
	}

	// The generation is read from the path the caller handed over, and checked
	// against what the caller declared. A silent mismatch would publish a
	// footprint under the wrong label.
	observed := filepath.Base(filepath.Dir(cfg.Graph))
	if strings.TrimSpace(cfg.Generation) != "" && observed != cfg.Generation {
		return fmt.Errorf("graph at %s is generation %q, but --generation says %q: refusing to publish",
			cfg.Graph, observed, cfg.Generation)
	}

	out := results{
		Benchmark:  "hot-snapshot-footprint",
		Task:       "LUQUE-2001",
		Date:       time.Now().UTC().Format("2006-01-02"),
		Generation: observed,
		GraphPath:  cfg.Graph,
		Environment: map[string]string{
			"go":   runtime.Version(),
			"os":   runtime.GOOS,
			"arch": runtime.GOARCH,
		},
	}
	if commit, err := currentCommit(); err == nil {
		out.Commit = commit
	}

	for index := 0; index < cfg.Repeat; index++ {
		measured, counts, err := measure(cfg.Graph)
		if err != nil {
			return err
		}
		measured.Index = index + 1
		out.Passes = append(out.Passes, measured)
		if out.Counts == nil {
			out.Counts = counts
		}
	}

	if strings.TrimSpace(cfg.HeapProfile) != "" {
		if err := writeHeapProfile(cfg.Graph, cfg.HeapProfile); err != nil {
			return err
		}
	}

	out.StabilityPct = stability(out.Passes)
	out.Dominant = dominant(out.Passes[0], 3)
	out.Limitations = limitations()

	if err := os.MkdirAll(cfg.Directory, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", cfg.Directory, err)
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(cfg.Directory, "results.json")
	if err := os.WriteFile(target, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", target, err)
	}
	printSummary(out)
	return nil
}

// measure loads the graph once and prices every component of the snapshot it
// produced.
func measure(graph string) (pass, map[string]uint64, error) {
	started := time.Now()

	// The baseline is taken before anything is read, so the delta below is what
	// a process ends up holding rather than what it touched on the way. The
	// first attempt at this subtracted a second reading taken "after releasing
	// the snapshot", which released nothing -- the variable was still live in
	// this function -- and reported a total of minus thirty-two bytes.
	runtime.GC()
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)

	// BuildSnapshot is the same path `kivgraph serve` takes: the snapshot is
	// derived from the definitive graph in LadybugDB, not from a facts.Set, so
	// what gets priced here is what a serving process actually holds. The rows
	// it reads on the way are its input, and a forced GC drops them before the
	// second reading.
	snapshot, _, err := rebuild.BuildSnapshot(context.Background(), rebuild.BuildSnapshotOptions{
		DatabasePath: graph,
		SnapshotID:   1,
	})
	if err != nil {
		return pass{}, nil, fmt.Errorf("build snapshot from %s: %w", graph, err)
	}

	runtime.GC()
	runtime.GC()
	var loaded runtime.MemStats
	runtime.ReadMemStats(&loaded)
	runtime.KeepAlive(snapshot)
	resident := int64(loaded.HeapAlloc) - int64(baseline.HeapAlloc)

	counts := snapshot.Metadata().Counts
	pinning := stableKeyCharacters(snapshot, counts)

	components := analyticComponents(counts)
	components = append(components, stringTableComponents(snapshot)...)
	components = append(components, indexComponents(snapshot, counts)...)

	accounted := int64(0)
	for _, item := range components {
		accounted += item.Bytes
	}
	coverage := 0.0
	if resident > 0 {
		coverage = 100 * float64(accounted) / float64(resident)
	}
	sort.SliceStable(components, func(left, right int) bool {
		return components[left].Bytes > components[right].Bytes
	})
	return pass{
		ResidentBytes: resident,
		Components:    components,
		AccountedFor:  accounted,
		Remainder:     resident - accounted,
		CoveragePct:   coverage,
		PinningBytes:  pinning,
		ElapsedMS:     time.Since(started).Milliseconds(),
	}, countsMap(counts), nil
}

// analyticComponents prices the flat tables and the two CSR arrays. The count
// times the element size is exact for a slice the builder filled once; the
// rounding a size class adds lands in the remainder rather than being smeared
// over the components.
func analyticComponents(counts hotsnapshot.IDCounts) []component {
	edges := counts.Edges
	symbols := counts.Symbols
	return []component{
		flat("symbols table", unsafe.Sizeof(hotsnapshot.SymbolRecord{}), symbols),
		flat("evidence table", unsafe.Sizeof(hotsnapshot.EvidenceRecord{}), counts.Evidence),
		flat("files table", unsafe.Sizeof(hotsnapshot.FileRecord{}), counts.Files),
		flat("packages table", unsafe.Sizeof(hotsnapshot.PackageRecord{}), counts.Packages),
		flat("repositories table", unsafe.Sizeof(hotsnapshot.RepositoryRecord{}), counts.Repositories),
		flat("unresolved table", unsafe.Sizeof(hotsnapshot.UnresolvedReferenceRecord{}), counts.Unresolved),
		flat("package dependencies table", unsafe.Sizeof(hotsnapshot.PackageDependencyRecord{}), counts.PackageEdges),
		flat("forward edges (CSR)", unsafe.Sizeof(hotsnapshot.PackedEdge{}), edges),
		flat("reverse edges (CSR)", unsafe.Sizeof(hotsnapshot.PackedEdge{}), edges),
		flat("forward offsets (CSR)", unsafe.Sizeof(uint32(0)), symbols+1),
		flat("reverse offsets (CSR)", unsafe.Sizeof(uint32(0)), symbols+1),
	}
}

func flat(name string, size uintptr, count uint64) component {
	bytes := int64(size) * int64(count)
	return component{
		Name: name, Method: "analytic", Bytes: bytes, Entries: int64(count),
		PerUnit: perUnit(bytes, int64(count)),
	}
}

// stringTableComponents prices what the interning table holds. Stats().Bytes is
// the value arena, and it is the one number this project could already cite
// before today -- but it is not the whole table: `offsets` and `order` carry one
// uint32 each per entry, and at six hundred thousand entries that is another
// five megabytes nobody had counted.
func stringTableComponents(snapshot *hotsnapshot.GraphSnapshot) []component {
	stats := snapshot.Strings().Stats()
	entries := int64(stats.Entries)
	const uint32Size = 4
	sidecar := entries * uint32Size * 2
	return []component{
		{
			Name: "string arena (values)", Method: "analytic",
			Bytes: int64(stats.Bytes), Entries: entries,
			PerUnit: perUnit(int64(stats.Bytes), entries),
		},
		{
			Name: "string table offsets+order", Method: "analytic",
			Bytes: sidecar, Entries: entries, PerUnit: perUnit(sidecar, entries),
		},
	}
}

// stableKeyCharacters is not a component, and finding out why was the point of
// this benchmark.
//
// StableKey is the one real Go string a record holds; every other string field
// is an InternedString, a uint32 into the arena. So unsafe.Sizeof counts its
// sixteen byte header and none of its 52 characters, and the obvious move is to
// add those characters as a component. That would count them twice.
//
// The characters are not separately allocated. ScanCanonical hands out every
// value as an unsafe.String over the Arrow chunk's own arena, and the adapter
// converts it to a StableKey with a string-to-string type conversion, which does
// not copy. So the characters live inside a buffer the loader read the database
// into -- and holding one of them holds all of it.
//
// That is what the remainder of this breakdown is, and the heap profile names
// it: 58 MB parked in ladybug.newCanonicalArrowChunk, reachable after two forced
// GCs, pinned by 6.4 MB of stable keys. It is direct evidence for the phase's
// own LUQUE-2002, which is titled "que ninguna clave estable ocupe un puntero".
func stableKeyCharacters(snapshot *hotsnapshot.GraphSnapshot, counts hotsnapshot.IDCounts) int64 {
	total := hotsnapshot.SymbolID(counts.Symbols)
	measured := int64(0)
	for id := hotsnapshot.SymbolID(0); id < total; id++ {
		record, found := snapshot.Symbol(id)
		if !found {
			continue
		}
		measured += int64(len(record.StableKey))
	}
	return measured
}

// indexComponents observes what the four exact indexes cost, by building each
// one on its own from the records the snapshot exposes and watching the heap
// across a forced GC. A rebuilt index has the same key and value types and the
// same cardinality as the one the snapshot holds, which is what makes it a
// measurement of that index and not of an analogy.
func indexComponents(snapshot *hotsnapshot.GraphSnapshot, counts hotsnapshot.IDCounts) []component {
	total := hotsnapshot.SymbolID(counts.Symbols)

	byStableKey := heapCost(func() any {
		index := make(map[hotsnapshot.StableKey]hotsnapshot.SymbolID, counts.Symbols)
		for id := hotsnapshot.SymbolID(0); id < total; id++ {
			record, found := snapshot.Symbol(id)
			if !found {
				continue
			}
			index[record.StableKey] = id
		}
		return index
	})

	byName := heapCost(func() any {
		index := make(map[hotsnapshot.InternedString][]hotsnapshot.SymbolID)
		for id := hotsnapshot.SymbolID(0); id < total; id++ {
			record, found := snapshot.Symbol(id)
			if !found {
				continue
			}
			index[record.Name] = append(index[record.Name], id)
		}
		return index
	})

	byQName := heapCost(func() any {
		index := make(map[hotsnapshot.InternedString][]hotsnapshot.SymbolID)
		for id := hotsnapshot.SymbolID(0); id < total; id++ {
			record, found := snapshot.Symbol(id)
			if !found {
				continue
			}
			index[record.QualifiedName] = append(index[record.QualifiedName], id)
		}
		return index
	})

	files := hotsnapshot.FileID(counts.Files)
	byRepoPath := heapCost(func() any {
		index := make(map[hotsnapshot.RepoPathKey]hotsnapshot.FileID, counts.Files)
		for id := hotsnapshot.FileID(0); id < files; id++ {
			record, found := snapshot.File(id)
			if !found {
				continue
			}
			index[hotsnapshot.RepoPathKey{Repository: record.Repository, Path: record.Path}] = id
		}
		return index
	})

	return []component{
		{
			Name: "symbolByStableKey (map)", Method: "heap", Bytes: byStableKey,
			Entries: int64(counts.Symbols), PerUnit: perUnit(byStableKey, int64(counts.Symbols)),
		},
		{
			Name: "symbolsByName (map)", Method: "heap", Bytes: byName,
			Entries: int64(counts.Symbols), PerUnit: perUnit(byName, int64(counts.Symbols)),
		},
		{
			Name: "symbolsByQName (map)", Method: "heap", Bytes: byQName,
			Entries: int64(counts.Symbols), PerUnit: perUnit(byQName, int64(counts.Symbols)),
		},
		{
			Name: "fileByRepoPath (map)", Method: "heap", Bytes: byRepoPath,
			Entries: int64(counts.Files), PerUnit: perUnit(byRepoPath, int64(counts.Files)),
		},
	}
}

// heapCost builds one value and reports the live heap it added, as the median of
// three attempts. The value is kept alive across the second reading, so what is
// measured is what it costs to hold rather than what it costed to allocate.
//
// The median is there because a single reading is noisy at this scale: the
// first version of this took one sample and clamped a negative result to zero,
// which reported the file index as costing nothing. Clamping hid the noise
// instead of averaging it out, and a zero that means "measurement failed" is
// indistinguishable from a zero that means "free".
func heapCost(build func() any) int64 {
	samples := make([]int64, 0, 3)
	for attempt := 0; attempt < 3; attempt++ {
		runtime.GC()
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)

		value := build()

		runtime.GC()
		runtime.GC()
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(value)

		samples = append(samples, int64(after.HeapAlloc)-int64(before.HeapAlloc))
	}
	sort.Slice(samples, func(left, right int) bool { return samples[left] < samples[right] })
	return samples[1]
}

func perUnit(bytes, entries int64) float64 {
	if entries == 0 {
		return 0
	}
	return float64(bytes) / float64(entries)
}

func countsMap(counts hotsnapshot.IDCounts) map[string]uint64 {
	return map[string]uint64{
		"repositories": counts.Repositories, "packages": counts.Packages,
		"files": counts.Files, "symbols": counts.Symbols,
		"evidence": counts.Evidence, "edges": counts.Edges,
		"package_edges": counts.PackageEdges, "unresolved": counts.Unresolved,
	}
}

// stability is the largest relative gap between passes of the resident total.
// It exists so the report cannot claim a reproducible number without showing
// how reproducible it was.
func stability(passes []pass) float64 {
	if len(passes) < 2 {
		return 0
	}
	low, high := passes[0].ResidentBytes, passes[0].ResidentBytes
	for _, item := range passes[1:] {
		if item.ResidentBytes < low {
			low = item.ResidentBytes
		}
		if item.ResidentBytes > high {
			high = item.ResidentBytes
		}
	}
	if high == 0 {
		return 0
	}
	return 100 * float64(high-low) / float64(high)
}

func dominant(measured pass, count int) []string {
	names := make([]string, 0, count)
	for index, item := range measured.Components {
		if index >= count {
			break
		}
		names = append(names, item.Name)
	}
	return names
}

func limitations() []string {
	return []string{
		"One generation on one machine. A corpus with a Rust sysroot indexed " +
			"would move the symbol and evidence tables and nothing else here predicts by how much.",
		"The four indexes are priced by rebuilding an equivalent map, not by " +
			"reading the snapshot's own. Same key and value types and same cardinality, " +
			"so the cost is the runtime's answer for that shape -- but a map the builder " +
			"grew differently could occupy a different number of buckets.",
		"The analytic figures are count times element size. A slice rounded up " +
			"to a size class costs slightly more than that, and the difference is left " +
			"in the remainder rather than spread across the tables.",
		"HeapAlloc is live heap, not process RSS. The phase's own figures put " +
			"RSS between 252 and 373 MB against 173 MB of live heap, and nothing here " +
			"accounts for that gap.",
	}
}

// writeHeapProfile builds the snapshot once more and dumps the live heap, so the
// remainder can be attributed to an allocation site instead of to a hypothesis.
func writeHeapProfile(graph, target string) error {
	snapshot, _, err := rebuild.BuildSnapshot(context.Background(), rebuild.BuildSnapshotOptions{
		DatabasePath: graph,
		SnapshotID:   1,
	})
	if err != nil {
		return fmt.Errorf("build snapshot for the profile: %w", err)
	}
	runtime.GC()
	runtime.GC()
	file, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("create %s: %w", target, err)
	}
	defer file.Close()
	if err := pprof.Lookup("heap").WriteTo(file, 0); err != nil {
		return fmt.Errorf("write heap profile: %w", err)
	}
	runtime.KeepAlive(snapshot)
	return nil
}

func currentCommit() (string, error) {
	output, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func printSummary(out results) {
	first := out.Passes[0]
	fmt.Printf("generation %s, %d symbols, %d edges\n",
		out.Generation, out.Counts["symbols"], out.Counts["edges"])
	fmt.Printf("%-32s %12s %10s %12s\n", "component", "bytes", "method", "per entry")
	for _, item := range first.Components {
		fmt.Printf("%-32s %12d %10s %12.1f\n", item.Name, item.Bytes, item.Method, item.PerUnit)
	}
	fmt.Printf("%-32s %12d\n", "accounted for", first.AccountedFor)
	fmt.Printf("%-32s %12d\n", "remainder", first.Remainder)
	fmt.Printf("%-32s %12d\n", "resident (HeapAlloc delta)", first.ResidentBytes)
	fmt.Printf("coverage %.1f %%, stability %.2f %% across %d passes\n",
		first.CoveragePct, out.StabilityPct, len(out.Passes))
}
