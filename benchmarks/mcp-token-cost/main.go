// Command mcp-token-cost measures what a coding session costs with Kivgraph
// and what the same session costs without it.
//
// The comparison is the whole point. A benchmark that measures only its own
// payload always looks good: it reports a lean response and never counts the
// file reads the agent performs afterwards, nor what the alternative would
// have cost. This harness therefore measures three arms per question and keeps
// the follow-up reads inside every one of them.
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
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	benchmarkName    = "mcp-token-cost"
	defaultDirectory = "benchmarks/mcp-token-cost"
	callTimeout      = 30 * time.Second

	// maximumSourceSymbols mirrors tools.MaximumSourceSymbols: one get_source
	// call assembles at most this many bodies.
	maximumSourceSymbols = 20
)

type config struct {
	Server      string
	ConfigPath  string
	Directory   string
	Repository  string
	SurfaceOnly bool
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Server, "server", "kivgraph", "kivgraph executable to measure")
	flag.StringVar(&cfg.ConfigPath, "config", "", "optional --config passed to kivgraph serve")
	flag.StringVar(&cfg.Directory, "dir", defaultDirectory, "benchmark directory holding questions.json and native captures")
	flag.StringVar(&cfg.Repository, "repository", "kivgraph", "indexed repository the questions belong to")
	flag.BoolVar(&cfg.SurfaceOnly, "smoke", false, "measure the surface and prove every tool answers, without writing anything")
	flag.Parse()

	// os.Args[0] is whatever temporary path `go run` linked, which changes on
	// every invocation. What belongs in a result is the command a reader can
	// type, plus the flags that were not left at their default.
	if err := run(context.Background(), cfg, canonicalCommand(cfg)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func canonicalCommand(cfg config) string {
	command := "go run ./" + defaultDirectory
	if cfg.Server != "kivgraph" {
		command += " --server " + cfg.Server
	}
	if cfg.ConfigPath != "" {
		command += " --config " + cfg.ConfigPath
	}
	if cfg.Directory != defaultDirectory {
		command += " --dir " + cfg.Directory
	}
	if cfg.Repository != "kivgraph" {
		command += " --repository " + cfg.Repository
	}
	if cfg.SurfaceOnly {
		command += " --smoke"
	}
	return command
}

// maximumResidentSurfaceTokens bounds what a host keeps resident for this
// server, in the unit an agent pays.
//
// internal/mcp guards the same surface in bytes, because that package has no
// tokenizer; this is the figure the reports and the protocol document quote, and
// it is the half a byte guard cannot see -- a description rewritten into fewer,
// longer words moves one number and not the other. Measured at 716 over
// generation 000206, guarded with the headroom of one description.
const maximumResidentSurfaceTokens = 800

func run(ctx context.Context, cfg config, command string) error {
	tokens, err := newCounter()
	if err != nil {
		return err
	}
	if cfg.SurfaceOnly {
		return runSmoke(ctx, cfg, tokens)
	}
	questions, err := loadQuestions(cfg.Directory)
	if err != nil {
		return err
	}
	reads, err := loadHostReads(cfg.Directory)
	if err != nil {
		return err
	}
	commit, err := currentCommit()
	if err != nil {
		return err
	}

	session, stderr, cleanup, err := connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	out := results{
		Benchmark:   benchmarkName,
		Command:     command,
		Commit:      commit,
		GeneratedAt: time.Now().UTC(),
		Tokenizer:   encodingName,
		Environment: environment{OS: runtime.GOOS, Arch: runtime.GOARCH, Go: runtime.Version()},
		QuestionSet: questionSetMeta{Version: questions.Version, Question: questions.Question},
	}
	if initResult := session.InitializeResult(); initResult != nil {
		out.ProtocolVersion = initResult.ProtocolVersion
		out.ServerInstructions = len(initResult.Instructions) > 0
	}
	if out.ServerVersion, err = serverVersion(cfg.Server); err != nil {
		return err
	}
	if out.Snapshot, err = readSnapshot(ctx, session); err != nil {
		return err
	}
	corpus, roots, err := readCorpus(ctx, session, cfg.Repository)
	if err != nil {
		return err
	}
	out.Corpus = corpus
	if out.Surface, err = measureSurface(ctx, session, tokens); err != nil {
		return err
	}
	for _, question := range questions.Questions {
		measured, err := measureQuestion(ctx, session, tokens, cfg.Directory, corpus, roots, reads, question)
		if err != nil {
			return fmt.Errorf("question %s: %w", question.Symbol, err)
		}
		out.Questions = append(out.Questions, measured)
	}
	if out.Traversal, err = measureTraversal(ctx, session, tokens, questions.TraversalRoot); err != nil {
		return err
	}
	if out.CrossRepository, err = measureCrossRepository(ctx, session, tokens, cfg.Directory, questions); err != nil {
		return err
	}
	// Every arm that opens a file is priced from a capture, so a gap is not a
	// missing number: it is an unpriced arm. The run walks the whole set first
	// and names all of them, because they move together -- one generation moved
	// the ranges -- and one key per build makes the recapture cost more than the
	// measurement it protects.
	if unmet := reads.unmet(); len(unmet) > 0 {
		return fmt.Errorf("no captured host read for %d range(s); recapture native/reads.json against this generation:\n%s",
			len(unmet), strings.Join(unmet, "\n"))
	}
	out.Totals = totalise(out.Questions)
	out.Limitations = limitations(out)

	if stderr.Len() > 0 {
		out.ServerDiagnostics = strings.Count(strings.TrimSpace(stderr.String()), "\n") + 1
	}
	if out.Digest, err = out.computeDigest(); err != nil {
		return err
	}
	if err := writeResults(cfg.Directory, out); err != nil {
		return err
	}
	if err := writeReport(cfg.Directory, out); err != nil {
		return err
	}
	printSummary(out)
	return nil
}

// runSmoke measures the half of this benchmark that transports between corpora
// and then proves every tool answers, which together are what a gate can demand
// on every commit.
//
// The arms cannot be demanded: their native captures are pinned to the line
// ranges of one commit, so asking for them here would fail closed on the first
// change that moves a line -- which is the harness working, and useless as a
// gate. Nothing here reads a capture. What it does read is questions.json, for
// the symbol names only: a name survives an edit that moves it, and calling the
// tools with a real symbol is the difference between proving they are registered
// and proving they work.
//
// That difference is the whole reason this exists. The two defects of PR #21
// answered SNAPSHOT_UNAVAILABLE for any Go type with methods at depth two, and
// they passed every fixture test in the repository: only a real published
// generation showed them.
//
// It writes nothing. A partial run must never overwrite the report that carries
// the corpus figures.
func runSmoke(ctx context.Context, cfg config, tokens *counter) error {
	questions, err := loadQuestions(cfg.Directory)
	if err != nil {
		return err
	}
	session, _, cleanup, err := connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	snapshot, err := readSnapshot(ctx, session)
	if err != nil {
		return err
	}
	measured, err := measureSurface(ctx, session, tokens)
	if err != nil {
		return err
	}
	fmt.Printf("%s  generation %06d  %s\n", benchmarkName, snapshot.ID, encodingName)
	fmt.Printf("resident surface: %d tok (%d routes + %d descriptions), %d tok of deferred schema, %d tools, %d annotated read-only\n",
		measured.ResidentOhMyPi, measured.RouteTokens, measured.DescriptionTokens,
		measured.DeferredSchemaTokens, measured.Tools, measured.ReadOnly)
	if measured.ResidentOhMyPi > maximumResidentSurfaceTokens {
		return fmt.Errorf("resident surface is %d tokens, want at most %d: every tool costs this in every request of every session",
			measured.ResidentOhMyPi, maximumResidentSurfaceTokens)
	}
	return probeTools(ctx, session, questions)
}

// probeTools calls the reference tools with a real symbol of the indexed corpus
// and requires an answer. It asserts nothing about the payload: an empty result
// is a legitimate answer, and a transport that refuses the question is not.
//
// Depth two is not arbitrary. It is the first depth that leaves the symbol and
// walks an edge of the graph, which is where a traversal meets containment, and
// where both traversal tools were broken while every fixture passed.
func probeTools(ctx context.Context, session *sdkmcp.ClientSession, questions questionSet) error {
	root := questions.TraversalRoot
	for _, probe := range []struct {
		tool      string
		arguments map[string]any
	}{
		{"graph_status", map[string]any{}},
		{"list_repositories", map[string]any{}},
		{"find_symbol", map[string]any{"name": root}},
		{"find_by_intent", map[string]any{"intent": "publish a generation"}},
		{"find_references", map[string]any{"name": root}},
		{"trace_dependencies", map[string]any{"qualified_name": root, "depth": 2}},
		{"get_blast_radius", map[string]any{"qualified_name": root, "depth": 2}},
	} {
		if _, _, err := call(ctx, session, probe.tool, probe.arguments); err != nil {
			return fmt.Errorf("%s answered nothing about %q: %w", probe.tool, root, err)
		}
		fmt.Printf("  %-20s answered\n", probe.tool)
	}
	return nil
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
		return nil, nil, nil, fmt.Errorf("connect to %s serve: %w (stderr: %s)", cfg.Server, err, strings.TrimSpace(stderr.String()))
	}
	return session, stderr, func() {
		_ = session.Close()
		cancel()
	}, nil
}

