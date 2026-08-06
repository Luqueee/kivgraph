package goloader

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/goworkspace"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

func reasonsOf(unresolved []UnresolvedReference) map[UnresolvedReason][]UnresolvedReference {
	grouped := make(map[UnresolvedReason][]UnresolvedReference)
	for _, entry := range unresolved {
		grouped[entry.Reason] = append(grouped[entry.Reason], entry)
	}
	return grouped
}

func TestClassifyUnresolvedReportsProviderFailures(t *testing.T) {
	fixture := newCrossFixture(t, "duplicate")
	references := fixture.resolve(t)
	result, err := Load(context.Background(), Options{
		Directory: fixture.consumer,
		WorkFile:  fixture.workFile,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	unresolved, err := ClassifyUnresolved(context.Background(), result, references,
		UnresolvedOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ClassifyUnresolved() error = %v", err)
	}
	grouped := reasonsOf(unresolved)

	ambiguous := grouped[UnresolvedAmbiguousModuleProvider]
	if len(ambiguous) == 0 {
		t.Fatalf("ambiguity was not reported: %#v", unresolved)
	}
	entry := ambiguous[0]
	if entry.RequestedModulePath != "example.com/provider" || len(entry.Candidates) != 2 {
		t.Fatalf("ambiguous entry = %#v", entry)
	}
	if entry.Detail != "repositories: duplicate, provider" {
		t.Fatalf("ambiguous detail = %q", entry.Detail)
	}
	if entry.StartLine == 0 || entry.RequestedSymbol == "" {
		t.Fatalf("ambiguous entry lost its evidence: %#v", entry)
	}
}

func TestClassifyUnresolvedReportsMissingProvider(t *testing.T) {
	root := t.TempDir()
	provider := filepath.Join(root, "provider")
	consumer := filepath.Join(root, "consumer")
	writeFiles(t, provider, map[string]string{
		"go.mod": "module example.com/provider\n\ngo 1.24\n",
		"api/api.go": `package api

type hidden struct{}

// Do belongs to an unexported type reachable through New.
func (value hidden) Do() int { return 1 }

// New returns the unexported type.
func New() hidden { return hidden{} }
`,
	})
	writeFiles(t, consumer, map[string]string{
		"go.mod":  "module example.com/consumer\n\ngo 1.24\n",
		"main.go": "package main\n\nimport \"example.com/provider/api\"\n\nfunc main() { _ = api.New().Do() }\n",
	})
	repositories := []workspace.Repository{
		{Name: "provider", Path: provider, RealPath: provider},
		{Name: "consumer", Path: consumer, RealPath: consumer},
	}
	plan, err := goworkspace.BuildPlan(context.Background(), repositories, goworkspace.Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	workFile := filepath.Join(root, "state", "go.work")
	if _, err := goworkspace.Write(context.Background(), workFile, plan, repositories); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	result, err := Load(context.Background(), Options{Directory: consumer, WorkFile: workFile})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}

	registry, err := NewModuleRegistry(context.Background(), repositories)
	if err != nil {
		t.Fatalf("NewModuleRegistry() error = %v", err)
	}
	references, err := ResolveCrossRepository(context.Background(), uses, registry,
		CrossRepositoryOptions{ConsumerRepository: "consumer"})
	if err != nil {
		t.Fatalf("ResolveCrossRepository() error = %v", err)
	}
	unresolved, err := ClassifyUnresolved(context.Background(), result, references,
		UnresolvedOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ClassifyUnresolved() error = %v", err)
	}
	grouped := reasonsOf(unresolved)

	// The checker addresses a method of an unexported type through the
	// exported API that exposes it, so nothing is unresolved here.
	if len(grouped[UnresolvedObjectPathUnavailable]) != 0 {
		t.Fatalf("unexpected object path failures = %#v", grouped[UnresolvedObjectPathUnavailable])
	}

	// Without the provider repository registered the module has no owner.
	onlyConsumer, err := NewModuleRegistry(context.Background(), repositories[1:])
	if err != nil {
		t.Fatalf("NewModuleRegistry() error = %v", err)
	}
	missing, err := ResolveCrossRepository(context.Background(), uses, onlyConsumer,
		CrossRepositoryOptions{ConsumerRepository: "consumer"})
	if err != nil {
		t.Fatalf("ResolveCrossRepository() error = %v", err)
	}
	missingUnresolved, err := ClassifyUnresolved(context.Background(), result, missing,
		UnresolvedOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ClassifyUnresolved() error = %v", err)
	}
	if len(reasonsOf(missingUnresolved)[UnresolvedModuleProviderNotFound]) == 0 {
		t.Fatalf("missing provider was not reported: %#v", missingUnresolved)
	}
}

// TestClassifyUnresolvedMapsObjectPathFailures exercises the classifier for a
// target the resolver could not address. The resolver reaches this state only
// when go/types offers no path at all, so the status is injected directly.
func TestClassifyUnresolvedMapsObjectPathFailures(t *testing.T) {
	references := []CrossRepositoryReference{{
		Use: Use{
			PackagePath:         "example.com/consumer",
			FileName:            "/workspace/consumer/main.go",
			TargetModulePath:    "example.com/provider",
			TargetPackagePath:   "example.com/provider/api",
			TargetQualifiedName: "Box.Unwrap",
			Offset:              42,
			StartLine:           7,
			StartColumn:         12,
		},
		Status: ObjectPathUnavailable,
	}}

	unresolved, err := ClassifyUnresolved(context.Background(), Result{}, references,
		UnresolvedOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ClassifyUnresolved() error = %v", err)
	}
	if len(unresolved) != 1 {
		t.Fatalf("unresolved = %#v", unresolved)
	}
	entry := unresolved[0]
	if entry.Reason != UnresolvedObjectPathUnavailable ||
		entry.RequestedSymbol != "Box.Unwrap" ||
		entry.RequestedModulePath != "example.com/provider" ||
		entry.StartLine != 7 {
		t.Fatalf("entry = %#v", entry)
	}
}

func TestClassifyUnresolvedReportsLoadAndWorkspaceFailures(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod": "module example.com/module\n\ngo 1.24\n\nrequire example.com/absent v1.0.0\n",
		"broken/broken.go": `package broken

import "example.com/absent/pkg"

// Value does not typecheck and its import cannot be loaded.
var Value int = "text"

var _ = pkg.Anything
`,
	})

	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	unresolved, err := ClassifyUnresolved(context.Background(), result, nil, UnresolvedOptions{
		Repository: "fixture",
		WorkspaceConflicts: []goworkspace.Conflict{
			{
				Kind:         goworkspace.ReplaceConflict,
				Subject:      "example.com/pinned",
				Repositories: []string{"first", "second"},
				Details:      []string{"first => v1.0.0", "second => v2.0.0"},
			},
			{
				Kind:    goworkspace.AmbiguousModule,
				Subject: "example.com/duplicated",
			},
		},
	})
	if err != nil {
		t.Fatalf("ClassifyUnresolved() error = %v", err)
	}
	grouped := reasonsOf(unresolved)

	notLoaded := grouped[UnresolvedPackageNotLoaded]
	if len(notLoaded) != 1 || notLoaded[0].RequestedPackagePath != "example.com/absent/pkg" {
		t.Fatalf("package not loaded = %#v", notLoaded)
	}
	if notLoaded[0].StartLine == 0 || notLoaded[0].Detail == "" {
		t.Fatalf("import failure lost its evidence: %#v", notLoaded[0])
	}

	failed := grouped[UnresolvedTypecheckFailed]
	if len(failed) == 0 {
		t.Fatalf("type errors were not reported: %#v", unresolved)
	}
	if !strings.HasPrefix(failed[0].Detail, "TYPE: ") || failed[0].StartLine == 0 {
		t.Fatalf("typecheck evidence = %#v", failed[0])
	}

	replaces := grouped[UnresolvedReplaceConflict]
	if len(replaces) != 1 || replaces[0].RequestedModulePath != "example.com/pinned" {
		t.Fatalf("replace conflicts = %#v", replaces)
	}
	if replaces[0].Detail != "first => v1.0.0; second => v2.0.0" {
		t.Fatalf("replace detail = %q", replaces[0].Detail)
	}
	// Ambiguous module conflicts of the workspace are reported per reference,
	// not duplicated here.
	if len(grouped[UnresolvedAmbiguousModuleProvider]) != 0 {
		t.Fatalf("workspace ambiguity was duplicated: %#v", grouped[UnresolvedAmbiguousModuleProvider])
	}
}

func TestClassifyUnresolvedIsCancellable(t *testing.T) {
	fixture := newCrossFixture(t)
	result, err := Load(context.Background(), Options{
		Directory: fixture.consumer,
		WorkFile:  fixture.workFile,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ClassifyUnresolved(ctx, result, nil, UnresolvedOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ClassifyUnresolved() error = %v, want context.Canceled", err)
	}
}
