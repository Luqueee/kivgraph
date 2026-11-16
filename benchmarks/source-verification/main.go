//go:build darwin || linux

// Command source-verification measures what a pass pays to prove that its
// sources did not change, and what a cheaper proof would cost and would miss.
//
// A full pass observes the source tree at its start and again inside the
// validation of the candidate generation, immediately before CURRENT can move,
// and compares the two. The second observation is `ObserveSources`: it hashes
// every analysed source file below every repository and asks git for the
// repository state. benchmarks/publication-profile measured that second
// observation at 1,25 to 1,39 s of a pass, and its profile shows that most of
// what the walks spend is opening and reading directories, not hashing.
//
// This harness answers the three questions an ADR on a cheaper verification
// needs, and nothing else:
//
//   - where the time of one observation goes: the traversal, the hashing, or
//     the git state;
//   - what a signature of the tree -- the stat of every directory it reads and of
//     every analysed file, compared again without reading a directory -- costs
//     to capture and to verify;
//   - which changes that signature detects, and whether it ever reports "unchanged"
//     for a tree the current digest reports as changed. That last number has to
//     be zero for any of this to be an option.
//
// It reads the registered repositories and writes nothing in them. The change
// matrix runs on a synthetic tree created under the temporary directory.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/sourceobservation"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const schemaVersion = "source-verification-v1"

// racyWindow is how close to the moment a signature was captured a modification
// time has to be for the entry to be re-read instead of trusted. A file written
// in the same tick as the capture can be written again in that tick without its
// stat changing; git calls those entries racily clean.
const racyWindow = 2 * time.Second

func main() {
	var (
		configPath = flag.String("config", "", "kivgraph config naming the repositories to measure")
		repeats    = flag.Int("repeats", 5, "how many times to time each operation; the median is reported")
		output     = flag.String("output", "", "where to write results.json; empty writes to stdout only")
	)
	flag.Parse()
	if err := run(context.Background(), *configPath, *repeats, *output); err != nil {
		fmt.Fprintf(os.Stderr, "source-verification: %v\n", err)
		os.Exit(1)
	}
}

type repositoryResult struct {
	// Label is an index, not the repository name: the corpus is private.
	Label string `json:"label"`

	Directories   int   `json:"directories_read"`
	Entries       int   `json:"entries_listed"`
	AnalysedFiles int   `json:"analysed_files"`
	AnalysedBytes int64 `json:"analysed_bytes"`
	RacyEntries   int   `json:"racy_entries_at_capture"`

	TreeDigestMS       float64 `json:"tree_digest_ms"`
	TraversalOnlyMS    float64 `json:"traversal_only_ms"`
	GitStateMS         float64 `json:"git_state_ms"`
	SignatureCaptureMS float64 `json:"signature_capture_ms"`
	SignatureVerifyMS  float64 `json:"signature_verify_ms"`
	SignatureStatCalls int     `json:"signature_stat_calls"`
}

type scenarioResult struct {
	Name             string `json:"name"`
	DigestChanged    bool   `json:"digest_changed"`
	SignatureChanged bool   `json:"signature_changed"`
	// Outcome is "agree", "false positive" (the signature reports a change the
	// digest does not, which costs a re-read) or "MISSED" (the signature
	// reports none for a tree the digest says changed, which disqualifies it).
	Outcome string `json:"outcome"`
}

type results struct {
	Benchmark    string             `json:"benchmark"`
	Commit       string             `json:"commit"`
	Schema       string             `json:"schema"`
	GeneratedAt  time.Time          `json:"generated_at"`
	Command      string             `json:"command"`
	Environment  map[string]string  `json:"environment"`
	Repeats      int                `json:"repeats"`
	Repositories []repositoryResult `json:"repositories"`
	Totals       repositoryResult   `json:"totals"`
	Matrix       []scenarioResult   `json:"change_matrix"`
	Missed       int                `json:"missed_changes"`
	Limitations  []string           `json:"limitations"`
}

