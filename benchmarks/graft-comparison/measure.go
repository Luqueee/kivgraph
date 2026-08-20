package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// codeExtensions are the files the native arm searches. It is the union of what
// both surfaces index, so neither is credited with an answer the other could not
// have been asked for.
var codeExtensions = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".go": true, ".rs": true,
}

// repositories maps a corpus-relative repository directory to the name kivgraph
// registered for it, which is the directory's base name.
type repositories struct {
	dirs  []string // longest first, so a nested repository wins
	names map[string]string
}

func discoverRepositories(corpus string) (repositories, error) {
	out := repositories{names: map[string]string{}}
	err := filepath.WalkDir(corpus, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if entry.Name() == "node_modules" {
			return fs.SkipDir
		}
		if entry.Name() != ".git" {
			return nil
		}
		dir := filepath.Dir(path)
		relative, relErr := filepath.Rel(corpus, dir)
		if relErr != nil {
			return nil
		}
		if relative == "." {
			return fs.SkipDir
		}
		out.dirs = append(out.dirs, relative)
		out.names[relative] = filepath.Base(relative)
		return fs.SkipDir
	})
	if err != nil {
		return repositories{}, fmt.Errorf("walk %s: %w", corpus, err)
	}
	sort.Slice(out.dirs, func(i, j int) bool { return len(out.dirs[i]) > len(out.dirs[j]) })
	if len(out.dirs) == 0 {
		return repositories{}, fmt.Errorf("no git repository under %s", corpus)
	}
	return out, nil
}

// canonical is the one address this harness compares on: the registered
// repository name and the path inside it. kivgraph answers repository-relative
// and graft answers corpus-relative, and two repositories can both hold a
// `src/index.ts`, so neither spelling is comparable on its own.
func (r repositories) canonical(corpusRelative string) string {
	corpusRelative = strings.TrimPrefix(filepath.ToSlash(corpusRelative), "./")
	for _, dir := range r.dirs {
		if corpusRelative == dir {
			return r.names[dir] + ":"
		}
		if strings.HasPrefix(corpusRelative, dir+"/") {
			return r.names[dir] + ":" + strings.TrimPrefix(corpusRelative, dir+"/")
		}
	}
	return ":" + corpusRelative
}

func (r repositories) canonicalAll(corpusRelative []string) []string {
	out := make([]string, 0, len(corpusRelative))
	for _, path := range corpusRelative {
		out = append(out, r.canonical(path))
	}
	return out
}

// score compares a claimed set of files against the truth. Precision and recall
// are file-level: a disagreement about which line inside a file holds the call
// is not counted, which is the same granularity the earlier comparison used.
type score struct {
	Files     []string `json:"files"`
	Precision float64  `json:"precision"`
	Recall    float64  `json:"recall"`
	Missing   []string `json:"missing"`
	Spurious  []string `json:"spurious"`
}

func scoreAgainst(claimed, truth []string) *score {
	claimedSet, truthSet := toSet(claimed), toSet(truth)
	out := score{Files: sortedKeys(claimedSet)}
	hits := 0
	for path := range claimedSet {
		if truthSet[path] {
			hits++
		} else {
			out.Spurious = append(out.Spurious, path)
		}
	}
	for path := range truthSet {
		if !claimedSet[path] {
			out.Missing = append(out.Missing, path)
		}
	}
	sort.Strings(out.Spurious)
	sort.Strings(out.Missing)
	if len(claimedSet) > 0 {
		out.Precision = float64(hits) / float64(len(claimedSet))
	}
	if len(truthSet) > 0 {
		out.Recall = float64(hits) / float64(len(truthSet))
	}
	return &out
}

func toSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			out[value] = true
		}
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// ---------- kivgraph ----------

