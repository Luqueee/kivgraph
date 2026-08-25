package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/retrieval"
)

const (
	DefaultIntentLimit      = 10
	MaximumIntentLimit      = 50
	MaximumIntentKeywords   = 16
	MaximumIntentChars      = 400
	SortingVersionIntentV1  = "intent-v1"
	findByIntentToolName    = "find_by_intent"
	maximumIntentCandidates = 4_000
)

// FindByIntentInput asks the one question the rest of this surface cannot: which
// symbols a description most likely names, when the caller does not know what
// anything is called.
//
// Keywords extend the question rather than replacing it, and they are where a
// caller supplies the vocabulary the code uses when it differs from the
// vocabulary the question used. There is no thesaurus here and no embedding: the
// model asking already knows more synonyms than a table would hold.
//
// repo, path_prefix and kind narrow which candidates are considered, so they
// change the answer rather than trimming it -- unlike the traversals of this
// surface, a retrieval has no reachability to preserve and a narrower corpus is
// simply a narrower question.
type FindByIntentInput struct {
	Intent         string   `json:"intent"`
	Keywords       []string `json:"keywords,omitempty"`
	Repo           string   `json:"repo,omitempty"`
	PathPrefix     string   `json:"path_prefix,omitempty"`
	Kind           string   `json:"kind,omitempty"`
	Limit          int      `json:"limit,omitempty"`
	Cursor         string   `json:"cursor,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
	View           string   `json:"view,omitempty"`
}

// IntentMatches is a page of ranked candidates and an account of the terms that
// produced them.
type IntentMatches struct {
	Terms     []IntentTerm   `json:"terms"`
	Unmatched []string       `json:"unmatched_terms,omitempty"`
	Symbols   []IntentSymbol `json:"symbols"`

	View string `json:"-"`
}

// IntentTerm is one term of the question and how much of the graph it reached.
//
// Frequency travels because it is the difference between a term that answered
// the question and a term that merely matched: a term carried by most of the
// corpus is why an unrelated file appears at all, and a caller that can see
// that can fix its question instead of doubting the tool.
type IntentTerm struct {
	Term      string `json:"term"`
	Symbols   int    `json:"symbols"`
	Frequency string `json:"frequency,omitempty"`
}

// IntentSymbol is one candidate, addressable the way every row of this surface
// is: repository, repository-relative path, qualified name and line range, so
// the next call is built from the answer without a key ever appearing.
//
// No score travels. It orders candidates inside one answer and means nothing on
// its own -- scaling every weight leaves the order identical -- so publishing it
// would invite a reader to treat it as a confidence this layer cannot claim.
//
// Match is `lexical` on every row, and it is not decoration. Every other row
// this server returns is an edge an analyser resolved; these rows are text that
// looked alike, and they must not be read with the same authority or counted in
// the same coverage.
type IntentSymbol struct {
	Name          string `json:"name,omitempty"`
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"`
	Repository    string `json:"repository"`
	FilePath      string `json:"file_path"`
	StartLine     uint32 `json:"start_line"`
	EndLine       uint32 `json:"end_line"`
	Terms         int    `json:"terms"`
	Match         string `json:"match"`

	StableKey string `json:"stable_key,omitempty"`
}

// intentFileCount is the whole of the `files` view: which files to open, and how
// many candidates each holds.
//
// It is the granularity the question is usually asked at. "Where do I look" is
// answered by a handful of paths, and the per-symbol rows underneath it are a
// second question the caller may never need to ask.
type intentFileCount struct {
	File    string `json:"file"`
	Repo    string `json:"repo,omitempty"`
	Symbols int    `json:"symbols"`
}

// MarshalJSON writes the page at the granularity the caller asked for.
func (matches IntentMatches) MarshalJSON() ([]byte, error) {
	type fullMatches IntentMatches
	switch matches.View {
	case ViewFiles:
		files := make([]intentFileCount, 0, len(matches.Symbols))
		seen := map[string]int{}
		for _, symbol := range matches.Symbols {
			key := symbol.Repository + "\x00" + symbol.FilePath
			if position, found := seen[key]; found {
				files[position].Symbols++
				continue
			}
			seen[key] = len(files)
			files = append(files, intentFileCount{File: symbol.FilePath, Repo: symbol.Repository, Symbols: 1})
		}
		return json.Marshal(struct {
			Terms     []IntentTerm      `json:"terms"`
			Unmatched []string          `json:"unmatched_terms,omitempty"`
			Files     []intentFileCount `json:"files"`
		}{Terms: matches.Terms, Unmatched: matches.Unmatched, Files: files})
	case ViewCompact:
		// Every row of this page shares its match kind, so it is stated once.
		rows := make([]IntentSymbol, len(matches.Symbols))
		copy(rows, matches.Symbols)
		for index := range rows {
			rows[index].Match = ""
		}
		return json.Marshal(struct {
			Terms     []IntentTerm   `json:"terms"`
			Unmatched []string       `json:"unmatched_terms,omitempty"`
			Match     string         `json:"match"`
			Symbols   []IntentSymbol `json:"symbols"`
		}{Terms: matches.Terms, Unmatched: matches.Unmatched, Match: "lexical", Symbols: rows})
	default:
		return json.Marshal(fullMatches(matches))
	}
}

