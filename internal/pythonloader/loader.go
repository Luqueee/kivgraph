// Package pythonloader turns the Python code of a registered repository into
// semantic facts, through Pyright where it is available and through a fallback
// worker where it is not.
package pythonloader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/toolchain"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const PayloadVersion = 1

// Options configures one Python facts producer. AnalyzerMode is `fallback`
// for the bundled AST worker or `exact` for a configured type-aware producer.
type Options struct {
	IndexerCommand   string
	AnalyzerCommand  string
	AnalyzerMode     string
	PythonPath       string
	IncludeTests     bool
	IncludeGenerated bool
	IncludeExternal  bool
}

// Run executes the configured Python indexer and decodes its versioned facts.
// When scip-python is not installed, the repository's deterministic AST
// worker is used as a development fallback; it intentionally reports dynamic
// cases as unresolved rather than guessing.
func Run(ctx context.Context, command, pythonPath string, repository workspace.Repository, workingDirectory string) (facts.SemanticPayload, error) {
	return RunWithOptions(ctx, Options{IndexerCommand: command, PythonPath: pythonPath}, repository, workingDirectory)
}

// RunWithOptions runs a type-aware producer when exact mode is requested and
// otherwise preserves the hermetic bundled fallback. External producers use
// the same JSON payload contract, so the normalizer remains the single source
// of durable identities and graph semantics.
func RunWithOptions(ctx context.Context, options Options, repository workspace.Repository, workingDirectory string) (facts.SemanticPayload, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root := repository.RealPath
	if root == "" {
		root = repository.Path
	}
	if strings.TrimSpace(workingDirectory) == "" {
		workingDirectory = "."
	}
	absoluteWorkingDirectory, err := filepath.Abs(workingDirectory)
	if err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("resolve Python working directory %q: %w", workingDirectory, err)
	}
	workingDirectory = absoluteWorkingDirectory
	command := options.IndexerCommand
	exact := strings.EqualFold(strings.TrimSpace(options.AnalyzerMode), "exact")
	if exact {
		command = options.AnalyzerCommand
	}
	args, executable, fallback, err := resolveCommand(command, options.PythonPath, workingDirectory)
	if err != nil {
		return facts.SemanticPayload{}, err
	}
	args = append(args, "--root", root)
	if options.IncludeTests {
		args = append(args, "--include-tests")
	}
	if options.IncludeGenerated {
		args = append(args, "--include-generated")
	}
	if options.IncludeExternal {
		args = append(args, "--include-external")
	}
	process := exec.CommandContext(ctx, executable, args...)
	process.Dir = workingDirectory
	output, err := process.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return facts.SemanticPayload{}, fmt.Errorf("python indexer %q failed: %w: %s", executable, err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return facts.SemanticPayload{}, fmt.Errorf("run python indexer %q: %w", executable, err)
	}
	var payload facts.SemanticPayload
	if err := json.Unmarshal(output, &payload); err != nil {
		return facts.SemanticPayload{}, fmt.Errorf("decode Python facts: %w", err)
	}
	if payload.Version != PayloadVersion || payload.Language != facts.LanguagePython {
		return facts.SemanticPayload{}, fmt.Errorf("unexpected Python facts version/language = %d/%q, want %d/%q", payload.Version, payload.Language, PayloadVersion, facts.LanguagePython)
	}
	payload.Authoritative = !fallback
	if fallback {
		payload.Analyzer = "python-ast-fallback"
	} else {
		payload.Analyzer = strings.TrimSpace(options.AnalyzerCommand)
	}
	return payload, nil
}

func resolveCommand(command, pythonPath, workingDirectory string) ([]string, string, bool, error) {
	fields, err := toolchain.SplitCommandLine(command)
	if err != nil {
		// Preserve the historical whitespace-based behavior for malformed or
		// unquoted commands. Generated managed-tool commands are quote-aware;
		// this fallback keeps an existing custom command from becoming a new
		// resolution error merely because its path contains an apostrophe.
		fields = strings.Fields(strings.TrimSpace(command))
	}
	if strings.EqualFold(strings.TrimSpace(command), "auto") || (len(fields) > 0 && strings.EqualFold(fields[0], config.DefaultPythonAnalyzerCommand)) {
		python, err := exec.LookPath(pythonPath)
		if err != nil {
			return nil, "", false, fmt.Errorf("unavailable Python executable %q: %w", pythonPath, err)
		}
		script := filepath.Join(workingDirectory, "python-worker", "pyright_index.py")
		if _, err := os.Stat(script); err != nil {
			_, source, _, _ := runtime.Caller(0)
			script = filepath.Join(filepath.Dir(source), "..", "..", "python-worker", "pyright_index.py")
		}
		if _, err := os.Stat(script); err != nil {
			if executable, executableErr := os.Executable(); executableErr == nil {
				script = filepath.Join(filepath.Dir(executable), "..", "worker", "python-worker", "pyright_index.py")
			}
		}
		if _, err := os.Stat(script); err != nil {
			return nil, "", false, fmt.Errorf("missing Python Pyright adapter: %s", script)
		}
		return append([]string{script}, fields[1:]...), python, false, nil
	}
	if len(fields) > 0 {
		if resolved, err := exec.LookPath(fields[0]); err == nil {
			return fields[1:], resolved, false, nil
		}
	}
	if strings.TrimSpace(pythonPath) == "" {
		pythonPath = "python3"
	}
	python, err := exec.LookPath(pythonPath)
	if err != nil {
		return nil, "", false, fmt.Errorf("unavailable Python executable %q: %w", pythonPath, err)
	}
	script := filepath.Join(workingDirectory, "python-worker", "index.py")
	if _, err := os.Stat(script); err != nil {
		_, source, _, _ := runtime.Caller(0)
		script = filepath.Join(filepath.Dir(source), "..", "..", "python-worker", "index.py")
	}
	if _, err := os.Stat(script); err != nil {
		if executable, executableErr := os.Executable(); executableErr == nil {
			script = filepath.Join(filepath.Dir(executable), "..", "worker", "python-worker", "index.py")
		}
	}
	if _, err := os.Stat(script); err != nil {
		return nil, "", false, fmt.Errorf("missing Python fallback worker: %s", script)
	}
	return []string{script}, python, true, nil
}

// ProducerFile is the file whose content decides the facts of one pass: the
// bundled script for the adapters that run one, or the resolved executable for
// an external producer. The fact cache fingerprints it, so a change to the
// producer forces a re-observation instead of reusing what the previous
// producer said.
//
// It resolves with the same rules as resolveCommand, on purpose: two resolution
// rules is how a cache ends up keyed on a file nobody runs. Editing the exact
// adapter used to leave the fingerprint untouched, and a rebuild republished
// facts the current code would not produce.
func ProducerFile(command, analyzerCommand, analyzerMode, pythonPath, workingDirectory string) string {
	effective := command
	if strings.EqualFold(strings.TrimSpace(analyzerMode), "exact") {
		effective = analyzerCommand
	}
	args, executable, _, err := resolveCommand(effective, pythonPath, workingDirectory)
	if err != nil {
		return ""
	}
	if len(args) > 0 && strings.HasSuffix(args[0], ".py") {
		return args[0]
	}
	return executable
}
