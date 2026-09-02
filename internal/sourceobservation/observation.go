// Package sourceobservation captures the mutable source state that produced a
// generation. A publication stores that observation and compares it again
// before CURRENT moves, so readers never receive a graph as current when one
// of its inputs changed while it was being built.
package sourceobservation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/topology"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const (
	// CurrentVersion is the schema version of a stored source observation.
	CurrentVersion = 1
	// FileName is the fixed filename a generation uses for its source inputs.
	FileName = "source-observations.json"
)

var (
	// ErrAbsent reports an input that was not present while an observation was
	// captured. It is distinct from an unreadable input: both stop publication,
	// but the remedy is different.
	ErrAbsent = errors.New("source input is absent")
	// ErrUnreadable reports an input that existed but could not be read
	// completely and therefore cannot support a truthful publication.
	ErrUnreadable = errors.New("source input is unreadable")
	// ErrChanged reports that the source state captured before analysis no
	// longer agrees with the state observed before publication.
	ErrChanged = errors.New("source observations changed during indexing")
)

// Policy records the provider configuration that decides which source files a
// repository may contribute. The content digest deliberately over-approximates
// this policy; storing it separately keeps a later diagnostic from guessing
// whether an excluded or generated input was considered.
type Policy struct {
	Languages  []string `json:"languages"`
	Manifests  []string `json:"manifests,omitempty"`
	Roots      []string `json:"roots,omitempty"`
	Exclusions []string `json:"exclusions,omitempty"`
}

// Source is one configured repository and the worktree state observed for a
// generation. Repository is the provider identity used by the graph; the
// observation carries the mutable worktree identity and its captured state.
type Source struct {
	Repository  string                     `json:"repository"`
	Derived     bool                       `json:"derived,omitempty"`
	Observation topology.SourceObservation `json:"observation"`
	Policy      Policy                     `json:"policy"`
}

// Manifest is the complete input record for one profile generation.
// AnalyzerFingerprint identifies the actual analyzer configuration and build;
// ResolverVersion identifies the graph resolver whose rules produced the
// facts. Neither is inferred from timestamps.
type Manifest struct {
	Version             int      `json:"version"`
	Profile             string   `json:"profile"`
	ResolverVersion     string   `json:"resolver_version"`
	AnalyzerFingerprint string   `json:"analyzer_fingerprint"`
	Sources             []Source `json:"sources"`
}

