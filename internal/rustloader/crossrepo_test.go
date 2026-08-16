package rustloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
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
	registry, err := NewCrateRegistry(context.Background(), repositories, nil)
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
	if _, err := NewCrateRegistry(context.Background(), []workspace.Repository{{RealPath: root}}, nil); err == nil {
		t.Fatal("NewCrateRegistry() accepted a repository without a name")
	}
}

// syntheticSysroot builds a sysroot provider over a directory laid out like the
// library workspace of a toolchain, so the registry tests never depend on one
// being installed.
func syntheticSysroot(t *testing.T, toolchain string, crates ...string) *SysrootProvider {
	t.Helper()
	root := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"),
		[]byte("[workspace]\nmembers = [\"core\"]\nresolver = \"2\"\n"), 0o600); err != nil {
		t.Fatalf("write workspace manifest: %v", err)
	}
	for _, crate := range crates {
		writeCrate(t, root, crate, crate, "0.0.0")
	}
	repository, err := workspace.NewSyntheticRepository(SyntheticRepositoryName(toolchain), root, []string{RustLanguage})
	if err != nil {
		t.Fatalf("NewSyntheticRepository() error = %v", err)
	}
	return &SysrootProvider{Repository: repository, Toolchain: toolchain, LibraryPath: repository.RealPath}
}

func sysrootRegistry(t *testing.T, sysroot *SysrootProvider, repositories ...workspace.Repository) *CrateRegistry {
	t.Helper()
	registry, err := NewCrateRegistry(context.Background(), repositories, sysroot)
	if err != nil {
		t.Fatalf("NewCrateRegistry() error = %v", err)
	}
	return registry
}

// TestCrateRegistryResolvesALangCrateByItsOrigin is the whole reason the sysroot
// can be a provider at all. The two sides of the boundary write different things
// in the version field of the moniker -- a consumer writes the origin URL and
// the library indexed as a workspace writes `0.0.0` -- so what is compared is
// the origin, which is evidence the analyzer produced, never the crate name.
func TestCrateRegistryResolvesALangCrateByItsOrigin(t *testing.T) {
	registry := sysrootRegistry(t, syntheticSysroot(t, "1.96.1", "core", "alloc"))

	provider, status := registry.Resolve("core", LangCrateOrigin+"core")
	if status != CrateResolved {
		t.Fatalf("Resolve(core, origin) = %q, want %q", status, CrateResolved)
	}
	if provider.Repository != "rust:1.96.1" || !provider.Lang {
		t.Fatalf("provider = %#v, want the synthetic sysroot", provider)
	}
	// A release is not an origin: the standard library has no version in the
	// moniker, so a reference that carries one was compiled against something
	// else and gets no edge.
	if _, status := registry.Resolve("core", "1.96.1"); status != CrateVersionMismatch {
		t.Fatalf("Resolve(core, release) = %q, want %q", status, CrateVersionMismatch)
	}
	// Name alone never resolves, here as everywhere else.
	if _, status := registry.Resolve("core", unknownCrateVersion); status != CrateVersionUnknown {
		t.Fatalf("Resolve(core, unknown) = %q, want %q", status, CrateVersionUnknown)
	}
	if _, status := registry.Resolve("std", LangCrateOrigin+"std"); status != CrateProviderNotFound {
		t.Fatalf("Resolve(std) = %q, want %q for a crate this toolchain does not carry", status, CrateProviderNotFound)
	}
}

// TestCrateRegistryDeclaresLangAmbiguity covers the two ways the standard
// library stops being the provider of its own crate: a registered repository
// that declares the same name, and two toolchains in one graph. Neither is
// resolved by preference.
func TestCrateRegistryDeclaresLangAmbiguity(t *testing.T) {
	shadow := testsupport.TempDir(t)
	writeCrate(t, shadow, ".", "core", "9.9.9")
	shadowRepository := workspace.Repository{Name: "shadow", RealPath: shadow}

	registry := sysrootRegistry(t, syntheticSysroot(t, "1.96.1", "core"), shadowRepository)
	if _, status := registry.Resolve("core", LangCrateOrigin+"core"); status != AmbiguousCrateProvider {
		t.Fatalf("Resolve(core) = %q, want %q when a repository shadows the standard library", status, AmbiguousCrateProvider)
	}
	// The registered crate is still reachable at its own version: the
	// ambiguity is about who provides the standard library, not about whose
	// code that repository holds.
	if provider, status := registry.Resolve("core", "9.9.9"); status != CrateResolved || provider.Repository != "shadow" {
		t.Fatalf("Resolve(core, 9.9.9) = %q %#v, want the registered repository", status, provider)
	}
}
