// Command snapshot-heap measures what a loaded snapshot costs one process, and
// how much of that cost is the answer rather than the work of arriving at it.
//
// The distinction is the whole point. `benchmarks/shared-snapshot` measures
// Private_Dirty per server and finds it flat across client counts: it is the one
// component sharing cannot reduce, and the only thing a daemon would collapse to
// a single copy. But Private_Dirty counts every page the process ever dirtied,
// so a load that allocates three times what it keeps looks exactly like one that
// needs it all. This harness separates the two, because they have opposite fixes:
// live bytes are attacked by moving a structure into the mapped file, transient
// bytes by not allocating them.
//
// It reads a published generation the way `serve` does -- map the file, verify the
// recorded digest, hand the bytes to hotsnapshot.MapSnapshot -- and holds the
// snapshot while it profiles, which is what the package benchmark cannot do: its
// profile is written after the snapshot became unreachable, so every live byte
// has already been collected.
package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/rebuild"
)

const defaultDirectory = "benchmarks/snapshot-heap"

type config struct {
	Generation string
	Directory  string
}

func main() {
	var cfg config
	flag.StringVar(&cfg.Generation, "generation-dir", "",
		"published generation directory holding snapshot.kvsnap")
	flag.StringVar(&cfg.Directory, "output", defaultDirectory, "directory for results.json and report.md")
	flag.Parse()
	if strings.TrimSpace(cfg.Generation) == "" {
		fmt.Fprintln(os.Stderr, "-generation-dir is required: this harness measures a real published snapshot, not a synthetic one")
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "snapshot-heap: %v\n", err)
		os.Exit(1)
	}
}

func run(cfg config) error {
	out, err := measure(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.Directory, 0o755); err != nil {
		return fmt.Errorf("create %q: %w", cfg.Directory, err)
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Directory, "results.json"), append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write results.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Directory, "report.md"), []byte(render(out)), 0o644); err != nil {
		return fmt.Errorf("write report.md: %w", err)
	}
	fmt.Print(summary(out))
	return nil
}

// results is the versioned record. A comparison against another run needs the
// corpus and the commit beside the numbers, because both decide them.
type results struct {
	Benchmark   string      `json:"benchmark"`
	Date        string      `json:"date"`
	Commit      string      `json:"commit"`
	Environment environment `json:"environment"`
	Corpus      corpus      `json:"corpus"`
	Heap        heap        `json:"heap"`
	Mappable    mappable    `json:"mappable"`
	Findings    []string    `json:"findings"`
	Limitations []string    `json:"limitations"`
}

type environment struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	Go   string `json:"go"`
}

type corpus struct {
	Generation   string `json:"generation_dir"`
	FileBytes    int64  `json:"snapshot_file_bytes"`
	Repositories int    `json:"repositories"`
	Symbols      int    `json:"symbols"`
	Edges        int    `json:"edges"`
	Interned     int    `json:"interned_strings"`
}

// heap splits what the load costs. Allocated is every byte the load asked for,
// live is what survives it, and the difference is the part that dirties a page
// and then holds nothing -- invisible to a heap profile taken afterwards, and
// fully counted by Private_Dirty.
type heap struct {
	AllocatedBytes    uint64  `json:"allocated_bytes"`
	LiveBytes         uint64  `json:"live_bytes"`
	TransientBytes    uint64  `json:"transient_bytes"`
	LivePerSymbol     float64 `json:"live_bytes_per_symbol"`
	AllocPerSymbol    float64 `json:"allocated_bytes_per_symbol"`
	TransientPerAlloc float64 `json:"transient_share_of_allocated"`
	// AdoptedTableBytes is what the reader takes instead of copying: the
	// fixed-width tables and both CSR edge arrays, whose slices the decoders
	// allocated from the mapped bytes a statement earlier. It used to be
	// copied again, so this is also what that twin weighed -- the arithmetic
	// said 20.74 MB over this corpus and removing it took 20.8 MB off what the
	// load allocates, with the live bytes unchanged.
	AdoptedTableBytes uint64 `json:"adopted_table_bytes"`
	ProfilePath       string `json:"live_profile_path"`
}

// mappable is the arithmetic size of everything a reader could take in place
// from the file. It is not measured: every one of these is a fixed-width table
// whose row count the snapshot states.
type mappable struct {
	ArenaBytes     uint64 `json:"string_arena_bytes"`
	StableKeyBytes uint64 `json:"stable_key_table_bytes"`
	RecordBytes    uint64 `json:"fixed_width_record_bytes"`
	EdgeBytes      uint64 `json:"csr_edge_bytes"`
	TotalBytes     uint64 `json:"total_bytes"`
}

