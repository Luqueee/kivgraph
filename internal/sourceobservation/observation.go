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
	// LegacyVersion is the source observation schema written before published
	// topology compositions were recorded. It remains readable as incomplete.
	LegacyVersion = 1
	// CompositionVersion is the source observation schema that first recorded
	// a generation-owned topology composition. It remains readable because it
	// cannot contain an overlay declaration introduced later.
	CompositionVersion = 2
	// CurrentVersion is the schema version of a stored source observation.
	CurrentVersion = 3
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
	// ErrInvalid reports a stored source observation document that could be
	// read but does not satisfy its persisted schema or integrity checks.
	ErrInvalid = errors.New("source observations are invalid")
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

// TopologyRepository is one stable repository identity recorded with a
// generation's effective composition.
type TopologyRepository struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// TopologyWorktree is one selected mutable worktree recorded with a
// generation's effective composition.
type TopologyWorktree struct {
	ID              string `json:"id"`
	Repository      string `json:"repository"`
	Path            string `json:"path"`
	GitDirectory    string `json:"git_directory,omitempty"`
	CommonDirectory string `json:"common_directory,omitempty"`
	Overlays        string `json:"overlays,omitempty"`
}

// TopologyComposition is the effective topology membership that selected the
// sources for a generation. It is separate from the live topology.yaml so a
// pinned response cannot combine old facts with a later worktree selection.
type TopologyComposition struct {
	Profile          string               `json:"profile"`
	Repositories     []TopologyRepository `json:"repositories"`
	Worktrees        []TopologyWorktree   `json:"worktrees"`
	OverlayWorktrees []TopologyWorktree   `json:"overlay_worktrees,omitempty"`
}

// NewTopologyComposition converts one validated effective composition into
// the persisted form stored beside a generation.
func NewTopologyComposition(value topology.ProfileComposition) (TopologyComposition, error) {
	if err := validateEffectiveComposition(value); err != nil {
		return TopologyComposition{}, err
	}
	profile, err := topology.NewProfileID(string(value.Profile.ID))
	if err != nil {
		return TopologyComposition{}, fmt.Errorf("topology composition profile: %w", err)
	}
	composition := TopologyComposition{
		Profile:          string(profile),
		Repositories:     make([]TopologyRepository, 0, len(value.Repositories)),
		Worktrees:        make([]TopologyWorktree, 0, len(value.Worktrees)),
		OverlayWorktrees: make([]TopologyWorktree, 0, len(value.OverlayWorktrees)),
	}
	for _, repository := range value.Repositories {
		composition.Repositories = append(composition.Repositories, TopologyRepository{
			ID: string(repository.ID), Name: repository.Name,
		})
	}
	for index, worktree := range value.Worktrees {
		composition.Worktrees = append(composition.Worktrees, TopologyWorktree{
			ID:              string(worktree.ID),
			Repository:      string(worktree.Repository),
			Path:            worktree.Path,
			GitDirectory:    worktree.Git.GitDirectory,
			CommonDirectory: worktree.Git.CommonDirectory,
			Overlays:        string(value.Profile.Worktrees[index].Overlays),
		})
	}
	for _, worktree := range value.OverlayWorktrees {
		composition.OverlayWorktrees = append(composition.OverlayWorktrees, TopologyWorktree{
			ID: string(worktree.ID), Repository: string(worktree.Repository), Path: worktree.Path,
			GitDirectory: worktree.Git.GitDirectory, CommonDirectory: worktree.Git.CommonDirectory,
		})
	}
	if _, err := composition.ProfileComposition(); err != nil {
		return TopologyComposition{}, err
	}
	return composition, nil
}