// referencesPage is the shape find_references answers in once ADR 0046 hoisted
// every field that repeated. What was a row per fact carrying its own
// repository, confidence and provenance is now the subject once, the repository
// once, and rows grouped by whatever they share -- so a row is a symbol and a
// line, and nothing else.
//
// The container has two forms and the parser must read both: one group collapses
// into `results.files`, several stay under `results.groups`. An `at` entry is a
// bare string when its group already hoisted the edge kind and a tuple when it
// did not. Reading only one form would silently score half an answer.
type referencesPage struct {
	Total      int     `json:"total"`
	Returned   int     `json:"returned"`
	Truncated  bool    `json:"truncated"`
	NextCursor *string `json:"next_cursor"`
	Results    struct {
		Subject    string      `json:"subject"`
		Direction  string      `json:"direction"`
		Repository string      `json:"repository"`
		Files      []fileRows  `json:"files"`
		Groups     []groupRows `json:"groups"`
	} `json:"results"`
}

// groupRows is one hoisting level: the rows that share a kind and an edge kind.
type groupRows struct {
	Kind       string     `json:"kind"`
	EdgeKind   string     `json:"edge_kind"`
	Repository string     `json:"repository"`
	Files      []fileRows `json:"files"`
}

// fileRows is one file and the positions inside it. `Repository` is empty
// whenever the answer hoisted it, which is the common case; a group or a file
// that names its own overrides it, so an answer spanning repositories is still
// addressed correctly rather than attributed to the subject's repository.
type fileRows struct {
	File       string            `json:"file"`
	Repository string            `json:"repository"`
	At         []json.RawMessage `json:"at"`
	// Count is how many facts the file holds, which is what the `files` view
	// sends instead of a position each. It is the same multiplicity `At` spells
	// out one entry at a time.
	Count int `json:"count"`
}

// rows walks the two container forms and yields one address per fact, keeping
// multiplicity: seven calls in one file are seven facts, and the scorer is what
// decides to compare sets of files.
//
// The `files` view carries no header repository, because a file list spanning
// repositories has nowhere to hoist one; each entry names its own as
// `repository/path`. So an empty header is not a missing field, it is the signal
// that the address is already whole.
func (page referencesPage) rows() []string {
	out := make([]string, 0, page.Returned)
	collect := func(files []fileRows, groupRepository string) {
		for _, file := range files {
			repository := page.Results.Repository
			if groupRepository != "" {
				repository = groupRepository
			}
			if file.Repository != "" {
				repository = file.Repository
			}
			address := repository + ":" + file.File
			if repository == "" {
				if name, path, found := strings.Cut(file.File, "/"); found {
					address = name + ":" + path
				}
			}
			facts := len(file.At)
			if facts == 0 {
				facts = file.Count
			}
			for range facts {
				out = append(out, address)
			}
		}
	}
	collect(page.Results.Files, "")
	for _, group := range page.Results.Groups {
		collect(group.Files, group.Repository)
	}
	return out
}

// kivgraphFiles reads the referencing files out of one page, as
// `repository:path`, which is already the canonical address, and returns the
// cursor when the answer did not fit.
//
// The cursor is not a detail to skip. "Which files call this" is a question
// about a set, and a first page of 50 out of 66 answers a different one, so the
// arm follows the cursor and pays for every page it needed.
func kivgraphFiles(text string) ([]string, referencesPage, error) {
	decoded := referencesPage{}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, decoded, fmt.Errorf("parse find_references page: %w", err)
	}
	return decoded.rows(), decoded, nil
}

// symbolsPage is the shape find_symbol answers in: the name once, then the
// declarations grouped by kind, and a symbol addressed as `repository:path:line`
// instead of three keys repeated per row.
type symbolsPage struct {
	Total   int `json:"total"`
	Results struct {
		Name   string `json:"name"`
		Groups []struct {
			Kind    string `json:"kind"`
			Symbols []struct {
				At string `json:"at"`
				QN string `json:"qn"`
			} `json:"symbols"`
		} `json:"groups"`
	} `json:"results"`
}

// addressOf drops the line from `repository:path:line`. The line is what makes
// two declarations in one file two rows, and the census counts files.
func addressOf(at string) string {
	if index := strings.LastIndex(at, ":"); index > 0 {
		return at[:index]
	}
	return at
}

