// Command intent-token-cost measures the one question this surface could not
// answer before: a plain-language description that names no symbol.
//
// The comparison is the whole point, and accuracy comes before cost. A cheap
// answer that names the wrong file is worse than an expensive one, because the
// agent pays the cheap answer and then pays a second search. So every question
// carries a ground truth established by reading the implementation -- never
// from a tool answer -- and both arms are scored against it before either is
// priced.
//
// Two arms per question:
//
//   - native: the single search an asker can actually run, which is a word from
//     the question, because the identifier that implements the behaviour is what
//     the asker is looking for and cannot spell yet.
//   - kivgraph: find_by_intent with view=files, the granularity the question
//     asks at.
//
// Both arms then open the file they landed on, and that body is priced inside
// both. It sets the floor: no answer, however lean, can beat the source.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

const (
	benchmarkName = "intent-token-cost"
	encodingName  = "cl100k_base"
	callTimeout   = 30 * time.Second

	// intentLimit is what a first call asks for. It is the tool's own default:
	// measuring a wider page would price a call no agent makes first.
	intentLimit = 10
)

type questionSet struct {
	Version     int    `json:"version"`
	Tokenizer   string `json:"tokenizer"`
	Repository  string `json:"repository"`
	Question    string `json:"question"`
	GroundTruth string `json:"ground_truth"`
	Questions   []struct {
		Intent string   `json:"intent"`
		Class  string   `json:"class"`
		Answer []string `json:"answer"`
		Native struct {
			Tool    string `json:"tool"`
			Pattern string `json:"pattern"`
			Note    string `json:"note"`
		} `json:"native"`
	} `json:"questions"`
}

type arm struct {
	// Rank is where the first ground-truth file appears in this arm's answer,
	// one-based. Zero means it does not appear at all, which is the outcome no
	// token count can compensate for.
	Rank      int  `json:"rank"`
	Files     int  `json:"files_offered"`
	Answer    int  `json:"answer_tokens"`
	Body      int  `json:"body_tokens"`
	Total     int  `json:"total"`
	Truncated bool `json:"truncated,omitempty"`
}

type questionResult struct {
	Intent   string   `json:"intent"`
	Class    string   `json:"class"`
	Answer   []string `json:"answer"`
	Pattern  string   `json:"native_pattern"`
	Native   arm      `json:"native"`
	Kivgraph arm      `json:"kivgraph"`
	// Two factors, because one alone misleads. Answer compares only what each
	// side spends to land on the file; session adds the body both sides then
	// read, and that body is identical, so the session factor has a ceiling
	// this question set cannot exceed.
	AnswerFactor   float64  `json:"answer_factor"`
	SessionFactor  float64  `json:"session_factor"`
	UnmatchedTerms []string `json:"unmatched_terms,omitempty"`
}

type totals struct {
	Questions      int `json:"questions"`
	NativeHits     int `json:"native_hits"`
	KivgraphHits   int `json:"kivgraph_hits"`
	NativeTopOne   int `json:"native_top_one"`
	KivgraphTopOne int `json:"kivgraph_top_one"`
	NativeAnswer   int `json:"native_answer_tokens"`
	KivgraphAnswer int `json:"kivgraph_answer_tokens"`
	Bodies         int `json:"body_tokens"`

	// A search that finds nothing is cheap, and averaging its zero into a cost
	// total pays the wrong arm for being wrong. These are the same sums over the
	// questions both arms answered, which is the only set where a token count
	// compares like for like.
	SharedHits           int     `json:"shared_hits"`
	NativeAnswerOnHits   int     `json:"native_answer_tokens_on_shared_hits"`
	KivgraphAnswerOnHits int     `json:"kivgraph_answer_tokens_on_shared_hits"`
	AnswerFactorOnHits   float64 `json:"answer_factor_on_shared_hits"`

	AnswerFactor       float64 `json:"answer_factor"`
	SessionFactor      float64 `json:"session_factor"`
	SessionCeiling     float64 `json:"session_ceiling"`
	MedianAnswerFactor float64 `json:"median_answer_factor"`
}