// ProfileComposition reconstructs and validates the effective composition
// persisted with a generation.
func (composition TopologyComposition) ProfileComposition() (topology.ProfileComposition, error) {
	profile, err := topology.NewProfileID(strings.TrimSpace(composition.Profile))
	if err != nil {
		return topology.ProfileComposition{}, fmt.Errorf("topology composition profile: %w", err)
	}
	value := topology.Topology{
		Version:      topology.CurrentSchemaVersion,
		Repositories: make([]topology.LogicalRepository, 0, len(composition.Repositories)),
		Worktrees:    make([]topology.Worktree, 0, len(composition.Worktrees)+len(composition.OverlayWorktrees)),
		Profiles:     []topology.Profile{{ID: profile, Worktrees: make([]topology.WorktreeSelection, 0, len(composition.Worktrees))}},
	}
	for _, repository := range composition.Repositories {
		value.Repositories = append(value.Repositories, topology.LogicalRepository{
			ID: topology.LogicalRepositoryID(repository.ID), Name: repository.Name,
		})
	}
	for _, worktree := range composition.Worktrees {
		id := topology.WorktreeID(worktree.ID)
		repository := topology.LogicalRepositoryID(worktree.Repository)
		value.Worktrees = append(value.Worktrees, topology.Worktree{
			ID: id, Repository: repository, Path: worktree.Path,
			Git: topology.GitLayout{GitDirectory: worktree.GitDirectory, CommonDirectory: worktree.CommonDirectory},
		})
		value.Profiles[0].Worktrees = append(value.Profiles[0].Worktrees, topology.WorktreeSelection{
			Repository: repository, Worktree: id, Overlays: topology.WorktreeID(worktree.Overlays),
		})
	}
	for _, worktree := range composition.OverlayWorktrees {
		value.Worktrees = append(value.Worktrees, topology.Worktree{
			ID: topology.WorktreeID(worktree.ID), Repository: topology.LogicalRepositoryID(worktree.Repository), Path: worktree.Path,
			Git: topology.GitLayout{GitDirectory: worktree.GitDirectory, CommonDirectory: worktree.CommonDirectory},
		})
	}
	resolved, err := value.Compose(profile)
	if err != nil {
		return topology.ProfileComposition{}, fmt.Errorf("topology composition: %w", err)
	}
	if len(resolved.Repositories) != len(composition.Repositories) ||
		len(resolved.Worktrees) != len(composition.Worktrees) ||
		len(composition.Repositories) != len(composition.Worktrees) {
		return topology.ProfileComposition{}, errors.New("topology composition: selected repositories and worktrees must have matching counts")
	}
	if len(resolved.OverlayWorktrees) != len(composition.OverlayWorktrees) {
		return topology.ProfileComposition{}, errors.New("topology composition: selected overlay worktrees differ from persisted composition")
	}
	for index := range resolved.Repositories {
		if resolved.Repositories[index].ID != topology.LogicalRepositoryID(composition.Repositories[index].ID) ||
			resolved.Repositories[index].Name != composition.Repositories[index].Name ||
			resolved.Worktrees[index].ID != topology.WorktreeID(composition.Worktrees[index].ID) ||
			resolved.Worktrees[index].Repository != topology.LogicalRepositoryID(composition.Worktrees[index].Repository) ||
			resolved.Worktrees[index].Path != composition.Worktrees[index].Path ||
			resolved.Worktrees[index].Git.GitDirectory != composition.Worktrees[index].GitDirectory ||
			resolved.Worktrees[index].Git.CommonDirectory != composition.Worktrees[index].CommonDirectory {
			return topology.ProfileComposition{}, fmt.Errorf("topology composition: selected entry %d differs from persisted order", index)
		}
	}
	for index := range resolved.OverlayWorktrees {
		if resolved.OverlayWorktrees[index].ID != topology.WorktreeID(composition.OverlayWorktrees[index].ID) ||
			resolved.OverlayWorktrees[index].Repository != topology.LogicalRepositoryID(composition.OverlayWorktrees[index].Repository) ||
			resolved.OverlayWorktrees[index].Path != composition.OverlayWorktrees[index].Path ||
			resolved.OverlayWorktrees[index].Git.GitDirectory != composition.OverlayWorktrees[index].GitDirectory ||
			resolved.OverlayWorktrees[index].Git.CommonDirectory != composition.OverlayWorktrees[index].CommonDirectory {
			return topology.ProfileComposition{}, fmt.Errorf("topology composition: overlay entry %d differs from persisted order", index)
		}
	}
	return resolved, nil
}

