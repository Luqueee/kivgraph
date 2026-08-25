// Command docstring-simulation answers one question before anybody pays for it:
// would the prose a codebase already carries turn the questions this graph
// cannot reach into answers?
//
// The measurement that motivated it found that fourteen of eighteen misses are
// not a ranking failure but an unreachable file -- asked with the search pointed
// straight at it, the file returns nothing, because no name, qualified name,
// kind or path of it carries a word of the question. Persisting docstrings would
// change that, and persisting them costs a canonical schema version, a snapshot
// format version, five loaders and a full rebuild for every installation.
//
// So this harness builds the index that change would produce, outside the
// product and over the generation already published: the same fold, the same
// score, the same neighbour walk, the same view. One text source is added per
// symbol and nothing else moves. Two arms run over one corpus -- without prose
// and with it -- and the arm without prose is the control: if it does not
// reproduce what the live tool answers, the simulation is measuring itself.
package main

import (
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/retrieval"
)

const (
	benchmarkName = "docstring-simulation"
	// These three mirror internal/mcp/tools/find_by_intent.go. They are declared
	// again rather than exported: the tool's bounds are not API, and a
	// simulation that quietly used different ones would answer a question about
	// a surface nobody ships.
	maximumIntentCandidates = 4_000
	maximumNeighbourTerms   = 32
	maximumNeighbourEdges   = 64
	// intentLimit is the page the measured harness asks for, in rows of the
	// files view, which is what an agent reads before opening anything.
	intentLimit = 10
)

// index is the inverted index this simulation builds. It is the shape
// internal/hotsnapshot/terms.go publishes -- sorted keys, run offsets, symbol
// ids -- because the point is to price that structure and not a map.
type index struct {
	keys    []retrieval.TermKey
	offsets []uint32
	values  []hotsnapshot.SymbolID
	// structural is aligned with values: false marks a posting that exists only
	// because of prose. Without it a term read from a comment weighs exactly as
	// much as one read from the declaration's own name, which is what the first
	// run of this harness measured -- and it is a wash, because the file that
	// mentions the words outranks the file that implements them.
	structural []bool
	postings   int
}

func (idx index) lookup(key retrieval.TermKey) ([]hotsnapshot.SymbolID, []bool) {
	position, found := slices.BinarySearch(idx.keys, key)
	if !found {
		return nil, nil
	}
	start, end := idx.offsets[position], idx.offsets[position+1]
	return idx.values[start:end], idx.structural[start:end]
}

// build indexes every symbol from the text sources the snapshot already carries,
// plus the docstring when one is supplied for it. A nil map is the control arm.
func build(snapshot *hotsnapshot.GraphSnapshot, docs map[hotsnapshot.SymbolID]string) index {
	type posting struct {
		key        retrieval.TermKey
		symbol     hotsnapshot.SymbolID
		structural bool
	}
	table := snapshot.Strings()
	total := int(snapshot.Metadata().Counts.Symbols)
	postings := make([]posting, 0, total*8)
	var (
		spans []retrieval.Span
		keys  []retrieval.TermKey
	)
	for id := 0; id < total; id++ {
		symbol, ok := snapshot.Symbol(hotsnapshot.SymbolID(id))
		if !ok {
			continue
		}
		keys = keys[:0]
		for _, interned := range [...]hotsnapshot.InternedString{symbol.Name, symbol.QualifiedName, symbol.Kind} {
			if text, ok := table.String(interned); ok {
				keys, spans = retrieval.AppendTextKeys(keys, spans, text)
			}
		}
		if file, ok := snapshot.File(symbol.File); ok {
			if path, ok := table.String(file.Path); ok {
				keys, spans = retrieval.AppendTextKeys(keys, spans, path)
			}
		}
		// The structural keys are everything the graph already holds. A prose key
		// that repeats one of them is structural: the weaker source does not
		// weaken evidence that also exists in the declaration itself.
		slices.Sort(keys)
		structural := slices.Compact(slices.Clone(keys))
		if doc := docs[hotsnapshot.SymbolID(id)]; doc != "" {
			keys, spans = retrieval.AppendTextKeys(keys, spans, doc)
		}
		// One posting per distinct term, exactly as the shipped index does: a
		// repeated term is the ranker's business and a duplicate posting would
		// corrupt the frequency this index publishes.
		slices.Sort(keys)
		for _, key := range slices.Compact(keys) {
			postings = append(postings, posting{
				key: key, symbol: hotsnapshot.SymbolID(id),
				structural: containsKey(structural, key),
			})
		}
	}
	slices.SortFunc(postings, func(left, right posting) int {
		return cmp.Or(cmp.Compare(left.key, right.key), cmp.Compare(left.symbol, right.symbol))
	})

	out := index{
		values:     make([]hotsnapshot.SymbolID, len(postings)),
		structural: make([]bool, len(postings)),
		postings:   len(postings),
	}
	out.offsets = append(out.offsets, 0)
	for position, item := range postings {
		out.values[position] = item.symbol
		out.structural[position] = item.structural
	}
	for position := 0; position < len(postings); {
		key := postings[position].key
		end := position + 1
		for end < len(postings) && postings[end].key == key {
			end++
		}
		out.keys = append(out.keys, key)
		out.offsets = append(out.offsets, uint32(end))
		position = end
	}
	return out
}

