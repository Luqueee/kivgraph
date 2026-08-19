package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// arm is one context condition. Only the context layer differs: the model, the
// prompt, the file tools and the turn budget are identical, so a difference in
// outcome is attributable to the context and not to the harness.
type arm struct {
	Name string
	// MCP is the server block written to a per-arm config, empty for cold.
	MCP map[string]any
	// Tools are the arm's MCP tools, appended to the shared file tools. They are
	// named explicitly because an allow-list that ended at a wildcard would let
	// one arm reach a tool the others cannot.
	Tools []string
}

// fileTools is what every arm gets. The shell is absent on purpose: with it an
// agent can run `git log`, and on a corpus built from real commits that is a
// route to the answer rather than to the code.
var fileTools = []string{"Read", "Glob", "Grep", "Edit", "Write", "MultiEdit", "NotebookEdit", "TodoWrite"}

func arms(cfg config) []arm {
	return []arm{
		{Name: "cold"},
		{
			Name: "kivgraph",
			MCP: map[string]any{"kivgraph": map[string]any{
				"command": cfg.Kivgraph, "args": []string{"serve"},
				"env": map[string]string{"HOME": cfg.Home},
			}},
			Tools: []string{
				"mcp__kivgraph__find_symbol", "mcp__kivgraph__find_references",
				"mcp__kivgraph__get_blast_radius", "mcp__kivgraph__trace_dependencies",
				"mcp__kivgraph__get_file_outline", "mcp__kivgraph__get_source",
				"mcp__kivgraph__get_symbol", "mcp__kivgraph__find_cross_repo_consumers",
				"mcp__kivgraph__list_repositories", "mcp__kivgraph__graph_status",
			},
		},
		{
			Name: "graft",
			MCP: map[string]any{"graft": map[string]any{
				"command": cfg.Graft,
				"args":    []string{"--dir", cfg.GraftContext, "mcp", cfg.Root},
			}},
			Tools: []string{
				"mcp__graft__graft_find_code", "mcp__graft__graft_file_api",
				"mcp__graft__graft_trace_calls", "mcp__graft__graft_find_all",
				"mcp__graft__graft_repo_map", "mcp__graft__graft_check_freshness",
			},
		},
	}
}

// prompt is what every arm is asked. It states the intent and the workspace, and
// says nothing about which tools exist: an instruction to "use the graph" would
// be the harness doing the arm's job.
func prompt(t task, root string) string {
	return fmt.Sprintf(`You are working in a multi-repository workspace at %s.

Implement this change:

%s

The change belongs in the %s repository. Work out which files to modify by
reading the code, then make the edits. Follow the conventions already in the
repository, including its tests if the change warrants one. Do not run builds or
tests. Stop when the edits are complete.`, root, t.Intent, t.Repo)
}

// run executes one arm on one task and returns what it cost and what it wrote.
func (a arm) run(cfg config, t task, trial int) (runResult, error) {
	result := runResult{Arm: a.Name, Task: t.ID, Trial: trial}
	arguments := []string{
		"-p", prompt(t, cfg.Root),
		"--model", cfg.Model,
		"--output-format", "stream-json", "--verbose",
		"--dangerously-skip-permissions",
	}
	allowed := append([]string{}, fileTools...)
	allowed = append(allowed, a.Tools...)
	arguments = append(arguments, "--allowedTools")
	arguments = append(arguments, allowed...)
	arguments = append(arguments, "--disallowedTools", "Bash", "WebFetch", "WebSearch", "Task")
	if a.MCP != nil {
		path := filepath.Join(cfg.Directory, "mcp-"+a.Name+".json")
		encoded, err := json.MarshalIndent(map[string]any{"mcpServers": a.MCP}, "", " ")
		if err != nil {
			return result, fmt.Errorf("encode %s mcp config: %w", a.Name, err)
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			return result, fmt.Errorf("write %s: %w", path, err)
		}
		arguments = append(arguments, "--mcp-config", path, "--strict-mcp-config")
	}

	command := exec.Command(cfg.Agent, arguments...)
	command.Dir = cfg.Root
	command.Env = append(os.Environ(), "HOME="+cfg.AgentHome)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return result, fmt.Errorf("pipe agent stdout: %w", err)
	}
	command.Stderr = os.Stderr
	started := time.Now()
	if err := command.Start(); err != nil {
		return result, fmt.Errorf("start agent: %w", err)
	}

	transcript := filepath.Join(cfg.Directory, "raw", fmt.Sprintf("%s-%s-t%d.jsonl", t.ID, a.Name, trial))
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		return result, err
	}
	sink, err := os.Create(transcript)
	if err != nil {
		return result, fmt.Errorf("create %s: %w", transcript, err)
	}
	defer sink.Close()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintln(sink, line)
		result.absorb(line)
	}
	waitErr := command.Wait()
	result.MS = float64(time.Since(started).Milliseconds())
	result.Transcript = transcript
	if waitErr != nil && result.Turns == 0 {
		return result, fmt.Errorf("agent failed: %w", waitErr)
	}
	return result, nil
}

