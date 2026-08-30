package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math/bits"
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
	Profile        []string `json:"profile,omitempty" jsonschema:"Profiles to query; omit for the default, or use * alone for all."`
	Intent         string   `json:"intent" jsonschema:"What the code you are looking for does, in plain language."`
	Keywords       []string `json:"keywords,omitempty" jsonschema:"Extra terms the code itself uses, when they differ from the words of the question."`
	Repo           string   `json:"repo,omitempty" jsonschema:"Consider only candidates in this repository. It narrows the question, not just the page."`
	PathPrefix     string   `json:"path_prefix,omitempty" jsonschema:"Consider only candidates under this repository-relative path prefix."`
	Kind           string   `json:"kind,omitempty" jsonschema:"Consider only symbols of this kind, such as function, struct or interface."`
	Limit          int      `json:"limit,omitempty" jsonschema:"Candidates in one page. Defaults to 10, maximum 50."`
	Cursor         string   `json:"cursor,omitempty" jsonschema:"The next_cursor of the previous page. Every other argument must stay the same."`
	ResponseFormat string   `json:"response_format,omitempty" jsonschema:"concise (the default) omits the derived identifiers; detailed returns them."`
	View           string   `json:"view,omitempty" jsonschema:"Granularity, never a different answer: compact (the default) states once what every row shares, full repeats it on each."`
}

// IntentMatches is a page of ranked candidates and an account of the terms that
// produced them.
type IntentMatches struct {
	Terms     []IntentTerm   `json:"terms,omitempty"`
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
// IntentTerm is one word of the question that the answer has to account for.
//
// Only two kinds of term earn a row here, and a roster of every term does not:
// measured on this repository, the full list was a quarter of a `view=files`
// payload and most of its lines said `to 70`, `is 178`, `when 1` -- the
// question's grammar, counted. A term that matched four symbols and produced the
// answer needs no line, because the rows are the line. What a caller can act on
// is the term that matched nothing, which is in Unmatched, and the term that
// matched so much it separated nothing, which is here.
type IntentTerm struct {
	Term      string `json:"term"`
	Symbols   int    `json:"symbols"`
	Frequency string `json:"frequency"`
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
	Profiles      ProfileNames `json:"profile,omitempty"`
	Name          string       `json:"name,omitempty"`
	QualifiedName string       `json:"qualified_name"`
	Kind          string       `json:"kind"`
	Repository    string       `json:"repository"`
	FilePath      string       `json:"file_path"`
	StartLine     uint32       `json:"start_line"`
	EndLine       uint32       `json:"end_line"`
	Terms         int          `json:"terms"`
	Match         string       `json:"match"`

	StableKey string `json:"stable_key,omitempty"`
}

// intentFileCount is the whole of the `files` view: which files to open, and how
// many candidates each holds.
//
// It is the granularity the question is usually asked at. "Where do I look" is
// answered by a handful of paths, and the per-symbol rows underneath it are a
// second question the caller may never need to ask.
type intentFileCount struct {
	Profiles ProfileNames `json:"profile,omitempty"`
	File     string       `json:"file"`
	Repo     string       `json:"repo,omitempty"`
	Symbols  int          `json:"symbols"`
}

// MarshalJSON writes the page at the granularity the caller asked for.
func (matches IntentMatches) MarshalJSON() ([]byte, error) {
	type fullMatches IntentMatches
	switch matches.View {
	case ViewFiles:
		files := make([]intentFileCount, 0, len(matches.Symbols))
		seen := map[string]int{}
		for _, symbol := range matches.Symbols {
			key := string(symbol.Profiles) + "\x00" + symbol.Repository + "\x00" + symbol.FilePath
			if position, found := seen[key]; found {
				files[position].Symbols++
				continue
			}
			seen[key] = len(files)
			files = append(files, intentFileCount{Profiles: symbol.Profiles, File: symbol.FilePath, Repo: symbol.Repository, Symbols: 1})
		}
		return json.Marshal(struct {
			Terms     []IntentTerm      `json:"terms,omitempty"`
			Unmatched []string          `json:"unmatched_terms,omitempty"`
			Files     []intentFileCount `json:"files"`
		}{Terms: matches.Terms, Unmatched: matches.Unmatched, Files: files})
	case ViewCompact:
		// A shared field is stated once, and a field the rows disagree on is not
		// shared. Hoisting `lexical` over a page where one row was credited for
		// the calls it reaches would put a claim in the header that the row it
		// describes does not make, which is the one thing this view may not do.
		rows := make([]IntentSymbol, len(matches.Symbols))
		copy(rows, matches.Symbols)
		shared := ""
		for index, row := range rows {
			if index == 0 {
				shared = row.Match
				continue
			}
			if row.Match != shared {
				shared = ""
				break
			}
		}
		if shared != "" {
			for index := range rows {
				rows[index].Match = ""
			}
		}
		return json.Marshal(struct {
			Terms     []IntentTerm   `json:"terms,omitempty"`
			Unmatched []string       `json:"unmatched_terms,omitempty"`
			Match     string         `json:"match,omitempty"`
			Symbols   []IntentSymbol `json:"symbols"`
		}{Terms: matches.Terms, Unmatched: matches.Unmatched, Match: shared, Symbols: rows})
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
		if snapshotStore != nil {
			selected, selectionErr := snapshotStore.ResolveProfiles(arguments.Profile)
			if selectionErr != nil {
				return nil, Response[IntentMatches]{}, WrapToolError(CodeInvalidArgument, selectionErr.Error(), selectionErr)
			}
			if len(selected) > 1 {
				return findByIntentAcrossProfiles(ctx, request, arguments, selected)
			}
		}
		store, profile, count, err := resolveSingleProfile(snapshotStore, arguments.Profile, "")
		if err != nil {
			return nil, Response[IntentMatches]{}, err
		}
		result, response, err := findByIntent(ctx, request, arguments, store)
		scopeResponse(&response, profile, count)
		return result, response, err
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
		Description: "Which symbols a plain-language description likely names, and the files to open. Start here when you have no name.",
		Annotations: readOnlyClosedWorld(),
		Meta:        alwaysLoadMeta(),
	}, handler)
}