func run(ctx context.Context, configPath string, repeats int, output string) error {
	if repeats < 1 {
		return fmt.Errorf("-repeats must be at least 1")
	}
	out := results{
		Benchmark:   "source-verification",
		Commit:      repositoryCommit(),
		Schema:      schemaVersion,
		GeneratedAt: time.Now().UTC(),
		Command:     strings.Join(os.Args, " "),
		Environment: map[string]string{
			"os": runtime.GOOS, "arch": runtime.GOARCH, "cpus": fmt.Sprint(runtime.NumCPU()), "go": runtime.Version(),
		},
		Repeats: repeats,
	}

	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		registry, err := workspace.NewRegistry(ctx, loaded.Repositories)
		if err != nil {
			return fmt.Errorf("register repositories: %w", err)
		}
		for index, repository := range registry.List() {
			root := repository.RealPath
			if root == "" {
				root = repository.Path
			}
			measured, err := measureRepository(ctx, repository, root, repeats)
			if err != nil {
				return fmt.Errorf("repository %d: %w", index+1, err)
			}
			measured.Label = fmt.Sprintf("repository-%d", index+1)
			out.Repositories = append(out.Repositories, measured)
			fmt.Fprintf(os.Stderr, "%s: %d directories, %d analysed files, digest %.1f ms, traversal %.1f ms, git %.1f ms, signature capture %.1f ms verify %.1f ms\n",
				measured.Label, measured.Directories, measured.AnalysedFiles, measured.TreeDigestMS,
				measured.TraversalOnlyMS, measured.GitStateMS, measured.SignatureCaptureMS, measured.SignatureVerifyMS)
		}
		out.Totals = sum(out.Repositories)
	}

	matrix, err := changeMatrix(ctx)
	if err != nil {
		return err
	}
	out.Matrix = matrix
	for _, scenario := range matrix {
		if scenario.Outcome == "MISSED" {
			out.Missed++
		}
	}
	out.Limitations = limitations()

	encoded, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if output != "" {
		if err := os.WriteFile(output, encoded, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", output)
	} else {
		fmt.Print(string(encoded))
	}
	if out.Missed != 0 {
		return fmt.Errorf("%d scenario(s) changed the tree and the signature reported nothing", out.Missed)
	}
	return nil
}

func sum(all []repositoryResult) repositoryResult {
	var total repositoryResult
	total.Label = "all repositories"
	for _, entry := range all {
		total.Directories += entry.Directories
		total.Entries += entry.Entries
		total.AnalysedFiles += entry.AnalysedFiles
		total.AnalysedBytes += entry.AnalysedBytes
		total.RacyEntries += entry.RacyEntries
		total.TreeDigestMS += entry.TreeDigestMS
		total.TraversalOnlyMS += entry.TraversalOnlyMS
		total.GitStateMS += entry.GitStateMS
		total.SignatureCaptureMS += entry.SignatureCaptureMS
		total.SignatureVerifyMS += entry.SignatureVerifyMS
		total.SignatureStatCalls += entry.SignatureStatCalls
	}
	return total
}

func measureRepository(ctx context.Context, repository workspace.Repository, root string, repeats int) (repositoryResult, error) {
	var result repositoryResult

	// One warm-up of each, so the first sample is not the one that reads the
	// directories from disk: a pass observes a tree it has just analysed.
	if _, err := sourceobservation.TreeDigest(ctx, root); err != nil {
		return result, err
	}
	signature, counts, err := captureSignature(root)
	if err != nil {
		return result, err
	}
	result.Directories, result.Entries = counts.directories, counts.entries
	result.AnalysedFiles, result.AnalysedBytes = counts.files, counts.bytes
	result.RacyEntries = signature.racy()
	result.SignatureStatCalls = len(signature.directories) + len(signature.files)

	result.TreeDigestMS = median(repeats, func() error {
		_, err := sourceobservation.TreeDigest(ctx, root)
		return err
	})
	result.TraversalOnlyMS = median(repeats, func() error {
		_, _, err := traverseOnly(root)
		return err
	})
	result.GitStateMS = median(repeats, func() error {
		_, err := workspace.RefreshRepositoryState(ctx, repository)
		return err
	})
	result.SignatureCaptureMS = median(repeats, func() error {
		_, _, err := captureSignature(root)
		return err
	})
	result.SignatureVerifyMS = median(repeats, func() error {
		changed, err := signature.verify()
		if err == nil && changed {
			return fmt.Errorf("the tree changed while it was being measured")
		}
		return err
	})
	return result, nil
}

