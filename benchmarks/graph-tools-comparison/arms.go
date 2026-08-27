package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// encodingName is the tokenizer every number here is counted with. It is a
// proxy for the Claude tokenizer, so the ratios between arms are the claim and
// the absolute values are not.
const encodingName = "o200k_base"

const callTimeout = 180 * time.Second

type counter struct{ encoding *tiktoken.Tiktoken }

var loadOffline sync.Once

// newCounter loads the embedded ranks. The offline loader is what keeps the run
// hermetic: the default one downloads the vocabulary on first use, which would
// make a measurement depend on network reachability.
func newCounter() (*counter, error) {
	loadOffline.Do(func() { tiktoken.SetBpeLoader(tiktokenloader.NewOfflineLoader()) })
	encoding, err := tiktoken.GetEncoding(encodingName)
	if err != nil {
		return nil, fmt.Errorf("load %s encoding: %w", encodingName, err)
	}
	return &counter{encoding: encoding}, nil
}

func (c *counter) count(text string) int { return len(c.encoding.Encode(text, nil, nil)) }

// observation is one call: what it cost and what it said.
type observation struct {
	Tool   string  `json:"tool"`
	Tokens int     `json:"tokens"`
	MS     float64 `json:"ms"`
	// Text is not serialised here -- it is written to raw/ instead, where a
	// reader can diff it -- but it travels so the scorers read the same bytes
	// that were priced.
	Text string `json:"-"`
	// Failed records a call that answered with an error. A failure is a
	// measurement, not a reason to abort: what it costs to be told "no", or to
	// be told "ambiguous", belongs in the total.
	Failed bool   `json:"failed,omitempty"`
	Error  string `json:"error,omitempty"`
	// Banner is what a response spends on text addressed to the model rather
	// than on the answer -- a saving estimate, an instruction to repeat it.
	// An agent is billed for it on every call, so it is counted separately
	// instead of being folded into the answer or quietly excluded from it.
	Banner int `json:"banner_tokens,omitempty"`
}

// armResult is what one tool spent on one question and what it claimed.
type armResult struct {
	Calls     []observation `json:"calls"`
	Tokens    int           `json:"tokens"`
	MS        float64       `json:"ms"`
	CallCount int           `json:"call_count"`
	// Claimed is what the tool said, canonicalised. Repetition is kept: seven
	// hits in one file are seven facts, and the scorer decides to compare sets.
	Claimed []string `json:"claimed,omitempty"`
	Score   *score   `json:"score,omitempty"`
	// Unsupported marks a family this tool does not answer at all. It is not a
	// zero: a zero would say it answered wrongly, and this says it was never
	// asked, which is a different fact about a different tool.
	Unsupported bool   `json:"unsupported,omitempty"`
	Note        string `json:"note,omitempty"`
	// Ambiguous is how many declarations a refusal refused between, so the
	// number means the same thing on every arm that refuses.
	Ambiguous int `json:"ambiguous_declarations,omitempty"`
}

func (a *armResult) add(o observation) {
	a.Calls = append(a.Calls, o)
	a.Tokens += o.Tokens
	a.MS += o.MS
	a.CallCount++
}

// score compares a claimed set against the truth. Precision and recall are
// computed over whatever unit the family declares -- files, or declaration
// names -- and never over lines: a disagreement about which line inside a file
// holds a call is not a disagreement about the answer.
type score struct {
	Claimed   []string `json:"claimed_set"`
	Precision float64  `json:"precision"`
	Recall    float64  `json:"recall"`
	Missing   []string `json:"missing"`
	Spurious  []string `json:"spurious"`
}