// call returns the text block of a tool result together with the size of the
// duplicate structured channel, when the server ships one. A response that
// carries the same rows twice is billed twice by any client that renders both.
func call(ctx context.Context, session *sdkmcp.ClientSession, name string, arguments map[string]any) (string, int, error) {
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	response, err := session.CallTool(callCtx, &sdkmcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return "", 0, fmt.Errorf("call %s: %w", name, err)
	}
	if response.IsError {
		return "", 0, fmt.Errorf("call %s returned an error result: %s", name, firstText(response))
	}
	text := firstText(response)
	if text == "" {
		return "", 0, fmt.Errorf("call %s returned no text content", name)
	}
	structured := 0
	if response.StructuredContent != nil {
		encoded, marshalErr := json.Marshal(response.StructuredContent)
		if marshalErr != nil {
			return "", 0, fmt.Errorf("marshal structured content of %s: %w", name, marshalErr)
		}
		structured = len(encoded)
	}
	return text, structured, nil
}

func firstText(response *sdkmcp.CallToolResult) string {
	for _, content := range response.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

func currentCommit() (string, error) {
	output, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("read git commit: %w", err)
	}
	commit := strings.TrimSpace(string(output))
	if commit == "" {
		return "", errors.New("git rev-parse HEAD returned an empty commit")
	}
	return commit, nil
}

