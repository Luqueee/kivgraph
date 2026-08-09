package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/testsupport"
)

func testEngine() TypeScriptEngine {
	return TypeScriptEngine{Version: "7.0.2", SupportedMin: "5.0.0", SupportedMax: "7.0.2"}
}

func newTestResolver(t *testing.T, root string, engine TypeScriptEngine) *TypeScriptVersionResolver {
	t.Helper()
	resolver, err := NewTypeScriptVersionResolver(Repository{Name: "repo", RealPath: root}, engine)
	if err != nil {
		t.Fatalf("NewTypeScriptVersionResolver() error = %v", err)
	}
	return resolver
}

// writeInstalledTypeScript materialises node_modules/typescript for one
// directory.
func writeInstalledTypeScript(t *testing.T, directory, version string) string {
	t.Helper()
	manifestPath := filepath.Join(directory, "node_modules", "typescript", "package.json")
	writeDiscoveryFile(t, manifestPath, `{"name": "typescript", "version": "`+version+`"}`)
	return manifestPath
}

func TestResolveTypeScriptVersionPrefersTheLocalInstall(t *testing.T) {
	root := testsupport.TempDir(t)
	core := filepath.Join(root, "packages", "core")
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root", "private": true, "devDependencies": {"typescript": "7.0.2"}}`)
	writeInstalledTypeScript(t, root, "7.0.2")
	writeDiscoveryFile(t, filepath.Join(core, "package.json"), `{"name": "core", "devDependencies": {"typescript": "^5.9.0"}}`)
	writeDiscoveryFile(t, filepath.Join(core, "tsconfig.json"), `{"compilerOptions": {}}`)
	localManifest := writeInstalledTypeScript(t, core, "5.9.3")

	resolver := newTestResolver(t, root, testEngine())
	resolved, err := resolver.Resolve(filepath.Join(core, "tsconfig.json"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Source != TypeScriptVersionLocal {
		t.Fatalf("Source = %q, want %q", resolved.Source, TypeScriptVersionLocal)
	}
	if resolved.Version != "5.9.3" {
		t.Fatalf("Version = %q, want the local install 5.9.3", resolved.Version)
	}
	if resolved.ManifestPath != localManifest {
		t.Fatalf("ManifestPath = %q, want %q", resolved.ManifestPath, localManifest)
	}
	// The declared range is evidence, never the answer.
	if resolved.Declared != "^5.9.0" {
		t.Fatalf("Declared = %q, want ^5.9.0", resolved.Declared)
	}
	if !resolved.WithinSupportedWindow {
		t.Fatalf("WithinSupportedWindow = false, reason %q", resolved.OutsideWindowReason)
	}
}

func TestResolveTypeScriptVersionFallsBackToTheWorkspaceInstall(t *testing.T) {
	root := testsupport.TempDir(t)
	core := filepath.Join(root, "packages", "core")
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root", "private": true, "workspaces": ["packages/*"]}`)
	hoisted := writeInstalledTypeScript(t, root, "6.4.1")
	writeDiscoveryFile(t, filepath.Join(core, "package.json"), `{"name": "core"}`)
	writeDiscoveryFile(t, filepath.Join(core, "tsconfig.json"), `{"compilerOptions": {}}`)

	resolver := newTestResolver(t, root, testEngine())
	resolved, err := resolver.Resolve(filepath.Join(core, "tsconfig.json"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Source != TypeScriptVersionWorkspace {
		t.Fatalf("Source = %q, want %q", resolved.Source, TypeScriptVersionWorkspace)
	}
	if resolved.Version != "6.4.1" || resolved.ManifestPath != hoisted {
		t.Fatalf("resolved = %#v, want the hoisted 6.4.1", resolved)
	}
	if resolved.Declared != "" || resolved.DeclaredBy != "" {
		t.Fatalf("Declared = %q by %q, want no declaration", resolved.Declared, resolved.DeclaredBy)
	}
}

func TestResolveTypeScriptVersionFallsBackToThePinnedCompiler(t *testing.T) {
	root := testsupport.TempDir(t)
	core := filepath.Join(root, "packages", "core")
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root", "private": true}`)
	writeDiscoveryFile(t, filepath.Join(core, "package.json"), `{"name": "core", "devDependencies": {"typescript": "^4.9.0"}}`)
	writeDiscoveryFile(t, filepath.Join(core, "tsconfig.json"), `{"compilerOptions": {}}`)

	resolver := newTestResolver(t, root, testEngine())
	resolved, err := resolver.Resolve(filepath.Join(core, "tsconfig.json"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Source != TypeScriptVersionPinned {
		t.Fatalf("Source = %q, want %q", resolved.Source, TypeScriptVersionPinned)
	}
	if resolved.Version != "7.0.2" {
		t.Fatalf("Version = %q, want the pinned 7.0.2", resolved.Version)
	}
	// Nothing is installed, so there is no manifest to point at. The declared
	// range is still recorded: it is what the project asked for and never got.
	if resolved.ManifestPath != "" {
		t.Fatalf("ManifestPath = %q, want empty for the pinned fallback", resolved.ManifestPath)
	}
	if resolved.Declared != "^4.9.0" {
		t.Fatalf("Declared = %q, want ^4.9.0", resolved.Declared)
	}
	if !resolved.WithinSupportedWindow {
		t.Fatalf("the pinned compiler must be inside its own window, reason %q", resolved.OutsideWindowReason)
	}
}

func TestResolveTypeScriptVersionStopsAtTheRepositoryRoot(t *testing.T) {
	outer := testsupport.TempDir(t)
	root := filepath.Join(outer, "repo")
	core := filepath.Join(root, "packages", "core")
	// A compiler installed above the registered repository must not decide the
	// confidence of its facts.
	writeInstalledTypeScript(t, outer, "5.0.0")
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root", "private": true}`)
	writeDiscoveryFile(t, filepath.Join(core, "package.json"), `{"name": "core"}`)
	writeDiscoveryFile(t, filepath.Join(core, "tsconfig.json"), `{"compilerOptions": {}}`)

	resolver := newTestResolver(t, root, testEngine())
	resolved, err := resolver.Resolve(filepath.Join(core, "tsconfig.json"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Source != TypeScriptVersionPinned {
		t.Fatalf("Source = %q, want %q: the walk left the repository", resolved.Source, TypeScriptVersionPinned)
	}
}

func TestResolveTypeScriptVersionFollowsAPnpmSymlink(t *testing.T) {
	root := testsupport.TempDir(t)
	store := filepath.Join(root, "node_modules", ".pnpm", "typescript@5.9.3", "node_modules", "typescript")
	writeDiscoveryFile(t, filepath.Join(store, "package.json"), `{"name": "typescript", "version": "5.9.3"}`)
	link := filepath.Join(root, "node_modules", "typescript")
	if err := os.Symlink(store, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root", "private": true}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions": {}}`)

	resolver := newTestResolver(t, root, testEngine())
	resolved, err := resolver.Resolve(filepath.Join(root, "tsconfig.json"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Version != "5.9.3" {
		t.Fatalf("Version = %q, want 5.9.3 through the symlink", resolved.Version)
	}
	// Classification follows where Node found the compiler, not where the
	// store keeps it.
	if resolved.Source != TypeScriptVersionLocal {
		t.Fatalf("Source = %q, want %q", resolved.Source, TypeScriptVersionLocal)
	}
}

func TestResolveTypeScriptVersionMarksVersionsOutsideTheWindow(t *testing.T) {
	engine := TypeScriptEngine{Version: "7.0.2", SupportedMin: "5.0.0", SupportedMax: "7.0.2"}
	cases := []struct {
		name     string
		version  string
		inWindow bool
		reason   string
	}{
		{name: "below minimum", version: "4.9.5", inWindow: false, reason: "below the supported minimum"},
		{name: "at minimum", version: "5.0.0", inWindow: true},
		{name: "at maximum", version: "7.0.2", inWindow: true},
		{name: "above maximum", version: "7.1.0", inWindow: false, reason: "above the supported maximum"},
		{name: "prerelease below its release", version: "7.0.2-dev.20260805", inWindow: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root", "private": true}`)
			writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions": {}}`)
			writeInstalledTypeScript(t, root, testCase.version)

			resolver := newTestResolver(t, root, engine)
			resolved, err := resolver.Resolve(filepath.Join(root, "tsconfig.json"))
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if resolved.WithinSupportedWindow != testCase.inWindow {
				t.Fatalf("WithinSupportedWindow = %v for %s, want %v (reason %q)",
					resolved.WithinSupportedWindow, testCase.version, testCase.inWindow, resolved.OutsideWindowReason)
			}
			if testCase.inWindow {
				if resolved.OutsideWindowReason != "" {
					t.Fatalf("OutsideWindowReason = %q, want empty inside the window", resolved.OutsideWindowReason)
				}
				return
			}
			if !strings.Contains(resolved.OutsideWindowReason, testCase.reason) {
				t.Fatalf("OutsideWindowReason = %q, want it to mention %q", resolved.OutsideWindowReason, testCase.reason)
			}
		})
	}
}

func TestResolveTypeScriptVersionRejectsAnUnparseableInstall(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root"}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions": {}}`)
	writeDiscoveryFile(t, filepath.Join(root, "node_modules", "typescript", "package.json"), `{"name": "typescript"`)

	resolver := newTestResolver(t, root, testEngine())
	if _, err := resolver.Resolve(filepath.Join(root, "tsconfig.json")); err == nil {
		t.Fatal("Resolve() accepted a broken compiler manifest")
	}
}

func TestResolveTypeScriptVersionRejectsAForeignPackageAtTheCompilerPath(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root"}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions": {}}`)
	writeDiscoveryFile(t, filepath.Join(root, "node_modules", "typescript", "package.json"), `{"name": "typescript-fork", "version": "9.9.9"}`)

	resolver := newTestResolver(t, root, testEngine())
	_, err := resolver.Resolve(filepath.Join(root, "tsconfig.json"))
	if err == nil {
		t.Fatal("Resolve() accepted a package that is not typescript")
	}
	if !strings.Contains(err.Error(), "typescript-fork") {
		t.Fatalf("error = %v, want it to name the foreign package", err)
	}
}

func TestResolveTypeScriptVersionRejectsPathsOutsideTheRepository(t *testing.T) {
	root := testsupport.TempDir(t)
	other := testsupport.TempDir(t)
	resolver := newTestResolver(t, root, testEngine())

	if _, err := resolver.Resolve(filepath.Join(other, "tsconfig.json")); err == nil {
		t.Fatal("Resolve() accepted a project outside the repository")
	}
	if _, err := resolver.Resolve("tsconfig.json"); err == nil {
		t.Fatal("Resolve() accepted a relative project path")
	}
}

func TestNewTypeScriptVersionResolverValidatesTheEngine(t *testing.T) {
	root := testsupport.TempDir(t)
	cases := []struct {
		name   string
		engine TypeScriptEngine
	}{
		{name: "missing version", engine: TypeScriptEngine{}},
		{name: "range as version", engine: TypeScriptEngine{Version: "^7.0.2"}},
		{name: "range as bound", engine: TypeScriptEngine{Version: "7.0.2", SupportedMin: ">=5"}},
		{name: "inverted window", engine: TypeScriptEngine{Version: "7.0.2", SupportedMin: "7.0.2", SupportedMax: "5.0.0"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewTypeScriptVersionResolver(Repository{RealPath: root}, testCase.engine); err == nil {
				t.Fatalf("NewTypeScriptVersionResolver() accepted %#v", testCase.engine)
			}
		})
	}
	if _, err := NewTypeScriptVersionResolver(Repository{RealPath: "relative"}, testEngine()); err == nil {
		t.Fatal("NewTypeScriptVersionResolver() accepted a relative repository path")
	}
}

func TestResolveProjectsCoversEveryDiscoveredProject(t *testing.T) {
	root := testsupport.TempDir(t)
	core := filepath.Join(root, "packages", "core")
	legacy := filepath.Join(root, "packages", "legacy")
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root", "private": true, "workspaces": ["packages/*"]}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions": {}}`)
	writeInstalledTypeScript(t, root, "7.0.2")
	writeDiscoveryFile(t, filepath.Join(core, "package.json"), `{"name": "core"}`)
	writeDiscoveryFile(t, filepath.Join(core, "tsconfig.json"), `{"compilerOptions": {}}`)
	writeDiscoveryFile(t, filepath.Join(legacy, "package.json"), `{"name": "legacy", "devDependencies": {"typescript": "4.9.5"}}`)
	writeDiscoveryFile(t, filepath.Join(legacy, "tsconfig.json"), `{"compilerOptions": {}}`)
	writeInstalledTypeScript(t, legacy, "4.9.5")

	discovery, err := DiscoverTypeScript(context.Background(), Repository{Name: "repo", RealPath: root})
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	resolver := newTestResolver(t, root, testEngine())
	resolved, err := resolver.ResolveProjects(discovery)
	if err != nil {
		t.Fatalf("ResolveProjects() error = %v", err)
	}
	if len(resolved) != len(discovery.Projects) || len(resolved) != 3 {
		t.Fatalf("ResolveProjects() returned %d entries, want 3", len(resolved))
	}

	byProject := make(map[string]TypeScriptProjectVersion, len(resolved))
	for index, entry := range resolved {
		if entry.ConfigPath != discovery.Projects[index].ConfigPath {
			t.Fatalf("entry %d = %q, want the discovery order %q", index, entry.ConfigPath, discovery.Projects[index].ConfigPath)
		}
		byProject[entry.ConfigPath] = entry
	}

	// Each project answers for itself: the hoisted compiler, the local old one
	// and the root install are three different answers in one repository.
	coreEntry := byProject[filepath.Join(core, "tsconfig.json")]
	if coreEntry.Source != TypeScriptVersionWorkspace || coreEntry.Version != "7.0.2" {
		t.Fatalf("core = %#v, want the hoisted 7.0.2", coreEntry)
	}
	legacyEntry := byProject[filepath.Join(legacy, "tsconfig.json")]
	if legacyEntry.Source != TypeScriptVersionLocal || legacyEntry.Version != "4.9.5" {
		t.Fatalf("legacy = %#v, want the local 4.9.5", legacyEntry)
	}
	if legacyEntry.WithinSupportedWindow {
		t.Fatal("legacy 4.9.5 must fall outside the supported window")
	}
	rootEntry := byProject[filepath.Join(root, "tsconfig.json")]
	if rootEntry.Source != TypeScriptVersionLocal || !rootEntry.WithinSupportedWindow {
		t.Fatalf("root = %#v, want a supported local install", rootEntry)
	}
}

func TestResolveTypeScriptVersionOpenWindowAcceptsEverything(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root"}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"compilerOptions": {}}`)
	writeInstalledTypeScript(t, root, "3.1.6")

	resolver := newTestResolver(t, root, TypeScriptEngine{Version: "7.0.2"})
	resolved, err := resolver.Resolve(filepath.Join(root, "tsconfig.json"))
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !resolved.WithinSupportedWindow {
		t.Fatalf("an open window rejected %s: %q", resolved.Version, resolved.OutsideWindowReason)
	}
}
