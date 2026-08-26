// Package indexing drives a full index as a service: it runs the pass in a
// detached process, reports progress on the wire, and keeps a snapshot store
// following what the pass published.
package indexing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/version"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// Project identifies a repository that a caller wants to add to the registry
// and index. Paths are resolved relative to WorkingDirectory when they are not
// absolute; the source repository itself is never modified by this service.
type Project struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Languages []string `json:"languages"`
}

// ProjectResult reports the generation published after a batch was indexed.
type ProjectResult struct {
	// Project is the first project of the batch, kept so a single-project
	// caller reads its own request back.
	Project      Project      `json:"project"`
	Projects     []Project    `json:"projects"`
	GenerationID string       `json:"generation_id"`
	SnapshotID   uint64       `json:"snapshot_id"`
	Counts       Counts       `json:"counts"`
	Index        IndexSummary `json:"index"`
}

// IndexSummary retains the useful phase counts from the full index pass.
//
// These count what each language pass **observed**, and Counts on the same
// result counts what the published graph **holds**. They differ on purpose and
// they differ by language: a file belonging to two packages -- `pkg` and
// `pkg.test` -- is observed twice and stored once, so on kena with
// include_tests: true GoDefinitions runs 1.63x over the graph's Go symbols and
// GoUnresolved 1.58x, TypeScript symbols 1.14x, and Rust exactly 1.00x because
// it loads one pass per workspace. With include_tests: false the Go unresolved
// counts match, 4397 either way.
//
// Neither number is wrong and neither is deduplicated here. Observations are
// the only figure that says how much work the pass did; the graph is the only
// one that says what a query will find. A reader comparing the two and
// expecting them to agree is comparing those two different things.
type IndexSummary struct {
	GoRepositories         int `json:"go_repositories"`
	GoModules              int `json:"go_modules"`
	GoDefinitions          int `json:"go_definitions"`
	GoReferences           int `json:"go_references"`
	GoUnresolved           int `json:"go_unresolved"`
	TypeScriptRepositories int `json:"typescript_repositories"`
	TypeScriptSymbols      int `json:"typescript_symbols"`
	TypeScriptReferences   int `json:"typescript_references"`
	TypeScriptUnresolved   int `json:"typescript_unresolved"`
	RustRepositories       int `json:"rust_repositories"`
	RustWorkspaces         int `json:"rust_workspaces"`
	RustSymbols            int `json:"rust_symbols"`
	RustReferences         int `json:"rust_references"`
	RustUnresolved         int `json:"rust_unresolved"`
	PythonRepositories     int `json:"python_repositories"`
	PythonSymbols          int `json:"python_symbols"`
	PythonReferences       int `json:"python_references"`
	PythonUnresolved       int `json:"python_unresolved"`
	DartRepositories       int `json:"dart_repositories"`
	DartSymbols            int `json:"dart_symbols"`
	DartReferences         int `json:"dart_references"`
	DartUnresolved         int `json:"dart_unresolved"`
	// The not-loaded counters are the ones that keep silence from reading as
	// coverage. A repository or module whose facts are absent contributes zero
	// symbols, and zero symbols is also what a language with no code
	// contributes: without these, a caller cannot tell an empty repository from
	// one this machine could not read.
	//
	// All four are here because the reason differs and the consequence does
	// not: a Cargo workspace the analyzer could not read, a Go module that did
	// not load, and a Python or Dart repository whose analyzer is not installed
	// on this machine.
	RustWorkspacesNotLoaded     int `json:"rust_workspaces_not_loaded"`
	GoModulesNotLoaded          int `json:"go_modules_not_loaded"`
	PythonRepositoriesNotLoaded int `json:"python_repositories_not_loaded"`
	DartRepositoriesNotLoaded   int `json:"dart_repositories_not_loaded"`
}

// ProjectIndexer is the mutation boundary used by the MCP tool. The caller
// must obtain explicit consent before invoking this method.
//
// The whole batch is registered before anything is built. Every project costs
// one full rebuild of the canonical graph -- cross-repository edges are
// resolved over the complete fact set, so there is no cheaper unit -- and
// rebuilding once per project throws away all but the last result. Eleven
// projects registered one call at a time cost eleven rebuilds for one useful
// graph; registered together they cost one.
type ProjectIndexer interface {
	IndexProjects(context.Context, []Project, func(ProjectProgress)) (ProjectResult, error)
}

