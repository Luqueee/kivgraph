package rustloader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/executable"
	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
)

// RunErrorKind classifies a failure of the external indexer. Every kind maps
// to one unresolved reason, so a repository that produced no facts always says
// why.
type RunErrorKind string

const (
	// RunErrorAnalyzerUnavailable means the configured command is not on the
	// PATH.
	RunErrorAnalyzerUnavailable RunErrorKind = "ANALYZER_UNAVAILABLE"
	// RunErrorWorkspaceNotLoaded means the analyzer refused the workspace.
	RunErrorWorkspaceNotLoaded RunErrorKind = "WORKSPACE_NOT_LOADED"
	// RunErrorIndexUnreadable means the index could not be read or decoded.
	RunErrorIndexUnreadable RunErrorKind = "INDEX_UNREADABLE"
	// RunErrorAnalyzerUnsupported means the analyzer produced an index this
	// build cannot turn into facts.
	RunErrorAnalyzerUnsupported RunErrorKind = "ANALYZER_UNSUPPORTED"
	// RunErrorRepositoryWritten means the run modified the sources it was
	// asked to read.
	RunErrorRepositoryWritten RunErrorKind = "REPOSITORY_WRITTEN"
	// RunErrorCanceled means the caller cancelled the run.
	RunErrorCanceled RunErrorKind = "CANCELED"
	// RunErrorInvalidOptions means the caller asked for something this
	// package refuses to do.
	RunErrorInvalidOptions RunErrorKind = "INVALID_OPTIONS"
)

// RunError is a classified failure of one analyzer invocation.
type RunError struct {
	Kind      RunErrorKind
	Workspace string
	Detail    string
	Err       error
}

func (err *RunError) Error() string {
	message := fmt.Sprintf("rust-analyzer %s: workspace %q", err.Kind, err.Workspace)
	if err.Detail != "" {
		message += ": " + err.Detail
	}
	if err.Err != nil {
		message += ": " + err.Err.Error()
	}
	return message
}

func (err *RunError) Unwrap() error {
	return err.Err
}

func newRunError(kind RunErrorKind, workspace, detail string, cause error) error {
	return &RunError{Kind: kind, Workspace: workspace, Detail: detail, Err: cause}
}

// AnalyzerSource says where the analyzer this build will run came from.
type AnalyzerSource string

const (
	// AnalyzerBundled is the binary that travels beside the executable.
	AnalyzerBundled AnalyzerSource = "bundled"
	// AnalyzerPath is a binary found through the PATH.
	AnalyzerPath AnalyzerSource = "path"
	// AnalyzerExplicit is a path the configuration spelled out.
	AnalyzerExplicit AnalyzerSource = "explicit"
)

// ResolveAnalyzer answers the analyzer to run and where it came from.
//
// The bundled binary wins over the PATH on purpose: an installation that ships
// its own engine must use it, or two machines with the same bundle would index
// the same repository with different analyzers. A configuration that spells a
// path out is always honoured.
func ResolveAnalyzer(command string) (string, AnalyzerSource, error) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return "", "", errors.New("the analyzer command is empty")
	}
	if strings.ContainsRune(trimmed, os.PathSeparator) {
		resolved, err := exec.LookPath(trimmed)
		if err != nil {
			return "", "", err
		}
		return resolved, AnalyzerExplicit, nil
	}
	if selfPath, err := os.Executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(selfPath), executable.Name(trimmed))
		if info, statErr := os.Stat(sibling); statErr == nil && executable.IsProgram(info) {
			return sibling, AnalyzerBundled, nil
		}
	}
	resolved, err := exec.LookPath(trimmed)
	if err != nil {
		return "", "", err
	}
	return resolved, AnalyzerPath, nil
}

// RunOptions configures one invocation over one Cargo workspace.
type RunOptions struct {
	// Workspace is the absolute path of the Cargo workspace root.
	Workspace string
	// OutputDirectory holds the generated configuration and the index. It
	// must live outside every indexed repository.
	OutputDirectory string
	// AnalyzerCommand is the command line of the analyzer, arguments
	// included.
	AnalyzerCommand string
	// TargetDirectory is where cargo writes the artifacts of this analysis.
	// rust-analyzer always runs build scripts, so a run without a target
	// directory outside the repository would write inside it.
	TargetDirectory string

	Features          []string
	AllFeatures       bool
	NoDefaultFeatures bool
	Cfgs              []string
	BuildScripts      bool
	ProcMacros        bool
	// IncludeTests sets `cfg(test)` for the crates of the workspace.
	IncludeTests bool
	AllowNetwork bool
	Sysroot      string
	// Threads bounds the analyzer's own cache priming. Zero lets it decide.
	Threads int
}