// Capture observes every registered repository now. It refreshes Git state
// instead of trusting state cached when the registry was opened: an edit or a
// checkout can happen in the interval before indexing starts.
func Capture(
	ctx context.Context,
	profile, resolverVersion, analyzerFingerprint string,
	repositories []workspace.Repository,
) (Manifest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	profile = strings.TrimSpace(profile)
	if profile == "" {
		profile = "default"
	}
	if _, err := topology.NewProfileID(profile); err != nil {
		return Manifest{}, fmt.Errorf("source observation profile: %w", err)
	}
	resolverVersion = strings.TrimSpace(resolverVersion)
	if resolverVersion == "" {
		return Manifest{}, errors.New("source observation resolver version is required")
	}
	analyzerFingerprint = strings.TrimSpace(analyzerFingerprint)
	if analyzerFingerprint == "" {
		return Manifest{}, errors.New("source observation analyzer fingerprint is required")
	}

	manifest := Manifest{
		Version:             CurrentVersion,
		Profile:             profile,
		ResolverVersion:     resolverVersion,
		AnalyzerFingerprint: analyzerFingerprint,
		Sources:             make([]Source, 0, len(repositories)),
	}
	seen := make(map[string]struct{}, len(repositories))
	for index, repository := range repositories {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		name := strings.TrimSpace(repository.Name)
		if name == "" {
			return Manifest{}, fmt.Errorf("source observations[%d].repository: must not be empty", index)
		}
		if _, exists := seen[name]; exists {
			return Manifest{}, fmt.Errorf("source observations[%d].repository %q: duplicate", index, name)
		}
		seen[name] = struct{}{}

		root := repository.RealPath
		if root == "" {
			root = repository.Path
		}
		digest, err := TreeDigest(ctx, root)
		if err != nil {
			return Manifest{}, fmt.Errorf("observe source repository %q content: %w", name, err)
		}
		refreshed := repository
		if repository.Derived {
			// A provider discovered by an analyzer has no Git worktree. Its
			// content digest is both the complete source state and its stable
			// revision token; the empty branch truthfully says no branch exists.
			refreshed.Commit = "content-" + digest
			refreshed.Branch = ""
			refreshed.Dirty = false
		} else {
			refreshed, err = workspace.RefreshRepositoryState(ctx, repository)
			if err != nil {
				return Manifest{}, fmt.Errorf("observe source repository %q state: %w", name, err)
			}
		}
		worktree, err := sourceWorktreeID(refreshed)
		if err != nil {
			return Manifest{}, fmt.Errorf("observe source repository %q worktree: %w", name, err)
		}
		observation, err := topology.NewSourceObservation(worktree, refreshed.Commit, refreshed.Branch, refreshed.Dirty, digest)
		if err != nil {
			return Manifest{}, fmt.Errorf("observe source repository %q state: %w", name, err)
		}
		manifest.Sources = append(manifest.Sources, Source{
			Repository:  name,
			Derived:     refreshed.Derived,
			Observation: observation,
			Policy: Policy{
				Languages:  append([]string(nil), refreshed.Languages...),
				Manifests:  append([]string(nil), refreshed.Manifests...),
				Roots:      append([]string(nil), refreshed.Roots...),
				Exclusions: append([]string(nil), refreshed.Exclusions...),
			},
		})
	}
	sort.Slice(manifest.Sources, func(left, right int) bool {
		return manifest.Sources[left].Repository < manifest.Sources[right].Repository
	})
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Compare reports a source state change with the first affected input. The
// caller supplies observations captured before and after a full pass.
func Compare(expected, actual Manifest) error {
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("validate expected source observations: %w", err)
	}
	if err := actual.Validate(); err != nil {
		return fmt.Errorf("validate current source observations: %w", err)
	}
	if bytes.Equal(mustEncode(expected), mustEncode(actual)) {
		return nil
	}
	if expected.Profile != actual.Profile {
		return fmt.Errorf("%w: profile changed from %q to %q", ErrChanged, expected.Profile, actual.Profile)
	}
	if expected.ResolverVersion != actual.ResolverVersion {
		return fmt.Errorf("%w: resolver changed from %q to %q", ErrChanged, expected.ResolverVersion, actual.ResolverVersion)
	}
	if expected.AnalyzerFingerprint != actual.AnalyzerFingerprint {
		return fmt.Errorf("%w: analyzer configuration changed", ErrChanged)
	}
	for index := 0; index < len(expected.Sources) && index < len(actual.Sources); index++ {
		before, after := expected.Sources[index], actual.Sources[index]
		if before.Repository != after.Repository {
			return fmt.Errorf("%w: provider set changed from %q to %q", ErrChanged, before.Repository, after.Repository)
		}
		if before.Derived != after.Derived || before.Observation.ID != after.Observation.ID ||
			!bytes.Equal(mustEncode(before.Policy), mustEncode(after.Policy)) {
			return fmt.Errorf("%w: source %q no longer matches observation %q", ErrChanged, before.Repository, before.Observation.ID)
		}
	}
	return fmt.Errorf("%w: source count changed from %d to %d", ErrChanged, len(expected.Sources), len(actual.Sources))
}

// Validate checks that the persisted record is complete and each source state
// retains the deterministic topology observation identity it claims.
func (manifest Manifest) Validate() error {
	if manifest.Version != CurrentVersion {
		return fmt.Errorf("source observation version %d: want %d", manifest.Version, CurrentVersion)
	}
	if _, err := topology.NewProfileID(strings.TrimSpace(manifest.Profile)); err != nil {
		return fmt.Errorf("source observation profile: %w", err)
	}
	if strings.TrimSpace(manifest.ResolverVersion) == "" {
		return errors.New("source observation resolver version is required")
	}
	if strings.TrimSpace(manifest.AnalyzerFingerprint) == "" {
		return errors.New("source observation analyzer fingerprint is required")
	}
	seen := make(map[string]struct{}, len(manifest.Sources))
	for index, source := range manifest.Sources {
		name := strings.TrimSpace(source.Repository)
		if name == "" {
			return fmt.Errorf("source observations[%d].repository: must not be empty", index)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("source observations[%d].repository %q: duplicate", index, name)
		}
		seen[name] = struct{}{}
		if err := source.Observation.Validate(); err != nil {
			return fmt.Errorf("source observations[%d] %q: %w", index, name, err)
		}
	}
	return nil
}