// runResult is one arm's run: its cost, its tool use and its edits.
type runResult struct {
	Task       string   `json:"task"`
	Arm        string   `json:"arm"`
	Trial      int      `json:"trial"`
	MS         float64  `json:"ms"`
	CostUSD    float64  `json:"cost_usd"`
	InTokens   int      `json:"input_tokens"`
	OutTokens  int      `json:"output_tokens"`
	CacheRead  int      `json:"cache_read_tokens"`
	CacheWrite int      `json:"cache_creation_tokens"`
	Turns      int      `json:"turns"`
	ToolCalls  int      `json:"tool_calls"`
	ToolsUsed  []string `json:"tools_used"`
	ContextUse int      `json:"context_tool_calls"`
	Errored    bool     `json:"errored"`
	Error      string   `json:"error,omitempty"`
	Transcript string   `json:"transcript"`

	// Wrote and Outside are filled by the scorer from git, not from the model's
	// account of itself: an agent that says it edited a file and did not is a
	// case this benchmark has to catch.
	Wrote     []string `json:"wrote"`
	Outside   []string `json:"wrote_outside_repo"`
	Precision float64  `json:"precision"`
	Recall    float64  `json:"recall"`
	Missing   []string `json:"missing"`
	Spurious  []string `json:"spurious"`
	Exact     bool     `json:"exact"`
	// Leak is set when the transcript shows the agent reached the answer instead
	// of deriving it: the commit under test, or a read of the object database.
	Leak string `json:"leak,omitempty"`

	tools map[string]int
}

// absorb reads one stream-json line. Everything the report claims about turns,
// tool calls and cost comes from the agent's own event stream.
func (r *runResult) absorb(line string) {
	event := struct {
		Type    string  `json:"type"`
		Subtype string  `json:"subtype"`
		IsError bool    `json:"is_error"`
		Result  string  `json:"result"`
		Cost    float64 `json:"total_cost_usd"`
		Turns   int     `json:"num_turns"`
		Usage   struct {
			Input      int `json:"input_tokens"`
			Output     int `json:"output_tokens"`
			CacheRead  int `json:"cache_read_input_tokens"`
			CacheWrite int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content"`
		} `json:"message"`
	}{}
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return
	}
	if r.tools == nil {
		r.tools = map[string]int{}
	}
	for _, block := range event.Message.Content {
		if block.Type == "tool_use" {
			r.ToolCalls++
			r.tools[block.Name]++
			if strings.HasPrefix(block.Name, "mcp__") {
				r.ContextUse++
			}
		}
	}
	if event.Type == "result" {
		r.CostUSD = event.Cost
		r.Turns = event.Turns
		r.InTokens = event.Usage.Input
		r.OutTokens = event.Usage.Output
		r.CacheRead = event.Usage.CacheRead
		r.CacheWrite = event.Usage.CacheWrite
		r.Errored = event.IsError
		if event.IsError {
			r.Error = firstLine(event.Result)
		}
		names := make([]string, 0, len(r.tools))
		for name, count := range r.tools {
			names = append(names, fmt.Sprintf("%s×%d", name, count))
		}
		sort.Strings(names)
		r.ToolsUsed = names
	}
}

// score compares what the arm wrote against what the author changed.
func (r *runResult) score(t task, inside, outside []string) {
	r.Wrote, r.Outside = inside, outside
	truth := map[string]bool{}
	for _, file := range t.Truth {
		truth[file] = true
	}
	wrote := map[string]bool{}
	for _, file := range inside {
		wrote[file] = true
	}
	hits := 0
	for file := range wrote {
		if truth[file] {
			hits++
		} else {
			r.Spurious = append(r.Spurious, file)
		}
	}
	for file := range truth {
		if !wrote[file] {
			r.Missing = append(r.Missing, file)
		}
	}
	sort.Strings(r.Spurious)
	sort.Strings(r.Missing)
	if len(wrote) > 0 {
		r.Precision = float64(hits) / float64(len(wrote))
	}
	if len(truth) > 0 {
		r.Recall = float64(hits) / float64(len(truth))
	}
	r.Exact = r.Precision == 1 && r.Recall == 1
}

// auditLeak looks for the two ways an arm could have been told the answer rather
// than found it. A run that trips this is reported, never silently kept.
func (r *runResult) auditLeak(t task) {
	raw, err := os.ReadFile(r.Transcript)
	if err != nil {
		return
	}
	text := string(raw)
	for _, needle := range []string{t.Commit, t.Commit[:12], "/.git/", "COMMIT_EDITMSG", "ORIG_HEAD"} {
		if strings.Contains(text, needle) {
			r.Leak = "transcript mentions " + needle
			return
		}
	}
}

func firstLine(text string) string {
	if index := strings.Index(text, "\n"); index >= 0 {
		return text[:index]
	}
	return text
}