type findByIntentOptions struct {
	Intent     string
	Keywords   []string
	Repo       string
	PathPrefix string
	Kind       string
	Limit      int
	Format     string
	View       string
}

type findByIntentQuery struct {
	Tool       string   `json:"tool"`
	Intent     string   `json:"intent"`
	Keywords   []string `json:"keywords,omitempty"`
	Repo       string   `json:"repo,omitempty"`
	PathPrefix string   `json:"path_prefix,omitempty"`
	Kind       string   `json:"kind,omitempty"`
}

// RegisterFindByIntent adds the read-only retrieval tool without a graph source.
func RegisterFindByIntent(server *sdkmcp.Server) {
	RegisterFindByIntentWithObserverAndSnapshotStore(server, nil, nil)
}

// RegisterFindByIntentWithObserver adds the tool and observes handler latency.
func RegisterFindByIntentWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterFindByIntentWithObserverAndSnapshotStore(server, observer, nil)
}

// RegisterFindByIntentWithSnapshotStore registers the tool over the immutable
// snapshot currently published by snapshotStore.
func RegisterFindByIntentWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterFindByIntentWithObserverAndSnapshotStore(server, nil, snapshotStore)
}

// RegisterFindByIntentWithObserverAndSnapshotStore registers the tool over an
// immutable snapshot and optionally observes latency.
func RegisterFindByIntentWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments FindByIntentInput,
	) (*sdkmcp.CallToolResult, Response[IntentMatches], error) {
		return findByIntent(ctx, request, arguments, snapshotStore)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments FindByIntentInput,
		) (*sdkmcp.CallToolResult, Response[IntentMatches], error) {
			start := time.Now()
			result, matches, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, findByIntentToolName, start, matches, err)
			return result, matches, err
		}
	}
	addQueryTool(server, &sdkmcp.Tool{
		Name: findByIntentToolName,
		// The rows match text rather than edges, and that caveat is deliberately
		// not here: it travels on every row as `match`, in the guidance, and in
		// the published skill. What this channel buys is routing, and the
		// resident budget leaves it a hundred and ten bytes for name and text
		// together -- the whole surface now sits within a few bytes of its
		// ceiling, so the next tool is a decision about that ceiling.
		Description: "Which symbols a plain-language description likely names, and the files to open.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
		Meta:        alwaysLoadMeta(),
	}, handler)
}

func findByIntent(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments FindByIntentInput,
	snapshotStore *hotsnapshot.SnapshotStore,
) (*sdkmcp.CallToolResult, Response[IntentMatches], error) {
	options, err := normalizeFindByIntentInput(arguments)
	if err != nil {
		return nil, Response[IntentMatches]{}, err
	}
	queryHash, err := HashQuery(findByIntentQuery{
		Tool: findByIntentToolName, Intent: options.Intent, Keywords: options.Keywords,
		Repo: options.Repo, PathPrefix: options.PathPrefix, Kind: options.Kind,
	})
	if err != nil {
		return nil, Response[IntentMatches]{}, err
	}
	if snapshotStore == nil {
		return nil, Response[IntentMatches]{}, ErrIndexNotReady()
	}
	snapshot := snapshotStore.Load()
	if snapshot == nil {
		return nil, Response[IntentMatches]{}, ErrIndexNotReady()
	}
	metadata := snapshot.Metadata()

	offset := 0
	if arguments.Cursor != "" {
		cursor, err := DecodeCursor(arguments.Cursor)
		if err != nil {
			return nil, Response[IntentMatches]{}, err
		}
		if err := cursor.ValidateAgainst(metadata.ID, queryHash, SortingVersionIntentV1); err != nil {
			return nil, Response[IntentMatches]{}, err
		}
		offset = cursor.Offset
	}

	ranked, terms, unmatched, err := rankIntentCandidates(snapshot, options)
	if err != nil {
		return nil, Response[IntentMatches]{}, err
	}

	total := len(ranked)
	if offset > total {
		offset = total
	}
	end := offset + options.Limit
	if end > total {
		end = total
	}
	page := append([]IntentSymbol(nil), ranked[offset:end]...)
	hasMore := end < total
	var nextCursor *string
	if hasMore {
		cursor, err := NewCursor(metadata.ID, queryHash, end, SortingVersionIntentV1)
		if err != nil {
			return nil, Response[IntentMatches]{}, err
		}
		encoded, err := cursor.Encode()
		if err != nil {
			return nil, Response[IntentMatches]{}, err
		}
		nextCursor = &encoded
	}

	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	return nil, Response[IntentMatches]{
		SnapshotID: &snapshotID, SnapshotAgeMS: &snapshotAgeMS,
		Total: total, Returned: len(page), Truncated: hasMore, NextCursor: nextCursor,
		Guidance: intentGuidance(total, len(page), hasMore, len(terms), len(unmatched)),
		Results: IntentMatches{
			Terms: terms, Unmatched: unmatched, Symbols: page, View: options.View,
		},
		View: options.View,
	}, nil
}