// bindingKinds are the rows a symbol search returns that are not declarations:
// an `import` binds a name into a file, and an `export` either re-exports one
// from elsewhere or is the export binding of a declaration the same page already
// carries as `function`. Counting them as declarations would inflate the census
// with the graph's own re-export chain, which is why the census filters on kind
// rather than on the row count.
var bindingKinds = map[string]bool{"import": true, "export": true}

// kivgraphDeclarations reads the declaration sites out of a find_symbol page:
// every row whose kind is a definition, deduplicated by file.
func kivgraphDeclarations(text string) ([]string, int, error) {
	decoded := symbolsPage{}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, 0, fmt.Errorf("parse find_symbol page: %w", err)
	}
	seen := map[string]bool{}
	out := []string{}
	for _, group := range decoded.Results.Groups {
		if bindingKinds[group.Kind] {
			continue
		}
		for _, symbol := range group.Symbols {
			address := addressOf(symbol.At)
			if !seen[address] {
				seen[address] = true
				out = append(out, address)
			}
		}
	}
	return out, decoded.Total, nil
}

// kivgraphAmbiguity reads how many declarations a refusal refused between, so
// the number means the same thing on both arms: graft says "7 definitions share
// the name", and this says `declares 7 symbols`.
var kivgraphAmbiguity = regexp.MustCompile(`declares (\d+) symbols`)

func ambiguousDeclarations(message string) int {
	match := kivgraphAmbiguity.FindStringSubmatch(message)
	if match == nil {
		return 0
	}
	count, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return count
}

// ---------- graft ----------

var (
	graftBlockHead  = regexp.MustCompile(`^(\S+) · ([a-z ]+) · (.+?):L(\d+)(?:-L(\d+))?\s*$`)
	graftCallerLine = regexp.MustCompile(`^\s+calls (?:←|<-|→|->) .*?\(([^()]+):L(\d+)`)
	graftAmbiguity  = regexp.MustCompile(`(\d+) definitions? share the name`)
	graftNoCallers  = regexp.MustCompile(`no indexed callers`)
	// A graft_find_all row comes in two shapes: a hit inside a symbol, and a hit
	// at a file's top level, which names the path before the separator instead of
	// after it. Reading only the first shape would credit graft with a fraction
	// of the answer it actually gave -- most import statements are module level.
	graftGrepSymbolRow = regexp.MustCompile(`^\S.*? · [a-z ]+ · (.+?):L\d+`)
	graftGrepModuleRow = regexp.MustCompile(`^(\S.*?) \(module level\) ·`)
)

// graftBlock is one declaration graft answered about, with the callers it
// attributed to that declaration.
type graftBlock struct {
	Name      string
	Path      string
	Callers   []string
	Ambiguous int
	Empty     bool
}

// parseGraftTrace splits a graft_trace_calls answer into one block per
// declaration. Attribution matters here: graft answers about every declaration
// sharing the name, so counting every caller it prints would credit it with
// callers of a different symbol -- the exact error this benchmark measures in
// other tools.
func parseGraftTrace(text string) []graftBlock {
	blocks := []graftBlock{}
	for _, line := range strings.Split(text, "\n") {
		if head := graftBlockHead.FindStringSubmatch(line); head != nil {
			blocks = append(blocks, graftBlock{Name: head[1], Path: filepath.ToSlash(head[3])})
			continue
		}
		if len(blocks) == 0 {
			continue
		}
		current := &blocks[len(blocks)-1]
		if caller := graftCallerLine.FindStringSubmatch(line); caller != nil {
			current.Callers = append(current.Callers, filepath.ToSlash(caller[1]))
			continue
		}
		if graftNoCallers.MatchString(line) {
			current.Empty = true
		}
		if ambiguity := graftAmbiguity.FindStringSubmatch(line); ambiguity != nil {
			fmt.Sscanf(ambiguity[1], "%d", &current.Ambiguous)
		}
	}
	return blocks
}

// graftCallersOf returns the callers graft attributed to one declaration, found
// by the path it was asked about. A block that is absent is not the same as a
// block with no callers, so the two are distinguished.
func graftCallersOf(blocks []graftBlock, corpusPath string) ([]string, bool) {
	for _, block := range blocks {
		if block.Path == corpusPath {
			return block.Callers, true
		}
	}
	return nil, false
}