// containsKey is a membership test over a sorted, compacted key list.
func containsKey(keys []retrieval.TermKey, key retrieval.TermKey) bool {
	_, found := slices.BinarySearch(keys, key)
	return found
}

// docstring is the comment block immediately above a declaration, which is what
// every loader would have to emit and what none emits today. It is read with a
// text rule and not a parser, and that is a declared limitation of this harness:
// a parser is what the real change would use, and it would find slightly more.
func docstring(lines []string, startLine uint32) string {
	if startLine == 0 || int(startLine) > len(lines) {
		return ""
	}
	end := int(startLine) - 1
	start := end
	for start > 0 {
		trimmed := strings.TrimSpace(lines[start-1])
		if !isCommentLine(trimmed) {
			break
		}
		start--
	}
	if start == end {
		return ""
	}
	return strings.Join(lines[start:end], " ")
}

// isCommentLine is the text rule, kept in one place so the limitation this
// harness declares is a function and not a condition spread over a loop.
func isCommentLine(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	for _, prefix := range [...]string{"//", "*", "/*"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return strings.HasSuffix(trimmed, "*/")
}

// corpusShare is how many symbols each repository contributes. It belongs in
// this report because it is the first thing that explains a repository the
// ranking never reaches: one window of ten rows is shared by every repository in
// the graph, and the largest one does not have to be right to fill it.
type corpusShare struct {
	Repository string `json:"repository"`
	Symbols    int    `json:"symbols"`
}

type proseStats struct {
	Documented   int `json:"documented_symbols"`
	Symbols      int `json:"symbols"`
	Bytes        int `json:"docstring_bytes"`
	WithLiterals int `json:"symbols_with_literals"`
	LiteralBytes int `json:"literal_bytes"`
	FilesRead    int `json:"files_read"`
	FilesMissed  int `json:"files_not_on_disk"`
}

// literals is the text a symbol's own body shows a user: messages, errors, keys.
// It is a different hypothesis from prose and the measurement that motivated it
// is grep's: the only two questions it answered in mole were answered by words
// living inside a string -- "already running", and a log line about a file that
// shrank -- not by a comment and not by a name. A message is also rarer than a
// comment, which is the property the prose arm lacked.
func literals(lines []string, startLine, endLine uint32) string {
	if startLine == 0 || int(startLine) > len(lines) {
		return ""
	}
	end := int(endLine)
	if end > len(lines) || end < int(startLine) {
		end = int(startLine)
	}
	body := strings.Join(lines[int(startLine)-1:end], "\n")
	found := literalPattern.FindAllStringSubmatch(body, -1)
	if len(found) == 0 {
		return ""
	}
	parts := make([]string, 0, len(found))
	for _, match := range found {
		for _, group := range match[1:] {
			if group != "" {
				parts = append(parts, group)
			}
		}
	}
	return strings.Join(parts, " ")
}

// literalPattern reads interpreted and raw string literals. It is a text rule,
// like the docstring one, and it declares the same limitation: a parser would
// tell a literal from a character class inside a regular expression, and this
// does not.
var literalPattern = regexp.MustCompile("\"([^\"\\n]{2,})\"|`([^`]{2,})`")

// collect reads the prose out of the checkouts the question set names. A file the
// graph indexed and the disk no longer holds is counted and skipped: it is the
// one way this simulation can quietly shrink, so it is reported instead.
func collect(
	snapshot *hotsnapshot.GraphSnapshot,
	checkouts map[string]string,
) (map[hotsnapshot.SymbolID]string, map[hotsnapshot.SymbolID]string, proseStats, error) {
	table := snapshot.Strings()
	total := int(snapshot.Metadata().Counts.Symbols)
	stats := proseStats{Symbols: total}
	docs := make(map[hotsnapshot.SymbolID]string, total/3)
	texts := make(map[hotsnapshot.SymbolID]string, total/3)
	cache := map[hotsnapshot.FileID][]string{}
	missing := map[hotsnapshot.FileID]bool{}
	for id := 0; id < total; id++ {
		symbol, ok := snapshot.Symbol(hotsnapshot.SymbolID(id))
		if !ok {
			continue
		}
		lines, cached := cache[symbol.File]
		if !cached && !missing[symbol.File] {
			file, ok := snapshot.File(symbol.File)
			if !ok {
				continue
			}
			path, pathOK := table.String(file.Path)
			repository, repoOK := snapshot.Repository(file.Repository)
			if !pathOK || !repoOK {
				continue
			}
			name, nameOK := table.String(repository.Name)
			checkout, declared := checkouts[name]
			if !nameOK || !declared {
				missing[symbol.File] = true
				stats.FilesMissed++
				continue
			}
			content, err := os.ReadFile(filepath.Join(checkout, path))
			if err != nil {
				missing[symbol.File] = true
				stats.FilesMissed++
				continue
			}
			lines = strings.Split(string(content), "\n")
			cache[symbol.File] = lines
			stats.FilesRead++
		}
		if lines == nil {
			continue
		}
		if doc := docstring(lines, symbol.StartLine); doc != "" {
			docs[hotsnapshot.SymbolID(id)] = doc
			stats.Documented++
			stats.Bytes += len(doc)
		}
		if text := literals(lines, symbol.StartLine, symbol.EndLine); text != "" {
			texts[hotsnapshot.SymbolID(id)] = text
			stats.WithLiterals++
			stats.LiteralBytes += len(text)
		}
	}
	return docs, texts, stats, nil
}

// fileRow is one row of the files view: the granularity the question asks at.
type fileRow struct {
	Repo string `json:"repo"`
	Path string `json:"file"`
}

// explanation is one candidate with the signals that placed it, which is the
// only way to tell a weight that is wrong from a corpus that is right.
type explanation struct {
	Repo       string
	Path       string
	Qualified  string
	Kind       string
	Exported   bool
	Hits       int
	Prose      int
	Neighbours int
	Callers    int
	Score      float64
}

// rank mirrors rankIntentCandidates over the supplied index. Every weight, bound
// and tie-break is the shipped one; only the index differs between the two arms.
func rank(
	snapshot *hotsnapshot.GraphSnapshot,
	idx index,
	intent string,
	keywords []string,
	limit int,
	proseWeight float64,
	explain *[]explanation,
) []fileRow {
	table := snapshot.Strings()
	corpus := int(snapshot.Metadata().Counts.Symbols)
	type candidate struct {
		symbol      hotsnapshot.SymbolID
		hits        int
		prose       int
		frequencies []int
		lengths     []int
		terms       uint32
		neighbours  int
	}
	candidates := map[hotsnapshot.SymbolID]*candidate{}
	used := 0
	for _, word := range retrieval.QueryWords(intent, keywords) {
		found, structural := idx.lookup(retrieval.Fold(word))
		if len(found) == 0 {
			continue
		}
		frequency := len(found)
		used++
		for position, id := range found {
			entry := candidates[id]
			if entry == nil {
				if len(candidates) >= maximumIntentCandidates {
					continue
				}
				entry = &candidate{symbol: id}
				candidates[id] = entry
			}
			entry.hits++
			if !structural[position] {
				entry.prose++
			}
			entry.frequencies = append(entry.frequencies, frequency)
			entry.lengths = append(entry.lengths, len([]rune(word)))
			if termIndex := used - 1; termIndex < maximumNeighbourTerms {
				entry.terms |= 1 << uint(termIndex)
			}
		}
	}
	for _, entry := range candidates {
		reached := uint32(0)
		for position, edge := range snapshot.Outgoing(entry.symbol) {
			if position >= maximumNeighbourEdges {
				break
			}
			if neighbour := candidates[edge.Target]; neighbour != nil {
				reached |= neighbour.terms
			}
		}
		entry.neighbours = bits.OnesCount32(reached & ^entry.terms)
	}
	type scored struct {
		repo  string
		path  string
		score float64
		id    hotsnapshot.SymbolID
	}
	rows := make([]scored, 0, len(candidates))
	for _, entry := range candidates {
		symbol, ok := snapshot.Symbol(entry.symbol)
		if !ok {
			continue
		}
		file, fileOK := snapshot.File(symbol.File)
		if !fileOK {
			continue
		}
		repository, repoOK := snapshot.Repository(file.Repository)
		path, pathOK := table.String(file.Path)
		name, nameOK := table.String(repository.Name)
		kind, kindOK := table.String(symbol.Kind)
		if !repoOK || !pathOK || !nameOK || !kindOK {
			continue
		}
		// The one multiplier this experiment adds, and the shape it would take in
		// the product: how much of a candidate's evidence is prose. At weight 1
		// it is the identity and this arm is the unweighted index; below 1 a
		// candidate reached only through comments keeps its place in the order
		// but loses to one whose declaration carries the same words.
		share := 1.0
		if entry.hits > 0 {
			structuralHits := entry.hits - entry.prose
			share = (float64(structuralHits) + proseWeight*float64(entry.prose)) / float64(entry.hits)
		}
		rows = append(rows, scored{
			repo: name, path: path, id: entry.symbol,
			score: share * retrieval.Score(retrieval.Signals{
				Hits: entry.hits, Frequencies: entry.frequencies,
				Lengths: entry.lengths, Symbols: corpus, Neighbours: entry.neighbours,
				Kind: kind, Exported: symbol.Exported, Path: path,
				Callers: snapshot.IncomingCount(entry.symbol),
			}),
		})
	}
	sort.SliceStable(rows, func(left, right int) bool {
		if rows[left].score != rows[right].score {
			return rows[left].score > rows[right].score
		}
		return rows[left].id < rows[right].id
	})
	files := make([]fileRow, 0, limit)
	seen := map[string]bool{}
	for _, row := range rows {
		key := row.repo + "\x00" + row.path
		if seen[key] {
			continue
		}
		seen[key] = true
		files = append(files, fileRow{Repo: row.repo, Path: row.path})
		if len(files) == limit {
			break
		}
	}
	if explain != nil {
		table := snapshot.Strings()
		for _, row := range rows {
			entry := candidates[row.id]
			symbol, _ := snapshot.Symbol(row.id)
			qualified, _ := table.String(symbol.QualifiedName)
			kind, _ := table.String(symbol.Kind)
			*explain = append(*explain, explanation{
				Repo: row.repo, Path: row.path, Qualified: qualified, Kind: kind,
				Exported: symbol.Exported, Hits: entry.hits, Prose: entry.prose,
				Neighbours: entry.neighbours, Callers: snapshot.IncomingCount(row.id),
				Score: row.score,
			})
			if len(*explain) == explainRows {
				break
			}
		}
	}
	return files
}

// explainRows bounds the trace: an order is explained by its head and by the row
// that should have been in it, not by four thousand candidates.
const explainRows = 400

type questionSet struct {
	Repositories map[string]string `json:"repositories"`
	Questions    []struct {
		Intent   string   `json:"intent"`
		Repo     string   `json:"repo"`
		Class    string   `json:"class"`
		Answer   []string `json:"answer"`
		Keywords []string `json:"keywords"`
	} `json:"questions"`
}

type questionResult struct {
	Intent string `json:"intent"`
	Repo   string `json:"repo"`
	Class  string `json:"class"`
	// Base is the rank without prose, which must reproduce what the shipped tool
	// answers; Prose is the same question over the index a schema version would
	// buy. Zero means the file is not in the page at all.
	Base int `json:"base_rank"`
	// Prose is positionally aligned with the sweep below: one rank per weight.
	Prose []int `json:"prose_ranks"`
}

type armStats struct {
	// Source names the text this arm indexed beside the graph's own fields.
	Source string `json:"source"`
	// Weight is the share a prose-only hit keeps. One is the unweighted index,
	// and it is reported because it is the arm that says prose alone is a wash.
	Weight   float64 `json:"prose_weight,omitempty"`
	Terms    int     `json:"terms,omitempty"`
	Postings int     `json:"postings,omitempty"`
	Found    int     `json:"found"`
	TopOne   int     `json:"top_one"`
}

type results struct {
	Benchmark   string            `json:"benchmark"`
	Command     string            `json:"command"`
	GeneratedAt string            `json:"generated_at"`
	Commit      string            `json:"commit"`
	Environment map[string]string `json:"environment"`
	Snapshot    string            `json:"snapshot"`
	QuestionSet string            `json:"question_set"`
	Questions   int               `json:"questions"`
	Corpus      []corpusShare     `json:"corpus"`
	Prose       proseStats        `json:"prose"`
	Base        armStats          `json:"base"`
	ProseIndex  armStats          `json:"prose_index"`
	Sweep       []armStats        `json:"sweep"`
	Results     []questionResult  `json:"results"`
	Limitations []string          `json:"limitations"`
}

func sortedShares(shares map[string]int) []corpusShare {
	out := make([]corpusShare, 0, len(shares))
	for name, count := range shares {
		out = append(out, corpusShare{Repository: name, Symbols: count})
	}
	slices.SortFunc(out, func(left, right corpusShare) int {
		return cmp.Or(cmp.Compare(right.Symbols, left.Symbols), cmp.Compare(left.Repository, right.Repository))
	})
	return out
}

func rankOf(offered []fileRow, repo string, wanted []string) int {
	for position, file := range offered {
		if file.Repo != repo {
			continue
		}
		if slices.Contains(wanted, file.Path) {
			return position + 1
		}
	}
	return 0
}

func main() {
	var (
		questions = flag.String("questions", "benchmarks/intent-token-cost/questions.json",
			"question set to reuse, so both harnesses answer the same questions")
		snapshotPath = flag.String("snapshot", "", "published snapshot to read; default is the newest one")
		directory    = flag.String("directory", "benchmarks/docstring-simulation", "where to write the artifacts")
		explain      = flag.String("explain", "",
			"substring of one question: print the signals that placed the head of its page and its answer, "+
				"asked the way callers are told to ask, and write no artifacts")
	)
	flag.Parse()
	if *explain != "" {
		if err := explainOne(*questions, *snapshotPath, *explain); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := run(*questions, *snapshotPath, *directory); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// currentCommit attributes the numbers to a tree. A dirty suffix is not a
// warning to be hidden: this harness reads the prose off the working tree, so an
// uncommitted comment is a number nobody else can reproduce.
func currentCommit() (string, error) {
	output, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown", nil
	}
	commit := strings.TrimSpace(string(output))
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return commit + "-unknown", nil
	}
	if strings.TrimSpace(string(status)) != "" {
		return commit + "-dirty", nil
	}
	return commit, nil
}

// explainOne prints why one question is ordered the way it is. It asks the way
// the skill tells callers to ask -- guessed words, repository named -- because an
// order explained for a call nobody makes explains nothing.
func explainOne(questionsPath, snapshotPath, needle string) error {
	if snapshotPath == "" {
		var err error
		if snapshotPath, err = newestSnapshot(); err != nil {
			return err
		}
	}
	raw, err := os.ReadFile(questionsPath)
	if err != nil {
		return err
	}
	var set questionSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return err
	}
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return err
	}
	snapshot, err := hotsnapshot.ReadSnapshot(data, [32]byte{})
	if err != nil {
		return err
	}
	for _, question := range set.Questions {
		if !strings.Contains(question.Intent, needle) {
			continue
		}
		trace := []explanation{}
		rank(snapshot, build(snapshot, nil), question.Intent, question.Keywords, intentLimit, 1, &trace)
		fmt.Printf("%s\n  answer %s in %s\n\n", question.Intent, question.Answer[0], question.Repo)
		fmt.Printf("  %-46s %-11s %4s %4s %4s %8s\n", "symbol", "kind", "hits", "near", "call", "score")
		printed := 0
		for position, row := range trace {
			wanted := row.Repo == question.Repo && slices.Contains(question.Answer, row.Path)
			if position >= 6 && !wanted {
				continue
			}
			marker := " "
			if wanted {
				marker = ">"
			}
			label := row.Qualified
			if len(label) > 44 {
				label = label[len(label)-44:]
			}
			fmt.Printf("%s %-46s %-11s %4d %4d %4d %8.2f  #%d %s\n", marker, label, row.Kind,
				row.Hits, row.Neighbours, row.Callers, row.Score, position+1, row.Repo)
			if printed++; wanted && printed > 12 {
				break
			}
		}
		fmt.Println()
	}
	return nil
}

