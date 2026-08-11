package rustloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/ladygraph/internal/testsupport"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

func writeCrate(t *testing.T, root, directory, name, version string) {
	t.Helper()
	target := filepath.Join(root, directory, "Cargo.toml")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(target), err)
	}
	manifest := "[package]\nname = \"" + name + "\"\nversion = \"" + version + "\"\nedition = \"2021\"\n"
	if err := os.WriteFile(target, []byte(manifest), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", target, err)
	}
}

func registryOver(t *testing.T, repositories ...workspace.Repository) *CrateRegistry {
	t.Helper()
	registry, err := NewCrateRegistry(context.Background(), repositories)
	if err != nil {
		t.Fatalf("NewCrateRegistry() error = %v", err)
	}
	return registry
}

func TestCrateRegistryResolvesOneProviderAtTheRequestedVersion(t *testing.T) {
	provider := testsupport.TempDir(t)
	consumer := testsupport.TempDir(t)
	writeCrate(t, provider, ".", "shared", "1.2.3")
	writeCrate(t, consumer, ".", "consumer", "0.1.0")

	registry := registryOver(t,
		workspace.Repository{Name: "provider", RealPath: provider},
		workspace.Repository{Name: "consumer", RealPath: consumer},
	)

	resolved, status := registry.Resolve("shared", "1.2.3")
	if status != CrateResolved {
		t.Fatalf("status = %q, want %q", status, CrateResolved)
	}
	if resolved.Repository != "provider" || resolved.ManifestPath != filepath.Join(provider, "Cargo.toml") {
		t.Fatalf("provider = %#v", resolved)
	}
	if names := registry.CrateNames(); len(names) != 2 || names[0] != "consumer" || names[1] != "shared" {
		t.Fatalf("CrateNames() = %v", names)
	}
}

// TestCrateRegistryRefusesToGuess is the contract that keeps a nominal match
// from becoming an exact edge: every answer other than a single provider at
// the exact version is a classified failure, not a best candidate.
func TestCrateRegistryRefusesToGuess(t *testing.T) {
	first := testsupport.TempDir(t)
	second := testsupport.TempDir(t)
	writeCrate(t, first, ".", "shared", "1.2.3")
	writeCrate(t, first, "other", "solo", "0.4.0")
	writeCrate(t, second, ".", "shared", "1.2.3")

	registry := registryOver(t,
		workspace.Repository{Name: "first", RealPath: first},
		workspace.Repository{Name: "second", RealPath: second},
	)

	tests := map[string]struct {
		crate   string
		version string
		want    CrateStatus
	}{
		"two providers at the same version":  {crate: "shared", version: "1.2.3", want: AmbiguousCrateProvider},
		"nobody declares it":                 {crate: "absent", version: "1.0.0", want: CrateProviderNotFound},
		"version rust-analyzer did not know": {crate: "solo", version: ".", want: CrateVersionUnknown},
		"empty version":                      {crate: "solo", version: "", want: CrateVersionUnknown},
		"another version":                    {crate: "solo", version: "0.5.0", want: CrateVersionMismatch},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			provider, status := registry.Resolve(test.crate, test.version)
			if status != test.want {
				t.Fatalf("status = %q, want %q", status, test.want)
			}
			if provider != (CrateProvider{}) {
				t.Fatalf("provider = %#v, want no provider", provider)
			}
		})
	}

	if providers := registry.Providers("shared"); len(providers) != 2 || providers[0].Repository != "first" || providers[1].Repository != "second" {
		t.Fatalf("Providers(shared) = %#v, want both, ordered by repository", providers)
	}
}

func TestCrateRegistryRejectsARepositoryWithoutAName(t *testing.T) {
	root := testsupport.TempDir(t)
	writeCrate(t, root, ".", "anything", "1.0.0")
	if _, err := NewCrateRegistry(context.Background(), []workspace.Repository{{RealPath: root}}); err == nil {
		t.Fatal("NewCrateRegistry() accepted a repository without a name")
	}
}