// graftDeclarations reads every declaration graft listed, which a trace answer
// carries for free when the name is ambiguous.
func graftDeclarations(blocks []graftBlock) []string {
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, block.Path)
	}
	return out
}

// graftGrepFiles reads the files out of a graft_find_all answer, which groups
// hits by enclosing symbol and prints file-level hits under the path itself.
func graftGrepFiles(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, line := range strings.Split(text, "\n") {
		match := graftGrepSymbolRow.FindStringSubmatch(line)
		if match == nil {
			match = graftGrepModuleRow.FindStringSubmatch(line)
		}
		if match == nil {
			continue
		}
		path := filepath.ToSlash(match[1])
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

// ---------- native arm ----------

// nativeArm is what the same question costs with the tools every agent already
// has: a corpus-wide regex search, plus a full read of every file that declares
// the name. The read is not optional padding -- it is the minimum needed to tell
// seven `withRetry` apart -- and leaving it out would price an answer `grep`
// cannot actually give.
type nativeArm struct {
	Pattern       string  `json:"pattern"`
	SearchedFiles int     `json:"searched_files"`
	MatchLines    int     `json:"match_lines"`
	SearchTokens  int     `json:"search_tokens"`
	ReadFiles     int     `json:"read_files"`
	ReadTokens    int     `json:"read_tokens"`
	Tokens        int     `json:"tokens"`
	MS            float64 `json:"ms"`
}

// searchCorpus imitates `rg -n` over the corpus: one `path:line:text` per hit.
func searchCorpus(corpus, name string) (string, int, int, error) {
	pattern, err := regexp.Compile(`\b` + regexp.QuoteMeta(name) + `\b`)
	if err != nil {
		return "", 0, 0, err
	}
	builder := strings.Builder{}
	searched, matched := 0, 0
	walkErr := filepath.WalkDir(corpus, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !codeExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			return nil
		}
		searched++
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if !pattern.Match(content) {
			return nil
		}
		relative, _ := filepath.Rel(corpus, path)
		for index, line := range strings.Split(string(content), "\n") {
			if pattern.MatchString(line) {
				matched++
				builder.WriteString(fmt.Sprintf("%s:%d:%s\n", filepath.ToSlash(relative), index+1, strings.TrimSpace(line)))
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", 0, 0, fmt.Errorf("search %s: %w", corpus, walkErr)
	}
	return builder.String(), searched, matched, nil
}

func measureNative(tokens *counter, corpus, name string, declarations []string) (nativeArm, error) {
	output, searched, matched, err := searchCorpus(corpus, name)
	if err != nil {
		return nativeArm{}, err
	}
	arm := nativeArm{
		Pattern: `\b` + name + `\b`, SearchedFiles: searched, MatchLines: matched,
		SearchTokens: tokens.count(output), ReadFiles: len(declarations),
	}
	for _, declaration := range declarations {
		content, readErr := os.ReadFile(filepath.Join(corpus, declaration))
		if readErr != nil {
			return nativeArm{}, fmt.Errorf("read %s: %w", declaration, readErr)
		}
		arm.ReadTokens += tokens.count(string(content))
	}
	arm.Tokens = arm.SearchTokens + arm.ReadTokens
	return arm, nil
}

// totalOf reads the `total` a kivgraph page declares, which is the number of
// facts the query matched rather than the number this page returned.
func totalOf(text string) int {
	header := struct {
		Total int `json:"total"`
	}{}
	if err := json.Unmarshal([]byte(text), &header); err != nil {
		return 0
	}
	return header.Total
}

// filepathWalkSize sums the bytes a state directory occupies. A build that is
// fast because it stored less is a different trade, not a free win, so the two
// surfaces' footprints are reported beside their timings.
func filepathWalkSize(path string, total *int64) error {
	return filepath.WalkDir(path, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if info, statErr := entry.Info(); statErr == nil {
			*total += info.Size()
		}
		return nil
	})
}