// intentCandidate accumulates what one symbol matched while the terms are read.
type intentCandidate struct {
	symbol      hotsnapshot.SymbolID
	hits        int
	frequencies []int
}

// rankIntentCandidates folds the question, reads every term, scores what the
// terms reached and orders it.
//
// The order is score descending, and ties break on symbol id -- which is
// stable-key order, the order every other page of this surface uses. That
// tie-break is not cosmetic: the cursor pages over this sequence, so two calls
// of one question must produce the same sequence or a second page would skip
// and repeat rows.
func rankIntentCandidates(
	snapshot *hotsnapshot.GraphSnapshot,
	options findByIntentOptions,
) ([]IntentSymbol, []IntentTerm, []string, error) {
	corpus := int(snapshot.Metadata().Counts.Symbols)
	words := retrieval.QueryWords(options.Intent, options.Keywords)
	candidates := map[hotsnapshot.SymbolID]*intentCandidate{}
	terms := make([]IntentTerm, 0, len(words))
	unmatched := make([]string, 0, len(words))

	for _, word := range words {
		key := retrieval.Fold(word)
		if key == retrieval.TermKeyNone {
			continue
		}
		found, frequency := snapshot.SymbolsByTerm(key)
		if frequency == 0 {
			unmatched = append(unmatched, word)
			continue
		}
		terms = append(terms, IntentTerm{
			Term: word, Symbols: frequency,
			Frequency: intentFrequencyLabel(frequency, corpus),
		})
		for _, id := range found {
			candidate := candidates[id]
			if candidate == nil {
				if len(candidates) >= maximumIntentCandidates {
					continue
				}
				candidate = &intentCandidate{symbol: id}
				candidates[id] = candidate
			}
			candidate.hits++
			candidate.frequencies = append(candidate.frequencies, frequency)
		}
	}

	type scored struct {
		row   IntentSymbol
		score float64
		id    hotsnapshot.SymbolID
	}
	rows := make([]scored, 0, len(candidates))
	for _, candidate := range candidates {
		symbol, file, repository, _, err := symbolReferenceLocation(snapshot, candidate.symbol)
		if err != nil {
			return nil, nil, nil, WrapToolError(
				CodeSnapshotUnavailable, "active snapshot contains invalid symbol metadata", err)
		}
		table := snapshot.Strings()
		name, nameOK := table.String(symbol.Name)
		qualifiedName, qualifiedOK := table.String(symbol.QualifiedName)
		kind, kindOK := table.String(symbol.Kind)
		if !nameOK || !qualifiedOK || !kindOK {
			return nil, nil, nil, WrapToolError(CodeSnapshotUnavailable,
				"active snapshot contains invalid symbol metadata",
				fmt.Errorf("symbol %d has invalid display strings", candidate.symbol))
		}
		if options.Repo != "" && repository.name != options.Repo {
			continue
		}
		if options.Kind != "" && kind != options.Kind {
			continue
		}
		if options.PathPrefix != "" && !strings.HasPrefix(file.path, options.PathPrefix) {
			continue
		}
		row := IntentSymbol{
			QualifiedName: qualifiedName, Kind: kind, Repository: repository.name,
			FilePath: file.path, StartLine: symbol.StartLine, EndLine: symbol.EndLine,
			Terms: candidate.hits, Match: "lexical",
		}
		if !nameIsLastSegment(name, qualifiedName) {
			row.Name = name
		}
		if options.Format == ResponseFormatDetailed {
			row.StableKey = symbolStableKey(snapshot, symbol)
		}
		rows = append(rows, scored{
			row: row,
			score: retrieval.Score(retrieval.Signals{
				Hits: candidate.hits, Frequencies: candidate.frequencies, Symbols: corpus,
				Kind: kind, Exported: symbol.Exported, Path: file.path,
				Callers: snapshot.IncomingCount(candidate.symbol),
			}),
			id: candidate.symbol,
		})
	}
	sort.SliceStable(rows, func(left, right int) bool {
		if rows[left].score != rows[right].score {
			return rows[left].score > rows[right].score
		}
		return rows[left].id < rows[right].id
	})
	ordered := make([]IntentSymbol, 0, len(rows))
	for _, row := range rows {
		ordered = append(ordered, row.row)
	}
	return ordered, terms, unmatched, nil
}