func findByIntentAcrossProfiles(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	arguments FindByIntentInput,
	selected []hotsnapshot.ProfileStore,
) (*sdkmcp.CallToolResult, Response[IntentMatches], error) {
	options, err := normalizeFindByIntentInput(arguments)
	if err != nil {
		return nil, Response[IntentMatches]{}, err
	}
	names := make([]string, 0, len(selected))
	profileSnapshots := make([]ProfileSnapshot, 0, len(selected))
	for _, profile := range selected {
		names = append(names, profile.Name)
	}
	queryHash, err := HashQuery(struct {
		Tool       string   `json:"tool"`
		Profiles   []string `json:"profiles"`
		Intent     string   `json:"intent"`
		Keywords   []string `json:"keywords,omitempty"`
		Repo       string   `json:"repo,omitempty"`
		PathPrefix string   `json:"path_prefix,omitempty"`
		Kind       string   `json:"kind,omitempty"`
	}{findByIntentToolName, names, options.Intent, options.Keywords, options.Repo, options.PathPrefix, options.Kind})
	if err != nil {
		return nil, Response[IntentMatches]{}, err
	}

	words := retrieval.QueryWords(options.Intent, options.Keywords)
	frequencies := make(map[string]int, len(words))
	totalCorpus := 0
	rows := make([]IntentSymbol, 0)
	variants := make(map[string]int)
	for _, profile := range selected {
		snapshot := profile.Store.Load()
		if snapshot == nil {
			return nil, Response[IntentMatches]{}, ErrIndexNotReady()
		}
		metadata := snapshot.Metadata()
		profileSnapshots = append(profileSnapshots, ProfileSnapshot{Name: profile.Name, SnapshotID: metadata.ID})
		totalCorpus += int(metadata.Counts.Symbols)
		for _, word := range words {
			_, frequency := snapshot.SymbolsByTerm(retrieval.Fold(word))
			frequencies[word] += frequency
		}
		profileOptions := options
		profileOptions.Format = ResponseFormatDetailed
		ranked, _, _, _, rankErr := rankIntentCandidates(snapshot, profileOptions)
		if rankErr != nil {
			return nil, Response[IntentMatches]{}, rankErr
		}
		for _, row := range ranked {
			stableKey := row.StableKey
			row.Profiles = ""
			if options.Format != ResponseFormatDetailed {
				row.StableKey = ""
			}
			payload, marshalErr := json.Marshal(row)
			if marshalErr != nil {
				return nil, Response[IntentMatches]{}, WrapToolError(CodeSnapshotUnavailable, "encode intent payload for profile merge", marshalErr)
			}
			key := stableKey + "\x00" + string(payload)
			if position, found := variants[key]; found {
				rows[position].Profiles = rows[position].Profiles.append(profile.Name)
				continue
			}
			row.Profiles = profileNames(profile.Name)
			variants[key] = len(rows)
			rows = append(rows, row)
		}
	}
	terms := make([]IntentTerm, 0, len(words))
	unmatched := make([]string, 0, len(words))
	used := 0
	for _, word := range words {
		frequency := frequencies[word]
		if frequency == 0 {
			unmatched = append(unmatched, word)
			continue
		}
		used++
		if label := intentFrequencyLabel(frequency, totalCorpus); label != "" {
			terms = append(terms, IntentTerm{Term: word, Symbols: frequency, Frequency: label})
		}
	}
	setID := ProfileSetSnapshotID(profileSnapshots)
	offset := 0
	if arguments.Cursor != "" {
		cursor, decodeErr := DecodeCursor(arguments.Cursor)
		if decodeErr != nil {
			return nil, Response[IntentMatches]{}, decodeErr
		}
		if validateErr := cursor.ValidateAgainst(setID, queryHash, SortingVersionIntentV1); validateErr != nil {
			return nil, Response[IntentMatches]{}, validateErr
		}
		offset = cursor.Offset
	}
	if offset > len(rows) {
		return nil, Response[IntentMatches]{}, NewToolError(CodeCursorInvalid, "cursor offset is beyond the merged result")
	}
	end, total := offset+options.Limit, len(rows)
	if options.View == ViewFiles {
		end, total = filePage(rows, offset, options.Limit)
	}
	if end > len(rows) {
		end = len(rows)
	}
	page := rows[offset:end]
	returned := len(page)
	if options.View == ViewFiles {
		returned = distinctFiles(page)
	}
	var nextCursor *string
	if end < len(rows) {
		cursor, cursorErr := NewCursor(setID, queryHash, end, SortingVersionIntentV1)
		if cursorErr != nil {
			return nil, Response[IntentMatches]{}, cursorErr
		}
		encoded, encodeErr := cursor.Encode()
		if encodeErr != nil {
			return nil, Response[IntentMatches]{}, encodeErr
		}
		nextCursor = &encoded
	}
	return nil, Response[IntentMatches]{
		Profiles: profileSnapshots, CrossProfileEdges: "not_resolved",
		Total: total, Returned: returned, Truncated: end < len(rows), NextCursor: nextCursor,
		Guidance: intentGuidance(total, returned, end < len(rows), used, len(unmatched), len(options.Keywords)),
		Results:  IntentMatches{Terms: terms, Unmatched: unmatched, Symbols: page, View: options.View},
		View:     options.View,
	}, nil
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

	ranked, terms, unmatched, used, err := rankIntentCandidates(snapshot, options)
	if err != nil {
		return nil, Response[IntentMatches]{}, err
	}

	if offset > len(ranked) {
		offset = len(ranked)
	}
	// A row of this answer is whatever the view spells, so a limit counts rows
	// and not symbols. Under `view: "files"` the page therefore walks the same
	// ranking until it has the files it was asked for -- same order, same
	// answer, one granularity -- rather than stopping at ten symbols that turn
	// out to be five files.
	end, total := offset+options.Limit, len(ranked)
	if options.View == ViewFiles {
		end, total = filePage(ranked, offset, options.Limit)
	}
	if end > len(ranked) {
		end = len(ranked)
	}
	page := append([]IntentSymbol(nil), ranked[offset:end]...)
	returned := len(page)
	if options.View == ViewFiles {
		returned = distinctFiles(page)
	}
	hasMore := end < len(ranked)
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
		Total: total, Returned: returned, Truncated: hasMore, NextCursor: nextCursor,
		Guidance: intentGuidance(total, returned, hasMore, used, len(unmatched), len(options.Keywords)),
		Results: IntentMatches{
			Terms: terms, Unmatched: unmatched, Symbols: page, View: options.View,
		},
		View: options.View,
	}, nil
}