func scoreAgainst(claimed, truth []string) *score {
	claimedSet, truthSet := map[string]bool{}, map[string]bool{}
	for _, item := range claimed {
		if item != "" {
			claimedSet[item] = true
		}
	}
	for _, item := range truth {
		truthSet[item] = true
	}
	hit := 0
	spurious := []string{}
	for item := range claimedSet {
		if truthSet[item] {
			hit++
			continue
		}
		spurious = append(spurious, item)
	}
	missing := []string{}
	for item := range truthSet {
		if !claimedSet[item] {
			missing = append(missing, item)
		}
	}
	out := &score{Claimed: sortedKeys(claimedSet), Missing: sorted(missing), Spurious: sorted(spurious)}
	// An empty truth is a question whose correct answer is "nothing", and it
	// has to be scorable: the ratios below are both undefined there, and
	// leaving them at zero would mark the only correct answer -- claiming
	// nothing -- as a total failure, which is why the set had no absence
	// question until now. So the conventions are stated rather than implied:
	// claiming nothing against an empty truth is exact, and claiming anything
	// against it is precision zero with nothing left to miss.
	switch {
	case len(truthSet) == 0:
		out.Recall = 1
		if len(claimedSet) == 0 {
			out.Precision = 1
		}
	default:
		out.Recall = float64(hit) / float64(len(truthSet))
		if len(claimedSet) > 0 {
			out.Precision = float64(hit) / float64(len(claimedSet))
		}
	}
	return out
}

func sorted(in []string) []string {
	sort.Strings(in)
	return in
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return sorted(out)
}

// repositories maps a corpus-relative path to the one address this benchmark
// compares on: the registered repository name and the path inside it. The tools
// answer in four different spellings -- repository-relative, corpus-relative,
// absolute, and repository-prefixed -- and two repositories can both hold a
// `src/index.ts`, so no single spelling is comparable on its own.
type repositories struct {
	dirs  []string
	names map[string]string
	root  string
}

// discoverRepositories walks until it meets a `.git`, at whatever depth it sits.
// A fixed two-level walk missed `workspace-cli`, which lives at the corpus root and
// is a repository like any other; a benchmark that silently indexes 36 of 37
// repositories is measuring a corpus nobody has.
func discoverRepositories(corpus string) (repositories, error) {
	out := repositories{names: map[string]string{}, root: corpus}
	err := filepath.WalkDir(corpus, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return nil
		}
		switch entry.Name() {
		case "node_modules", "target", "dist", "graphify-out":
			return fs.SkipDir
		}
		if _, statErr := os.Stat(filepath.Join(path, ".git")); statErr != nil {
			return nil
		}
		relative, relErr := filepath.Rel(corpus, path)
		if relErr != nil || relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		out.dirs = append(out.dirs, relative)
		out.names[relative] = filepath.Base(relative)
		// A repository inside a repository would be indexed twice and its files
		// attributed to whichever matched first, so the walk stops here.
		return fs.SkipDir
	})
	if err != nil {
		return repositories{}, fmt.Errorf("walk %s: %w", corpus, err)
	}
	// Longest first: `services/go-svc-a` must win over `services` when a path
	// is canonicalised.
	sort.Slice(out.dirs, func(i, j int) bool { return len(out.dirs[i]) > len(out.dirs[j]) })
	if len(out.dirs) == 0 {
		return repositories{}, fmt.Errorf("no git repository under %s", corpus)
	}
	return out, nil
}

// canonical accepts any of the spellings the arms produce and returns
// `repositoryName:pathInsideIt`.
func (r repositories) canonical(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	for _, prefix := range []string{r.root + "/", filepath.ToSlash(r.root) + "/"} {
		path = strings.TrimPrefix(path, prefix)
	}
	// A private copy of the corpus answers with its own root.
	if index := strings.Index(path, "/workspace-copy/"); index >= 0 {
		path = path[index+len("/workspace-copy/"):]
	}
	for _, dir := range r.dirs {
		if path == dir {
			return r.names[dir] + ":"
		}
		if strings.HasPrefix(path, dir+"/") {
			return r.names[dir] + ":" + strings.TrimPrefix(path, dir+"/")
		}
	}
	// A repository-prefixed spelling: `library-shared/src/utils/retry.ts`.
	if name, rest, found := strings.Cut(path, "/"); found {
		for _, registered := range r.names {
			if registered == name {
				return name + ":" + rest
			}
		}
	}
	return ":" + path
}

