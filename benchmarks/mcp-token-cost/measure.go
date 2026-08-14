package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// questionSet is the versioned corpus of questions. It lives on disk so one
// result can be compared against another run of the same set.
type questionSet struct {
	Version    int    `json:"version"`
	Tokenizer  string `json:"tokenizer"`
	Repository string `json:"repository"`
	Question   string `json:"question"`
	// CrossRepositoryRoot is the symbol whose consumers in other repositories are
	// priced. Empty means this corpus has none, which the report declares instead
	// of inventing a question the graph cannot answer.
	CrossRepositoryRoot string `json:"cross_repository_root,omitempty"`
	// CrossRepositoryRepository and CrossRepositoryPath name the declaration, for
	// the same reason a question does: a bare name is ambiguous in any corpus
	// with more than one repository.
	CrossRepositoryRepository string `json:"cross_repository_repository,omitempty"`
	CrossRepositoryPath       string `json:"cross_repository_path,omitempty"`
	// CrossRepositoryNative is the captured host answer for the same question:
	// its grep across every repository root of the corpus.
	CrossRepositoryNative string `json:"cross_repository_native,omitempty"`
	// TraversalRoot is the symbol the two traversal tools are priced on. It is
	// versioned with the questions so a payload comparison is between the same
	// two answers.
	TraversalRoot string     `json:"traversal_root"`
	Questions     []question `json:"questions"`
}

type question struct {
	Symbol string `json:"symbol"`
	Class  string `json:"class"`
	// Repository and Path name the declaration this question is about. They are
	// required whenever the corpus declares the name more than once, which is
	// every name in a monorepo: without them the subject is whichever row a page
	// happened to return first.
	Repository string    `json:"repository,omitempty"`
	Path       string    `json:"path,omitempty"`
	Qualified  string    `json:"qualified_name,omitempty"`
	Native     nativeArm `json:"native"`
}

// QualifiedName is the name to resolve inside the named file, defaulting to the
// symbol itself.
func (asked question) QualifiedName() string {
	if asked.Qualified != "" {
		return asked.Qualified
	}
	return asked.Symbol
}

// nativeArm records how the host's own answer was captured. The capture is a
// verbatim tool result, not a reimplementation: a native arm written by us
// would be an imitation, and an imitation is always worse than the tool it
// stands in for, which biases the comparison in our favour.
type nativeArm struct {
	Tool    string `json:"tool"`
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Capture string `json:"capture"`
}

// hostRead is one captured range read. Both arms that make the agent open a
// file are billed from these captures rather than from the raw slice: the
// host's read prepends a snapshot header and a number to every line, which
// measured 38 % on top of the bytes themselves. Charging the raw slice would
// quietly discount the alternative.
type hostRead struct {
	File   string `json:"file"`
	Tokens int    `json:"tokens"`
}