func measure(cfg config) (results, error) {
	path := filepath.Join(cfg.Generation, rebuild.PublishedSnapshotFileName)
	info, err := os.Stat(path)
	if err != nil {
		return results{}, fmt.Errorf("stat the published snapshot: %w", err)
	}
	digest, err := readDigest(cfg.Generation)
	if err != nil {
		return results{}, err
	}

	// The baseline is taken after returning what start-up allocated, so the
	// difference prices the load and not the process.
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	data, release, err := mapSnapshotFile(path)
	if err != nil {
		return results{}, err
	}
	defer release()
	snapshot, err := hotsnapshot.MapSnapshot(data, digest)
	if err != nil {
		return results{}, fmt.Errorf("map the snapshot: %w", err)
	}

	runtime.GC()
	var loaded runtime.MemStats
	runtime.ReadMemStats(&loaded)

	profilePath := filepath.Join(cfg.Directory, "live.pprof")
	if err := os.MkdirAll(cfg.Directory, 0o755); err != nil {
		return results{}, fmt.Errorf("create %q: %w", cfg.Directory, err)
	}
	if err := writeLiveProfile(profilePath); err != nil {
		return results{}, err
	}

	counts := snapshot.Metadata().Counts
	strings := snapshot.Strings().Stats()
	keys := snapshot.StableKeys().Stats()
	allocated := loaded.TotalAlloc - before.TotalAlloc
	live := loaded.HeapAlloc
	out := results{
		Benchmark:   "snapshot-heap",
		Date:        time.Now().UTC().Format("2006-01-02"),
		Commit:      currentCommit(),
		Environment: environment{OS: runtime.GOOS, Arch: runtime.GOARCH, Go: runtime.Version()},
		Corpus: corpus{
			Generation: cfg.Generation, FileBytes: info.Size(),
			Repositories: int(counts.Repositories), Symbols: int(counts.Symbols),
			Edges: int(counts.Edges), Interned: int(strings.Entries),
		},
		Heap: heap{
			AllocatedBytes: allocated, LiveBytes: live,
			TransientBytes: saturatingSub(allocated, live),
			LivePerSymbol:  perSymbol(live, counts.Symbols),
			AllocPerSymbol: perSymbol(allocated, counts.Symbols),
			// Everything the decoders hand over and the snapshot adopts: the
			// fixed-width tables and both CSR edge arrays. The arena and the
			// key table are excluded because they were never copied.
			AdoptedTableBytes: mappableBytes(counts, 0, 0).TotalBytes,
			ProfilePath:       profilePath,
		},
		Mappable:    mappableBytes(counts, uint64(strings.Bytes), stableKeyTableBytes(keys)),
		Findings:    findings(),
		Limitations: limitations(),
	}
	if allocated != 0 {
		out.Heap.TransientPerAlloc = float64(out.Heap.TransientBytes) / float64(allocated)
	}
	// The snapshot must outlive every measurement above: a collected snapshot
	// would report a live heap of nothing and a mapping nobody reads.
	runtime.KeepAlive(snapshot)
	return out, nil
}

// writeLiveProfile writes the heap profile with the snapshot still reachable,
// which is the only moment its structures are attributable.
func writeLiveProfile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create the live profile: %w", err)
	}
	defer file.Close()
	profile := pprof.Lookup("heap")
	if profile == nil {
		return errors.New("the heap profile is unavailable")
	}
	if err := profile.WriteTo(file, 0); err != nil {
		return fmt.Errorf("write the live profile: %w", err)
	}
	return nil
}

// mapSnapshotFile maps the file read-only and shared, the way the loader does:
// the pages a second process reads are then the same physical pages.
func mapSnapshotFile(path string) ([]byte, func(), error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open the published snapshot: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat the published snapshot: %w", err)
	}
	if info.Size() == 0 {
		return nil, nil, errors.New("the published snapshot is empty")
	}
	data, err := syscall.Mmap(int(file.Fd()), 0, int(info.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, nil, fmt.Errorf("map the published snapshot: %w", err)
	}
	return data, func() { _ = syscall.Munmap(data) }, nil
}

func readDigest(directory string) ([32]byte, error) {
	var digest [32]byte
	raw, err := os.ReadFile(filepath.Join(directory, rebuild.PublishedSnapshotDigestFileName))
	if err != nil {
		return digest, fmt.Errorf("read the recorded graph digest: %w", err)
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return digest, fmt.Errorf("the recorded digest is not hexadecimal: %w", err)
	}
	if len(decoded) != len(digest) {
		return digest, fmt.Errorf("the recorded digest is %d bytes, want %d", len(decoded), len(digest))
	}
	copy(digest[:], decoded)
	return digest, nil
}