func newestSnapshot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, ".local", "state", "kivgraph", "generations")
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("read generations: %w", err)
	}
	newest := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(root, entry.Name(), "snapshot.kvsnap")
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		if candidate > newest {
			newest = candidate
		}
	}
	if newest == "" {
		return "", fmt.Errorf("no published snapshot under %s", root)
	}
	return newest, nil
}

func run(questionsPath, snapshotPath, directory string) error {
	if snapshotPath == "" {
		var err error
		if snapshotPath, err = newestSnapshot(); err != nil {
			return err
		}
	}
	raw, err := os.ReadFile(questionsPath)
	if err != nil {
		return fmt.Errorf("read question set: %w", err)
	}
	var set questionSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return fmt.Errorf("decode question set: %w", err)
	}
	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	// A zero content digest is the documented way to read a snapshot without
	// asserting which generation it belongs to, which is all this harness needs.
	snapshot, err := hotsnapshot.ReadSnapshot(data, [32]byte{})
	if err != nil {
		return fmt.Errorf("read snapshot %s: %w", snapshotPath, err)
	}
	docs, texts, prose, err := collect(snapshot, set.Repositories)
	if err != nil {
		return err
	}
	shares := map[string]int{}
	table := snapshot.Strings()
	for id := 0; id < int(snapshot.Metadata().Counts.Symbols); id++ {
		symbol, ok := snapshot.Symbol(hotsnapshot.SymbolID(id))
		if !ok {
			continue
		}
		file, fileOK := snapshot.File(symbol.File)
		if !fileOK {
			continue
		}
		repository, repoOK := snapshot.Repository(file.Repository)
		if !repoOK {
			continue
		}
		if name, ok := table.String(repository.Name); ok {
			shares[name]++
		}
	}
	commit, err := currentCommit()
	if err != nil {
		return err
	}
	both := make(map[hotsnapshot.SymbolID]string, len(docs)+len(texts))
	for id, doc := range docs {
		both[id] = doc
	}
	for id, text := range texts {
		both[id] = both[id] + " " + text
	}
	// The control comes first and it is the arm that must reproduce the product.
	arms := []struct {
		name  string
		extra map[hotsnapshot.SymbolID]string
	}{
		{name: "names only", extra: nil},
		{name: "prose", extra: docs},
		{name: "literals", extra: texts},
		{name: "prose and literals", extra: both},
	}
	built := make([]index, 0, len(arms))
	for _, arm := range arms {
		built = append(built, build(snapshot, arm.extra))
	}
	base := built[0]

	out := results{
		Benchmark:   benchmarkName,
		Command:     "go run ./benchmarks/docstring-simulation",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Environment: map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version()},
		Snapshot:    filepath.Base(filepath.Dir(snapshotPath)),
		QuestionSet: questionsPath,
		Commit:      commit,
		Questions:   len(set.Questions),
		Corpus:      sortedShares(shares),
		Prose:       prose,
		Base:        armStats{Terms: len(base.keys), Postings: base.postings},
	}
	// Weight one is the identity, so the first column of every arm is that arm's
	// index without any discount, and the control arm ignores the weight
	// entirely: it has no borrowed text to discount. Every arm runs twice: with
	// the question as asked, and with the words a caller would guess beside it.
	weights := []float64{1.0, 0.3}
	type column struct {
		arm      int
		weight   float64
		keywords bool
	}
	columns := []column{}
	for position, arm := range arms {
		for _, weight := range weights {
			if position == 0 && weight != 1 {
				continue
			}
			for _, guessed := range [...]bool{false, true} {
				columns = append(columns, column{arm: position, weight: weight, keywords: guessed})
				source := arm.name
				if guessed {
					source += " + guessed words"
				}
				out.Sweep = append(out.Sweep, armStats{
					Source: source, Weight: weight,
					Terms: len(built[position].keys), Postings: built[position].postings,
				})
			}
		}
	}
	for _, question := range set.Questions {
		row := questionResult{Intent: question.Intent, Repo: question.Repo, Class: question.Class}
		row.Base = rankOf(
			rank(snapshot, base, question.Intent, nil, intentLimit, 1, nil), question.Repo, question.Answer)
		if row.Base > 0 {
			out.Base.Found++
		}
		if row.Base == 1 {
			out.Base.TopOne++
		}
		for index, spec := range columns {
			var guessed []string
			if spec.keywords {
				guessed = question.Keywords
			}
			place := rankOf(
				rank(snapshot, built[spec.arm], question.Intent, guessed, intentLimit, spec.weight, nil),
				question.Repo, question.Answer)
			row.Prose = append(row.Prose, place)
			if place > 0 {
				out.Sweep[index].Found++
			}
			if place == 1 {
				out.Sweep[index].TopOne++
			}
		}
		out.Results = append(out.Results, row)
	}
	out.Limitations = []string{
		"the docstring is taken with a text rule and not a parser: a loader would attach slightly more, so the prose arm is a floor and not a ceiling",
		"this index is built in memory over a published snapshot; it prices the retrieval a schema version would buy and not the cost of persisting it",
		"the prose arm indexes every comment block above a declaration, including the ones a loader might decline to emit, such as a directive or a licence header",
		fmt.Sprintf("%d questions over %d repositories: a set this size states a direction, not a rate", len(set.Questions), len(set.Repositories)),
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "results.json"), append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	if err := writeReport(directory, out); err != nil {
		return err
	}
	fmt.Printf("%s @ generation %s\n", benchmarkName, out.Snapshot)
	fmt.Printf("prose      %d of %d symbols documented, %.2f MB, %d files read, %d missing\n",
		prose.Documented, prose.Symbols, float64(prose.Bytes)/1e6, prose.FilesRead, prose.FilesMissed)
	fmt.Printf("prose      %d symbols documented; literals %d symbols, %.2f MB\n",
		out.Prose.Documented, out.Prose.WithLiterals, float64(out.Prose.LiteralBytes)/1e6)
	for _, arm := range out.Sweep {
		fmt.Printf("%-19s w=%.2f  %6d postings  %d/%d found, %d top-1\n",
			arm.Source, arm.Weight, arm.Postings, arm.Found, out.Questions, arm.TopOne)
	}
	return nil
}