func validateEffectiveComposition(value topology.ProfileComposition) error {
	if _, err := topology.NewProfileID(string(value.Profile.ID)); err != nil {
		return fmt.Errorf("topology composition profile: %w", err)
	}
	if len(value.Repositories) != len(value.Worktrees) || len(value.Profile.Worktrees) != len(value.Worktrees) {
		return errors.New("topology composition: selected repositories and worktrees must have matching counts")
	}
	for index := range value.Worktrees {
		if value.Profile.Worktrees[index].Repository != value.Repositories[index].ID ||
			value.Profile.Worktrees[index].Worktree != value.Worktrees[index].ID {
			return fmt.Errorf("topology composition: selected entry %d differs from effective worktree", index)
		}
	}
	allWorktrees := append([]topology.Worktree(nil), value.Worktrees...)
	allWorktrees = append(allWorktrees, value.OverlayWorktrees...)
	valueAsTopology := topology.Topology{
		Version:      topology.CurrentSchemaVersion,
		Repositories: append([]topology.LogicalRepository(nil), value.Repositories...),
		Worktrees:    allWorktrees,
		Profiles:     []topology.Profile{value.Profile},
	}
	if _, err := valueAsTopology.Compose(value.Profile.ID); err != nil {
		return fmt.Errorf("topology composition: %w", err)
	}
	return nil
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
	// Composition is optional so generations written before composition
	// persistence remain readable and can be reported as incomplete.
	Composition *TopologyComposition `json:"composition,omitempty"`
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

// CaptureWithRepositories returns the provider metadata observed while
// creating the manifest as well as the manifest itself. The full pass uses the
// refreshed values when it stamps repository provenance onto merged facts;
// otherwise a registry opened before a checkout or edit would publish stale
// commit, branch, or dirty state.
func CaptureWithRepositories(
	ctx context.Context,
	profile, resolverVersion, analyzerFingerprint string,
	repositories []workspace.Repository,
) (Manifest, []workspace.Repository, error) {
	manifest, err := Capture(ctx, profile, resolverVersion, analyzerFingerprint, repositories)
	if err != nil {
		return Manifest{}, nil, err
	}

	byName := make(map[string]Source, len(manifest.Sources))
	for _, source := range manifest.Sources {
		byName[source.Repository] = source
	}
	observed := append([]workspace.Repository(nil), repositories...)
	for index := range observed {
		source, ok := byName[strings.TrimSpace(observed[index].Name)]
		if !ok {
			return Manifest{}, nil, fmt.Errorf("source observation repository %q missing from manifest", observed[index].Name)
		}
		observed[index].Commit = source.Observation.Commit
		observed[index].Branch = source.Observation.Branch
		observed[index].Dirty = source.Observation.Dirty
	}
	return manifest, observed, nil
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
	if expected.Version != actual.Version {
		return fmt.Errorf("%w: source observation schema changed from version %d to %d", ErrChanged, expected.Version, actual.Version)
	}
	if !bytes.Equal(mustEncode(expected.Composition), mustEncode(actual.Composition)) {
		return fmt.Errorf("%w: topology composition changed", ErrChanged)
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
	if len(expected.Sources) != len(actual.Sources) {
		return fmt.Errorf("%w: source count changed from %d to %d", ErrChanged, len(expected.Sources), len(actual.Sources))
	}
	return fmt.Errorf("%w: source observations differ in representation", ErrChanged)
}

// Validate checks that the persisted record is complete and each source state
// retains the deterministic topology observation identity it claims.
func (manifest Manifest) Validate() error {
	if manifest.Version != LegacyVersion && manifest.Version != CompositionVersion && manifest.Version != CurrentVersion {
		return fmt.Errorf("source observation version %d: want %d, %d or %d", manifest.Version, LegacyVersion, CompositionVersion, CurrentVersion)
	}
	if manifest.Version == LegacyVersion && manifest.Composition != nil {
		return errors.New("source observation version 1 must not contain a topology composition")
	}
	profile, err := topology.NewProfileID(strings.TrimSpace(manifest.Profile))
	if err != nil {
		return fmt.Errorf("source observation profile: %w", err)
	}
	if strings.TrimSpace(manifest.ResolverVersion) == "" {
		return errors.New("source observation resolver version is required")
	}
	if strings.TrimSpace(manifest.AnalyzerFingerprint) == "" {
		return errors.New("source observation analyzer fingerprint is required")
	}
	var composition topology.ProfileComposition
	if manifest.Composition != nil {
		var err error
		composition, err = manifest.Composition.ProfileComposition()
		if err != nil {
			return err
		}
		if composition.Profile.ID != profile {
			return fmt.Errorf("topology composition profile %q does not match source observation profile %q", composition.Profile.ID, manifest.Profile)
		}
		if manifest.Version == CompositionVersion && len(composition.OverlayWorktrees) != 0 {
			return errors.New("source observation version 2 must not contain worktree overlays")
		}
	}
	sources := make(map[string]Source, len(manifest.Sources))
	for index, source := range manifest.Sources {
		name := strings.TrimSpace(source.Repository)
		if name == "" {
			return fmt.Errorf("source observations[%d].repository: must not be empty", index)
		}
		if _, exists := sources[name]; exists {
			return fmt.Errorf("source observations[%d].repository %q: duplicate", index, name)
		}
		sources[name] = source
		if err := source.Observation.Validate(); err != nil {
			return fmt.Errorf("source observations[%d] %q: %w", index, name, err)
		}
	}
	if manifest.Composition == nil {
		return nil
	}
	selected := make(map[string]topology.WorktreeID, len(composition.Repositories))
	for index, repository := range composition.Repositories {
		name := string(repository.ID)
		source, exists := sources[name]
		if !exists {
			return fmt.Errorf("topology composition repositories[%d] %q has no observed source", index, name)
		}
		if source.Derived {
			return fmt.Errorf("topology composition repositories[%d] %q selects a derived source", index, name)
		}
		worktree := composition.Worktrees[index].ID
		if source.Observation.Worktree != worktree {
			return fmt.Errorf("topology composition repositories[%d] %q selects worktree %q, observed %q", index, name, worktree, source.Observation.Worktree)
		}
		selected[name] = worktree
	}
	for index, source := range manifest.Sources {
		if source.Derived {
			// Derived analyzer providers are additional, content-addressed inputs;
			// they have no profile-selectable Git worktree.
			continue
		}
		name := strings.TrimSpace(source.Repository)
		if _, exists := selected[name]; !exists {
			return fmt.Errorf("source observations[%d] %q is not selected by topology composition", index, source.Repository)
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
		return Manifest{}, fmt.Errorf("%w: decode source observations: %w", ErrInvalid, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Manifest{}, fmt.Errorf("%w: decode source observations: multiple documents", ErrInvalid)
		}
		return Manifest{}, fmt.Errorf("%w: decode source observations: %w", ErrInvalid, err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("%w: validate source observations: %w", ErrInvalid, err)
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
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !IsAnalyzedSource(relative) {
			return nil
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
	clean := filepath.ToSlash(filepath.Clean(name))
	lower := strings.ToLower(clean)
	base := filepath.Base(lower)
	if _, ok := analyzedManifests[lower]; ok {
		return true
	}
	if _, ok := analyzedManifests[base]; ok {
		return true
	}
	if strings.HasSuffix(lower, "/.dart_tool/package_config.json") {
		return true
	}
	if strings.HasPrefix(base, "requirements-") && strings.HasSuffix(base, ".txt") {
		return true
	}
	if (strings.HasPrefix(base, "tsconfig.") || strings.HasPrefix(base, "jsconfig.")) &&
		strings.HasSuffix(base, ".json") {
		return true
	}
	for _, suffix := range []string{".csproj", ".fsproj", ".vbproj", ".sln", ".slnx"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return config.HasSourceExtension(analyzedExtensions, name)
}

var analyzedExtensions = config.SourceExtensionSet(config.SupportedLanguages())

var analyzedManifests = map[string]struct{}{
	"go.mod": {}, "go.sum": {}, "go.work": {}, "go.work.sum": {},
	"package.json": {}, "pnpm-lock.yaml": {}, "pnpm-workspace.yaml": {}, "pnpm-workspace.yml": {},
	"package-lock.json": {}, "npm-shrinkwrap.json": {}, "yarn.lock": {},
	"cargo.toml": {}, "cargo.lock": {},
	"pyproject.toml": {}, "setup.py": {}, "setup.cfg": {}, "requirements.txt": {},
	"pipfile": {}, "pipfile.lock": {}, "poetry.lock": {}, "uv.lock": {},
	"pubspec.yaml": {}, "pubspec.lock": {}, "analysis_options.yaml": {},
	".dart_tool/package_config.json": {},
	"pom.xml":                        {}, "build.gradle": {}, "build.gradle.kts": {}, "settings.gradle": {},
	"settings.gradle.kts": {}, "gradle.properties": {}, "build.sbt": {},
	"directory.build.props": {}, "directory.build.targets": {},
	"directory.packages.props": {}, "nuget.config": {}, "global.json": {},
	"packages.lock.json": {},
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