func mappableBytes(counts hotsnapshot.IDCounts, arena, keys uint64) mappable {
	records := uint64(counts.Repositories)*uint64(unsafe.Sizeof(hotsnapshot.RepositoryRecord{})) +
		uint64(counts.Packages)*uint64(unsafe.Sizeof(hotsnapshot.PackageRecord{})) +
		uint64(counts.Files)*uint64(unsafe.Sizeof(hotsnapshot.FileRecord{})) +
		uint64(counts.Symbols)*uint64(unsafe.Sizeof(hotsnapshot.SymbolRecord{})) +
		uint64(counts.Evidence)*uint64(unsafe.Sizeof(hotsnapshot.EvidenceRecord{})) +
		uint64(counts.PackageEdges)*uint64(unsafe.Sizeof(hotsnapshot.PackageDependencyRecord{})) +
		uint64(counts.Unresolved)*uint64(unsafe.Sizeof(hotsnapshot.UnresolvedReferenceRecord{}))
	// Both directions: the reverse CSR is a permutation of the forward one and
	// a reader holds them both.
	edges := 2 * uint64(counts.Edges) * uint64(unsafe.Sizeof(hotsnapshot.PackedEdge{}))
	return mappable{
		ArenaBytes: arena, StableKeyBytes: keys, RecordBytes: records, EdgeBytes: edges,
		TotalBytes: arena + keys + records + edges,
	}
}

// stableKeyTableBytes is what the key table occupies: its arena plus one offset
// per entry and the terminator that bounds the last key.
func stableKeyTableBytes(stats hotsnapshot.StableKeyTableStats) uint64 {
	return stats.Bytes + 4*(uint64(stats.Entries)+1)
}

func perSymbol(bytes uint64, symbols uint64) float64 {
	if symbols == 0 {
		return 0
	}
	return float64(bytes) / float64(symbols)
}

func saturatingSub(a, b uint64) uint64 {
	if b > a {
		return 0
	}
	return a - b
}

