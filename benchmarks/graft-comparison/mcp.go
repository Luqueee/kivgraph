package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pkoukk/tiktoken-go"
	tiktokenloader "github.com/pkoukk/tiktoken-go-loader"
)

// encodingName is the tokenizer every number in this benchmark is counted with.
// It is a proxy for the Claude tokenizer, so the ratios between columns are the
// claim and the absolute values are not.
const encodingName = "o200k_base"

const callTimeout = 120 * time.Second

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

// server is one measured MCP server over stdio.
type server struct {
	name    string
	session *sdkmcp.ClientSession
	stderr  *bytes.Buffer
	close   func()
	// captures holds every raw response this run observed, so a parsing claim
	// in the report can be checked against the bytes it was made from.
	captures map[string]string
}

// dial starts a server and completes the MCP handshake. environment entries are
// applied on top of the harness's own: kivgraph resolves its configuration and
// its published generation from HOME, so the isolated state travels here and
// never through a mutated process environment.
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

// observation is one tool call: what it cost and what it said.
type observation struct {
	Tool   string  `json:"tool"`
	Tokens int     `json:"tokens"`
	MS     float64 `json:"ms"`
	// Text is not serialised into results.json -- it is written to raw/ instead,
	// where a reader can diff it -- but it travels here so the scorers read the
	// same bytes that were priced.
	Text string `json:"-"`
	// Failed records a tool that answered with an error result. A failure is a
	// measurement, not a reason to abort: what it costs an agent to be told
	// "no" belongs in the total.
	Failed bool   `json:"failed,omitempty"`
	Error  string `json:"error,omitempty"`
	// Banner is what the response spends on text addressed to the model rather
	// than on the answer. graft prefixes every payload with an estimate of the
	// tokens it saved and an instruction to repeat that figure to the user; an
	// agent is billed for it on every call, so it is counted separately instead
	// of being folded into the answer or quietly excluded from it.
	Banner int `json:"banner_tokens,omitempty"`
}

// call prices one tool call. An error result is returned as an observation
// rather than an error: the tokens were spent and the agent still has no answer,
// which is exactly the case this benchmark exists to count.
func (s *server) call(ctx context.Context, tokens *counter, capture, tool string, arguments map[string]any) observation {
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	started := time.Now()
	response, err := s.session.CallTool(callCtx, &sdkmcp.CallToolParams{Name: tool, Arguments: arguments})
	elapsed := float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		return observation{Tool: tool, MS: elapsed, Failed: true, Error: err.Error()}
	}
	text := firstText(response)
	if response.StructuredContent != nil && text == "" {
		if encoded, marshalErr := json.Marshal(response.StructuredContent); marshalErr == nil {
			text = string(encoded)
		}
	}
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

// surface is what a session pays before it asks anything: one route line and one
// description per tool, plus the server's instructions. Neither Oh My Pi nor
// Claude Code keeps the JSON schemas resident, so the schema total is reported
// beside the resident number instead of inside it.
type surface struct {
	Tools           int      `json:"tools"`
	Names           []string `json:"names"`
	DescriptionToks int      `json:"description_tokens"`
	InstructionToks int      `json:"instruction_tokens"`
	SchemaToks      int      `json:"schema_tokens"`
	Resident        int      `json:"resident_tokens"`
}

func (s *server) measureSurface(ctx context.Context, tokens *counter) (surface, error) {
	listed, err := s.session.ListTools(ctx, nil)
	if err != nil {
		return surface{}, fmt.Errorf("list %s tools: %w", s.name, err)
	}
	out := surface{Tools: len(listed.Tools)}
	for _, tool := range listed.Tools {
		out.Names = append(out.Names, tool.Name)
		out.DescriptionToks += tokens.count(tool.Name) + tokens.count(tool.Description)
		if tool.InputSchema != nil {
			if encoded, marshalErr := json.Marshal(tool.InputSchema); marshalErr == nil {
				out.SchemaToks += tokens.count(string(encoded))
			}
		}
	}
	if result := s.session.InitializeResult(); result != nil {
		out.InstructionToks = tokens.count(result.Instructions)
	}
	out.Resident = out.DescriptionToks + out.InstructionToks
	if encoded, err := json.Marshal(listed.Tools); err == nil {
		s.captures["tools"] = string(encoded)
	}
	return out, nil
}

// bannerTokens prices the prologue a response spends on the model rather than on
// the answer. graft opens every payload with a saving estimate and an
// instruction to repeat it to the user; the tokens are billed either way, so
// they are measured rather than argued about.
func bannerTokens(tokens *counter, text string) int {
	if !strings.HasPrefix(text, "[graft]") {
		return 0
	}
	line := text
	if end := strings.Index(text, "\n"); end >= 0 {
		line = text[:end]
	}
	return tokens.count(line)
}