// environment is the machine, because a token count is portable and the search
// it is compared against is not: git grep walks the working tree of a checkout.
type environment struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	GoVersion string `json:"go_version"`
}

// dataset declares what was asked and over what, so a reader can tell two runs
// apart when the question set changes underneath them.
type dataset struct {
	QuestionSet        string `json:"question_set"`
	QuestionSetVersion int    `json:"question_set_version"`
	Questions          int    `json:"questions"`
	Repository         string `json:"repository"`
	NativeScope        string `json:"native_scope"`
	// There is no seed: every arm of this benchmark is deterministic given a
	// generation and a working tree, and a seed nobody uses is a field that
	// implies a sampling this harness does not do.
	Seed string `json:"seed"`
}

type results struct {
	Benchmark   string           `json:"benchmark"`
	Command     string           `json:"command"`
	Commit      string           `json:"commit"`
	GeneratedAt string           `json:"generated_at"`
	Environment environment      `json:"environment"`
	Dataset     dataset          `json:"dataset"`
	Tokenizer   string           `json:"tokenizer"`
	SnapshotID  uint64           `json:"snapshot_id"`
	Question    string           `json:"question"`
	GroundTruth string           `json:"ground_truth"`
	Results     []questionResult `json:"results"`
	Totals      totals           `json:"totals"`
	Limitations []string         `json:"limitations"`
}

type config struct {
	Server     string
	ConfigPath string
	Directory  string
	Repository string
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Server, "server", "kivgraph", "kivgraph executable to measure")
	flag.StringVar(&cfg.ConfigPath, "config", "", "optional --config passed to kivgraph serve")
	flag.StringVar(&cfg.Directory, "dir", "benchmarks/intent-token-cost", "benchmark directory holding questions.json")
	flag.StringVar(&cfg.Repository, "repository", "kivgraph", "indexed repository the questions belong to")
	flag.Parse()
	if err := run(context.Background(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	set, err := readQuestions(cfg.Directory)
	if err != nil {
		return err
	}
	if set.Tokenizer != encodingName {
		return fmt.Errorf("question set declares tokenizer %q, this harness counts in %q", set.Tokenizer, encodingName)
	}
	tokens, err := newCounter()
	if err != nil {
		return err
	}
	session, stderr, closeSession, err := connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeSession()

	out := results{
		Benchmark:   benchmarkName,
		Command:     canonicalCommand(cfg),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Environment: environment{OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version()},
		Dataset: dataset{
			QuestionSet: filepath.Join(cfg.Directory, "questions.json"), QuestionSetVersion: set.Version,
			Questions: len(set.Questions), Repository: set.Repository,
			NativeScope: "git grep -l -i over internal and cmd", Seed: "none: both arms are deterministic",
		},
		Tokenizer:   encodingName,
		Question:    set.Question,
		GroundTruth: set.GroundTruth,
	}
	if out.Commit, err = currentCommit(); err != nil {
		return err
	}
	if out.SnapshotID, err = readSnapshotID(ctx, session); err != nil {
		return fmt.Errorf("%w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	for _, question := range set.Questions {
		result := questionResult{
			Intent: question.Intent, Class: question.Class,
			Answer: question.Answer, Pattern: question.Native.Pattern,
		}
		body, err := bodyTokens(question.Answer, tokens)
		if err != nil {
			return err
		}
		if result.Native, err = measureNative(question.Native.Pattern, question.Answer, body, tokens); err != nil {
			return err
		}
		result.Kivgraph, result.UnmatchedTerms, err = measureIntent(
			ctx, session, question.Intent, question.Answer, body, tokens)
		if err != nil {
			return err
		}
		result.AnswerFactor = factor(result.Native.Answer, result.Kivgraph.Answer)
		result.SessionFactor = factor(result.Native.Total, result.Kivgraph.Total)
		out.Results = append(out.Results, result)
	}
	out.Totals = summarize(out.Results)
	out.Limitations = limitations(out)

	if err := writeJSON(filepath.Join(cfg.Directory, "results.json"), out); err != nil {
		return err
	}
	if err := writeReport(cfg.Directory, out); err != nil {
		return err
	}
	printSummary(out)
	return nil
}

// measureNative prices the search an asker can run and scores where the answer
// lands in it. The rank is the position of the first ground-truth file in the
// order the search reports its files, because that is the order the reader
// opens them in.
func measureNative(pattern string, answer []string, body int, tokens *counter) (arm, error) {
	// The strongest native arm, not the most flattering one. Full match lines
	// over the whole tree price a payload no agent pastes -- one word of these
	// questions reports over two million tokens of lockfiles and generated
	// assets. What an agent actually runs first is a file list scoped to the
	// source, and that is both the cheapest baseline and the one at the same
	// granularity as view=files, so the comparison is like for like.
	command := exec.Command("git", "grep", "-l", "-i", "--", pattern, "internal", "cmd")
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		// Exit status 1 is "no match", which is a result and not a failure.
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return arm{}, fmt.Errorf("git grep %q: %w", pattern, err)
		}
	}
	text := string(output)
	files := strings.Fields(text)
	return arm{
		Rank:   rankOf(files, answer),
		Files:  len(files),
		Answer: tokens.count(text),
		Body:   body,
		Total:  tokens.count(text) + body,
	}, nil
}

