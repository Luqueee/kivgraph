package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type results struct {
	Benchmark string `json:"benchmark"`
	Command   string `json:"command"`
	Commit    string `json:"commit"`
	// Digest covers everything a rerun must reproduce. GeneratedAt and the
	// snapshot build time are deliberately outside it: the server rebuilds its
	// HotSnapshot projection on every start, so its build time moves while the
	// published generation and its counts do not.
	Digest             string                 `json:"digest"`
	GeneratedAt        time.Time              `json:"generated_at"`
	Tokenizer          string                 `json:"tokenizer"`
	ProtocolVersion    string                 `json:"protocol_version"`
	ServerVersion      string                 `json:"server_version"`
	ServerInstructions bool                   `json:"server_instructions"`
	ServerDiagnostics  int                    `json:"server_diagnostic_lines"`
	Environment        environment            `json:"environment"`
	Snapshot           snapshot               `json:"snapshot"`
	Corpus             corpus                 `json:"corpus"`
	QuestionSet        questionSetMeta        `json:"question_set"`
	Surface            surface                `json:"surface"`
	Questions          []questionResult       `json:"questions"`
	Traversal          []traversalResult      `json:"traversal"`
	CrossRepository    *crossRepositoryResult `json:"cross_repository,omitempty"`
	Totals             totals                 `json:"totals"`
	Limitations        []string               `json:"limitations"`
}