// filePage walks a ranked page to the row count a files view was asked for, and
// counts the total in the same unit. It never reorders: the answer is the same
// ranking read at a coarser grain.
func filePage(ranked []IntentSymbol, offset, limit int) (int, int) {
	seen := make(map[string]struct{}, limit)
	end := offset
	for end < len(ranked) {
		key := string(ranked[end].Profiles) + "\x00" + ranked[end].Repository + "\x00" + ranked[end].FilePath
		if _, found := seen[key]; !found && len(seen) == limit {
			break
		}
		seen[key] = struct{}{}
		end++
	}
	return end, distinctFiles(ranked)
}

func distinctFiles(rows []IntentSymbol) int {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		seen[string(row.Profiles)+"\x00"+row.Repository+"\x00"+row.FilePath] = struct{}{}
	}
	return len(seen)
}

// intentCandidate accumulates what one symbol matched while the terms are read.
type intentCandidate struct {
	symbol      hotsnapshot.SymbolID
	hits        int
	frequencies []int
	lengths     []int
	// terms is which terms this symbol carries, one bit each, in the order the
	// question spelled them. It exists so a candidate can be asked what its
	// neighbours carry without building a second table: every symbol that
	// carries any term is already a candidate, so the map below is the term
	// membership index too.
	terms uint32
	// neighbours is how many terms this symbol reaches but does not carry.
	neighbours int
}

