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

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const PayloadVersion = 1

// Run executes the configured Python indexer and decodes its versioned facts.
// When scip-python is not installed, the repository's deterministic AST
// worker is used as a development fallback; it intentionally reports dynamic
// cases as unresolved rather than guessing.
func Run(ctx context.Context, command, pythonPath string, repository workspace.Repository, workingDirectory string) (facts.SemanticPayload, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root := repository.RealPath
	if root == "" {
		root = repository.Path
	}
	args, executable, fallback, err := resolveCommand(command, pythonPath, workingDirectory)
	if err != nil {
		return facts.SemanticPayload{}, err
	}
	args = append(args, "--root", root)
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
		return facts.SemanticPayload{}, fmt.Errorf("Python facts version/language = %d/%q, want %d/%q", payload.Version, payload.Language, PayloadVersion, facts.LanguagePython)
	}
	payload.Authoritative = !fallback
	return payload, nil
}

func resolveCommand(command, pythonPath, workingDirectory string) ([]string, string, bool, error) {
	fields := strings.Fields(strings.TrimSpace(command))
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
		return nil, "", false, fmt.Errorf("Python executable %q is unavailable: %w", pythonPath, err)
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
		return nil, "", false, fmt.Errorf("Python fallback worker not found: %s", script)
	}
	return []string{script}, python, true, nil
}