// computeDigest is the identity of a run: the same corpus, generation, question
// set and surface must produce the same string. The gate compares this, so a
// timestamp cannot fail a comparison and a changed number cannot pass one.
func (out results) computeDigest() (string, error) {
	comparable := struct {
		Tokenizer   string                 `json:"tokenizer"`
		Generation  int                    `json:"generation"`
		Symbols     int                    `json:"symbols"`
		Files       int                    `json:"files"`
		Edges       int                    `json:"edges"`
		Schema      int                    `json:"schema"`
		Resolver    string                 `json:"resolver"`
		Indexed     string                 `json:"indexed_commit"`
		QuestionSet int                    `json:"question_set_version"`
		Surface     surface                `json:"surface"`
		Questions   []questionResult       `json:"questions"`
		Traversal   []traversalResult      `json:"traversal"`
		CrossRepo   *crossRepositoryResult `json:"cross_repository"`
		Totals      totals                 `json:"totals"`
	}{
		Tokenizer:   out.Tokenizer,
		Generation:  out.Snapshot.ID,
		Symbols:     out.Snapshot.Symbols,
		Files:       out.Snapshot.Files,
		Edges:       out.Snapshot.Edges,
		Schema:      out.Snapshot.SchemaVersion,
		Resolver:    out.Snapshot.ResolverVersion,
		Indexed:     out.Corpus.IndexedCommit,
		QuestionSet: out.QuestionSet.Version,
		Surface:     out.Surface,
		Questions:   out.Questions,
		Traversal:   out.Traversal,
		CrossRepo:   out.CrossRepository,
		Totals:      out.Totals,
	}
	encoded, err := json.Marshal(comparable)
	if err != nil {
		return "", fmt.Errorf("marshal comparable results: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

type environment struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	Go   string `json:"go"`
}

type snapshot struct {
	ID              int    `json:"id"`
	Symbols         int    `json:"symbols"`
	Files           int    `json:"files"`
	Edges           int    `json:"edges"`
	BuiltAt         string `json:"built_at"`
	ResolverVersion string `json:"resolver_version"`
	SchemaVersion   int    `json:"schema_version"`
}

type corpus struct {
	Repository    string `json:"repository"`
	Path          string `json:"path"`
	Branch        string `json:"branch"`
	IndexedCommit string `json:"indexed_commit"`
	CurrentCommit string `json:"current_commit"`
	Fresh         bool   `json:"fresh"`
	Repositories  int    `json:"repositories_in_generation"`
}

type questionSetMeta struct {
	Version  int    `json:"version"`
	Question string `json:"question"`
}

// surface is what a host keeps resident. See measureSurface for why the schema
// total is reported separately from the resident number.
type surface struct {
	Tools                int `json:"tools"`
	ReadOnly             int `json:"read_only_annotated"`
	RouteTokens          int `json:"route_tokens"`
	DescriptionTokens    int `json:"description_tokens"`
	ResidentOhMyPi       int `json:"resident_oh_my_pi"`
	DeferredSchemaTokens int `json:"deferred_schema_tokens"`
}

type arm struct {
	Answer    int    `json:"answer_tokens,omitempty"`
	Calls     int    `json:"call_tokens,omitempty"`
	Bodies    int    `json:"body_tokens"`
	Total     int    `json:"total"`
	Projected bool   `json:"projected,omitempty"`
	Note      string `json:"note"`
}

type questionResult struct {
	Symbol                string `json:"symbol"`
	Class                 string `json:"class"`
	References            int    `json:"references"`
	ExtraCalls            int    `json:"extra_calls"`
	DuplicateChannelBytes int    `json:"duplicate_channel_bytes"`
	// FloorBytes is the source itself, with no envelope of any kind: nothing that
	// shows the code can carry less.
	FloorBytes int `json:"floor_bytes"`
	Native     arm `json:"native"`
	Today      arm `json:"today"`
	Projected  arm `json:"projected"`
	// Two factors, because one alone misleads. Answer compares only what each
	// side spends to answer the question; session adds the bodies the agent
	// then opens, which both sides pay identically. Publishing the flattering
	// one is how this field arrives at its headline numbers.
	AnswerFactorToday      float64 `json:"answer_factor_today"`
	AnswerFactorProjected  float64 `json:"answer_factor_projected"`
	SessionFactorToday     float64 `json:"session_factor_today"`
	SessionFactorProjected float64 `json:"session_factor_projected"`
}

// traversalResult prices a traversal payload. It has no factor: `grep` cannot
// answer a transitive question, so there is nothing to divide by. RowsWithoutRange
// is the acceptance criterion of LUQUE-1901 in one integer -- any row above zero
// is a row an agent must spend another call to open.
type traversalResult struct {
	Tool                  string  `json:"tool"`
	Root                  string  `json:"root"`
	Rows                  int     `json:"rows"`
	Tokens                int     `json:"tokens"`
	TokensPerRow          float64 `json:"tokens_per_row"`
	RowsWithoutRange      int     `json:"rows_without_range"`
	DuplicateChannelBytes int     `json:"duplicate_channel_bytes"`
}

// crossRepositoryResult prices the question a host cannot answer at all.
type crossRepositoryResult struct {
	Root                  string  `json:"root"`
	Rows                  int     `json:"rows"`
	Exact                 int     `json:"exact"`
	PackageLevel          int     `json:"package_level"`
	Native                int     `json:"native"`
	Tokens                int     `json:"tokens"`
	TokensPerRow          float64 `json:"tokens_per_row"`
	RowsWithoutRange      int     `json:"rows_without_range"`
	DuplicateChannelBytes int     `json:"duplicate_channel_bytes"`
	Factor                float64 `json:"factor"`
}

type totals struct {
	Native                 int     `json:"native"`
	NativeAnswer           int     `json:"native_answer"`
	Today                  int     `json:"today"`
	TodayAnswer            int     `json:"today_answer"`
	Projected              int     `json:"projected"`
	ProjectedAnswer        int     `json:"projected_answer"`
	Bodies                 int     `json:"body_tokens"`
	ServedBodies           int     `json:"served_body_tokens"`
	ExtraCalls             int     `json:"extra_calls"`
	DuplicateChannelBytes  int     `json:"duplicate_channel_bytes"`
	AnswerFactorToday      float64 `json:"answer_factor_today"`
	AnswerFactorProjected  float64 `json:"answer_factor_projected"`
	SessionFactorToday     float64 `json:"session_factor_today"`
	SessionFactorProjected float64 `json:"session_factor_projected"`
	// SessionCeiling is the best a perfect answer could do on this question
	// set: the bodies alone. It bounds every future task in the phase.
	SessionCeiling float64 `json:"session_ceiling"`
}

// limitations are emitted from what the run observed, not written by hand: a
// limitation nobody can derive from the result is a limitation nobody checks.
func limitations(out results) []string {
	notes := []string{
		"Both arms pay the same body cost, computed from the graph's exact line ranges. The native arm would in practice read wider, because grep reports a match line and not where the declaration ends, so the comparison is conservative in favour of the native path.",
		"The arms that open files are billed from captured host reads, so the line prefixes they carry are counted; the served arm is billed from the bytes alone. A missing capture fails the run instead of falling back to the raw slice.",
		"The served arm is measured against the real get_source, not projected.",
		"Adoption is not measured here. Whether an agent calls these tools at all is an observation over real sessions and belongs to LUQUE-1904.",
		"Neither money nor latency is measured. Prompt caching makes cost depend on the order the arms run in, so a token count is the only figure that survives a reordering.",
	}
	if out.CrossRepository != nil {
		notes = append(notes, fmt.Sprintf("The cross-repository question is measured on %q, and its native column is a floor rather than a ceiling: a grep finds the name but cannot tell whether the hit is the same symbol, and says nothing about a consumer that depends on the provider package without using the symbol.", out.CrossRepository.Root))
	} else if out.Corpus.Repositories < 2 {
		notes = append(notes, "The generation holds a single repository, so no question exercises cross-repository resolution, which is where an exact graph has no native competitor at all.")
	} else {
		notes = append(notes, fmt.Sprintf("The generation holds %d repositories but no question in the set has consumers outside its own, so the cross-repository case is unmeasured.", out.Corpus.Repositories))
	}
	if !out.Corpus.Fresh {
		notes = append(notes, fmt.Sprintf("The working tree moved from the indexed commit %s to %s, so the line ranges the graph reports may not describe the files on disk.", short(out.Corpus.IndexedCommit), short(out.Corpus.CurrentCommit)))
	}
	if !out.ServerInstructions {
		notes = append(notes, "The server sends no MCP instructions field, so under Claude Code the surface is a list of bare tool names.")
	}
	return notes
}

func short(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}

func writeReport(directory string, out results) error {
	report := &strings.Builder{}
	fmt.Fprintf(report, "# %s\n\n", out.Benchmark)
	fmt.Fprintf(report, "Question: %s.\n\n", out.QuestionSet.Question)
	fmt.Fprintf(report, "- Command: `%s`\n", out.Command)
	fmt.Fprintf(report, "- Commit: `%s`\n", out.Commit)
	fmt.Fprintf(report, "- Generated: `%s`\n", out.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(report, "- Server: `%s`, MCP protocol `%s`\n", out.ServerVersion, out.ProtocolVersion)
	fmt.Fprintf(report, "- Environment: `%s/%s`, `%s`\n", out.Environment.OS, out.Environment.Arch, out.Environment.Go)
	fmt.Fprintf(report, "- Generation: `%06d`, %d symbols, %d files, %d edges, schema `%d`, resolver `%s`\n",
		out.Snapshot.ID, out.Snapshot.Symbols, out.Snapshot.Files, out.Snapshot.Edges,
		out.Snapshot.SchemaVersion, out.Snapshot.ResolverVersion)
	fmt.Fprintf(report, "- Corpus: `%s` at `%s`, indexed commit `%s`, tree %s\n",
		out.Corpus.Repository, out.Corpus.Path, short(out.Corpus.IndexedCommit),
		map[bool]string{true: "unchanged", false: "moved since indexing"}[out.Corpus.Fresh])
	fmt.Fprintf(report, "- Tokenizer: `%s`, question set version `%d`\n\n", out.Tokenizer, out.QuestionSet.Version)

	report.WriteString("## Resident surface\n\n")
	fmt.Fprintf(report, "%d tools, %d annotated read-only.\n\n", out.Surface.Tools, out.Surface.ReadOnly)
	report.WriteString("| what | tokens |\n| --- | ---: |\n")
	fmt.Fprintf(report, "| route lines, Oh My Pi | %d |\n", out.Surface.RouteTokens)
	fmt.Fprintf(report, "| descriptions, Oh My Pi | %d |\n", out.Surface.DescriptionTokens)
	fmt.Fprintf(report, "| **resident total, Oh My Pi** | **%d** |\n", out.Surface.ResidentOhMyPi)
	fmt.Fprintf(report, "| full schemas, deferred by both hosts | %d |\n\n", out.Surface.DeferredSchemaTokens)
	report.WriteString("Neither host holds the schemas: Oh My Pi mounts each MCP tool as a device whose documentation is read on demand, and Claude Code defers them behind its tool search. The resident number is what a surface regression is measured against.\n\n")

	report.WriteString("## Answering the question\n\n")
	report.WriteString("What each side spends to say who calls the symbol. This is the part a graph server owns.\n\n")
	report.WriteString("| symbol | class | refs | native | today | projected | today | projected |\n")
	report.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, question := range out.Questions {
		fmt.Fprintf(report, "| `%s` | %s | %d | %d | %d | %d | %.2fx | %.2fx |\n",
			question.Symbol, question.Class, question.References,
			question.Native.Answer, question.Today.Calls, question.Projected.Calls,
			question.AnswerFactorToday, question.AnswerFactorProjected)
	}
	fmt.Fprintf(report, "| **total** | | | **%d** | **%d** | **%d** | **%.2fx** | **%.2fx** |\n\n",
		out.Totals.NativeAnswer, out.Totals.TodayAnswer, out.Totals.ProjectedAnswer,
		out.Totals.AnswerFactorToday, out.Totals.AnswerFactorProjected)

	report.WriteString("## The whole session\n\n")
	report.WriteString("The same answer plus the bodies the agent then opens. Both sides pay those identically, so this factor is bounded no matter how lean the answer gets.\n\n")
	report.WriteString("| symbol | native | today | projected | today | projected |\n")
	report.WriteString("| --- | ---: | ---: | ---: | ---: | ---: |\n")
	for _, question := range out.Questions {
		fmt.Fprintf(report, "| `%s` | %d | %d | %d | %.2fx | %.2fx |\n",
			question.Symbol, question.Native.Total, question.Today.Total, question.Projected.Total,
			question.SessionFactorToday, question.SessionFactorProjected)
	}
	fmt.Fprintf(report, "| **total** | **%d** | **%d** | **%d** | **%.2fx** | **%.2fx** |\n\n",
		out.Totals.Native, out.Totals.Today, out.Totals.Projected,
		out.Totals.SessionFactorToday, out.Totals.SessionFactorProjected)

	fmt.Fprintf(report, "Of the session totals, %d tokens are source bodies. That is the floor: an answer that cost nothing at all would still land at **%.2fx**, so no amount of payload work on this question class can go past it. Removing the per-reference `get_symbol` round trip, and paying for `end_line` on every row instead, is worth %d tokens net.\n\n",
		out.Totals.Bodies, out.Totals.SessionCeiling, out.Totals.Today-out.Totals.Projected)

	report.WriteString("Publishing only one of the two factors is how this field arrives at its headline numbers. The answer factor flatters us and the session factor flatters the alternative; both are here.\n\n")
	if out.Totals.DuplicateChannelBytes > 0 {
		fmt.Fprintf(report, "Every measured response also shipped the same rows a second time as `structuredContent`: %d bytes across the run. Oh My Pi discards that channel; a client that renders both is billed twice.\n\n",
			out.Totals.DuplicateChannelBytes)
	}

	report.WriteString("## Arms\n\n")
	report.WriteString("- **native**: the host's own captured answer for the same question, plus the bodies the agent then opens. Both captures are verbatim tool results committed under `native/`, never a reimplementation.\n")
	report.WriteString("- **today**: the MCP calls a session needs against the published generation, plus the same host reads.\n")
	report.WriteString("- **served**: the same calls plus one `get_source` that returns every body the answer named, measured against the real tool.\n\n")
	report.WriteString("The two body figures are not the same number, and that is the point. A host range read prepends a snapshot header and a line number to every line, which measures 38 % on top of the bytes. Charging the raw slice to every arm, as this harness first did, discounted the alternative and made a source-serving tool look worthless.\n\n")

	report.WriteString("## Limitations\n\n")
	for _, note := range out.Limitations {
		fmt.Fprintf(report, "- %s\n", note)
	}

	path := filepath.Join(directory, "report.md")
	if err := os.WriteFile(path, []byte(report.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