// Write atomically places one validated manifest in a candidate generation.
// The generation store syncs the candidate tree before publishing it, so this
// helper only has to ensure no partial file is observable inside the candidate.
func Write(candidatePath string, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(candidatePath) == "" {
		return errors.New("source observation candidate path is required")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode source observations: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(candidatePath, ".source-observations-*.tmp")
	if err != nil {
		return fmt.Errorf("create source observation candidate: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure source observation candidate: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write source observation candidate: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close source observation candidate: %w", err)
	}
	if err := os.Rename(temporaryPath, filepath.Join(candidatePath, FileName)); err != nil {
		return fmt.Errorf("publish source observation candidate: %w", err)
	}
	return nil
}

// Read loads the manifest published with one generation.
func Read(generationPath string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(generationPath, FileName))
	if err != nil {
		return Manifest{}, fmt.Errorf("read source observations: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode source observations: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, errors.New("decode source observations: multiple documents")
		}
		return Manifest{}, fmt.Errorf("decode source observations: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// TreeDigest hashes every source file and build manifest a language provider
// may read below root. It deliberately over-approximates language-specific
// exclusions: omitting a source that a provider may have read could publish a
// graph that cannot be reproduced, while an extra rebuild is safe.
func TreeDigest(ctx context.Context, root string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return "", ErrAbsent
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", classifyReadError(err)
	}
	if !info.IsDir() {
		return FileDigest(ctx, root)
	}
	hash := sha256.New()
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", ".git":
				return fs.SkipDir
			}
			return nil
		}
		if !IsAnalyzedSource(entry.Name()) {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		if _, err := fmt.Fprintf(hash, "%s\x00size=%d\x00", filepath.ToSlash(relative), info.Size()); err != nil {
			return err
		}
		if _, err := io.Copy(hash, file); err != nil {
			return err
		}
		_, err = fmt.Fprint(hash, "\x00")
		return err
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", classifyReadError(err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// FileDigest hashes one source input with its byte length, matching the file
// fingerprint used by the fact cache.
func FileDigest(ctx context.Context, path string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", classifyReadError(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("%w: stat source input %q: %v", ErrUnreadable, path, err)
	}
	hash := sha256.New()
	if _, err := fmt.Fprintf(hash, "size=%d\x00", info.Size()); err != nil {
		return "", fmt.Errorf("%w: encode source input size: %v", ErrUnreadable, err)
	}
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		read, readErr := file.Read(buffer)
		if read > 0 {
			if _, err := hash.Write(buffer[:read]); err != nil {
				return "", fmt.Errorf("%w: hash source input: %v", ErrUnreadable, err)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", fmt.Errorf("%w: read source input %q: %v", ErrUnreadable, path, readErr)
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// IsAnalyzedSource reports whether a path can change the facts produced by a
// supported language or the manifests that select those facts.
func IsAnalyzedSource(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	if _, ok := analyzedManifests[base]; ok {
		return true
	}
	if strings.HasPrefix(base, "requirements-") && strings.HasSuffix(base, ".txt") {
		return true
	}
	return config.HasSourceExtension(analyzedExtensions, name)
}

var analyzedExtensions = config.SourceExtensionSet(config.SupportedLanguages())

var analyzedManifests = map[string]struct{}{
	"go.mod": {}, "go.sum": {}, "go.work": {}, "go.work.sum": {},
	"package.json": {}, "tsconfig.json": {},
	"cargo.toml": {}, "cargo.lock": {},
	"pyproject.toml": {}, "setup.py": {}, "setup.cfg": {}, "requirements.txt": {},
	"pipfile": {}, "pipfile.lock": {}, "poetry.lock": {}, "uv.lock": {},
	"pubspec.yaml": {}, "pubspec.lock": {}, "analysis_options.yaml": {},
}

func sourceWorktreeID(repository workspace.Repository) (topology.WorktreeID, error) {
	if repository.Worktree != "" {
		return topology.NewWorktreeID(string(repository.Worktree))
	}
	if repository.Derived {
		return topology.NewWorktreeID("derived:" + strings.TrimSpace(repository.Name))
	}
	// Legacy registries predate topology.yaml. Their configured repository name
	// is path-independent and unique within a profile, so it is a conservative
	// compatibility identity until a declared worktree replaces it.
	return topology.NewWorktreeID("legacy:" + strings.TrimSpace(repository.Name))
}

func classifyReadError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %v", ErrAbsent, err)
	}
	return fmt.Errorf("%w: %v", ErrUnreadable, err)
}

func mustEncode(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