func loadHostReads(directory string) (map[string]hostRead, error) {
	path := filepath.Join(directory, "native", "reads.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	reads := map[string]hostRead{}
	if err := json.Unmarshal(data, &reads); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return reads, nil
}

func loadQuestions(directory string) (questionSet, error) {
	path := filepath.Join(directory, "questions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return questionSet{}, fmt.Errorf("read %s: %w", path, err)
	}
	set := questionSet{}
	if err := json.Unmarshal(data, &set); err != nil {
		return questionSet{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if set.Tokenizer != encodingName {
		return questionSet{}, fmt.Errorf("%s declares tokenizer %q but this harness counts in %q", path, set.Tokenizer, encodingName)
	}
	if len(set.Questions) == 0 {
		return questionSet{}, fmt.Errorf("%s declares no questions", path)
	}
	return set, nil
}

// The response types decode only the fields this harness needs. Decoding the
// whole response would couple the benchmark to every field the surface happens
// to carry today.
type findSymbolResponse struct {
	// Total is how many symbols carry the name, which is not len(Results): a
	// page is bounded and the count is what says whether the name is ambiguous.
	Total   int         `json:"total"`
	Results []symbolRow `json:"results"`
}

type symbolRow struct {
	StableKey string `json:"stable_key"`
	// Repository is the name every row of this surface carries, and the value
	// the triple selector takes. It used to be spelled two ways -- `repository`
	// in reference rows and `repository_name` in symbol rows -- and reading only
	// one left the subject of every question without a repository: harmless in a
	// corpus of one, a body looked for under the wrong tree in a monorepo.
	Repository    string `json:"repository"`
	QualifiedName string `json:"qualified_name"`
	FilePath      string `json:"file_path"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
}

type findReferencesResponse struct {
	Results struct {
		References []symbolRow `json:"references"`
	} `json:"results"`
}

type getSymbolResponse struct {
	Results symbolRow `json:"results"`
}

type graphStatusResponse struct {
	Results struct {
		SnapshotID      int    `json:"snapshot_id"`
		Symbols         int    `json:"symbols"`
		Files           int    `json:"files"`
		Edges           int    `json:"edges"`
		SnapshotBuiltAt string `json:"snapshot_built_at"`
		ResolverVersion string `json:"resolver_version"`
		SchemaVersion   int    `json:"schema_version"`
	} `json:"results"`
}

type listRepositoriesResponse struct {
	Results []struct {
		Name          string `json:"name"`
		Path          string `json:"path"`
		IndexedCommit string `json:"indexed_commit"`
		CurrentCommit string `json:"current_commit"`
		IndexedBranch string `json:"indexed_branch"`
		Moved         bool   `json:"moved"`
	} `json:"results"`
}

func readSnapshot(ctx context.Context, session *sdkmcp.ClientSession) (snapshot, error) {
	text, _, err := call(ctx, session, "graph_status", map[string]any{})
	if err != nil {
		return snapshot{}, err
	}
	decoded := graphStatusResponse{}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return snapshot{}, fmt.Errorf("parse graph_status: %w", err)
	}
	if decoded.Results.SnapshotID == 0 {
		return snapshot{}, fmt.Errorf("graph_status reports no published generation; run ladygraph index --full first")
	}
	return snapshot{
		ID:              decoded.Results.SnapshotID,
		Symbols:         decoded.Results.Symbols,
		Files:           decoded.Results.Files,
		Edges:           decoded.Results.Edges,
		BuiltAt:         decoded.Results.SnapshotBuiltAt,
		ResolverVersion: decoded.Results.ResolverVersion,
		SchemaVersion:   decoded.Results.SchemaVersion,
	}, nil
}

// readCorpus records whether the working tree still is what was indexed. A
// token comparison against a generation that no longer describes the source on
// disk measures nothing, so the divergence travels in the result instead of
// being discovered later.
// readCorpus also returns where every repository of the generation lives. A
// reference to a symbol can be in another repository, so a body cannot be
// resolved against one root: the cross-repository corpus is what proved it.
func readCorpus(ctx context.Context, session *sdkmcp.ClientSession, repository string) (corpus, map[string]string, error) {
	text, _, err := call(ctx, session, "list_repositories", map[string]any{})
	if err != nil {
		return corpus{}, nil, err
	}
	decoded := listRepositoriesResponse{}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return corpus{}, nil, fmt.Errorf("parse list_repositories: %w", err)
	}
	roots := make(map[string]string, len(decoded.Results))
	for _, row := range decoded.Results {
		roots[row.Name] = row.Path
	}
	for _, row := range decoded.Results {
		if row.Name != repository {
			continue
		}
		return corpus{
			Repository:    row.Name,
			Path:          row.Path,
			IndexedCommit: row.IndexedCommit,
			CurrentCommit: row.CurrentCommit,
			Branch:        row.IndexedBranch,
			Fresh:         row.IndexedCommit != "" && row.IndexedCommit == row.CurrentCommit && !row.Moved,
			Repositories:  len(decoded.Results),
		}, roots, nil
	}
	return corpus{}, nil, fmt.Errorf("repository %q is not registered in the published generation", repository)
}

// measureSurface reports what each host keeps resident. Neither Oh My Pi nor
// Claude Code holds the JSON schemas: omp mounts every MCP tool as a device
// whose documentation is read on demand, and Claude Code defers schemas behind
// its tool search. What omp does keep is one route line and one description per
// tool, so that is the number a surface regression has to be measured against.
func measureSurface(ctx context.Context, session *sdkmcp.ClientSession, tokens *counter) (surface, error) {
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		return surface{}, fmt.Errorf("list MCP tools: %w", err)
	}
	measured := surface{Tools: len(listed.Tools)}
	schemas := make([]any, 0, len(listed.Tools))
	routes := &strings.Builder{}
	descriptions := &strings.Builder{}
	for _, tool := range listed.Tools {
		fmt.Fprintf(routes, "- %q → xd://mcp__ladygraph_%s\n", tool.Name, tool.Name)
		fmt.Fprintf(descriptions, "- xd://mcp__ladygraph_%s — %s\n", tool.Name, tool.Description)
		schemas = append(schemas, tool)
		if tool.Annotations != nil && tool.Annotations.ReadOnlyHint {
			measured.ReadOnly++
		}
	}
	encoded, err := json.Marshal(schemas)
	if err != nil {
		return surface{}, fmt.Errorf("marshal tool schemas: %w", err)
	}
	measured.RouteTokens = tokens.count(routes.String())
	measured.DescriptionTokens = tokens.count(descriptions.String())
	measured.ResidentOhMyPi = measured.RouteTokens + measured.DescriptionTokens
	measured.DeferredSchemaTokens = tokens.count(string(encoded))
	return measured, nil
}

// measureQuestion prices one question three ways.
//
//   - native: the host's captured answer plus the bodies the agent still opens.
//   - today: the MCP calls a session actually needs now, plus the same bodies.
//     find_references carries no end_line, so today includes one get_symbol per
//     reference; that round trip is the cost LUQUE-1901 removes.
//   - projected: today without those round trips, with end_line priced into
//     each row. Marked projected because it is arithmetic over measured parts,
//     not an observation, and it becomes measured once LUQUE-1901 lands.
func measureQuestion(
	ctx context.Context,
	session *sdkmcp.ClientSession,
	tokens *counter,
	directory string,
	source corpus,
	roots map[string]string,
	reads map[string]hostRead,
	asked question,
) (questionResult, error) {
	result := questionResult{Symbol: asked.Symbol, Class: asked.Class}

	target, findText, findStructured, err := resolveSubject(ctx, session, asked)
	if err != nil {
		return questionResult{}, err
	}

	referenceText, referenceStructured, err := call(ctx, session, "find_references", map[string]any{
		"stable_key": target.StableKey,
		"direction":  "incoming",
	})
	if err != nil {
		return questionResult{}, err
	}
	references := findReferencesResponse{}
	if err := json.Unmarshal([]byte(referenceText), &references); err != nil {
		return questionResult{}, fmt.Errorf("parse find_references: %w", err)
	}
	rows := references.Results.References
	result.References = len(rows)

	callTokens := tokens.count(findText) + tokens.count(referenceText)
	structuredBytes := findStructured + referenceStructured

	// readTokens is what the agent pays opening the bodies with the host's own
	// read; servedTokens is what the same bytes cost if the graph hands them
	// over instead. The difference is the whole case for a source-serving tool.
	readTokens, err := hostReadCost(reads, target.FilePath, target.StartLine, target.EndLine)
	if err != nil {
		return questionResult{}, err
	}
	// servedTokens is the irreducible floor: the bytes themselves, which is what
	// the ceiling in the totals is measured against.
	servedTokens, err := bodyCost(tokens, repositoryRoot(roots, target.Repository, source.Path), target.FilePath, target.StartLine, target.EndLine)
	if err != nil {
		return questionResult{}, err
	}
	bodies := 1

	roundTripTokens := 0
	for _, row := range rows {
		start, end, path := row.StartLine, row.EndLine, row.FilePath
		if end == 0 {
			// The row cannot be opened as it stands, so the session pays for
			// one more call before it can read anything.
			symbolText, symbolStructured, callErr := call(ctx, session, "get_symbol", map[string]any{"stable_key": row.StableKey})
			if callErr != nil {
				return questionResult{}, callErr
			}
			roundTripTokens += tokens.count(symbolText)
			structuredBytes += symbolStructured
			decoded := getSymbolResponse{}
			if err := json.Unmarshal([]byte(symbolText), &decoded); err != nil {
				return questionResult{}, fmt.Errorf("parse get_symbol: %w", err)
			}
			start, end, path = decoded.Results.StartLine, decoded.Results.EndLine, decoded.Results.FilePath
			result.ExtraCalls++
		}
		read, err := hostReadCost(reads, path, start, end)
		if err != nil {
			return questionResult{}, err
		}
		served, err := bodyCost(tokens, repositoryRoot(roots, row.Repository, source.Path), path, start, end)
		if err != nil {
			return questionResult{}, err
		}
		readTokens += read
		servedTokens += served
		bodies++
	}

	nativeTokens, err := nativeCost(tokens, directory, asked.Native.Capture)
	if err != nil {
		return questionResult{}, err
	}
	result.Native = arm{
		Answer: nativeTokens,
		Bodies: readTokens,
		Total:  nativeTokens + readTokens,
		Note:   fmt.Sprintf("%s %s over %s, then the host reads each range", asked.Native.Tool, asked.Native.Pattern, asked.Native.Path),
	}
	result.Today = arm{
		Calls:  callTokens + roundTripTokens,
		Bodies: readTokens,
		Total:  callTokens + roundTripTokens + readTokens,
		Note:   "the MCP calls a session needs, then the host reads each range the answer names",
	}
	// The served arm is measured, not projected, once get_source exists: the
	// bodies come back without a line number on every line.
	//
	// Every symbol is named the way the rows name it: a reference row carries no
	// stable key any more, and this is the addressing an agent reading the answer
	// actually has.
	//
	// One call assembles at most twenty bodies, so an answer with eighty-three
	// consumers is five calls and the arm pays for five. Asking for all of them
	// at once is an error the tool refuses, and pricing one call would credit
	// this arm with an envelope it never gets.
	requests := make([]map[string]any, 0, len(rows)+1)
	requests = append(requests, map[string]any{"stable_key": target.StableKey})
	for _, row := range rows {
		requests = append(requests, map[string]any{
			"repository": row.Repository, "path": row.FilePath, "qualified_name": row.QualifiedName,
		})
	}
	served := 0
	for offset := 0; offset < len(requests); offset += maximumSourceSymbols {
		end := offset + maximumSourceSymbols
		if end > len(requests) {
			end = len(requests)
		}
		servedText, servedStructured, servedErr := call(ctx, session, "get_source",
			map[string]any{"symbols": requests[offset:end]})
		if servedErr != nil {
			return questionResult{}, servedErr
		}
		structuredBytes += servedStructured
		served += tokens.count(servedText)
	}
	result.Projected = arm{
		Calls:  callTokens,
		Bodies: served,
		Total:  callTokens + served,
		Note:   "get_source returns the bodies the answer named, twenty to a call, so nothing goes through the host read",
	}
	result.FloorBytes = servedTokens
	result.DuplicateChannelBytes = structuredBytes
	result.AnswerFactorToday = ratio(result.Native.Answer, result.Today.Calls)
	result.AnswerFactorProjected = ratio(result.Native.Answer, result.Projected.Calls)
	result.SessionFactorToday = ratio(result.Native.Total, result.Today.Total)
	result.SessionFactorProjected = ratio(result.Native.Total, result.Projected.Total)
	return result, nil
}

// bodyCost prices the source an agent still has to open. Both arms pay it, so
// the harness never lets a lean payload look like a saving on its own.
//
// It is the raw slice, without the line numbers a host read prepends. That
// understates both arms by the same shape, and the alternative -- guessing how
// each host decorates a read -- would be the imitation this benchmark avoids.
// hostReadCost bills what the agent really pays to open a range, from the
// capture. A missing capture is a failure, not a fallback: silently charging the
// raw slice instead would understate the arm that reads files, which is the arm
// this benchmark exists to compare against.
func hostReadCost(reads map[string]hostRead, relativePath string, start, end int) (int, error) {
	if relativePath == "" || start <= 0 || end < start {
		return 0, nil
	}
	key := fmt.Sprintf("%s:%d-%d", relativePath, start, end)
	read, found := reads[key]
	if !found {
		return 0, fmt.Errorf("no captured host read for %s; recapture native/reads.json against this generation", key)
	}
	return read.Tokens, nil
}

func bodyCost(tokens *counter, repositoryPath, relativePath string, start, end int) (int, error) {
	if relativePath == "" || start <= 0 || end < start {
		return 0, nil
	}
	path := relativePath
	if repositoryPath != "" {
		path = filepath.Join(repositoryPath, relativePath)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}

	lines := strings.Split(string(data), "\n")
	if start > len(lines) {
		return 0, fmt.Errorf("%s reports lines %d-%d but the file has %d", relativePath, start, end, len(lines))
	}
	if end > len(lines) {
		end = len(lines)
	}
	return tokens.count(strings.Join(lines[start-1:end], "\n")), nil
}

func nativeCost(tokens *counter, directory, capture string) (int, error) {
	if capture == "" {
		return 0, fmt.Errorf("question declares no native capture")
	}
	path := filepath.Join(directory, capture)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read native capture %s: %w", path, err)
	}
	return tokens.count(string(data)), nil
}

func ratio(native, ours int) float64 {
	if ours == 0 {
		return 0
	}
	return float64(native) / float64(ours)
}
func totalise(questions []questionResult) totals {
	sum := totals{}
	for _, question := range questions {
		sum.Native += question.Native.Total
		sum.NativeAnswer += question.Native.Answer
		sum.Today += question.Today.Total
		sum.TodayAnswer += question.Today.Calls
		sum.Projected += question.Projected.Total
		sum.ProjectedAnswer += question.Projected.Calls
		sum.Bodies += question.Today.Bodies
		sum.ServedBodies += question.FloorBytes
		sum.ExtraCalls += question.ExtraCalls
		sum.DuplicateChannelBytes += question.DuplicateChannelBytes
	}
	sum.AnswerFactorToday = ratio(sum.NativeAnswer, sum.TodayAnswer)
	sum.AnswerFactorProjected = ratio(sum.NativeAnswer, sum.ProjectedAnswer)
	sum.SessionFactorToday = ratio(sum.Native, sum.Today)
	sum.SessionFactorProjected = ratio(sum.Native, sum.Projected)
	// The floor is the bytes themselves, served without a line prefix on every
	// line: no answer can carry less and still show the code.
	sum.SessionCeiling = ratio(sum.Native, sum.ServedBodies)
	return sum
}

// measureTraversal prices the two traversal tools on one fixed root.
//
// There is no native arm here, and inventing one would be dishonest: `grep`
// cannot answer a transitive question at all, so any comparison would flatter
// us by construction. What this measures is the payload itself -- tokens per
// row, and whether every row can be opened without another call -- which is a
// before-and-after claim rather than a competitive one.
func measureTraversal(
	ctx context.Context,
	session *sdkmcp.ClientSession,
	tokens *counter,
	symbol string,
) ([]traversalResult, error) {
	findText, _, err := call(ctx, session, "find_symbol", map[string]any{"name": symbol})
	if err != nil {
		return nil, err
	}
	found := findSymbolResponse{}
	if err := json.Unmarshal([]byte(findText), &found); err != nil {
		return nil, fmt.Errorf("parse find_symbol: %w", err)
	}
	if len(found.Results) == 0 {
		return nil, fmt.Errorf("find_symbol found no symbol named %q", symbol)
	}
	measured := make([]traversalResult, 0, 2)
	for _, tool := range []string{"trace_dependencies", "get_blast_radius"} {
		text, structured, callErr := call(ctx, session, tool, map[string]any{"stable_key": found.Results[0].StableKey})
		if callErr != nil {
			return nil, callErr
		}
		rows, missing, parseErr := traversalRows(tool, text)
		if parseErr != nil {
			return nil, parseErr
		}
		total := tokens.count(text)
		row := traversalResult{
			Tool: tool, Root: symbol, Rows: rows, Tokens: total,
			RowsWithoutRange:      missing,
			DuplicateChannelBytes: structured,
		}
		if rows > 0 {
			row.TokensPerRow = float64(total) / float64(rows)
		}
		measured = append(measured, row)
	}
	return measured, nil
}

func traversalRows(tool, text string) (int, int, error) {
	switch tool {
	case "trace_dependencies":
		decoded := struct {
			Results struct {
				Nodes []symbolRow `json:"nodes"`
			} `json:"results"`
		}{}
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return 0, 0, fmt.Errorf("parse %s: %w", tool, err)
		}
		return len(decoded.Results.Nodes), rowsWithoutRange(decoded.Results.Nodes), nil
	case "get_blast_radius":
		decoded := struct {
			Results struct {
				Symbols []symbolRow `json:"symbols"`
			} `json:"results"`
		}{}
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return 0, 0, fmt.Errorf("parse %s: %w", tool, err)
		}
		return len(decoded.Results.Symbols), rowsWithoutRange(decoded.Results.Symbols), nil
	default:
		return 0, 0, fmt.Errorf("unknown traversal tool %q", tool)
	}
}

func rowsWithoutRange(rows []symbolRow) int {
	missing := 0
	for _, row := range rows {
		if row.StartLine == 0 || row.EndLine < row.StartLine {
			missing++
		}
	}
	return missing
}

// measureCrossRepository prices the one question no host tool can answer.
//
// A grep across the repository roots finds the name; it cannot tell whether the
// hit is the same symbol, and it has nothing to say about a consumer that reaches
// the provider through a package dependency rather than a use. The factor is
// therefore a floor on the value, not a ceiling: it compares a complete answer
// against an incomplete one, which is why the native column is reported and not
// hidden.
func measureCrossRepository(
	ctx context.Context,
	session *sdkmcp.ClientSession,
	tokens *counter,
	directory string,
	set questionSet,
) (*crossRepositoryResult, error) {
	if set.CrossRepositoryRoot == "" {
		return nil, nil
	}
	subject, _, _, err := resolveSubject(ctx, session, question{
		Symbol:     set.CrossRepositoryRoot,
		Repository: set.CrossRepositoryRepository,
		Path:       set.CrossRepositoryPath,
	})
	if err != nil {
		return nil, err
	}
	text, structured, err := call(ctx, session, "find_cross_repo_consumers", map[string]any{
		"stable_key": subject.StableKey,
	})
	if err != nil {
		return nil, err
	}
	decoded := struct {
		Coverage struct {
			Exact        int `json:"exact"`
			PackageLevel int `json:"package_level"`
		} `json:"coverage"`
		Results struct {
			Consumers []struct {
				Category  string `json:"category"`
				StartLine int    `json:"start_line"`
				EndLine   int    `json:"end_line"`
			} `json:"consumers"`
		} `json:"results"`
	}{}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, fmt.Errorf("parse find_cross_repo_consumers: %w", err)
	}
	nativeTokens, err := nativeCost(tokens, directory, set.CrossRepositoryNative)
	if err != nil {
		return nil, err
	}
	result := &crossRepositoryResult{
		Root:                  set.CrossRepositoryRoot,
		Rows:                  len(decoded.Results.Consumers),
		Exact:                 decoded.Coverage.Exact,
		PackageLevel:          decoded.Coverage.PackageLevel,
		Native:                nativeTokens,
		Tokens:                tokens.count(text),
		DuplicateChannelBytes: structured,
	}
	for _, row := range decoded.Results.Consumers {
		// A package-level consumer has no symbol and therefore no position: the
		// edge proves the dependency, never a use. Counting it as an unopenable
		// row would be asking for a line nobody observed.
		if row.Category != "exact_symbol" && row.Category != "candidate" {
			continue
		}
		if row.StartLine == 0 || row.EndLine < row.StartLine {
			result.RowsWithoutRange++
		}
	}
	if result.Rows > 0 {
		result.TokensPerRow = float64(result.Tokens) / float64(result.Rows)
	}
	result.Factor = ratio(result.Native, result.Tokens)
	return result, nil
}

// repositoryRoot resolves which repository a row's path is relative to, falling
// back to the corpus root when a row does not name one.
func repositoryRoot(roots map[string]string, repository, fallback string) string {
	if root, found := roots[repository]; found && root != "" {
		return root
	}
	return fallback
}

// resolveSubject answers the declaration a question is about, and refuses to
// guess one.
//
// A bare name does not identify a symbol in a corpus of any size: the kena
// monorepo declares 87 rows named `RedisAdapter`, one class and eighty-six
// imports and aliases of it. Taking the first row made the measurement depend on
// page order, and it took the cheap side: consumers of an import alias are a
// handful of package-level rows, so the graph arm looked twelve times cheaper
// than the host while answering a different question.
//
// So an ambiguous name is an error, and the question has to name the triple the
// surface accepts -- repository, path, qualified name -- which is exactly what
// that triple exists for.
func resolveSubject(
	ctx context.Context,
	session *sdkmcp.ClientSession,
	asked question,
) (symbolRow, string, int, error) {
	if asked.Repository != "" && asked.Path != "" {
		arguments := map[string]any{
			"repository":     asked.Repository,
			"path":           asked.Path,
			"qualified_name": asked.QualifiedName(),
		}
		text, structured, err := call(ctx, session, "get_symbol", arguments)
		if err != nil {
			return symbolRow{}, "", 0, err
		}
		decoded := getSymbolResponse{}
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return symbolRow{}, "", 0, fmt.Errorf("parse get_symbol: %w", err)
		}
		row := symbolRow{
			StableKey:     decoded.Results.StableKey,
			Repository:    asked.Repository,
			QualifiedName: decoded.Results.QualifiedName,
			FilePath:      decoded.Results.FilePath,
			StartLine:     decoded.Results.StartLine,
			EndLine:       decoded.Results.EndLine,
		}
		if row.StableKey == "" {
			return symbolRow{}, "", 0, fmt.Errorf("get_symbol returned no symbol for %s %s %s",
				asked.Repository, asked.Path, asked.QualifiedName())
		}
		return row, text, structured, nil
	}

	text, structured, err := call(ctx, session, "find_symbol", map[string]any{"name": asked.Symbol})
	if err != nil {
		return symbolRow{}, "", 0, err
	}
	found := findSymbolResponse{}
	if err := json.Unmarshal([]byte(text), &found); err != nil {
		return symbolRow{}, "", 0, fmt.Errorf("parse find_symbol: %w", err)
	}
	if len(found.Results) == 0 {
		return symbolRow{}, "", 0, fmt.Errorf("find_symbol found no symbol named %q", asked.Symbol)
	}
	if found.Total > 1 {
		return symbolRow{}, "", 0, fmt.Errorf(
			"%q names %d symbols in this corpus: give the question a repository and a path so both arms answer about the same declaration",
			asked.Symbol, found.Total)
	}
	return found.Results[0], text, structured, nil
}