func median(repeats int, work func() error) float64 {
	samples := make([]float64, 0, repeats)
	for range repeats {
		started := time.Now()
		if err := work(); err != nil {
			fmt.Fprintf(os.Stderr, "  sample failed: %v\n", err)
			return -1
		}
		samples = append(samples, float64(time.Since(started))/float64(time.Millisecond))
	}
	sort.Float64s(samples)
	return samples[len(samples)/2]
}

// ---------------------------------------------------------------------------
// the traversal TreeDigest does, without the hashing
// ---------------------------------------------------------------------------

type counts struct {
	directories, entries, files int
	bytes                       int64
}

// skipped mirrors TreeDigest: it enters every directory except these two.
func skipped(name string) bool { return name == "node_modules" || name == ".git" }

func traverseOnly(root string) (int, int, error) {
	directories, files := 0, 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path != root {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			if skipped(entry.Name()) {
				return fs.SkipDir
			}
			directories++
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if sourceobservation.IsAnalyzedSource(relative) {
			files++
		}
		return nil
	})
	return directories, files, err
}

// ---------------------------------------------------------------------------
// the signature
// ---------------------------------------------------------------------------

// stamp is what one stat says about an entry. ctime is the inode change time:
// unlike the modification time, no program can set it, so a write that restores
// the modification time still moves it.
type stamp struct {
	size          int64
	mtime, ctime  int64
	inode         uint64
	mode          uint32
	racyAtCapture bool
}

type signature struct {
	root        string
	captured    time.Time
	directories map[string]stamp
	files       map[string]stamp
}

func stampOf(info fs.FileInfo, captured time.Time) stamp {
	raw, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return stamp{size: info.Size(), mtime: info.ModTime().UnixNano()}
	}
	s := stamp{
		size: info.Size(), mtime: info.ModTime().UnixNano(),
		ctime: ctimeNanoseconds(raw), inode: uint64(raw.Ino), mode: uint32(raw.Mode),
	}
	s.racyAtCapture = captured.Sub(info.ModTime()) < racyWindow
	return s
}

// captureSignature records, for the same traversal TreeDigest does, the stamp of
// every directory it enters and of every analysed file it would hash.
func captureSignature(root string) (*signature, counts, error) {
	captured := time.Now()
	sig := &signature{root: root, captured: captured, directories: map[string]stamp{}, files: map[string]stamp{}}
	var seen counts
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path != root {
				return nil
			}
			return walkErr
		}
		seen.entries++
		if entry.IsDir() {
			if skipped(entry.Name()) {
				return fs.SkipDir
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			seen.directories++
			sig.directories[path] = stampOf(info, captured)
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !sourceobservation.IsAnalyzedSource(relative) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		seen.files++
		seen.bytes += info.Size()
		sig.files[path] = stampOf(info, captured)
		return nil
	})
	return sig, seen, err
}

// racy counts the entries a verification would have to read again whatever
// their stat says.
func (sig *signature) racy() int {
	racy := 0
	for _, entries := range []map[string]stamp{sig.directories, sig.files} {
		for _, entry := range entries {
			if entry.racyAtCapture {
				racy++
			}
		}
	}
	return racy
}