// measureIntent prices one find_by_intent call at the granularity the question
// asks at, and reports the terms it could not match: a term with no hits is the
// one signal that tells an asker the vocabulary is wrong rather than the answer
// missing.
func measureIntent(
	ctx context.Context,
	session *sdkmcp.ClientSession,
	intent string,
	answer []string,
	body int,
	tokens *counter,
) (arm, []string, error) {
	text, err := call(ctx, session, "find_by_intent", map[string]any{
		"intent": intent, "view": "files", "limit": intentLimit,
	})
	if err != nil {
		return arm{}, nil, err
	}
	var payload struct {
		Truncated bool `json:"truncated"`
		Results   struct {
			Files []struct {
				File string `json:"file"`
			} `json:"files"`
			UnmatchedTerms []string `json:"unmatched_terms"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return arm{}, nil, fmt.Errorf("decode find_by_intent payload: %w", err)
	}
	files := make([]string, 0, len(payload.Results.Files))
	for _, file := range payload.Results.Files {
		files = append(files, file.File)
	}
	return arm{
		Rank:      rankOf(files, answer),
		Files:     len(files),
		Answer:    tokens.count(text),
		Body:      body,
		Total:     tokens.count(text) + body,
		Truncated: payload.Truncated,
	}, payload.Results.UnmatchedTerms, nil
}

// bodyTokens is the source both arms read once they have landed. It is the same
// number on both sides by construction, which is what makes the session factor
// a bound rather than a headline.
func bodyTokens(answer []string, tokens *counter) (int, error) {
	total := 0
	for _, path := range answer {
		content, err := os.ReadFile(path)
		if err != nil {
			return 0, fmt.Errorf("read ground truth %s: %w", path, err)
		}
		total += tokens.count(string(content))
	}
	return total, nil
}

// rankOf is the one-based position of the first wanted file, or zero when no
// wanted file is offered at all.
func rankOf(offered, wanted []string) int {
	for index, file := range offered {
		for _, want := range wanted {
			if file == want {
				return index + 1
			}
		}
	}
	return 0
}

func factor(native, ours int) float64 {
	if ours == 0 {
		return 0
	}
	return float64(native) / float64(ours)
}

func summarize(rows []questionResult) totals {
	out := totals{Questions: len(rows)}
	factors := make([]float64, 0, len(rows))
	for _, row := range rows {
		if row.Native.Rank > 0 {
			out.NativeHits++
		}
		if row.Native.Rank == 1 {
			out.NativeTopOne++
		}
		if row.Kivgraph.Rank > 0 {
			out.KivgraphHits++
		}
		if row.Kivgraph.Rank == 1 {
			out.KivgraphTopOne++
		}
		out.NativeAnswer += row.Native.Answer
		out.KivgraphAnswer += row.Kivgraph.Answer
		out.Bodies += row.Kivgraph.Body
		factors = append(factors, row.AnswerFactor)
		if row.Native.Rank > 0 && row.Kivgraph.Rank > 0 {
			out.SharedHits++
			out.NativeAnswerOnHits += row.Native.Answer
			out.KivgraphAnswerOnHits += row.Kivgraph.Answer
		}
	}
	out.AnswerFactor = factor(out.NativeAnswer, out.KivgraphAnswer)
	out.AnswerFactorOnHits = factor(out.NativeAnswerOnHits, out.KivgraphAnswerOnHits)
	out.SessionFactor = factor(out.NativeAnswer+out.Bodies, out.KivgraphAnswer+out.Bodies)
	out.SessionCeiling = factor(out.NativeAnswer+out.Bodies, out.Bodies)
	sort.Float64s(factors)
	if len(factors) > 0 {
		middle := len(factors) / 2
		if len(factors)%2 == 1 {
			out.MedianAnswerFactor = factors[middle]
		} else {
			out.MedianAnswerFactor = (factors[middle-1] + factors[middle]) / 2
		}
	}
	return out
}

// limitations are emitted from what the run observed, not written by hand: a
// caveat nobody can forget to update is worth more than a paragraph.
func limitations(out results) []string {
	notes := []string{}
	switch {
	case strings.HasSuffix(out.Commit, "-dirty"):
		notes = append(notes, "the working tree carried uncommitted changes: git grep walked them, and so did the server binary only if it was rebuilt from them")
	case strings.HasSuffix(out.Commit, "-unknown"):
		notes = append(notes, "the state of the working tree could not be read, so these numbers are attributed to no commit")
	}
	notes = append(notes,
		"a native search that matches nothing costs zero tokens and answers nothing, so the totals over every question flatter whichever arm missed; the shared-hit factor is the honest one",
		"the ground truth is one reader's judgement of which file answers each question; a second reader may accept a file this run scores as a miss",
		fmt.Sprintf("%d questions over one repository at generation %d: a set this size states a direction, not a rate",
			out.Totals.Questions, out.SnapshotID),
		fmt.Sprintf("measured on %s/%s with %s: the native arm walks a working tree, so its cost is not portable across checkouts",
			out.Environment.OS, out.Environment.Arch, out.Environment.GoVersion),
	)
	if out.Totals.NativeHits == out.Totals.Questions {
		notes = append(notes, "every native search did reach the answer somewhere in its output, so this set measures the cost of reaching it and not the failure to")
	}
	misses := out.Totals.Questions - out.Totals.KivgraphHits
	if misses > 0 {
		notes = append(notes, fmt.Sprintf("%d question(s) are not answered by this tool at any rank; the vocabulary of the phrase is not in the graph", misses))
	}
	if out.Totals.SessionFactor < out.Totals.AnswerFactor {
		notes = append(notes, fmt.Sprintf("the session factor is bounded at %.2fx by the bodies both arms read, which is the same source on both sides",
			out.Totals.SessionCeiling))
	}
	return notes
}

func printSummary(out results) {
	fmt.Printf("%s @ generation %d, commit %s\n", out.Benchmark, out.SnapshotID, short(out.Commit))
	fmt.Printf("accuracy   native %d/%d found, %d top-1   kivgraph %d/%d found, %d top-1\n",
		out.Totals.NativeHits, out.Totals.Questions, out.Totals.NativeTopOne,
		out.Totals.KivgraphHits, out.Totals.Questions, out.Totals.KivgraphTopOne)
	fmt.Printf("answer     native %d tokens, kivgraph %d tokens -> %.2fx (median %.2fx)\n",
		out.Totals.NativeAnswer, out.Totals.KivgraphAnswer, out.Totals.AnswerFactor, out.Totals.MedianAnswerFactor)
	fmt.Printf("on the %d questions both answered: %d vs %d -> %.2fx\n",
		out.Totals.SharedHits, out.Totals.NativeAnswerOnHits, out.Totals.KivgraphAnswerOnHits, out.Totals.AnswerFactorOnHits)
	fmt.Printf("session    %.2fx, ceiling %.2fx set by %d body tokens\n",
		out.Totals.SessionFactor, out.Totals.SessionCeiling, out.Totals.Bodies)
}

func writeReport(directory string, out results) error {
	report := &strings.Builder{}
	fmt.Fprintf(report, "# %s\n\n", benchmarkName)
	fmt.Fprintf(report, "Question: %s\n\n", out.Question)
	fmt.Fprintf(report, "Generated %s from commit `%s` on %s/%s with %s, generation `%06d`, counted in `%s`.\n\n",
		out.GeneratedAt, short(out.Commit), out.Environment.OS, out.Environment.Arch,
		out.Environment.GoVersion, out.SnapshotID, out.Tokenizer)
	fmt.Fprintf(report, "Command: `%s`. Dataset: `%s` v%d, %d questions over `%s`, native arm scoped to %s.\n\n",
		out.Command, out.Dataset.QuestionSet, out.Dataset.QuestionSetVersion,
		out.Dataset.Questions, out.Dataset.Repository, out.Dataset.NativeScope)
	fmt.Fprintf(report, "Ground truth: %s\n\n", out.GroundTruth)

	fmt.Fprintf(report, "## Accuracy first\n\n")
	fmt.Fprintf(report, "|question|class|native rank|kivgraph rank|\n|---|---|---|---|\n")
	for _, row := range out.Results {
		fmt.Fprintf(report, "|%s|%s|%s|%s|\n", row.Intent, row.Class, rankCell(row.Native), rankCell(row.Kivgraph))
	}
	fmt.Fprintf(report, "\nFound at any rank: native %d of %d, kivgraph %d of %d. First: native %d, kivgraph %d.\n\n",
		out.Totals.NativeHits, out.Totals.Questions, out.Totals.KivgraphHits, out.Totals.Questions,
		out.Totals.NativeTopOne, out.Totals.KivgraphTopOne)

	fmt.Fprintf(report, "## Then cost\n\n")
	fmt.Fprintf(report, "|question|native answer|kivgraph answer|answer|session|\n|---|---|---|---|---|\n")
	for _, row := range out.Results {
		fmt.Fprintf(report, "|%s|%d|%d|%.2fx|%.2fx|\n",
			row.Intent, row.Native.Answer, row.Kivgraph.Answer, row.AnswerFactor, row.SessionFactor)
	}
	fmt.Fprintf(report, "\nOn the %d questions both arms answered: %d vs %d tokens = **%.2fx**. A search that finds nothing is cheap, so this is the only cost comparison that is like for like.\n",
		out.Totals.SharedHits, out.Totals.NativeAnswerOnHits, out.Totals.KivgraphAnswerOnHits, out.Totals.AnswerFactorOnHits)
	fmt.Fprintf(report, "\nOver every question: answer %d vs %d tokens = **%.2fx**, median %.2fx. Session %.2fx, ceiling %.2fx from %d body tokens.\n\n",
		out.Totals.NativeAnswer, out.Totals.KivgraphAnswer, out.Totals.AnswerFactor,
		out.Totals.MedianAnswerFactor, out.Totals.SessionFactor, out.Totals.SessionCeiling, out.Totals.Bodies)

	fmt.Fprintf(report, "## What each phrase could not match\n\n")
	for _, row := range out.Results {
		if len(row.UnmatchedTerms) == 0 {
			continue
		}
		fmt.Fprintf(report, "- `%s`: %s\n", row.Intent, strings.Join(row.UnmatchedTerms, ", "))
	}

	fmt.Fprintf(report, "\n## Limitations\n\n")
	for _, note := range out.Limitations {
		fmt.Fprintf(report, "- %s\n", note)
	}
	return os.WriteFile(filepath.Join(directory, "report.md"), []byte(report.String()), 0o644)
}

func rankCell(row arm) string {
	if row.Rank == 0 {
		return fmt.Sprintf("**not offered** (%d files)", row.Files)
	}
	return fmt.Sprintf("%d of %d", row.Rank, row.Files)
}

func readQuestions(directory string) (questionSet, error) {
	content, err := os.ReadFile(filepath.Join(directory, "questions.json"))
	if err != nil {
		return questionSet{}, fmt.Errorf("read question set: %w", err)
	}
	set := questionSet{}
	if err := json.Unmarshal(content, &set); err != nil {
		return questionSet{}, fmt.Errorf("decode question set: %w", err)
	}
	if len(set.Questions) == 0 {
		return questionSet{}, errors.New("question set is empty")
	}
	return set, nil
}

func readSnapshotID(ctx context.Context, session *sdkmcp.ClientSession) (uint64, error) {
	text, err := call(ctx, session, "graph_status", map[string]any{})
	if err != nil {
		return 0, err
	}
	var payload struct {
		Results struct {
			Status     string `json:"status"`
			SnapshotID uint64 `json:"snapshot_id"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return 0, fmt.Errorf("decode graph_status: %w", err)
	}
	if payload.Results.Status != "ready" {
		return 0, fmt.Errorf("graph_status reports %q: run kivgraph index --full before measuring", payload.Results.Status)
	}
	return payload.Results.SnapshotID, nil
}

func connect(ctx context.Context, cfg config) (*sdkmcp.ClientSession, *bytes.Buffer, func(), error) {
	arguments := []string{"serve"}
	if cfg.ConfigPath != "" {
		arguments = append(arguments, "--config", cfg.ConfigPath)
	}
	serverCommand := exec.Command(cfg.Server, arguments...)
	stderr := &bytes.Buffer{}
	serverCommand.Stderr = stderr
	transport := &sdkmcp.CommandTransport{Command: serverCommand, TerminateDuration: 5 * time.Second}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: benchmarkName, Version: "1.0.0"}, nil)
	callContext, cancel := context.WithCancel(ctx)
	session, err := client.Connect(callContext, transport, nil)
	if err != nil {
		cancel()
		return nil, nil, nil, fmt.Errorf("connect to %s serve: %w (stderr: %s)",
			cfg.Server, err, strings.TrimSpace(stderr.String()))
	}
	return session, stderr, func() {
		_ = session.Close()
		cancel()
	}, nil
}

