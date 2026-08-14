// Command mcp-token-cost measures what a coding session costs with Ladygraph
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

	// getSourceEnvelope and getSourceBodyHeader price the response a
	// source-serving tool would add around the bytes: one envelope per call and
	// one path-and-range line per body. They are the rates the other tools
	// already measure, and they are declared here rather than buried so the
	// projection can be argued with.
	getSourceEnvelope   = 40
	getSourceBodyHeader = 10
	// maximumSourceSymbols mirrors tools.MaximumSourceSymbols: one get_source
	// call assembles at most this many bodies.
	maximumSourceSymbols = 20
)

type config struct {
	Server     string
	ConfigPath string
	Directory  string
	Repository string
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Server, "server", "ladygraph", "ladygraph executable to measure")
	flag.StringVar(&cfg.ConfigPath, "config", "", "optional --config passed to ladygraph serve")
	flag.StringVar(&cfg.Directory, "dir", defaultDirectory, "benchmark directory holding questions.json and native captures")
	flag.StringVar(&cfg.Repository, "repository", "ladygraph", "indexed repository the questions belong to")
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
	if cfg.Server != "ladygraph" {
		command += " --server " + cfg.Server
	}
	if cfg.ConfigPath != "" {
		command += " --config " + cfg.ConfigPath
	}
	if cfg.Directory != defaultDirectory {
		command += " --dir " + cfg.Directory
	}
	if cfg.Repository != "ladygraph" {
		command += " --repository " + cfg.Repository
	}
	return command
}

func run(ctx context.Context, cfg config, command string) error {
	tokens, err := newCounter()
	if err != nil {
		return err
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