// RunResult is one decoded index and what the run reported about itself.
type RunResult struct {
	Index scipwire.Index
	// ToolVersion is the analyzer that produced the index, as it named
	// itself. It belongs to the identity of any cached facts.
	ToolVersion string
	// Diagnostics are the analyzer's own warnings and errors, kept because a
	// degraded load is the usual reason a crate is missing from the graph.
	Diagnostics []string
	Duration    time.Duration
}

// Run indexes one Cargo workspace and answers the decoded index.
//
// The analyzer runs build scripts whether or not the caller wants them -- the
// scip command hardcodes it -- so hermeticity is imposed from the outside: the
// target directory is redirected out of the repository, cargo runs offline and
// locked unless the caller allows the network, and the run refuses to succeed
// if the workspace was written to anyway.
func Run(ctx context.Context, options RunOptions) (RunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	workspace := strings.TrimSpace(options.Workspace)
	if err := validateRunOptions(options); err != nil {
		return RunResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RunResult{}, newRunError(RunErrorCanceled, workspace, "", err)
	}

	command := strings.Fields(strings.TrimSpace(options.AnalyzerCommand))
	resolved, _, err := ResolveAnalyzer(command[0])
	if err != nil {
		return RunResult{}, newRunError(RunErrorAnalyzerUnavailable, workspace,
			fmt.Sprintf("command %q is not executable", command[0]), err)
	}
	command[0] = resolved

	configPath := filepath.Join(options.OutputDirectory, "rust-analyzer.json")
	if err := writeAnalyzerConfig(configPath, options); err != nil {
		return RunResult{}, newRunError(RunErrorInvalidOptions, workspace, "write analyzer configuration", err)
	}
	indexPath := filepath.Join(options.OutputDirectory, "index.scip")

	arguments := append(append([]string(nil), command[1:]...),
		"scip", workspace,
		"--output", indexPath,
		"--config-path", configPath,
		"--exclude-vendored-libraries",
	)
	if options.Threads > 0 {
		arguments = append(arguments, "--num-threads", strconv.Itoa(options.Threads))
	}

	before, err := writeGuardSnapshot(workspace)
	if err != nil {
		return RunResult{}, newRunError(RunErrorInvalidOptions, workspace, "inspect workspace", err)
	}

	started := time.Now()
	process := exec.CommandContext(ctx, command[0], arguments...)
	// The analyzer resolves its path argument against the working directory,
	// which is already absolute here. Running from the output directory keeps
	// a relative default output out of the sources.
	process.Dir = options.OutputDirectory
	var stderr bytes.Buffer
	process.Stdout = &stderr
	process.Stderr = &stderr
	runErr := process.Run()
	duration := time.Since(started)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return RunResult{}, newRunError(RunErrorCanceled, workspace, "", ctxErr)
	}
	if runErr != nil {
		return RunResult{}, newRunError(RunErrorWorkspaceNotLoaded, workspace,
			lastDiagnostics(stderr.String()), runErr)
	}
	if written, err := writeGuardViolation(workspace, before); err != nil {
		return RunResult{}, newRunError(RunErrorInvalidOptions, workspace, "inspect workspace", err)
	} else if written != "" {
		return RunResult{}, newRunError(RunErrorRepositoryWritten, workspace,
			fmt.Sprintf("the analysis wrote %q inside the repository", written), nil)
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return RunResult{}, newRunError(RunErrorIndexUnreadable, workspace, "read index", err)
	}
	index, err := scipwire.Decode(data)
	if err != nil {
		return RunResult{}, newRunError(RunErrorIndexUnreadable, workspace, "decode index", err)
	}
	if err := validateIndex(index); err != nil {
		return RunResult{}, newRunError(RunErrorAnalyzerUnsupported, workspace, err.Error(), nil)
	}
	return RunResult{
		Index:       index,
		ToolVersion: index.ToolVersion,
		Diagnostics: classifyDiagnostics(stderr.String()),
		Duration:    duration,
	}, nil
}

