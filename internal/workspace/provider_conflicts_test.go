package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectProviderConflictsClassifiesPackageAndModuleConflicts(t *testing.T) {
	repositoryA := makeProviderRepository(t, "repo-a", "1.0.0", "dep-a")
	repositoryB := makeProviderRepository(t, "repo-b", "2.0.0", "dep-b")

	report, err := DetectProviderConflicts(context.Background(), []Repository{repositoryA, repositoryB})
	if err != nil {
		t.Fatalf("DetectProviderConflicts() error = %v", err)
	}
	if !report.HasConflicts() {
		t.Fatal("DetectProviderConflicts() reported no conflicts")
	}
	conflicts := report.List()
	if len(conflicts) != 4 {
		t.Fatalf("conflicts = %#v, want four classified conflicts", conflicts)
	}
	wantKinds := []ProviderConflictKind{
		AmbiguousModuleProvider,
		AmbiguousPackageProvider,
		ModuleReplaceConflict,
		PackageVersionMismatch,
	}
	wantProviders := []string{
		"example.com/shared",
		"@example/shared",
		"example.com/shared",
		"@example/shared",
	}
	for index, kind := range wantKinds {
		if conflicts[index].Kind != kind || conflicts[index].Provider != wantProviders[index] {
			t.Fatalf("conflicts[%d] = %#v, want kind %q for %q", index, conflicts[index], kind, wantProviders[index])
		}
	}
	for _, conflict := range conflicts {
		if len(conflict.Repositories) != 2 || conflict.Repositories[0] != "repo-a" || conflict.Repositories[1] != "repo-b" {
			t.Fatalf("repositories for %s = %#v", conflict.Kind, conflict.Repositories)
		}
		if len(conflict.ManifestPaths) != 2 {
			t.Fatalf("manifest paths for %s = %#v", conflict.Kind, conflict.ManifestPaths)
		}
		if conflict.Provider == "@example/shared" && conflict.Kind == PackageVersionMismatch && len(conflict.Versions) != 2 {
			t.Fatalf("package versions = %#v", conflict.Versions)
		}
	}

	conflicts[0].Repositories[0] = "mutated"
	if report.List()[0].Repositories[0] == "mutated" {
		t.Fatal("ProviderConflictReport.List() returned mutable internal state")
	}
}

func TestDetectProviderConflictsReportsOnlyAmbiguityWhenMetadataMatches(t *testing.T) {
	repositoryA := makeProviderRepository(t, "repo-a", "1.0.0", "")
	repositoryB := makeProviderRepository(t, "repo-b", "1.0.0", "")

	report, err := DetectProviderConflicts(context.Background(), []Repository{repositoryA, repositoryB})
	if err != nil {
		t.Fatalf("DetectProviderConflicts() error = %v", err)
	}
	conflicts := report.List()
	if len(conflicts) != 2 {

		t.Fatalf("conflicts = %#v, want only package and module ambiguity", conflicts)
	}
	if conflicts[0].Kind != AmbiguousModuleProvider || conflicts[1].Kind != AmbiguousPackageProvider {
		t.Fatalf("conflict kinds = %#v", conflicts)
	}
}
func TestDetectProviderConflictsFindsReplacementConflictsAcrossDistinctModules(t *testing.T) {
	repositoryA := makeModuleReplacementRepository(t, "repo-a", "@example/one", "example.com/one", "dep-a")
	repositoryB := makeModuleReplacementRepository(t, "repo-b", "@example/two", "example.com/two", "dep-b")

	report, err := DetectProviderConflicts(context.Background(), []Repository{repositoryA, repositoryB})
	if err != nil {
		t.Fatalf("DetectProviderConflicts() error = %v", err)
	}
	conflicts := report.List()
	if len(conflicts) != 1 || conflicts[0].Kind != ModuleReplaceConflict || conflicts[0].Provider != "example.com/dependency" {
		t.Fatalf("conflicts = %#v, want one dependency replacement conflict", conflicts)
	}
	if len(conflicts[0].Repositories) != 2 || len(conflicts[0].ManifestPaths) != 2 {
		t.Fatalf("replacement conflict locations = %#v", conflicts[0])
	}
}

func TestDetectProviderConflictsReportsNoConflictForUniqueProviders(t *testing.T) {
	repositoryA := makeProviderRepository(t, "repo-a", "1.0.0", "")
	rootB := t.TempDir()
	writeDiscoveryFile(t, filepath.Join(rootB, "package.json"), `{"name":"@example/other","version":"1.0.0"}`)
	writeGoDiscoveryFile(t, filepath.Join(rootB, "go.mod"), "module example.com/other\n\ngo 1.24\n")

	report, err := DetectProviderConflicts(context.Background(), []Repository{repositoryA, {Name: "repo-b", RealPath: rootB}})
	if err != nil {
		t.Fatalf("DetectProviderConflicts() error = %v", err)
	}
	if report.HasConflicts() || len(report.List()) != 0 {
		t.Fatalf("unique providers produced conflicts: %#v", report.List())
	}
}

func TestDetectProviderConflictsValidatesRepositoriesAndCancellation(t *testing.T) {
	root := t.TempDir()
	repository := Repository{Name: "repo", RealPath: root}
	if _, err := DetectProviderConflicts(context.Background(), []Repository{{RealPath: root}}); err == nil {
		t.Fatal("missing repository name was accepted")
	}
	if _, err := DetectProviderConflicts(context.Background(), []Repository{repository, repository}); err == nil {
		t.Fatal("duplicate repository name was accepted")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := DetectProviderConflicts(ctx, []Repository{repository})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled detector error = %v, want context.Canceled", err)
	}
}

func makeProviderRepository(t *testing.T, name, version, replacementDirectory string) Repository {
	t.Helper()
	root := t.TempDir()
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"@example/shared","version":"`+version+`"}`)
	module := "module example.com/shared\n\ngo 1.24\n"
	if replacementDirectory != "" {
		if err := os.MkdirAll(filepath.Join(root, replacementDirectory), 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		module += "replace example.com/dependency => ./" + replacementDirectory + "\n"
	}
	writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), module)
	return Repository{Name: name, RealPath: root}
}
func makeModuleReplacementRepository(t *testing.T, name, packageName, modulePath, replacementDirectory string) Repository {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, replacementDirectory), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name":"`+packageName+`","version":"1.0.0"}`)
	writeGoDiscoveryFile(t, filepath.Join(root, "go.mod"), "module "+modulePath+"\n\ngo 1.24\nreplace example.com/dependency => ./"+replacementDirectory+"\n")
	return Repository{Name: name, RealPath: root}
}
