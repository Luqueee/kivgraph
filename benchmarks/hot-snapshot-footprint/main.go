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
// honest. Flat tables, the two CSR arrays and the two arenas are computed
// analytically -- unsafe.Sizeof times the element count, or the arena's own
// reported size, is exact and needs no instrumentation. Every part is priced that
// way since LUQUE-2003, and that is itself a result: while the exact indexes were
// Go maps they had to be observed on the heap, because a map costs what the
// runtime decides it costs and no arithmetic here could predict it.
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
	// Method says how the number was obtained, because different ways carry
	// different error and a reader has to know which one they are trusting.
	// Everything is "analytic" today; "heap" existed while the exact indexes
	// were maps and nothing could compute their layout.
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
	ElapsedMS     int64       `json:"elapsed_ms"`
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

	components := analyticComponents(counts)
	components = append(components, stringTableComponents(snapshot)...)
	components = append(components, stableKeyComponents(snapshot)...)
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

// stableKeyComponents prices the table every symbol's stable key lives in.
//
// Until LUQUE-2002 this was not a component, and finding out why was the point
// of this benchmark.
//
// StableKey used to be the one real Go string a record held; every other string
// field is an InternedString, a uint32 into the arena. So unsafe.Sizeof counted
// its sixteen byte header and none of its 52 characters, and the obvious move --
// adding those characters as a component -- would have counted them twice.
//
// The characters were not separately allocated. ScanCanonical hands out every
// value as an unsafe.String over the Arrow chunk's own arena, and the adapter
// converted it to a StableKey with a string-to-string type conversion, which
// does not copy. So the characters lived inside a buffer the loader had read the
// database into -- and holding one of them held all of it.
//
// That was the remainder of this breakdown, and the heap profile named it: 58 MB
// parked in ladybug.newCanonicalArrowChunk, reachable after two forced GCs,
// pinned by 6.4 MB of stable keys. StableKeyTable closed it: a record carries a
// dense uint32 into a table that copies its bytes, so the keys are storage the
// snapshot owns and they pin nothing. That is why they can be priced like any
// other table here instead of being reported as what held the remainder open.
func stableKeyComponents(snapshot *hotsnapshot.GraphSnapshot) []component {
	stats := snapshot.StableKeys().Stats()
	entries := int64(stats.Entries)
	const uint32Size = 4
	// One offset per entry plus the terminator that bounds the last key.
	offsets := (entries + 1) * uint32Size
	return []component{
		{
			Name: "stable key arena (values)", Method: "analytic",
			Bytes: int64(stats.Bytes), Entries: entries,
			PerUnit: perUnit(int64(stats.Bytes), entries),
		},
		{
			Name: "stable key offsets", Method: "analytic",
			Bytes: offsets, Entries: entries, PerUnit: perUnit(offsets, entries),
		},
	}
}

// indexComponents prices the three exact indexes and the package back-index.
//
// Until LUQUE-2003 the first three were Go maps, and the only honest way to
// price a map was to rebuild an equivalent one and watch the heap across a
// forced GC: a map has no layout a caller can compute, because its bucket count
// is the runtime's business and its keys are placed by a per-process hash seed.
//
// Flat arrays do have a layout, so these rows are analytic now -- and that
// change of method is itself the result. A number nobody can derive is a number
// nobody can design against, which is what this whole phase is about.
//
// The package back-index was never a component at all. It was a
// map[PackageID][]PackageDependencyRecord holding copies of the rows, so pricing
// it meant pricing a second copy of the dependency table; now it is offsets
// addressed by the dense id itself plus one uint32 per dependency, and it is
// cheap enough to name.
func indexComponents(snapshot *hotsnapshot.GraphSnapshot, counts hotsnapshot.IDCounts) []component {
	const uint32Size = 4
	repoPathKeySize := int64(unsafe.Sizeof(hotsnapshot.RepoPathKey{}))

	names := make(map[hotsnapshot.InternedString]struct{})
	qualified := make(map[hotsnapshot.InternedString]struct{})
	for id := hotsnapshot.SymbolID(0); id < hotsnapshot.SymbolID(counts.Symbols); id++ {
		record, found := snapshot.Symbol(id)
		if !found {
			continue
		}
		names[record.Name] = struct{}{}
		qualified[record.QualifiedName] = struct{}{}
	}

	// A CSR index costs one key and one offset per distinct value, plus the
	// terminating offset, plus one id per symbol -- every symbol appears exactly
	// once across the runs, which is the invariant the snapshot validates.
	csr := func(keys int64) int64 {
		return keys*uint32Size + (keys+1)*uint32Size + int64(counts.Symbols)*uint32Size
	}
	byName, byQName := csr(int64(len(names))), csr(int64(len(qualified)))
	byRepoPath := int64(counts.Files) * (repoPathKeySize + uint32Size)
	packageIncoming := (int64(counts.Packages)+1)*uint32Size + int64(counts.PackageEdges)*uint32Size

	return []component{
		{
			Name: "symbolsByName (CSR)", Method: "analytic", Bytes: byName,
			Entries: int64(len(names)), PerUnit: perUnit(byName, int64(len(names))),
		},
		{
			Name: "symbolsByQName (CSR)", Method: "analytic", Bytes: byQName,
			Entries: int64(len(qualified)), PerUnit: perUnit(byQName, int64(len(qualified))),
		},
		{
			Name: "fileByRepoPath (sorted)", Method: "analytic", Bytes: byRepoPath,
			Entries: int64(counts.Files), PerUnit: perUnit(byRepoPath, int64(counts.Files)),
		},
		{
			Name: "packageIncoming (CSR)", Method: "analytic", Bytes: packageIncoming,
			Entries: int64(counts.PackageEdges), PerUnit: perUnit(packageIncoming, int64(counts.PackageEdges)),
		},
	}
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
		"The exact indexes are priced from their declared layout and the distinct " +
			"key counts read out of the records, not from the snapshot's own arrays, " +
			"which are private. The arithmetic is exact for the shape; a builder that " +
			"over-allocated a slice would hold slightly more than this says.",
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