// ProjectProgress is one step of a project index.
//
// A full rebuild takes minutes on a large registry, and an MCP client applies
// its own timeout to a request. Without a sign of life it cancels a call that
// is working, so every unit of work reports one.
type ProjectProgress struct {
	Phase      string `json:"phase,omitempty"`
	Repository string `json:"repository,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Completed  int    `json:"completed,omitempty"`
	Total      int    `json:"total,omitempty"`
}

// Service serializes registry mutations and full rebuilds while preserving the
// immutable snapshot publication contract used by MCP query tools.
type Service struct {
	gate             chan struct{}
	loaded           config.Loaded
	snapshotStore    *hotsnapshot.SnapshotStore
	resolverVersion  string
	workingDirectory string

	// index runs the full pass, out of this process. It is a field so a test
	// can substitute the child it would otherwise spawn, the same way
	// rebuild.BuildSnapshotOptions.Scan stands in for the canonical reader.
	index func(context.Context, DetachedOptions) (FullDocument, error)
}

// NewService creates a project indexer over the configured registry and
// snapshot store. A nil snapshot store is accepted so construction remains
// cheap, but IndexProject will reject it before changing the registry.
func NewService(
	loaded config.Loaded,
	snapshotStore *hotsnapshot.SnapshotStore,
	resolverVersion string,
	workingDirectory string,
) *Service {
	if resolverVersion == "" {
		resolverVersion = version.Value
	}
	if workingDirectory == "" {
		workingDirectory, _ = os.Getwd()
	}
	return &Service{
		gate:             make(chan struct{}, 1),
		loaded:           cloneLoaded(loaded),
		snapshotStore:    snapshotStore,
		resolverVersion:  resolverVersion,
		workingDirectory: workingDirectory,
		index:            RunDetached,
	}
}

// IndexProjects adds every project to a candidate registry, persists that
// candidate, rebuilds the complete canonical graph once, and publishes the
// resulting HotSnapshot. A failed index or rebuild restores the prior
// registry and leaves the prior generation active. If publication of the
// already-validated active graph fails, the candidate registry is retained and
// the error is returned so the caller can retry publication without
// reindexing.
//
// The batch is the unit on purpose. A rebuild resolves cross-repository edges
// over the complete fact set, so it costs the whole corpus whatever was added;
// doing it once per project throws away every result but the last.
func (service *Service) IndexProjects(
	ctx context.Context,
	projects []Project,
	progress func(ProjectProgress),
) (ProjectResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ProjectResult{}, err
	}
	if service == nil {
		return ProjectResult{}, errors.New("project indexer is nil")
	}
	if service.snapshotStore == nil {
		return ProjectResult{}, errors.New("project indexer has no snapshot store")
	}
	if len(projects) == 0 {
		return ProjectResult{}, errors.New("no project was requested")
	}
	select {
	case service.gate <- struct{}{}:
		defer func() { <-service.gate }()
	case <-ctx.Done():
		return ProjectResult{}, ctx.Err()
	}

	original := cloneRepositories(service.loaded.Repositories)
	candidate := cloneRepositories(service.loaded.Repositories)
	normalizedProjects := make([]Project, 0, len(projects))
	registered := false
	for index, project := range projects {
		normalized, err := normalizeProject(project, service.workingDirectory)
		if err != nil {
			return ProjectResult{}, fmt.Errorf("project %d: %w", index+1, err)
		}
		changed, err := upsertRepository(&candidate, config.Repository{
			Name:      normalized.Name,
			Path:      normalized.Path,
			Languages: append([]string(nil), normalized.Languages...),
		})
		if err != nil {
			return ProjectResult{}, err
		}
		registered = registered || changed
		normalizedProjects = append(normalizedProjects, normalized)
	}
	normalized := normalizedProjects[0]

	// The registry is validated here and read from disk by the child, which is
	// why the validated value is not carried any further: persisting it below
	// is what hands the batch over.
	if _, err := workspace.NewRegistry(ctx, candidate); err != nil {
		return ProjectResult{}, fmt.Errorf("validate project registry: %w", err)
	}
	// A registration that changes nothing leaves the file alone, so nothing
	// has to be restored if the index that follows fails.
	if registered {
		if err := config.SaveRepositories(service.loaded.RepositoriesPath, candidate); err != nil {
			return ProjectResult{}, fmt.Errorf("persist project registry: %w", err)
		}
		service.loaded.Repositories = candidate
	}

	document, err := service.runIndex(ctx, progress)
	if err != nil {
		if !registered {
			return ProjectResult{}, err
		}
		if restoreErr := config.SaveRepositories(service.loaded.RepositoriesPath, original); restoreErr != nil {
			return ProjectResult{}, errors.Join(
				err,
				fmt.Errorf("restore project registry: %w", restoreErr),
			)
		}
		service.loaded.Repositories = original
		return ProjectResult{}, err
	}

	snapshotID, err := service.publishActiveSnapshot(ctx)
	if err != nil {
		return ProjectResult{}, fmt.Errorf("publish project snapshot: %w", err)
	}

	return ProjectResult{
		Project:      normalized,
		Projects:     normalizedProjects,
		GenerationID: document.GenerationID,
		SnapshotID:   snapshotID,
		Counts:       document.Counts,
		Index:        document.Index,
	}, nil
}

// Reindex rebuilds the graph of the repositories already registered and
// publishes it. It never touches the registry: keeping the graph on the code
// that is checked out is not the same act as registering a project, and the
// consent for it was given when the repository was registered.
//
// It shares the gate with IndexProjects, so a resynchronisation and an
// index_project cannot run at once inside one process.
func (service *Service) Reindex(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if service == nil {
		return errors.New("project indexer is nil")
	}
	if service.snapshotStore == nil {
		return errors.New("project indexer has no snapshot store")
	}
	select {
	case service.gate <- struct{}{}:
		defer func() { <-service.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}

	registry, err := workspace.NewRegistry(ctx, service.loaded.Repositories)
	if err != nil {
		return fmt.Errorf("validate repository registry: %w", err)
	}
	repositories := registry.List()
	if len(repositories) == 0 {
		return nil
	}

	if _, err := service.runIndex(ctx, nil); err != nil {
		return fmt.Errorf("reindex registered repositories: %w", err)
	}
	if _, err := service.publishActiveSnapshot(ctx); err != nil {
		return fmt.Errorf("publish reindexed snapshot: %w", err)
	}
	return nil
}

// runIndex indexes the registered repositories in a child process.
//
// Both callers do exactly this, and neither may do it in this process: a full
// pass peaks in gigabytes, and a server that once allocated them keeps the
// arena for as long as it runs. The child's peak dies with the child. See
// ADR 0042.
func (service *Service) runIndex(
	ctx context.Context,
	progress func(ProjectProgress),
) (FullDocument, error) {
	run := service.index
	if run == nil {
		run = RunDetached
	}
	return run(ctx, DetachedOptions{
		ConfigPath:       service.loaded.ConfigPath,
		RepositoriesPath: service.loaded.RepositoriesPath,
		ResolverVersion:  service.resolverVersion,
		WorkingDirectory: service.workingDirectory,
		Progress:         progress,
		// The child logs what a loader reported without failing the pass, and
		// this is where a server's own records already go.
		Log: os.Stderr,
	})
}

func (service *Service) publishActiveSnapshot(ctx context.Context) (uint64, error) {
	layout, err := rebuild.Roles(ctx, rebuild.LayoutOptions{
		Root:  filepath.Dir(service.loaded.Config.Storage.DatabasePath),
		Store: generation.DefaultConfig(),
	})
	if err != nil {
		return 0, fmt.Errorf("resolve published generation: %w", err)
	}
	if layout.Active.ID == "" {
		return 0, errors.New("rebuild published no active generation")
	}
	generationID, err := parseSnapshotID(layout.Active.ID)
	if err != nil {
		return 0, err
	}
	// The child that ran the pass wrote the snapshot into the generation it
	// published, so the parent reads it instead of scanning the graph again.
	snapshot, report, err := rebuild.LoadOrBuildSnapshot(ctx, rebuild.BuildSnapshotOptions{
		DatabasePath: layout.Active.DatabasePath,
		SnapshotID:   generationID,
	})
	// The build's inputs die here whether or not the publication below wins.
	defer rebuild.ReturnBuildMemory()
	if err != nil {
		return 0, fmt.Errorf("build published snapshot: %w", err)
	}
	if !report.Passed {
		return 0, errors.New("build published snapshot did not pass")
	}
	// Losing this race is success, not failure. The generation follower runs
	// in the same process and installs whatever CURRENT points at; when it
	// gets there first, the store is already serving exactly the snapshot
	// this rebuild produced. Reporting an error here would make the caller
	// retry a rebuild that already landed.
	if err := service.snapshotStore.Publish(snapshot); err != nil {
		if !errors.Is(err, hotsnapshot.ErrSnapshotGeneration) {
			return 0, fmt.Errorf("publish HotSnapshot: %w", err)
		}
		// By identifier, not by snapshot: this arm runs when a concurrent
		// publisher won, and mapping the graph to confirm it would do the work
		// a deferred store exists to postpone.
		if served, serving := service.snapshotStore.ActiveID(); !serving || served < generationID {
			return 0, fmt.Errorf("publish HotSnapshot: %w", err)
		}
	}
	return generationID, nil
}

func parseSnapshotID(value string) (uint64, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse snapshot generation %q: %w", value, err)
	}
	return parsed, nil
}

// upsertRepository places entry in the registry and reports whether the
// registry changed.
//
// Indexing a project that is already registered is not a conflict: the caller
// asked for that repository to be in the graph, and it is. Re-registering it
// with the same directory replaces the entry -- the languages of the request
// are the ones asked for -- and an identical request changes nothing at all.
// Only a name already held by a different directory is a real conflict,
// because then nothing can decide which of the two the name means.
func upsertRepository(registry *config.RepositoriesFile, entry config.Repository) (bool, error) {
	// The name is matched exactly, the same way workspace.ValidatePaths
	// compares it: two repositories whose names differ only in case are
	// two repositories, and a monorepo can hold both.
	key := strings.TrimSpace(entry.Name)
	for index, existing := range registry.Repositories {
		if strings.TrimSpace(existing.Name) != key {
			continue
		}
		if filepath.Clean(existing.Path) != filepath.Clean(entry.Path) {
			return false, fmt.Errorf(
				"project %q is already registered at %q: choose another name or remove that entry",
				entry.Name, existing.Path)
		}
		if sameRepositoryEntry(existing, entry) {
			return false, nil
		}
		// The request cannot express exclusions, so the ones already on
		// file survive it. Dropping them would silently widen the index
		// to directories the operator excluded on purpose.
		entry.Exclusions = append([]string(nil), existing.Exclusions...)
		registry.Repositories[index] = entry
		return true, nil
	}
	registry.Repositories = append(registry.Repositories, entry)
	return true, nil
}

// sameRepositoryEntry compares what a registration decides. Exclusions belong
// to the entry already on file: the project request cannot express them, so
// re-registering must not drop them.
func sameRepositoryEntry(existing, entry config.Repository) bool {
	if len(existing.Languages) != len(entry.Languages) {
		return false
	}
	for index := range existing.Languages {
		if existing.Languages[index] != entry.Languages[index] {
			return false
		}
	}
	return true
}

func normalizeProject(project Project, workingDirectory string) (Project, error) {
	project.Name = strings.TrimSpace(project.Name)
	if project.Name == "" {
		return Project{}, errors.New("project name must not be empty")
	}
	if project.Name == "." || project.Name == ".." || strings.ContainsAny(project.Name, `/\\`) {
		return Project{}, fmt.Errorf("project name %q is not a valid repository identifier", project.Name)
	}
	project.Path = strings.TrimSpace(project.Path)
	if project.Path == "" {
		return Project{}, errors.New("project path must not be empty")
	}
	if strings.HasPrefix(project.Path, "~/") || project.Path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Project{}, fmt.Errorf("resolve project home path: %w", err)
		}
		project.Path = filepath.Join(home, strings.TrimPrefix(project.Path, "~/"))
	}
	if !filepath.IsAbs(project.Path) {
		if workingDirectory == "" {
			return Project{}, errors.New("project path is relative but working directory is unavailable")
		}
		project.Path = filepath.Join(workingDirectory, project.Path)
	}
	absolute, err := filepath.Abs(project.Path)
	if err != nil {
		return Project{}, fmt.Errorf("resolve project path: %w", err)
	}
	project.Path = filepath.Clean(absolute)
	info, err := os.Stat(project.Path)
	if err != nil {
		return Project{}, fmt.Errorf("inspect project path: %w", err)
	}
	if !info.IsDir() {
		return Project{}, fmt.Errorf("project path %q is not a directory", project.Path)
	}

	languages, err := normalizeProjectLanguages(project.Languages)
	if err != nil {
		return Project{}, err
	}
	project.Languages = languages
	return project, nil
}

func normalizeProjectLanguages(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("project languages must contain at least one language")
	}
	languages := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		language := strings.ToLower(strings.TrimSpace(value))
		if language == "" {
			return nil, fmt.Errorf("project languages[%d] must not be empty", index)
		}
		if !config.SupportedLanguage(language) {
			return nil, fmt.Errorf("project languages[%d] unsupported language %q, want one of %s",
				index, value, strings.Join(config.SupportedLanguages(), ", "))
		}
		if _, exists := seen[language]; exists {
			return nil, fmt.Errorf("project languages[%d] duplicate language %q", index, value)
		}
		seen[language] = struct{}{}
		languages = append(languages, language)
	}
	return languages, nil
}

func cloneLoaded(loaded config.Loaded) config.Loaded {
	loaded.Repositories = cloneRepositories(loaded.Repositories)
	return loaded
}

func cloneRepositories(source config.RepositoriesFile) config.RepositoriesFile {
	copyFile := source
	copyFile.Repositories = make([]config.Repository, len(source.Repositories))
	for index, repository := range source.Repositories {
		copyFile.Repositories[index] = repository
		copyFile.Repositories[index].Languages = append([]string(nil), repository.Languages...)
		copyFile.Repositories[index].Manifests = append([]string(nil), repository.Manifests...)
		copyFile.Repositories[index].Roots = append([]string(nil), repository.Roots...)
		copyFile.Repositories[index].Exclusions = append([]string(nil), repository.Exclusions...)
	}
	return copyFile
}