func (r repositories) canonicalAll(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, r.canonical(path))
	}
	return out
}

// ---------- MCP arms ----------

// server is one measured MCP server over stdio.
type server struct {
	name     string
	session  *sdkmcp.ClientSession
	stderr   *bytes.Buffer
	close    func()
	captures map[string]string
}

// dial starts a server and completes the MCP handshake. environment entries are
// applied on top of the harness's own, which is how each arm's isolated state
// travels without mutating the process environment: kivgraph reads HOME for its
// configuration and its published generation, and code-review-graph keeps a
// registry there too.
func dial(ctx context.Context, name, command string, arguments []string, environment map[string]string) (*server, error) {
	serverCommand := exec.Command(command, arguments...)
	serverCommand.Env = os.Environ()
	for key, value := range environment {
		serverCommand.Env = append(serverCommand.Env, key+"="+value)
	}
	stderr := &bytes.Buffer{}
	serverCommand.Stderr = stderr
	transport := &sdkmcp.CommandTransport{Command: serverCommand, TerminateDuration: 5 * time.Second}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: benchmarkName, Version: "1.0.0"}, nil)
	callContext, cancel := context.WithCancel(ctx)
	session, err := client.Connect(callContext, transport, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect to %s: %w (stderr: %s)", name, err, strings.TrimSpace(stderr.String()))
	}
	return &server{
		name: name, session: session, stderr: stderr, captures: map[string]string{},
		close: func() { _ = session.Close(); cancel() },
	}, nil
}

// call prices one MCP tool call. An error result is returned as an observation
// rather than an error: that is exactly the case this benchmark exists to count.
func (s *server) call(
	ctx context.Context, tokens *counter, capture, tool string, arguments map[string]any,
) observation {
	callContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	started := time.Now()
	response, err := s.session.CallTool(callContext, &sdkmcp.CallToolParams{Name: tool, Arguments: arguments})
	elapsed := float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		return observation{Tool: tool, MS: elapsed, Failed: true, Error: err.Error()}
	}
	text := firstText(response)
	s.captures[capture] = text
	out := observation{Tool: tool, Tokens: tokens.count(text), MS: elapsed, Text: text, Banner: bannerTokens(tokens, text)}
	if response.IsError {
		out.Failed = true
		out.Error = strings.TrimSpace(text)
	}
	return out
}

func firstText(response *sdkmcp.CallToolResult) string {
	parts := make([]string, 0, len(response.Content))
	for _, content := range response.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// bannerTokens prices the prologue a response spends on the model rather than
// on the answer. graft opens every payload with a saving estimate and an
// instruction to repeat it; others do not, and then this is zero.
func bannerTokens(tokens *counter, text string) int {
	line, _, found := strings.Cut(text, "\n")
	if !found || !strings.Contains(line, "tokens saved") {
		return 0
	}
	return tokens.count(line)
}

// ---------- CLI arms ----------

// runCLI prices one command-line answer. Output is captured whole -- stdout and
// stderr together, because a tool that explains itself on stderr still spends
// the tokens -- and a non-zero exit is an observation, not an abort.
func runCLI(
	ctx context.Context, tokens *counter, captures map[string]string,
	capture, workingDir string, environment map[string]string, command string, arguments ...string,
) observation {
	callContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	cmd := exec.CommandContext(callContext, command, arguments...)
	cmd.Dir = workingDir
	cmd.Env = os.Environ()
	for key, value := range environment {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	started := time.Now()
	output, err := cmd.CombinedOutput()
	elapsed := float64(time.Since(started).Microseconds()) / 1000
	text := string(output)
	captures[capture] = text
	out := observation{
		Tool:   filepath.Base(command) + " " + firstArgument(arguments),
		Tokens: tokens.count(text), MS: elapsed, Text: text,
	}
	if err != nil {
		out.Failed = true
		out.Error = strings.TrimSpace(err.Error())
	}
	return out
}

func firstArgument(arguments []string) string {
	if len(arguments) == 0 {
		return ""
	}
	return arguments[0]
}
