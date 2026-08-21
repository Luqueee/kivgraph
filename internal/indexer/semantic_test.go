package indexer

import (
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
)

func TestResolveSemanticPackageDependenciesRequiresUniqueProvider(t *testing.T) {
	consumerRepository := facts.Repository{Key: facts.RepositoryKey("consumer"), Name: "consumer", RootPath: "/repos/consumer", Languages: []facts.Language{facts.LanguageDart}}
	providerRepository := facts.Repository{Key: facts.RepositoryKey("provider"), Name: "provider", RootPath: "/repos/provider", Languages: []facts.Language{facts.LanguageDart}}
	consumerPackage := facts.Package{Key: facts.PackageKey(facts.LanguageDart, "consumer", "consumer_app"), RepositoryKey: consumerRepository.Key, Language: facts.LanguageDart, Name: "consumer_app", RootPath: "/repos/consumer"}
	providerPackage := facts.Package{Key: facts.PackageKey(facts.LanguageDart, "provider", "shared_widgets"), RepositoryKey: providerRepository.Key, Language: facts.LanguageDart, Name: "shared_widgets", RootPath: "/repos/provider"}
	file := facts.File{Key: facts.FileKey("consumer", "lib/main.dart"), RepositoryKey: consumerRepository.Key, PackageKey: consumerPackage.Key, Language: facts.LanguageDart, Path: "lib/main.dart"}
	set := facts.Set{
		Repositories: []facts.Repository{consumerRepository, providerRepository},
		Packages:     []facts.Package{consumerPackage, providerPackage},
		Files:        []facts.File{file},
		Unresolved: []facts.UnresolvedReference{{
			RepositoryKey:    consumerRepository.Key,
			FileKey:          file.Key,
			Language:         facts.LanguageDart,
			RequestedPackage: "shared_widgets",
			RequestedSymbol:  "package:shared_widgets/widgets.dart",
			Reason:           "IMPORT_NOT_RESOLVED",
		}},
	}
	set.Sort()
	resolved := resolveSemanticPackageDependencies(set)
	found := false
	for _, edge := range resolved.Edges {
		if edge.Kind == facts.PackageDependsOn && edge.SourceKey == consumerPackage.Key && edge.TargetKey == providerPackage.Key {
			found = true
			if edge.Confidence != facts.ExactPackageMapped || edge.Provenance != facts.PackageManifest {
				t.Fatalf("package dependency = %#v, want package-mapped manifest evidence", edge)
			}
		}
	}
	if !found {
		t.Fatalf("resolved edges = %#v, want consumer dependency on shared_widgets", resolved.Edges)
	}
	if err := resolved.Validate(); err != nil {
		t.Fatalf("resolved semantic facts are invalid: %v", err)
	}
}

func TestResolveSemanticPackageDependenciesLeavesAmbiguousProviderUnresolved(t *testing.T) {
	consumerRepository := facts.Repository{Key: facts.RepositoryKey("consumer"), Name: "consumer", RootPath: "/repos/consumer", Languages: []facts.Language{facts.LanguageDart}}
	consumerPackage := facts.Package{Key: facts.PackageKey(facts.LanguageDart, "consumer", "consumer_app"), RepositoryKey: consumerRepository.Key, Language: facts.LanguageDart, Name: "consumer_app", RootPath: "/repos/consumer"}
	file := facts.File{Key: facts.FileKey("consumer", "lib/main.dart"), RepositoryKey: consumerRepository.Key, PackageKey: consumerPackage.Key, Language: facts.LanguageDart, Path: "lib/main.dart"}
	makeProvider := func(name string) facts.Package {
		repository := facts.Repository{Key: facts.RepositoryKey(name), Name: name, RootPath: "/repos/" + name, Languages: []facts.Language{facts.LanguageDart}}
		return facts.Package{Key: facts.PackageKey(facts.LanguageDart, name, "shared_widgets"), RepositoryKey: repository.Key, Language: facts.LanguageDart, Name: "shared_widgets", RootPath: repository.RootPath}
	}
	left, right := makeProvider("left"), makeProvider("right")
	set := facts.Set{
		Repositories: []facts.Repository{consumerRepository, {Key: left.RepositoryKey, Name: "left", RootPath: "/repos/left"}, {Key: right.RepositoryKey, Name: "right", RootPath: "/repos/right"}},
		Packages:     []facts.Package{consumerPackage, left, right},
		Files:        []facts.File{file},
		Unresolved: []facts.UnresolvedReference{{
			RepositoryKey:    consumerRepository.Key,
			FileKey:          file.Key,
			Language:         facts.LanguageDart,
			RequestedPackage: "shared_widgets",
			Reason:           "IMPORT_NOT_RESOLVED",
		}},
	}
	set.Sort()
	resolved := resolveSemanticPackageDependencies(set)
	for _, edge := range resolved.Edges {
		if edge.Kind == facts.PackageDependsOn {
			t.Fatalf("ambiguous provider created dependency: %#v", edge)
		}
	}
}