// verify stats every recorded directory and file again. It reads no directory
// and opens no file. It reports a change as soon as one stamp differs, or when an
// entry that was racy at capture would have to be read again.
func (sig *signature) verify() (changed bool, err error) {
	for _, entries := range []map[string]stamp{sig.directories, sig.files} {
		for path, before := range entries {
			info, err := os.Lstat(path)
			if err != nil {
				if os.IsNotExist(err) {
					return true, nil
				}
				return false, err
			}
			if after := stampOf(info, sig.captured); !before.sameAs(after) {
				return true, nil
			}
		}
	}
	return false, nil
}

// ignoreChangeTime exists for one row of the matrix: the ablation that shows what
// the signature would miss without the inode change time. Nothing else sets it.
var ignoreChangeTime bool

func (s stamp) sameAs(other stamp) bool {
	if s.racyAtCapture {
		return false
	}
	return s.size == other.size && s.mtime == other.mtime && (ignoreChangeTime || s.ctime == other.ctime) &&
		s.inode == other.inode && s.mode == other.mode
}

// ---------------------------------------------------------------------------
// what the signature misses
// ---------------------------------------------------------------------------

// changeMatrix applies, one at a time, the modifications a tree can go through
// during a pass to a fresh copy of a small synthetic tree, and records whether
// the digest the pass uses today and the signature each report a change.
func changeMatrix(ctx context.Context) ([]scenarioResult, error) {
	type scenario struct {
		name  string
		apply func(root string) error
	}
	write := func(root, name, content string) error {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(content), 0o644)
	}
	scenarios := []scenario{
		{"edit an analysed file, new size", func(r string) error { return write(r, "src/a.go", "package a\n\nvar A = 1\n") }},
		{"edit an analysed file, same size, new mtime", func(r string) error { return write(r, "src/a.go", "package a\n\nvar A = 2\n") }},
		{"same-size edit, then the file's own mtime restored", func(r string) error {
			path := filepath.Join(r, "src/a.go")
			before, err := os.Stat(path)
			if err != nil {
				return err
			}
			if err := write(r, "src/a.go", "package a\n\nvar A = 3\n"); err != nil {
				return err
			}
			return os.Chtimes(path, before.ModTime(), before.ModTime())
		}},
		{"replace an analysed file by rename over it", func(r string) error {
			if err := write(r, "src/a.go.tmp", "package a\n\nvar A = 4\n"); err != nil {
				return err
			}
			return os.Rename(filepath.Join(r, "src/a.go.tmp"), filepath.Join(r, "src/a.go"))
		}},
		{"add an analysed file", func(r string) error { return write(r, "src/new.go", "package a\n") }},
		{"delete an analysed file", func(r string) error { return os.Remove(filepath.Join(r, "src/b.go")) }},
		{"rename an analysed file", func(r string) error {
			return os.Rename(filepath.Join(r, "src/b.go"), filepath.Join(r, "src/c.go"))
		}},
		{"add a file in a new analysed directory", func(r string) error { return write(r, "pkg/new/n.go", "package n\n") }},
		{"edit a file that is not analysed, in place", func(r string) error { return write(r, "docs/readme.md", "# changed\n") }},
		{"add a file that is not analysed", func(r string) error { return write(r, "docs/extra.md", "x\n") }},
		{"build output churn in an ignored directory", func(r string) error {
			if err := write(r, "target/debug/deps/tmp/x.o", "x"); err != nil {
				return err
			}
			return os.RemoveAll(filepath.Join(r, "target/debug/deps/tmp"))
		}},
		{"touch an analysed file, content unchanged", func(r string) error {
			now := time.Now()
			return os.Chtimes(filepath.Join(r, "src/a.go"), now, now)
		}},
		{"chmod an analysed file", func(r string) error { return os.Chmod(filepath.Join(r, "src/a.go"), 0o600) }},
		{"nothing at all", func(string) error { return nil }},
	}

	var matrix []scenarioResult
	for _, one := range scenarios {
		root, err := os.MkdirTemp("", "source-verification-*")
		if err != nil {
			return nil, err
		}
		result, err := runScenario(ctx, root, one.name, one.apply, write)
		_ = os.RemoveAll(root)
		if err != nil {
			return nil, fmt.Errorf("scenario %q: %w", one.name, err)
		}
		matrix = append(matrix, result)
	}

	// The ablation: the scenario that restores the modification time, with the
	// inode change time left out of the comparison. It is expected to be missed,
	// and it is what makes the change time a requirement and not a precaution.
	ignoreChangeTime = true
	defer func() { ignoreChangeTime = false }()
	root, err := os.MkdirTemp("", "source-verification-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	ablation, err := runScenario(ctx, root, "ablation: the old mtime restored, ctime not compared", scenarios[2].apply, write)
	if err != nil {
		return nil, err
	}
	if ablation.Outcome == "MISSED" {
		ablation.Outcome = "ablation: missed"
	}
	return append(matrix, ablation), nil
}

