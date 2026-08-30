package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
)

const (
	benchmarkName = "build-system-cost"
	schemaName    = "build-system-cost-v1"
	target        = "//cmd/kivgraph:kivgraph"
	goTarget      = "./cmd/kivgraph"
	editFile      = "internal/version/version.go"
	armGo         = "go"
	armBazel      = "bazel"
	maxTrials     = 9
)

type config struct {
	Trials int
	Output string
}

type result struct {
	Benchmark   string        `json:"benchmark"`
	Schema      string        `json:"schema"`
	Command     string        `json:"command"`
	Commit      string        `json:"commit"`
	GeneratedAt time.Time     `json:"generated_at"`
	Environment environment   `json:"environment"`
	Corpus      corpus        `json:"corpus"`
	CachePolicy cachePolicy   `json:"cache_policy"`
	Trials      []trialResult `json:"trials"`
	Summary     summary       `json:"summary"`
	Limitations []string      `json:"limitations"`
}

type environment struct {
	Kind         string `json:"kind"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	CPUs         int    `json:"cpus"`
	RunnerImage  string `json:"runner_image,omitempty"`
	GoVersion    string `json:"go_version"`
	BazelVersion string `json:"bazel_version"`
	BazelCommand string `json:"bazel_command"`
}

type corpus struct {
	Repository       string `json:"repository"`
	Commit           string `json:"commit"`
	BazelTarget      string `json:"bazel_target"`
	GoTarget         string `json:"go_target"`
	EditedFile       string `json:"edited_file"`
	TrackedFiles     int    `json:"tracked_files"`
	TrackedBytes     int64  `json:"tracked_bytes"`
	GoModSHA256      string `json:"go_mod_sha256"`
	ModuleSHA256     string `json:"module_bazel_sha256"`
	ModuleLockSHA256 string `json:"module_lock_sha256"`
}

type cachePolicy struct {
	Scope        string `json:"scope"`
	InitialState string `json:"initial_state"`
	RemoteCache  bool   `json:"remote_cache"`
}

type trialResult struct {
	Index int         `json:"index"`
	Order []string    `json:"order"`
	Arms  []armResult `json:"arms"`
}

type armResult struct {
	Name     string     `json:"name"`
	Commands commands   `json:"commands"`
	Clean    phases     `json:"clean"`
	Warm     buildPhase `json:"warm"`
	Edit     editPhase  `json:"edit"`
}

type commands struct {
	Setup        string `json:"setup"`
	Dependencies string `json:"dependencies"`
	Build        string `json:"build"`
}

type phases struct {
	SetupSeconds      float64 `json:"setup_seconds"`
	DependencySeconds float64 `json:"dependency_seconds"`
	BuildSeconds      float64 `json:"build_seconds"`
	TotalSeconds      float64 `json:"total_seconds"`
}

type buildPhase struct {
	BuildSeconds float64 `json:"build_seconds"`
}

type editPhase struct {
	EditedFile   string  `json:"edited_file"`
	BuildSeconds float64 `json:"build_seconds"`
}

type summary struct {
	Go     armSummary `json:"go"`
	Bazel  armSummary `json:"bazel"`
	Ratios ratios     `json:"ratios"`
}

type armSummary struct {
	SetupSeconds      float64 `json:"median_setup_seconds"`
	DependencySeconds float64 `json:"median_dependency_seconds"`
	CleanBuildSeconds float64 `json:"median_clean_build_seconds"`
	CleanTotalSeconds float64 `json:"median_clean_total_seconds"`
	WarmBuildSeconds  float64 `json:"median_warm_build_seconds"`
	EditBuildSeconds  float64 `json:"median_edit_build_seconds"`
}

type ratios struct {
	GoOverBazelClean float64 `json:"go_over_bazel_clean_build"`
	GoOverBazelWarm  float64 `json:"go_over_bazel_warm_build"`
	GoOverBazelEdit  float64 `json:"go_over_bazel_edit_build"`
}

func main() {
	var cfg config
	flag.IntVar(&cfg.Trials, "trials", 3, "number of isolated trials (1-9)")
	flag.StringVar(&cfg.Output, "output", "", "directory for results.json, report.md, and logs")
	flag.Parse()
	if err := run(context.Background(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, "build-system-cost:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	if err := validateConfig(cfg); err != nil {
		return err
	}
	root, err := repositoryRoot(ctx)
	if err != nil {
		return err
	}
	commit, files, err := cleanTrackedFiles(ctx, root)
	if err != nil {
		return err
	}
	if err := requireCommands(); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.Output, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	metadata, err := collectMetadata(ctx, root, commit, files)
	if err != nil {
		return err
	}
	trials := make([]trialResult, 0, cfg.Trials)
	for index := 1; index <= cfg.Trials; index++ {
		trial, err := measureTrial(ctx, root, cfg.Output, files, index)
		if err != nil {
			return fmt.Errorf("trial %d: %w", index, err)
		}
		trials = append(trials, trial)
	}
	summary, err := summarize(trials)
	if err != nil {
		return err
	}
	output := result{
		Benchmark:   benchmarkName,
		Schema:      schemaName,
		Command:     fmt.Sprintf("go run ./benchmarks/%s --trials %d --output %s", benchmarkName, cfg.Trials, cfg.Output),
		Commit:      commit,
		GeneratedAt: time.Now().UTC(),
		Environment: metadata.Environment,
		Corpus:      metadata.Corpus,
		CachePolicy: cachePolicy{Scope: "private", InitialState: "empty for every arm and trial", RemoteCache: false},
		Trials:      trials,
		Summary:     summary,
		Limitations: []string{
			"GitHub Actions setup-go time is outside the harness; Bazel launcher bootstrap is measured inside the setup phase.",
			"Bazel fetch combines the Go SDK and external dependency downloads because Bazel does not expose a stable boundary between them here.",
			"Shared developer machines and hosted runners have uncontrolled background load; medians are evidence for this corpus, not performance guarantees.",
			"The comparison covers the ordinary Go binary without the LadybugDB native tag, pnpm projects, packaging, or release bundles.",
		},
	}
	if err := writeOutputs(cfg.Output, output); err != nil {
		return err
	}
	fmt.Printf("BUILD_SYSTEM_COST_RECORDED results=%s\n", filepath.Join(cfg.Output, "results.json"))
	return nil
}

func validateConfig(cfg config) error {
	if cfg.Trials < 1 || cfg.Trials > maxTrials {
		return fmt.Errorf("trials must be between 1 and %d, got %d", maxTrials, cfg.Trials)
	}
	if strings.TrimSpace(cfg.Output) == "" {
		return errors.New("output directory is required")
	}
	return nil
}

type metadata struct {
	Environment environment
	Corpus      corpus
}

func collectMetadata(ctx context.Context, root, commit string, files []string) (metadata, error) {
	goVersion, err := commandOutput(ctx, root, nil, "go", "version")
	if err != nil {
		return metadata{}, fmt.Errorf("read Go version: %w", err)
	}
	bazelVersionBytes, err := os.ReadFile(filepath.Join(root, ".bazelversion"))
	if err != nil {
		return metadata{}, fmt.Errorf("read .bazelversion: %w", err)
	}
	launcher, err := exec.LookPath("bazel")
	if err != nil {
		return metadata{}, fmt.Errorf("find bazel launcher: %w", err)
	}
	var trackedBytes int64
	for _, relative := range files {
		path := filepath.Join(root, relative)
		info, err := os.Lstat(path)
		if err != nil {
			return metadata{}, fmt.Errorf("inspect tracked file %s: %w", relative, err)
		}
		switch {
		case info.Mode().IsRegular():
			trackedBytes += info.Size()
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return metadata{}, fmt.Errorf("read tracked link %s: %w", relative, err)
			}
			trackedBytes += int64(len(target))
		}
	}
	goModDigest, err := fileDigest(filepath.Join(root, "go.mod"))
	if err != nil {
		return metadata{}, err
	}
	moduleDigest, err := fileDigest(filepath.Join(root, "MODULE.bazel"))
	if err != nil {
		return metadata{}, err
	}
	moduleLockDigest, err := fileDigest(filepath.Join(root, "MODULE.bazel.lock"))
	if err != nil {
		return metadata{}, err
	}
	kind := "developer"
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		kind = "github-actions"
	}
	return metadata{
		Environment: environment{
			Kind: kind, OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(),
			RunnerImage: os.Getenv("ImageOS"), GoVersion: strings.TrimSpace(goVersion),
			BazelVersion: strings.TrimSpace(string(bazelVersionBytes)), BazelCommand: launcher,
		},
		Corpus: corpus{
			Repository: "github.com/Luqueee/kivgraph", Commit: commit, BazelTarget: target,
			GoTarget: goTarget, EditedFile: editFile, TrackedFiles: len(files), TrackedBytes: trackedBytes,
			GoModSHA256:      goModDigest,
			ModuleSHA256:     moduleDigest,
			ModuleLockSHA256: moduleLockDigest,
		},
	}, nil
}

func measureTrial(ctx context.Context, root, output string, files []string, index int) (trialResult, error) {
	trialRoot, err := os.MkdirTemp("", fmt.Sprintf("kivgraph-build-cost-%02d-", index))
	if err != nil {
		return trialResult{}, err
	}
	defer os.RemoveAll(trialRoot)
	result := trialResult{Index: index, Order: armOrder(index)}
	for _, name := range result.Order {
		source := filepath.Join(trialRoot, name, "source")
		if err := copyFiles(root, source, files); err != nil {
			return trialResult{}, fmt.Errorf("copy %s source: %w", name, err)
		}
		logPath := filepath.Join(output, fmt.Sprintf("trial-%02d-%s.log", index, name))
		arm, err := measureArm(ctx, name, source, filepath.Join(trialRoot, name, "state"), files, index, logPath)
		if err != nil {
			return trialResult{}, fmt.Errorf("measure %s arm: %w", name, err)
		}
		result.Arms = append(result.Arms, arm)
	}
	return result, nil
}

func measureArm(ctx context.Context, name, source, state string, files []string, trial int, logPath string) (armResult, error) {
	if err := os.MkdirAll(state, 0o755); err != nil {
		return armResult{}, err
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return armResult{}, err
	}
	defer logFile.Close()
	before, err := hashFiles(source, files)
	if err != nil {
		return armResult{}, err
	}
	var output armResult
	output.Name = name
	var setup, dependencies, clean, warm, edited float64
	switch name {
	case armGo:
		goCache := filepath.Join(state, "go-build")
		goPath := filepath.Join(state, "gopath")
		env := []string{"GOCACHE=" + goCache, "GOMODCACHE=" + filepath.Join(goPath, "pkg", "mod"), "GOPATH=" + goPath, "GOTOOLCHAIN=local"}
		output.Commands = commands{Setup: "go version", Dependencies: "go mod download", Build: "go build -o <private>/kivgraph " + goTarget}
		setup, err = timedCommand(ctx, source, env, logFile, "go", "version")
		if err == nil {
			dependencies, err = timedCommand(ctx, source, env, logFile, "go", "mod", "download")
		}
		if err == nil {
			clean, err = timedCommand(ctx, source, env, logFile, "go", "build", "-o", filepath.Join(state, "kivgraph"), goTarget)
		}
		if err == nil {
			warm, err = timedCommand(ctx, source, env, logFile, "go", "build", "-o", filepath.Join(state, "kivgraph"), goTarget)
		}
		if err == nil {
			err = applyEdit(source, editFile, trial)
		}
		if err == nil {
			edited, err = timedCommand(ctx, source, env, logFile, "go", "build", "-o", filepath.Join(state, "kivgraph"), goTarget)
		}
	case armBazel:
		userRoot := filepath.Join(state, "output-user-root")
		repositoryCache := filepath.Join(state, "repository-cache")
		diskCache := filepath.Join(state, "disk-cache")
		env := []string{"BAZELISK_HOME=" + filepath.Join(state, "bazelisk")}
		base := []string{"--ignore_all_rc_files", "--output_user_root=" + userRoot}
		output.Commands = commands{Setup: "bazel --ignore_all_rc_files --output_user_root=<private> version", Dependencies: "bazel --ignore_all_rc_files --output_user_root=<private> fetch " + target, Build: "bazel --ignore_all_rc_files --output_user_root=<private> build " + target}
		setup, err = timedCommand(ctx, source, env, logFile, "bazel", append(base, "version")...)
		cacheOptions := []string{"--repository_cache=" + repositoryCache, "--disk_cache=" + diskCache}
		fetch := append(append(append([]string{}, base...), "fetch"), cacheOptions...)
		fetch = append(fetch, target)
		build := append(append(append([]string{}, base...), "build"), cacheOptions...)
		build = append(build, target)
		if err == nil {
			dependencies, err = timedCommand(ctx, source, env, logFile, "bazel", fetch...)
		}
		if err == nil {
			clean, err = timedCommand(ctx, source, env, logFile, "bazel", build...)
		}
		if err == nil {
			warm, err = timedCommand(ctx, source, env, logFile, "bazel", build...)
		}
		if err == nil {
			err = applyEdit(source, editFile, trial)
		}
		if err == nil {
			edited, err = timedCommand(ctx, source, env, logFile, "bazel", build...)
		}
	default:
		return armResult{}, fmt.Errorf("unknown arm %q", name)
	}
	if err != nil {
		return armResult{}, err
	}
	after, err := hashFiles(source, files)
	if err != nil {
		return armResult{}, err
	}
	if changed := changedFiles(before, after); len(changed) != 1 || changed[0] != editFile {
		return armResult{}, fmt.Errorf("edit changed %v, want only %s", changed, editFile)
	}
	output.Clean = phases{SetupSeconds: setup, DependencySeconds: dependencies, BuildSeconds: clean, TotalSeconds: setup + dependencies + clean}
	output.Warm = buildPhase{BuildSeconds: warm}
	output.Edit = editPhase{EditedFile: editFile, BuildSeconds: edited}
	return output, nil
}

func timedCommand(ctx context.Context, directory string, environment []string, logFile *os.File, name string, args ...string) (float64, error) {
	fmt.Fprintf(logFile, "$ %s %s\n", name, strings.Join(args, " "))
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = mergedEnvironment(environment)
	command.Stdout = logFile
	command.Stderr = logFile
	started := time.Now()
	err := command.Run()
	seconds := time.Since(started).Seconds()
	fmt.Fprintf(logFile, "elapsed_seconds=%.6f\n\n", seconds)
	if err != nil {
		return 0, fmt.Errorf("%s %s failed after %.3fs (see %s): %w", name, strings.Join(args, " "), seconds, logFile.Name(), err)
	}
	return seconds, nil
}

func armOrder(index int) []string {
	if index%2 == 0 {
		return []string{armBazel, armGo}
	}
	return []string{armGo, armBazel}
}

func summarize(trials []trialResult) (summary, error) {
	byArm := map[string][]armResult{armGo: {}, armBazel: {}}
	for _, trial := range trials {
		seen := map[string]bool{}
		for _, arm := range trial.Arms {
			if _, ok := byArm[arm.Name]; !ok {
				return summary{}, fmt.Errorf("unknown arm %q in trial %d", arm.Name, trial.Index)
			}
			if seen[arm.Name] {
				return summary{}, fmt.Errorf("duplicate %s arm in trial %d", arm.Name, trial.Index)
			}
			seen[arm.Name] = true
			byArm[arm.Name] = append(byArm[arm.Name], arm)
		}
		for _, name := range []string{armGo, armBazel} {
			if !seen[name] {
				return summary{}, fmt.Errorf("missing %s arm in trial %d", name, trial.Index)
			}
		}
	}
	goSummary, err := summarizeArm(byArm[armGo])
	if err != nil {
		return summary{}, err
	}
	bazelSummary, err := summarizeArm(byArm[armBazel])
	if err != nil {
		return summary{}, err
	}
	if bazelSummary.CleanBuildSeconds == 0 || bazelSummary.WarmBuildSeconds == 0 || bazelSummary.EditBuildSeconds == 0 {
		return summary{}, errors.New("Bazel median is zero; cannot calculate ratios")
	}
	return summary{
		Go: goSummary, Bazel: bazelSummary,
		Ratios: ratios{
			GoOverBazelClean: goSummary.CleanBuildSeconds / bazelSummary.CleanBuildSeconds,
			GoOverBazelWarm:  goSummary.WarmBuildSeconds / bazelSummary.WarmBuildSeconds,
			GoOverBazelEdit:  goSummary.EditBuildSeconds / bazelSummary.EditBuildSeconds,
		},
	}, nil
}

func summarizeArm(arms []armResult) (armSummary, error) {
	if len(arms) == 0 {
		return armSummary{}, errors.New("cannot summarize an empty arm")
	}
	values := func(selectValue func(armResult) float64) []float64 {
		result := make([]float64, len(arms))
		for index, arm := range arms {
			result[index] = selectValue(arm)
		}
		return result
	}
	setup, _ := median(values(func(a armResult) float64 { return a.Clean.SetupSeconds }))
	dependencies, _ := median(values(func(a armResult) float64 { return a.Clean.DependencySeconds }))
	clean, _ := median(values(func(a armResult) float64 { return a.Clean.BuildSeconds }))
	total, _ := median(values(func(a armResult) float64 { return a.Clean.TotalSeconds }))
	warm, _ := median(values(func(a armResult) float64 { return a.Warm.BuildSeconds }))
	edit, _ := median(values(func(a armResult) float64 { return a.Edit.BuildSeconds }))
	return armSummary{SetupSeconds: setup, DependencySeconds: dependencies, CleanBuildSeconds: clean, CleanTotalSeconds: total, WarmBuildSeconds: warm, EditBuildSeconds: edit}, nil
}

func median(values []float64) (float64, error) {
	if len(values) == 0 {
		return 0, errors.New("median requires at least one value")
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle], nil
	}
	return (sorted[middle-1] + sorted[middle]) / 2, nil
}

func writeOutputs(directory string, output result) error {
	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "results.json"), append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	report := renderReport(output)
	return os.WriteFile(filepath.Join(directory, "report.md"), []byte(report), 0o644)
}

func renderReport(output result) string {
	return fmt.Sprintf(`# Build system cost

Commit: %s  
Environment: %s/%s (%s)  
Trials: %d

| median phase | Go | Bazel | Go / Bazel |
| --- | ---: | ---: | ---: |
| setup | %.3f s | %.3f s | — |
| dependencies | %.3f s | %.3f s | — |
| clean build | %.3f s | %.3f s | %.2fx |
| warm no-op build | %.3f s | %.3f s | %.2fx |
| one-file edit build | %.3f s | %.3f s | %.2fx |
| clean total | %.3f s | %.3f s | — |

No timing threshold is applied. A successful run means only that both arms were measured.
`, output.Commit, output.Environment.OS, output.Environment.Arch, output.Environment.Kind, len(output.Trials),
		output.Summary.Go.SetupSeconds, output.Summary.Bazel.SetupSeconds,
		output.Summary.Go.DependencySeconds, output.Summary.Bazel.DependencySeconds,
		output.Summary.Go.CleanBuildSeconds, output.Summary.Bazel.CleanBuildSeconds, output.Summary.Ratios.GoOverBazelClean,
		output.Summary.Go.WarmBuildSeconds, output.Summary.Bazel.WarmBuildSeconds, output.Summary.Ratios.GoOverBazelWarm,
		output.Summary.Go.EditBuildSeconds, output.Summary.Bazel.EditBuildSeconds, output.Summary.Ratios.GoOverBazelEdit,
		output.Summary.Go.CleanTotalSeconds, output.Summary.Bazel.CleanTotalSeconds)
}

func repositoryRoot(ctx context.Context) (string, error) {
	root, err := commandOutput(ctx, ".", nil, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("find repository root: %w", err)
	}
	return strings.TrimSpace(root), nil
}

func cleanTrackedFiles(ctx context.Context, root string) (string, []string, error) {
	status, err := commandOutput(ctx, root, nil, "git", "status", "--porcelain")
	if err != nil {
		return "", nil, fmt.Errorf("inspect worktree: %w", err)
	}
	if strings.TrimSpace(status) != "" {
		return "", nil, errors.New("worktree is dirty; commit or stash changes before publishing a benchmark")
	}
	commit, err := commandOutput(ctx, root, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", nil, fmt.Errorf("resolve commit: %w", err)
	}
	listed, err := commandOutput(ctx, root, nil, "git", "ls-files", "-z")
	if err != nil {
		return "", nil, fmt.Errorf("list tracked files: %w", err)
	}
	files := strings.Split(listed, "\x00")
	if len(files) > 0 && files[len(files)-1] == "" {
		files = files[:len(files)-1]
	}
	if len(files) == 0 {
		return "", nil, errors.New("repository has no tracked files")
	}
	return strings.TrimSpace(commit), files, nil
}

func requireCommands() error {
	for _, name := range []string{"git", "go", "bazel"} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("required command %q is unavailable: %w", name, err)
		}
	}
	return nil
}

func commandOutput(ctx context.Context, directory string, environment []string, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = mergedEnvironment(environment)
	bytes, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func mergedEnvironment(overrides []string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		key, _, _ := strings.Cut(entry, "=")
		keys[key] = struct{}{}
	}
	merged := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, overridden := keys[key]; !overridden {
			merged = append(merged, entry)
		}
	}
	return append(merged, overrides...)
}

func copyFiles(source, destination string, files []string) error {
	for _, relative := range files {
		from := filepath.Join(source, relative)
		info, err := os.Lstat(from)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", relative, err)
		}
		to := filepath.Join(destination, relative)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(from)
			if err != nil {
				return fmt.Errorf("read link %s: %w", relative, err)
			}
			if err := os.Symlink(target, to); err != nil {
				return fmt.Errorf("copy link %s: %w", relative, err)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		content, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, content, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func hashFiles(root string, files []string) (map[string]string, error) {
	hashes := make(map[string]string, len(files))
	for _, relative := range files {
		path := filepath.Join(root, relative)
		info, err := os.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return nil, fmt.Errorf("read link %s: %w", relative, err)
			}
			digest := sha256.Sum256([]byte(target))
			hashes[relative] = hex.EncodeToString(digest[:])
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", relative, err)
		}
		digest := sha256.Sum256(content)
		hashes[relative] = hex.EncodeToString(digest[:])
	}
	return hashes, nil
}

func changedFiles(before, after map[string]string) []string {
	set := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		set[path] = struct{}{}
	}
	for path := range after {
		set[path] = struct{}{}
	}
	var changed []string
	for path := range set {
		if before[path] != after[path] {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

func applyEdit(root, relative string, trial int) error {
	path := filepath.Join(root, relative)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open edit target %s: %w", relative, err)
	}
	defer file.Close()
	if _, err := fmt.Fprintf(file, "\n// build-system-cost trial %d\n", trial); err != nil {
		return fmt.Errorf("edit %s: %w", relative, err)
	}
	return nil
}

func fileDigest(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("digest %s: %w", path, err)
	}
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:]), nil
}
