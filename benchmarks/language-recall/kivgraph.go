package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const callTimeout = 2 * time.Minute

// indexEnvironment moves the state of Kivgraph without moving the toolchains it
// drives: HOME is the only thing a rustup or a Go module cache would notice.
func indexEnvironment(home string) []string {
	environment := append(toolEnvironment(), "HOME="+home)
	if real, err := os.UserHomeDir(); err == nil {
		for name, directory := range map[string]string{"GOMODCACHE": "go/pkg/mod", "GOPATH": "go", "RUSTUP_HOME": ".rustup", "CARGO_HOME": ".cargo"} {
			if _, set := os.LookupEnv(name); !set {
				environment = append(environment, name+"="+real+"/"+directory)
			}
		}
	}
	return environment
}

// index registers the corpus and builds the graph with the real binary. It runs
// from a Kivgraph checkout because the TypeScript worker is started from there.
func index(ctx context.Context, binary, root, home string, repositories []corpusRepository, excludeTests string) error {
	var languages []string
	seen := map[string]bool{}
	arguments := []string{"init"}
	for _, repository := range repositories {
		if !seen[repository.Language] {
			seen[repository.Language] = true
			languages = append(languages, repository.Language)
		}
		arguments = append(arguments, "--repository", repository.Name+"="+repository.Path)
	}
	arguments = append(arguments, "--languages", strings.Join(languages, ","))
	for _, step := range [][]string{arguments, {"index", "--full"}} {
		if step[0] == "index" && excludeTests != "" {
			if err := disableTests(home, excludeTests); err != nil {
				return err
			}
		}
		process := exec.CommandContext(ctx, binary, step...)
		process.Dir = root
		process.Env = indexEnvironment(home)
		if output, err := process.CombinedOutput(); err != nil {
			return fmt.Errorf("kivgraph %s: %w: %s", step[0], err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

// disableTests sets `<section>.include_tests: false` in the configuration `init`
// wrote. Both languages read their tests by default, so this is the arm that
// shows what reading them buys. The key is the first `include_tests` after the
// section, which the check below confirms rather than assumes.
func disableTests(home, section string) error {
	path := filepath.Join(home, ".config", "kivgraph", "config.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	start := strings.Index(string(content), "\n"+section+":\n")
	if start < 0 {
		return fmt.Errorf("%s has no %s section", path, section)
	}
	const on, off = "    include_tests: true\n", "    include_tests: false\n"
	offset := strings.Index(string(content[start:]), on)
	if offset < 0 {
		return fmt.Errorf("%s does not declare %s.include_tests as true", path, section)
	}
	offset += start
	updated := string(content[:offset]) + off + string(content[offset+len(on):])
	return os.WriteFile(path, []byte(updated), 0o644)
}

func connect(ctx context.Context, binary, home string) (*sdkmcp.ClientSession, func(), error) {
	process := exec.Command(binary, "serve")
	process.Env = indexEnvironment(home)
	transport := &sdkmcp.CommandTransport{Command: process, TerminateDuration: 5 * time.Second}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: benchmarkName, Version: "1.0.0"}, nil)
	callContext, cancel := context.WithCancel(ctx)
	session, err := client.Connect(callContext, transport, nil)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("connect to the server: %w", err)
	}
	return session, func() { _ = session.Close(); cancel() }, nil
}

// call runs a tool and decodes its answer into out. A refusal is an error: this
// harness never asks a question it expects to be refused.
func call(ctx context.Context, session *sdkmcp.ClientSession, tool string, arguments map[string]any, out any) error {
	callContext, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	response, err := session.CallTool(callContext, &sdkmcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		return err
	}
	var text string
	for _, content := range response.Content {
		if block, ok := content.(*sdkmcp.TextContent); ok {
			text = block.Text
			break
		}
	}
	if response.IsError {
		return fmt.Errorf("%s refused: %s", tool, strings.TrimSpace(text))
	}
	return json.Unmarshal([]byte(text), out)
}

type declaration struct {
	Repository string
	Path       string
	Name       string
	Kind       string
	Line       int
	StableKey  string
}

type outlineAnswer struct {
	Truncated  bool   `json:"truncated"`
	NextCursor string `json:"next_cursor"`
	Results    struct {
		Files []struct {
			Path    string `json:"path"`
			Symbols []struct {
				Name      string `json:"name"`
				Kind      string `json:"kind"`
				StartLine int    `json:"start_line"`
				StableKey string `json:"stable_key"`
			} `json:"symbols"`
		} `json:"files"`
	} `json:"results"`
}

// declarations lists what Kivgraph says a file declares. It is the sampling
// frame, which is the one place this harness takes a fact from the thing it
// measures; the report says what that costs.
func declarations(ctx context.Context, session *sdkmcp.ClientSession, repository, path string) ([]declaration, error) {
	var found []declaration
	cursor := ""
	for {
		arguments := map[string]any{
			"repository": repository, "path": path, "view": "full", "include_members": true,
			"response_format": "detailed", "limit": 500,
		}
		if cursor != "" {
			arguments["cursor"] = cursor
		}
		var answer outlineAnswer
		if err := call(ctx, session, "get_file_outline", arguments, &answer); err != nil {
			return nil, err
		}
		for _, file := range answer.Results.Files {
			for _, symbol := range file.Symbols {
				found = append(found, declaration{
					Repository: repository, Path: file.Path, Name: symbol.Name, Kind: symbol.Kind,
					Line: symbol.StartLine, StableKey: symbol.StableKey,
				})
			}
		}
		if !answer.Truncated || answer.NextCursor == "" {
			return found, nil
		}
		cursor = answer.NextCursor
	}
}

type referencesAnswer struct {
	NextCursor   string `json:"next_cursor"`
	Completeness *struct {
		Verdict string `json:"verdict"`
	} `json:"completeness"`
	Results struct {
		Subject struct {
			Path string `json:"file_path"`
			Line int    `json:"start_line"`
		} `json:"subject"`
		References []struct {
			Path string `json:"file_path"`
			Line int    `json:"start_line"`
			Edge string `json:"edge_kind"`
		} `json:"references"`
	} `json:"results"`
}

// referencingFiles asks which files reference the declaration and returns them
// with the completeness verdict of the answer. Every edge kind is requested and
// two are then set aside, for the same reason the oracle sets aside the errors
// they correspond to:
//
//   - IMPLEMENTS and OVERRIDES are relations, which `find_implementations`
//     answers. A structural match makes a class "implement" an interface it
//     never names, and no use of the interface is in that file.
//   - An EXPORTS edge from the declaring file, on the line of the declaration,
//     is the declaration's own export binding (`export const x = ...`). An
//     export written on another line (`export default x;`) is kept, like
//     another file exporting the symbol: it is a use, and the compiler rejects
//     it when the name is gone.
func referencingFiles(ctx context.Context, session *sdkmcp.ClientSession, repository, stableKey string) (map[string]bool, string, error) {
	files := map[string]bool{}
	var verdict string
	cursor := ""
	for {
		arguments := map[string]any{
			"stable_key": stableKey, "direction": "incoming", "view": "full",
			"edge_kinds": []string{"*"}, "limit": 500,
		}
		if cursor != "" {
			arguments["cursor"] = cursor
		}
		var answer referencesAnswer
		if err := call(ctx, session, "find_references", arguments, &answer); err != nil {
			return nil, "", err
		}
		if answer.Completeness != nil {
			verdict = answer.Completeness.Verdict
		}
		for _, row := range answer.Results.References {
			if row.Edge == "IMPLEMENTS" || row.Edge == "OVERRIDES" {
				continue
			}
			if row.Edge == "EXPORTS" && row.Path == answer.Results.Subject.Path && row.Line == answer.Results.Subject.Line {
				continue
			}
			files[row.Path] = true
		}
		if answer.NextCursor == "" {
			return files, verdict, nil
		}
		cursor = answer.NextCursor
	}
}