func runScenario(
	ctx context.Context, root, name string,
	apply func(string) error, write func(root, name, content string) error,
) (scenarioResult, error) {
	for path, content := range map[string]string{
		"src/a.go": "package a\n\nvar A = 0\n", "src/b.go": "package a\n\nvar B = 0\n",
		"docs/readme.md": "# readme\n", "target/debug/deps/keep.o": "o",
	} {
		if err := write(root, path, content); err != nil {
			return scenarioResult{}, err
		}
	}
	// Age every file past the racy window, as the files of a tree that has been
	// sitting there are. A tree written a moment ago is racy by construction and
	// would make every scenario a re-read.
	aged := time.Now().Add(-time.Hour)
	if err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, aged, aged)
	}); err != nil {
		return scenarioResult{}, err
	}

	before, err := sourceobservation.TreeDigest(ctx, root)
	if err != nil {
		return scenarioResult{}, err
	}
	signature, _, err := captureSignature(root)
	if err != nil {
		return scenarioResult{}, err
	}
	if err := apply(root); err != nil {
		return scenarioResult{}, err
	}
	after, err := sourceobservation.TreeDigest(ctx, root)
	if err != nil {
		return scenarioResult{}, err
	}
	signatureChanged, err := signature.verify()
	if err != nil {
		return scenarioResult{}, err
	}

	result := scenarioResult{Name: name, DigestChanged: before != after, SignatureChanged: signatureChanged}
	switch {
	case result.DigestChanged && !result.SignatureChanged:
		result.Outcome = "MISSED"
	case !result.DigestChanged && result.SignatureChanged:
		result.Outcome = "false positive"
	default:
		result.Outcome = "agree"
	}
	return result, nil
}

func limitations() []string {
	return []string{
		"The matrix runs on a local APFS or ext4 volume, where the modification time has nanosecond granularity. It says nothing about a filesystem with coarse timestamps or stale attribute caches (NFS, SMB, FAT): there the signature has no guarantee, and that is the reason an ADR has to make it an opt-in or fall back, not something this run can show.",
		"Timings are medians of a warm run: the pass that verifies has just analysed the tree, so its directories are in the page cache. A cold tree costs more for both methods.",
		"The signature is a prototype in this harness. It is not wired into the pass and nothing here changes what a pass publishes.",
		"A file written during the capture tick can be written again in that tick without its stat changing. The racy window (two seconds) makes the signature re-read such entries; the matrix cannot reproduce a hardware clock tick, so this rule is argued, not measured.",
	}
}

// repositoryCommit names the commit the harness ran from. A tree with changes
// carries `-dirty`, and one whose state could not be read `-unknown`: a number
// attributed to a commit that did not produce it is the defect this suffix prevents.
func repositoryCommit() string {
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(string(head))
	status, err := exec.Command("git", "status", "--porcelain").Output()
	switch {
	case err != nil:
		return commit + "-unknown"
	case strings.TrimSpace(string(status)) != "":
		return commit + "-dirty"
	}
	return commit
}