func writeReport(directory string, out results) error {
	report := &strings.Builder{}
	fmt.Fprintf(report, "# %s\n\n", benchmarkName)
	fmt.Fprintf(report, "Question: would the prose this code already carries answer what the graph cannot reach?\n\n")
	fmt.Fprintf(report, "Generated %s from commit `%s` on %s/%s with %s, over published generation `%s`.\n\n",
		out.GeneratedAt, out.Commit, out.Environment["os"], out.Environment["arch"],
		out.Environment["go"], out.Snapshot)
	fmt.Fprintf(report, "Command: `%s`. Dataset: `%s`, %d questions.\n\n",
		out.Command, out.QuestionSet, out.Questions)
	fmt.Fprintf(report, "## The corpus one window is shared by\n\n")
	fmt.Fprintf(report, "|repository|symbols|share|\n|---|---|---|\n")
	for _, share := range out.Corpus {
		fmt.Fprintf(report, "|`%s`|%d|%.0f%%|\n", share.Repository, share.Symbols,
			100*float64(share.Symbols)/float64(out.Prose.Symbols))
	}
	fmt.Fprintf(report, "\n")

	fmt.Fprintf(report, "## What the prose is\n\n")
	fmt.Fprintf(report, "|documented symbols|corpus|bytes|files read|files not on disk|\n|---|---|---|---|---|\n")
	fmt.Fprintf(report, "|%d|%d|%.2f MB|%d|%d|\n\n", out.Prose.Documented, out.Prose.Symbols,
		float64(out.Prose.Bytes)/1e6, out.Prose.FilesRead, out.Prose.FilesMissed)
	fmt.Fprintf(report, "## What each arm costs and buys\n\n")
	fmt.Fprintf(report, "|indexed text|borrowed hit worth|terms|postings|per symbol|found|first|\n")
	fmt.Fprintf(report, "|---|---|---|---|---|---|---|\n")
	for _, arm := range out.Sweep {
		fmt.Fprintf(report, "|%s|%.2f|%d|%d|%.2f|%d of %d|%d|\n",
			arm.Source, arm.Weight, arm.Terms, arm.Postings,
			float64(arm.Postings)/float64(out.Prose.Symbols),
			arm.Found, out.Questions, arm.TopOne)
	}
	fmt.Fprintf(report, "\n")

	fmt.Fprintf(report, "|question|repo|names")
	for _, arm := range out.Sweep[1:] {
		fmt.Fprintf(report, "|%s w=%.2f", arm.Source, arm.Weight)
	}
	fmt.Fprintf(report, "|\n|---|---|---")
	for range out.Sweep {
		fmt.Fprintf(report, "|---")
	}
	fmt.Fprintf(report, "|\n")
	for _, row := range out.Results {
		fmt.Fprintf(report, "|%s|`%s`|%s", row.Intent, row.Repo, cell(row.Base))
		for _, place := range row.Prose {
			fmt.Fprintf(report, "|%s", cell(place))
		}
		fmt.Fprintf(report, "|\n")
	}

	fmt.Fprintf(report, "\n## Limitations\n\n")
	for _, note := range out.Limitations {
		fmt.Fprintf(report, "- %s\n", note)
	}
	return os.WriteFile(filepath.Join(directory, "report.md"), []byte(report.String()), 0o644)
}

func cell(rank int) string {
	if rank == 0 {
		return "**not offered**"
	}
	return fmt.Sprintf("%d", rank)
}