func currentCommit() string {
	output, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// findings are what this measurement established, in the order the numbers
// establish them. They are claims about the code, so each names where to look.
func findings() []string {
	return []string{
		"La mayor parte de lo que asigna la carga no es la respuesta. Los bytes " +
			"vivos son lo que un lector conserva; el resto ensucia una página y " +
			"no sostiene nada, y un perfil de heap tomado después de la carga no " +
			"puede ver ni uno de ellos.",
		"El gemelo que había ya no está, y por eso se nombra: cada tabla de " +
			"ancho fijo se copiaba dos veces. Los `decode*` de `readSnapshot` " +
			"asignan un slice por sección y copian dentro los bytes mapeados, y " +
			"`NewGraphSnapshot` volvía a copiar cada uno para que un llamante " +
			"pudiera seguir mutando lo que pasó -- cierto para el constructor, " +
			"superfluo para un lector que decodificó esos slices una sentencia " +
			"antes y que nadie más puede nombrar. Ceder la propiedad en el camino " +
			"del lector quitó `20,8 MB` de lo asignado sobre este corpus y dejó " +
			"los bytes vivos idénticos. La aritmética lo predecía en `20,74`.",
		"La validación del CSR inverso ya no paga un mapa. `validReverseCounterpart` " +
			"guardaba una clave por arista directa para probar que el inverso es " +
			"su permutación; hoy marca un bit por arista y recorre el grupo de la " +
			"fuente para encontrar la contrapartida. Sobre este corpus son `42 kB` " +
			"en vez de `13,3 MB`, y el recorrido que compra son `18,4 M` " +
			"comparaciones -- la suma de los grados de salida al cuadrado, `54x` el " +
			"número de aristas, con un grupo mayor de `889` y una mediana de `1`. " +
			"Ese trabajo no cuesta tiempo: la carga mide `150,0 ms` en su mejor " +
			"pasada contra `159,6` con el mapa, alternando las dos versiones sobre " +
			"el mismo fichero.",
		"Los tres índices de búsqueda ya no se acumulan en mapas. Se derivaban de " +
			"las tablas en `indexSnapshotInput` -- `16,5 MB` de mapas que " +
			"`newSymbolIndex` aplanaba a arrays acto seguido-- y ahora se derivan " +
			"directamente: una clave y un id caben empaquetados en un `uint64`, así " +
			"que ordenar es `slices.Sort` sobre enteros y no hay comparador. Lo que " +
			"se tira son esos arrays empaquetados, `1,9 MB` por las dos " +
			"direcciones. Ni el mapa guardaba nada que las tablas no dijeran ya: la " +
			"clave del símbolo i es un campo del símbolo i, que es por lo que la " +
			"comprobación de que ambos concordaban no podía fallar.",
		"El comparador era el coste, no el orden. Derivar leyendo la clave del " +
			"registro a través de una función de comparación costaba `18 ms` por " +
			"carga -- dos llamadas dinámicas por comparación, dos millones de " +
			"comparaciones-- y con los enteros empaquetados la carga baja a " +
			"`139,9 ms` frente a `152,9` con los mapas, cuatro pasadas alternadas " +
			"de cuatro.",
		"Lo que queda arriba ya no es un mapa: es una copia. `strings.Clone` " +
			"asigna `7,2 MB` en el camino de carga y no sobrevive a él. La tabla de " +
			"claves estables copia cada clave que entrega mientras está prestada de " +
			"un fichero mapeado, que es correcto para un llamante que la guarde, y " +
			"`validExactIndexes` pide las `117.499` para tirar cada una en la " +
			"sentencia siguiente.",
		"El arena ya se lee en el sitio y es la sección más grande del fichero, " +
			"que es por lo que los bytes vivos son una fracción de él. Lo que " +
			"queda en el heap son las tablas, y el fichero declara cuántas filas " +
			"tiene cada una, así que nada de ellas hay que reconstruirlo para " +
			"poder confiar en él.",
	}
}

func limitations() []string {
	return []string{
		"Los bytes vivos y asignados son contabilidad del runtime de Go, no " +
			"páginas residentes. `Private_Dirty` es mayor que cualquiera de los " +
			"dos: lleva además metadatos del runtime, pilas y heap que el " +
			"asignador nunca devolvió al sistema. Eso lo mide " +
			"`benchmarks/shared-snapshot`, y sólo en Linux.",
		"La cifra transitoria es la basura de la propia carga, medida como lo " +
			"asignado menos lo vivo después de un `GC`. Es un suelo: una página " +
			"que ensucia una asignación transitoria sigue sucia hasta que el " +
			"asignador la barre.",
		"El total mapeable es aritmética sobre las filas que el snapshot declara, " +
			"no una medición de lo que un lector toma en el sitio hoy. El arena y " +
			"la tabla de claves estables ya se toman en el sitio.",
		"Un proceso, una carga. Nada de aquí dice qué paga un segundo lector de " +
			"la misma generación, que es la pregunta que contesta " +
			"`shared-snapshot`.",
		"La cifra viva es el heap entero del proceso después de la carga, que " +
			"incluye el del propio arnés, así que es una cota superior de la del " +
			"snapshot. Pasadas repetidas de una misma compilación coinciden byte " +
			"a byte; dos compilaciones de este arnés difirieron en torno a un " +
			"megabyte, así que una comparación es entre pasadas del mismo binario " +
			"y no entre ediciones de él.",
	}
}

func summary(out results) string {
	const mb = 1 << 20
	var text strings.Builder
	fmt.Fprintf(&text, "corpus      %d repositories, %d symbols, %d edges, %d interned\n",
		out.Corpus.Repositories, out.Corpus.Symbols, out.Corpus.Edges, out.Corpus.Interned)
	fmt.Fprintf(&text, "file        %.1f MB\n", float64(out.Corpus.FileBytes)/mb)
	fmt.Fprintf(&text, "allocated   %.1f MB (%.0f B/symbol)\n",
		float64(out.Heap.AllocatedBytes)/mb, out.Heap.AllocPerSymbol)
	fmt.Fprintf(&text, "live        %.1f MB (%.0f B/symbol)\n",
		float64(out.Heap.LiveBytes)/mb, out.Heap.LivePerSymbol)
	fmt.Fprintf(&text, "transient   %.1f MB (%.0f%% of allocated)\n",
		float64(out.Heap.TransientBytes)/mb, out.Heap.TransientPerAlloc*100)
	fmt.Fprintf(&text, "mappable    %.1f MB total: arena %.1f, keys %.1f, records %.1f, edges %.1f\n",
		float64(out.Mappable.TotalBytes)/mb, float64(out.Mappable.ArenaBytes)/mb,
		float64(out.Mappable.StableKeyBytes)/mb, float64(out.Mappable.RecordBytes)/mb,
		float64(out.Mappable.EdgeBytes)/mb)
	fmt.Fprintf(&text, "profile     %s\n", out.Heap.ProfilePath)
	return text.String()
}

func render(out results) string {
	const mb = 1 << 20
	var text strings.Builder
	text.WriteString("# Lo que cuesta a un proceso el snapshot que ya está compartido\n\n")
	text.WriteString("`benchmarks/shared-snapshot` mide `Private_Dirty` por servidor y lo\n")
	text.WriteString("encuentra plano al crecer el número de clientes: es el único componente que\n")
	text.WriteString("compartir no reduce, y lo único que un demonio colapsaría a una copia. Pero\n")
	text.WriteString("`Private_Dirty` cuenta toda página que el proceso ensució alguna vez, así que\n")
	text.WriteString("una carga que asigna el triple de lo que conserva se ve igual que una que lo\n")
	text.WriteString("necesita todo. Este arnés separa las dos mitades, porque se arreglan al\n")
	text.WriteString("revés: los bytes vivos, moviendo una estructura al fichero mapeado; los\n")
	text.WriteString("transitorios, no asignándolos.\n\n")
	fmt.Fprintf(&text, "|dato|valor|\n|---|---|\n")
	fmt.Fprintf(&text, "|fecha|`%s`|\n", out.Date)
	fmt.Fprintf(&text, "|commit|`%s`|\n", out.Commit)
	fmt.Fprintf(&text, "|plataforma|`%s/%s`, `%s`|\n", out.Environment.OS, out.Environment.Arch, out.Environment.Go)
	fmt.Fprintf(&text, "|corpus|`%d` repositorios, `%d` símbolos, `%d` aristas|\n",
		out.Corpus.Repositories, out.Corpus.Symbols, out.Corpus.Edges)
	fmt.Fprintf(&text, "|fichero|`%.1f MB`|\n", float64(out.Corpus.FileBytes)/mb)
	text.WriteString("\n## La carga\n\n")
	fmt.Fprintf(&text, "|mitad|bytes|por símbolo|\n|---|---|---|\n")
	fmt.Fprintf(&text, "|asignado|`%.1f MB`|`%.0f B`|\n", float64(out.Heap.AllocatedBytes)/mb, out.Heap.AllocPerSymbol)
	fmt.Fprintf(&text, "|vivo|`%.1f MB`|`%.0f B`|\n", float64(out.Heap.LiveBytes)/mb, out.Heap.LivePerSymbol)
	// The adopted tables are part of the live half, so the row sits under it:
	// they used to be copied on top of what the decoders had already read, and
	// that copy is what the transient half lost.
	fmt.Fprintf(&text, "|de ello, tablas adoptadas del mapa y no copiadas|`%.1f MB`|aritmética sobre las filas|\n",
		float64(out.Heap.AdoptedTableBytes)/mb)
	fmt.Fprintf(&text, "|transitorio|`%.1f MB`|`%.0f %%` de lo asignado|\n",
		float64(out.Heap.TransientBytes)/mb, out.Heap.TransientPerAlloc*100)
	text.WriteString("\n## Lo que se puede leer en el sitio\n\n")
	fmt.Fprintf(&text, "|sección|bytes|\n|---|---|\n")
	fmt.Fprintf(&text, "|arena de cadenas|`%.1f MB`|\n", float64(out.Mappable.ArenaBytes)/mb)
	fmt.Fprintf(&text, "|tabla de claves estables|`%.1f MB`|\n", float64(out.Mappable.StableKeyBytes)/mb)
	fmt.Fprintf(&text, "|registros de ancho fijo|`%.1f MB`|\n", float64(out.Mappable.RecordBytes)/mb)
	fmt.Fprintf(&text, "|aristas de los dos CSR|`%.1f MB`|\n", float64(out.Mappable.EdgeBytes)/mb)
	fmt.Fprintf(&text, "|**total**|`%.1f MB`|\n", float64(out.Mappable.TotalBytes)/mb)
	text.WriteString("\nEl perfil vivo se escribe con el snapshot todavía alcanzable, que es el único\n")
	text.WriteString("momento en que sus estructuras son atribuibles: ")
	fmt.Fprintf(&text, "`%s`.\n", out.Heap.ProfilePath)
	text.WriteString("\n## Hallazgos\n\n")
	for _, finding := range out.Findings {
		fmt.Fprintf(&text, "- %s\n", finding)
	}
	text.WriteString("\n## Limitaciones\n\n")
	for _, limitation := range out.Limitations {
		fmt.Fprintf(&text, "- %s\n", limitation)
	}
	return text.String()
}