func validateRunOptions(options RunOptions) error {
	workspace := strings.TrimSpace(options.Workspace)
	if workspace == "" || !filepath.IsAbs(workspace) {
		return newRunError(RunErrorInvalidOptions, workspace, "workspace path must be absolute", nil)
	}
	output := strings.TrimSpace(options.OutputDirectory)
	if output == "" || !filepath.IsAbs(output) {
		return newRunError(RunErrorInvalidOptions, workspace, "output directory must be absolute", nil)
	}
	if pathInside(workspace, output) {
		return newRunError(RunErrorInvalidOptions, workspace,
			fmt.Sprintf("output directory %q is inside the repository", output), nil)
	}
	target := strings.TrimSpace(options.TargetDirectory)
	if target == "" || !filepath.IsAbs(target) {
		return newRunError(RunErrorInvalidOptions, workspace, "target directory must be absolute", nil)
	}
	if pathInside(workspace, target) {
		return newRunError(RunErrorInvalidOptions, workspace,
			fmt.Sprintf("target directory %q is inside the repository", target), nil)
	}
	if len(strings.Fields(strings.TrimSpace(options.AnalyzerCommand))) == 0 {
		return newRunError(RunErrorInvalidOptions, workspace, "analyzer command is empty", nil)
	}
	if options.AllFeatures && len(options.Features) != 0 {
		return newRunError(RunErrorInvalidOptions, workspace, "all features and a feature list are exclusive", nil)
	}
	return nil
}