func call(ctx context.Context, session *sdkmcp.ClientSession, name string, arguments map[string]any) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	response, err := session.CallTool(callCtx, &sdkmcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return "", fmt.Errorf("call %s: %w", name, err)
	}
	if response.IsError {
		return "", fmt.Errorf("call %s returned an error result: %s", name, firstText(response))
	}
	text := firstText(response)
	if text == "" {
		return "", fmt.Errorf("call %s returned no text content", name)
	}
	return text, nil
}

func firstText(response *sdkmcp.CallToolResult) string {
	for _, content := range response.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

// currentCommit carries the state of the tree, not only its commit. Numbers
// that justify a change are measured before it is committed, and without the
// suffix the artefact attributes them to code it did not run.
func currentCommit() (string, error) {
	output, err := exec.Command("git", "rev-parse", "HEAD").Output()
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

func canonicalCommand(cfg config) string {
	command := "go run ./benchmarks/intent-token-cost"
	if cfg.Server != "kivgraph" {
		command += " -server " + cfg.Server
	}
	if cfg.ConfigPath != "" {
		command += " -config " + cfg.ConfigPath
	}
	return command
}

// short keeps whatever suffix the state of the tree added: truncating it away
// would publish a dirty measurement as a clean one.
func short(commit string) string {
	for _, suffix := range []string{"-dirty", "-unknown"} {
		if base, found := strings.CutSuffix(commit, suffix); found {
			return short(base) + suffix
		}
	}
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

// counter counts tokens the way a model is billed for them. Bytes are not a
// usable proxy: a path costs far more per byte than prose, and this benchmark
// compares paths against prose.
type counter struct {
	encoding *tiktoken.Tiktoken
}

func newCounter() (*counter, error) {
	tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader())
	encoding, err := tiktoken.GetEncoding(encodingName)
	if err != nil {
		return nil, fmt.Errorf("load %s encoding: %w", encodingName, err)
	}
	return &counter{encoding: encoding}, nil
}

func (c *counter) count(text string) int {
	return len(c.encoding.Encode(text, nil, nil))
}
