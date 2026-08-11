package indexing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Luqueee/ladygraph/internal/config"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/version"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

// Project identifies a repository that a caller wants to add to the registry
// and index. Paths are resolved relative to WorkingDirectory when they are not
// absolute; the source repository itself is never modified by this service.
type Project struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Languages []string `json:"languages"`
}

// ProjectResult reports the generation published after a project was indexed.
type ProjectResult struct {
	Project      Project      `json:"project"`
	GenerationID string       `json:"generation_id"`
	SnapshotID   uint64       `json:"snapshot_id"`
	Counts       Counts       `json:"counts"`
	Index        IndexSummary `json:"index"`
}

// IndexSummary retains the useful phase counts from the full index pass.
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
}

// ProjectIndexer is the mutation boundary used by the MCP tool. The caller
// must obtain explicit consent before invoking this method.
type ProjectIndexer interface {
	IndexProject(context.Context, Project) (ProjectResult, error)
}

// Service serializes registry mutations and full rebuilds while preserving the
// immutable snapshot publication contract used by MCP query tools.
type Service struct {
	gate             chan struct{}
	loaded           config.Loaded
	snapshotStore    *hotsnapshot.SnapshotStore
	resolverVersion  string
	workingDirectory string
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
	}
}

// IndexProject adds one repository to a candidate registry, persists that
// candidate, rebuilds the complete canonical graph, and publishes the
// resulting HotSnapshot. A failed index or rebuild restores the prior
// registry and leaves the prior generation active. If publication of the
// already-validated active graph fails, the candidate registry is retained and
// the error is returned so the caller can retry publication without reindexing.
func (service *Service) IndexProject(ctx context.Context, project Project) (ProjectResult, error) {
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
	select {
	case service.gate <- struct{}{}:
		defer func() { <-service.gate }()
	case <-ctx.Done():
		return ProjectResult{}, ctx.Err()
	}

	normalized, err := normalizeProject(project, service.workingDirectory)
	if err != nil {
		return ProjectResult{}, err
	}
	original := cloneRepositories(service.loaded.Repositories)
	candidate := cloneRepositories(service.loaded.Repositories)
	registered, err := upsertRepository(&candidate, config.Repository{
		Name:      normalized.Name,
		Path:      normalized.Path,
		Languages: append([]string(nil), normalized.Languages...),
	})
	if err != nil {
		return ProjectResult{}, err
	}

	registry, err := workspace.NewRegistry(ctx, candidate)
	if err != nil {
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

	fullResult, err := RunFull(ctx, FullOptions{
		Repositories:      registry.List(),
		SyntheticWorkFile: service.loaded.Config.Go.SyntheticWorkFile,
		IncludeTests:      service.loaded.Config.Go.IncludeTests,
		GoBuildTags:       service.loaded.Config.Go.BuildTags,
		GoAllowNetwork:    service.loaded.Config.Go.AllowNetwork,
		TypeScriptWorker:  service.loaded.Config.TypeScript.WorkerCommand,
		WorkingDirectory:  service.workingDirectory,
		Root:              filepath.Dir(service.loaded.Config.Storage.DatabasePath),
		ResolverVersion:   service.resolverVersion,
		Store:             generation.DefaultConfig(),
	})
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
		GenerationID: fullResult.RebuildReport.GenerationID,
		SnapshotID:   snapshotID,
		Counts:       fullResult.Counts,
		Index: IndexSummary{
			GoRepositories:         fullResult.IndexReport.GoRepositories,
			GoModules:              fullResult.IndexReport.GoModules,
			GoDefinitions:          fullResult.IndexReport.GoDefinitions,
			GoReferences:           fullResult.IndexReport.GoReferences,
			GoUnresolved:           fullResult.IndexReport.GoUnresolved,
			TypeScriptRepositories: fullResult.IndexReport.TypeScriptRepositories,
			TypeScriptSymbols:      fullResult.IndexReport.TypeScriptSymbols,
			TypeScriptReferences:   fullResult.IndexReport.TypeScriptReferences,
			TypeScriptUnresolved:   fullResult.IndexReport.TypeScriptUnresolved,
		},
	}, nil
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
	snapshot, report, err := rebuild.BuildSnapshot(ctx, rebuild.BuildSnapshotOptions{
		DatabasePath: layout.Active.DatabasePath,
		SnapshotID:   generationID,
	})
	if err != nil {
		return 0, fmt.Errorf("build published snapshot: %w", err)
	}
	if !report.Passed {
		return 0, errors.New("build published snapshot did not pass")
	}
	if err := service.snapshotStore.Publish(snapshot); err != nil {
		return 0, fmt.Errorf("publish HotSnapshot: %w", err)
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
	key := strings.ToLower(strings.TrimSpace(entry.Name))
	for index, existing := range registry.Repositories {
		if strings.ToLower(strings.TrimSpace(existing.Name)) != key {
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
		switch language {
		case "go", "typescript", "javascript", "ts", "js":
		default:
			return nil, fmt.Errorf("project languages[%d] unsupported language %q", index, value)
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