// intentFrequencyLabel says how much of the corpus a term matched, in words
// rather than a number, because a number derived from the graph would change
// with every reindex and this text travels in the answer a client may cache.
func intentFrequencyLabel(frequency, corpus int) string {
	if corpus <= 0 {
		return ""
	}
	switch share := float64(frequency) / float64(corpus); {
	case share >= 0.25:
		return "most of the graph, so it separated little"
	case share >= 0.05:
		return "common"
	default:
		return ""
	}
}

// intentGuidance speaks when the count misleads, and says something different
// for each way a retrieval comes back empty.
func intentGuidance(total, returned int, truncated bool, matched, unmatched int) string {
	switch {
	case total == 0 && matched == 0 && unmatched > 0:
		return "no word of this question appears in any name, qualified name, kind or path of the graph; the index holds no prose, so rephrase with the vocabulary the code would use, or pass keywords"
	case total == 0 && matched == 0:
		return "this question folded to no term at all; single characters are not indexed, so ask with words"
	case total == 0:
		return "the terms matched symbols, but every one of them was excluded by repo, kind or path_prefix; widen the narrowing"
	case truncated:
		return truncatedGuidance(returned, total, "repo, kind or path_prefix, or ask with view=files first")
	case unmatched > 0:
		return "some words of this question matched nothing and are listed in unmatched_terms; the ranking used the rest"
	default:
		return ""
	}
}

func normalizeFindByIntentInput(arguments FindByIntentInput) (findByIntentOptions, error) {
	intent := strings.TrimSpace(arguments.Intent)
	if intent == "" {
		return findByIntentOptions{}, NewToolError(CodeInvalidArgument, "intent is required")
	}
	if len(intent) > MaximumIntentChars {
		return findByIntentOptions{}, NewToolError(CodeInvalidArgument,
			"intent is a question, not a document; shorten it and pass the vocabulary as keywords")
	}
	if len(arguments.Keywords) > MaximumIntentKeywords {
		return findByIntentOptions{}, NewToolError(CodeInvalidArgument,
			"too many keywords; they extend one question rather than replacing it")
	}
	keywords := make([]string, 0, len(arguments.Keywords))
	for _, keyword := range arguments.Keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			return findByIntentOptions{}, NewToolError(CodeInvalidArgument, "a keyword cannot be empty")
		}
		keywords = append(keywords, trimmed)
	}
	repo, err := normalizeReferenceFilter(arguments.Repo, "repo")
	if err != nil {
		return findByIntentOptions{}, err
	}
	kind, err := normalizeReferenceFilter(arguments.Kind, "kind")
	if err != nil {
		return findByIntentOptions{}, err
	}
	pathPrefix, err := normalizeReferenceFilter(arguments.PathPrefix, "path_prefix")
	if err != nil {
		return findByIntentOptions{}, err
	}
	limit := arguments.Limit
	if limit == 0 {
		limit = DefaultIntentLimit
	}
	if limit < 1 || limit > MaximumIntentLimit {
		return findByIntentOptions{}, NewToolError(CodeInvalidArgument,
			fmt.Sprintf("limit must be between 1 and %d", MaximumIntentLimit))
	}
	format, err := normalizeResponseFormat(arguments.ResponseFormat)
	if err != nil {
		return findByIntentOptions{}, err
	}
	view, err := normalizeView(arguments.View, true)
	if err != nil {
		return findByIntentOptions{}, err
	}
	return findByIntentOptions{
		Intent: intent, Keywords: keywords, Repo: repo, PathPrefix: pathPrefix,
		Kind: kind, Limit: limit, Format: format, View: view,
	}, nil
}