func pathInside(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// analyzerConfig is the client configuration rust-analyzer reads through
// --config-path. The keys are the ones its manual documents, without the
// `rust-analyzer.` prefix a client would use.
type analyzerConfig struct {
	Cargo     analyzerCargoConfig `json:"cargo"`
	ProcMacro analyzerToggle      `json:"procMacro"`
	Cfg       analyzerCfgConfig   `json:"cfg"`
}

type analyzerCfgConfig struct {
	SetTest bool `json:"setTest"`
}

type analyzerCargoConfig struct {
	ExtraEnv          map[string]string `json:"extraEnv"`
	ExtraArgs         []string          `json:"extraArgs"`
	Features          any               `json:"features,omitempty"`
	NoDefaultFeatures bool              `json:"noDefaultFeatures"`
	Cfgs              []string          `json:"cfgs,omitempty"`
	BuildScripts      analyzerToggle    `json:"buildScripts"`
	Sysroot           *string           `json:"sysroot"`
}

type analyzerToggle struct {
	Enable bool `json:"enable"`
}

func writeAnalyzerConfig(path string, options RunOptions) error {
	environment := map[string]string{"CARGO_TARGET_DIR": options.TargetDirectory}
	arguments := []string{"--locked"}
	if !options.AllowNetwork {
		environment["CARGO_NET_OFFLINE"] = "true"
		arguments = append([]string{"--offline"}, arguments...)
	}

	configuration := analyzerConfig{
		Cargo: analyzerCargoConfig{
			ExtraEnv:          environment,
			ExtraArgs:         arguments,
			NoDefaultFeatures: options.NoDefaultFeatures,
			Cfgs:              append([]string(nil), options.Cfgs...),
			BuildScripts:      analyzerToggle{Enable: options.BuildScripts},
			Sysroot:           sysrootValue(options.Sysroot),
		},
		ProcMacro: analyzerToggle{Enable: options.ProcMacros},
		Cfg:       analyzerCfgConfig{SetTest: options.IncludeTests},
	}
	switch {
	case options.AllFeatures:
		configuration.Cargo.Features = "all"
	case len(options.Features) != 0:
		configuration.Cargo.Features = options.Features
	}

	data, err := json.Marshal(configuration)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// sysrootValue answers the JSON value for the sysroot key. `none` is spelled
// as null, which is how the analyzer is told not to load one at all.
func sysrootValue(sysroot string) *string {
	value := strings.TrimSpace(sysroot)
	if value == "" || strings.EqualFold(value, "none") {
		return nil
	}
	return &value
}

// validateIndex refuses an index this build cannot place in a file or
// attribute to a declaration.
func validateIndex(index scipwire.Index) error {
	if index.TextEncoding != scipwire.EncodingUTF8 && index.TextEncoding != scipwire.EncodingUnspecified {
		return fmt.Errorf("index is encoded as %d, and positions are only read as UTF-8", index.TextEncoding)
	}
	definitions := 0
	bodies := 0
	for _, document := range index.Documents {
		if document.PositionEncoding != scipwire.PositionUTF8 && document.PositionEncoding != scipwire.PositionUnspecified {
			return fmt.Errorf("document %q uses position encoding %d, and positions are only read as UTF-8",
				document.RelativePath, document.PositionEncoding)
		}
		for _, occurrence := range document.Occurrences {
			if !occurrence.Definition() {
				continue
			}
			definitions++
			if occurrence.EnclosingRange.Present {
				bodies++
			}
		}
	}
	if definitions > 0 && bodies == 0 {
		// Without a body range there is nothing to attribute a reference to,
		// so every edge would silently lose its source.
		return errors.New("no definition carries an enclosing range: this analyzer is too old for Kivgraph")
	}
	return nil
}

// writeGuardSnapshot records the two artefacts a cargo invocation would create
// inside a workspace. Everything else it writes goes to the target directory,
// which is redirected out of the repository.
type writeGuard struct {
	lockExists   bool
	lockSize     int64
	lockModified time.Time
	targetExists bool
}

func writeGuardSnapshot(workspace string) (writeGuard, error) {
	guard := writeGuard{}
	info, err := os.Stat(filepath.Join(workspace, "Cargo.lock"))
	switch {
	case err == nil:
		guard.lockExists = true
		guard.lockSize = info.Size()
		guard.lockModified = info.ModTime()
	case !errors.Is(err, os.ErrNotExist):
		return writeGuard{}, err
	}
	if _, err := os.Stat(filepath.Join(workspace, "target")); err == nil {
		guard.targetExists = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return writeGuard{}, err
	}
	return guard, nil
}

// writeGuardViolation answers the path the run created or changed, empty when
// the workspace is untouched.
func writeGuardViolation(workspace string, before writeGuard) (string, error) {
	after, err := writeGuardSnapshot(workspace)
	if err != nil {
		return "", err
	}
	if after.targetExists && !before.targetExists {
		return filepath.Join(workspace, "target"), nil
	}
	if after.lockExists && !before.lockExists {
		return filepath.Join(workspace, "Cargo.lock"), nil
	}
	if after.lockExists && before.lockExists {
		if after.lockSize != before.lockSize || !after.lockModified.Equal(before.lockModified) {
			return filepath.Join(workspace, "Cargo.lock"), nil
		}
	}
	return "", nil
}

// classifyDiagnostics keeps the analyzer's warnings and errors and drops its
// progress log and panic-style stack frames, which say nothing about the code
// being indexed.
func classifyDiagnostics(output string) []string {
	diagnostics := make([]string, 0, 4)
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "rust-analyzer: Loading") || strings.HasPrefix(trimmed, "Generating SCIP") {
			continue
		}
		if isStackFrame(trimmed) {
			continue
		}
		// The analyzer reports duplicate symbols as a plain block with no
		// level prefix. Dropping it would hide the one thing that makes a
		// lookup ambiguous.
		duplicate := strings.HasPrefix(trimmed, "Duplicate symbol:") ||
			strings.HasPrefix(trimmed, "Encountered duplicate scip symbols")
		if !duplicate && !strings.Contains(trimmed, "WARN") && !strings.Contains(trimmed, "ERROR") {
			continue
		}
		if strings.Contains(trimmed, "Config Error(s) error_sink=ConfigErrors([])") {
			// The analyzer logs an empty error list on every start.
			continue
		}
		diagnostics = append(diagnostics, trimmed)
	}
	return diagnostics
}

func isStackFrame(line string) bool {
	index := strings.Index(line, ":")
	if index <= 0 {
		return false
	}
	if _, err := strconv.Atoi(line[:index]); err != nil {
		return false
	}
	return true
}

// lastDiagnostics keeps the tail of an aborted run, which is where the reason
// is.
func lastDiagnostics(output string) string {
	lines := make([]string, 0, 8)
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isStackFrame(trimmed) {
			continue
		}
		lines = append(lines, trimmed)
	}
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	return strings.Join(lines, "; ")
}