const (
	// maximumNeighbourTerms is how many terms fit in the mask above. A question
	// with more terms than this still answers; its later terms simply earn no
	// neighbour credit, which is the cheap end of a bound that keeps one
	// question from walking the graph.
	maximumNeighbourTerms = 32
	// maximumNeighbourEdges bounds the fan-out inspected per candidate. A hub
	// with four hundred callees would otherwise price one question at the width
	// of the graph, and the terms a hub reaches say little about it anyway.
	//
	// No test asserts this number, and that is deliberate: the credit saturates
	// at three reached terms, so a fixture wide enough to cross this bound would
	// score identically on either side of it. It is a cost guard with no
	// observable contract, and a test of it would only restate the constant.
	maximumNeighbourEdges = 64
)

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
) ([]IntentSymbol, []IntentTerm, []string, int, error) {
	corpus := int(snapshot.Metadata().Counts.Symbols)
	words := retrieval.QueryWords(options.Intent, options.Keywords)
	candidates := map[hotsnapshot.SymbolID]*intentCandidate{}
	// used counts the terms the ranking read, which is no longer the length of
	// the reported list: a term that discriminated is reported by the rows.
	used := 0
	terms := make([]IntentTerm, 0, len(words))
	unmatched := make([]string, 0, len(words))

	for _, word := range words {
		// QueryWords only ever returns words that fold to a key, so there is no
		// guard here: a word it dropped never reaches this loop.
		found, frequency := snapshot.SymbolsByTerm(retrieval.Fold(word))
		if frequency == 0 {
			unmatched = append(unmatched, word)
			continue
		}
		used++
		if label := intentFrequencyLabel(frequency, corpus); label != "" {
			terms = append(terms, IntentTerm{Term: word, Symbols: frequency, Frequency: label})
		}
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
			candidate.lengths = append(candidate.lengths, len([]rune(word)))
			if termIndex := used - 1; termIndex < maximumNeighbourTerms {
				candidate.terms |= 1 << uint(termIndex)
			}
		}
	}

	// The terms a candidate reaches, read from the candidates themselves: a
	// symbol carrying any term is in this map, so the map is both the candidate
	// set and the membership index of every term.
	for _, candidate := range candidates {
		reached := uint32(0)
		for index, edge := range snapshot.Outgoing(candidate.symbol) {
			if index >= maximumNeighbourEdges {
				break
			}
			if neighbour := candidates[edge.Target]; neighbour != nil {
				reached |= neighbour.terms
			}
		}
		candidate.neighbours = bits.OnesCount32(reached & ^candidate.terms)
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
			return nil, nil, nil, 0, WrapToolError(
				CodeSnapshotUnavailable, "active snapshot contains invalid symbol metadata", err)
		}
		table := snapshot.Strings()
		name, nameOK := table.String(symbol.Name)
		qualifiedName, qualifiedOK := table.String(symbol.QualifiedName)
		kind, kindOK := table.String(symbol.Kind)
		if !nameOK || !qualifiedOK || !kindOK {
			return nil, nil, nil, 0, WrapToolError(CodeSnapshotUnavailable,
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
			Terms: candidate.hits, Match: matchLabel(candidate.neighbours),
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
				Hits: candidate.hits, Frequencies: candidate.frequencies,
				Lengths: candidate.lengths, Symbols: corpus, Neighbours: candidate.neighbours,
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
	return ordered, terms, unmatched, used, nil
}

// intentFrequencyLabel says how much of the corpus a term matched, in words
// rather than a number, because a number derived from the graph would change
// with every reindex and this text travels in the answer a client may cache.
// matchLabel says how a row was reached. A row credited for what it calls did
// not match the question's text alone, and calling it lexical would be the kind
// of small lie that makes a whole answer unciteable.
func matchLabel(neighbours int) string {
	if neighbours > 0 {
		return "lexical+calls"
	}
	return "lexical"
}

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
func intentGuidance(total, returned int, truncated bool, matched, unmatched, guessed int) string {
	switch {
	case total == 0 && matched == 0 && unmatched > 0:
		return "no word of this question appears in any name, qualified name, kind or path of the graph; the index holds no prose, so rephrase with the vocabulary the code would use, or pass keywords"
	case total == 0 && matched == 0:
		return "this question folded to no term at all; single characters are not indexed, so ask with words"
	case total == 0:
		return "the terms matched symbols, but every one of them was excluded by repo, kind or path_prefix; widen the narrowing"
	case truncated && guessed == 0:
		// The page is wide and the caller asked in prose only. Measured over
		// three repositories, passing the identifier words a caller can guess
		// from its own question -- without knowing the code -- moves more
		// answers into the page than any narrowing does, and it is why this
		// tool takes keywords instead of embeddings. So it is named before
		// paging, which is the advice that costs another call.
		return truncatedGuidance(returned, total,
			"keywords with the identifier words you would guess this code uses, or repo, kind or path_prefix")
	case truncated:
		return truncatedGuidance(returned, total, "repo, kind or path_prefix, or ask with view=files first")
	case unmatched > 0 && guessed == 0:
		return "some words of this question matched nothing and are listed in unmatched_terms; the ranking used the rest, and keywords is where the vocabulary the code uses belongs"
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