func serverVersion(server string) (string, error) {
	output, err := exec.Command(server, "version").Output()
	if err != nil {
		return "", fmt.Errorf("read %s version: %w", server, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func writeResults(directory string, out results) error {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	path := filepath.Join(directory, "results.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func printSummary(out results) {
	fmt.Printf("%s  generation %06d  %s  digest %s\n", out.Benchmark, out.Snapshot.ID, out.Tokenizer, out.Digest[:12])
	fmt.Printf("resident surface: %d tok in Oh My Pi (%d routes + %d descriptions), %d tok of deferred schema, %d tools\n",
		out.Surface.ResidentOhMyPi, out.Surface.RouteTokens, out.Surface.DescriptionTokens,
		out.Surface.DeferredSchemaTokens, out.Surface.Tools)
	fmt.Printf("%-18s %8s %8s %8s   %8s %8s %8s\n", "", "answer:", "native", "today", "session:", "native", "today")
	for _, question := range out.Questions {
		fmt.Printf("%-18s %8s %8d %8d %7.2fx %8d %8d %7.2fx\n", question.Symbol, "",
			question.Native.Answer, question.Today.Calls, question.AnswerFactorToday,
			question.Native.Total, question.Today.Total, question.SessionFactorToday)
	}
	fmt.Printf("%-18s %8s %8d %8d %7.2fx %8d %8d %7.2fx\n", "TOTAL", "",
		out.Totals.NativeAnswer, out.Totals.TodayAnswer, out.Totals.AnswerFactorToday,
		out.Totals.Native, out.Totals.Today, out.Totals.SessionFactorToday)
	fmt.Printf("serving the bodies with get_source: session %d tok, %.2fx; floor %.2fx\n",
		out.Totals.Projected, out.Totals.SessionFactorProjected, out.Totals.SessionCeiling)
}
