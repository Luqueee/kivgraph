package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func writeCargoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

// TestDiscoverCargoResolvesWorkspaceMembersAndInheritance covers the shape a
// real Cargo repository has: a virtual root, a member that spells out its own
// version, a member that inherits it, and a crate reached only as a path
// dependency. The last one is the reason membership follows directories: if it
// became a workspace of its own, the pass would load the same workspace twice.
func TestDiscoverCargoResolvesWorkspaceMembersAndInheritance(t *testing.T) {
	root := testsupport.TempDir(t)
	writeCargoFile(t, filepath.Join(root, "Cargo.toml"), `[workspace]
members = ["crates/engine"]
resolver = "2"

[workspace.package]
version = "1.4.0"
edition = "2021"
`)
	writeCargoFile(t, filepath.Join(root, "Cargo.lock"), "version = 4\n")
	writeCargoFile(t, filepath.Join(root, "crates", "engine", "Cargo.toml"), `[package]
name = "engine"
version.workspace = true
edition.workspace = true

[dependencies]
support = { path = "../support" }
`)
	writeCargoFile(t, filepath.Join(root, "crates", "engine", "src", "lib.rs"), "pub fn run() {}\n")
	writeCargoFile(t, filepath.Join(root, "crates", "support", "Cargo.toml"), `[package]
name = "support"
version = "0.2.1"
edition = "2018"
`)
	writeCargoFile(t, filepath.Join(root, "target", "debug", "Cargo.toml"), "[package]\nname = \"artifact\"\n")

	discovery, err := DiscoverCargo(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("DiscoverCargo() error = %v", err)
	}
	if len(discovery.Workspaces) != 1 {
		t.Fatalf("Workspaces = %#v, want the single root workspace", discovery.Workspaces)
	}
	workspace := discovery.Workspaces[0]
	if workspace.ManifestPath != filepath.Join(root, "Cargo.toml") || !workspace.Virtual {
		t.Fatalf("workspace = %#v, want the virtual root", workspace)
	}
	if workspace.LockPath != filepath.Join(root, "Cargo.lock") {
		t.Fatalf("workspace lock = %q", workspace.LockPath)
	}
	wantMembers := []string{
		filepath.Join(root, "crates", "engine", "Cargo.toml"),
		filepath.Join(root, "crates", "support", "Cargo.toml"),
	}
	if strings.Join(workspace.Members, "|") != strings.Join(wantMembers, "|") {
		t.Fatalf("members = %#v, want %#v", workspace.Members, wantMembers)
	}
	if len(discovery.Crates) != 2 {
		t.Fatalf("Crates = %#v, want two crates", discovery.Crates)
	}
	engine := discovery.Crates[0]
	support := discovery.Crates[1]
	if engine.Name != "engine" || engine.Version != "1.4.0" || engine.Edition != "2021" {
		t.Fatalf("engine crate = %#v, want the inherited version and edition", engine)
	}
	if engine.WorkspacePath != workspace.ManifestPath {
		t.Fatalf("engine workspace = %q", engine.WorkspacePath)
	}
	if support.Name != "support" || support.Version != "0.2.1" || support.Edition != "2018" {
		t.Fatalf("support crate = %#v", support)
	}
	if len(discovery.LockFiles) != 1 {
		t.Fatalf("LockFiles = %#v", discovery.LockFiles)
	}
}

// TestDiscoverCargoSeparatesExcludedAndStandaloneCrates proves the two ways a
// crate stops being part of the workspace above it, because each one costs a
// separate unit in the indexing pass.
func TestDiscoverCargoSeparatesExcludedAndStandaloneCrates(t *testing.T) {
	root := testsupport.TempDir(t)
	writeCargoFile(t, filepath.Join(root, "workspace", "Cargo.toml"), `[workspace]
members = ["member"]
exclude = ["fixtures/standalone"]
`)
	writeCargoFile(t, filepath.Join(root, "workspace", "member", "Cargo.toml"), `[package]
name = "member"
version = "0.1.0"
edition = "2021"
`)
	writeCargoFile(t, filepath.Join(root, "workspace", "fixtures", "standalone", "Cargo.toml"), `[package]
name = "standalone"
version = "9.9.9"
`)
	writeCargoFile(t, filepath.Join(root, "tool", "Cargo.toml"), `[package]
name = "tool"
`)

	discovery, err := DiscoverCargo(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("DiscoverCargo() error = %v", err)
	}
	if len(discovery.Workspaces) != 3 {
		t.Fatalf("Workspaces = %#v, want the workspace plus two standalone crates", discovery.Workspaces)
	}
	byManifest := make(map[string]CargoWorkspace, len(discovery.Workspaces))
	for _, workspace := range discovery.Workspaces {
		byManifest[workspace.ManifestPath] = workspace
	}
	shared := byManifest[filepath.Join(root, "workspace", "Cargo.toml")]
	if len(shared.Members) != 1 || shared.Members[0] != filepath.Join(root, "workspace", "member", "Cargo.toml") {
		t.Fatalf("workspace members = %#v, want only the listed member", shared.Members)
	}
	standalone := byManifest[filepath.Join(root, "workspace", "fixtures", "standalone", "Cargo.toml")]
	if standalone.Virtual || len(standalone.Members) != 1 {
		t.Fatalf("excluded crate workspace = %#v", standalone)
	}
	tool := byManifest[filepath.Join(root, "tool", "Cargo.toml")]
	if len(tool.Members) != 1 {
		t.Fatalf("standalone crate workspace = %#v", tool)
	}
	byName := make(map[string]CargoCrate, len(discovery.Crates))
	for _, crate := range discovery.Crates {
		byName[crate.Name] = crate
	}
	if byName["standalone"].WorkspacePath != standalone.ManifestPath {
		t.Fatalf("excluded crate = %#v", byName["standalone"])
	}
	// Cargo defaults a package with no version to 0.0.0 and no edition to
	// 2015. The crate identity depends on both, so neither may stay empty.
	if byName["tool"].Version != "0.0.0" || byName["tool"].Edition != "2015" {
		t.Fatalf("defaulted crate = %#v", byName["tool"])
	}
}

func TestDiscoverCargoRejectsManifestsItCannotResolve(t *testing.T) {
	tests := map[string]struct {
		setup func(root string)
		want  string
	}{
		"inherited version without workspace keys": {
			setup: func(root string) {
				writeCargoFile(t, filepath.Join(root, "Cargo.toml"), "[workspace]\nmembers = [\"member\"]\n")
				writeCargoFile(t, filepath.Join(root, "member", "Cargo.toml"), "[package]\nname = \"member\"\nversion.workspace = true\n")
			},
			want: "inherits from the workspace",
		},
		"package without a name": {
			setup: func(root string) {
				writeCargoFile(t, filepath.Join(root, "Cargo.toml"), "[package]\nversion = \"1.0.0\"\n")
			},
			want: "declares no name",
		},
		// Cargo calls this «multiple workspace roots found in the same
		// workspace»: the inner manifest is a member and a root at once.
		"nested workspace claimed as a member": {
			setup: func(root string) {
				writeCargoFile(t, filepath.Join(root, "Cargo.toml"), "[workspace]\nmembers = [\"inner\"]\n")
				writeCargoFile(t, filepath.Join(root, "inner", "Cargo.toml"), "[workspace]\n\n[package]\nname = \"inner\"\nversion = \"0.1.0\"\n")
			},
			want: "is a workspace root and a member of the workspace",
		},
		"nested workspace claimed by a glob": {
			setup: func(root string) {
				writeCargoFile(t, filepath.Join(root, "Cargo.toml"), "[workspace]\nmembers = [\"crates/*\"]\n")
				writeCargoFile(t, filepath.Join(root, "crates", "inner", "Cargo.toml"), "[workspace]\n\n[package]\nname = \"inner\"\nversion = \"0.1.0\"\n")
			},
			want: "is a workspace root and a member of the workspace",
		},
		"unparseable manifest": {
			setup: func(root string) {
				writeCargoFile(t, filepath.Join(root, "Cargo.toml"), "[package\nname = \"broken\"\n")
			},
			want: "parse Cargo manifest",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			test.setup(root)
			_, err := DiscoverCargo(context.Background(), Repository{RealPath: root})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DiscoverCargo() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDiscoverCargoHonoursRegistryExclusions(t *testing.T) {
	root := testsupport.TempDir(t)
	writeCargoFile(t, filepath.Join(root, "Cargo.toml"), "[package]\nname = \"kept\"\nversion = \"1.0.0\"\n")
	writeCargoFile(t, filepath.Join(root, "examples", "generated", "Cargo.toml"), "[package]\nname = \"generated\"\nversion = \"1.0.0\"\n")

	discovery, err := DiscoverCargo(context.Background(), Repository{
		RealPath:   root,
		Exclusions: []string{"examples/**"},
	})
	if err != nil {
		t.Fatalf("DiscoverCargo() error = %v", err)
	}
	if len(discovery.Crates) != 1 || discovery.Crates[0].Name != "kept" {
		t.Fatalf("Crates = %#v, want only the crate outside the exclusion", discovery.Crates)
	}
}

// TestCargoExcludesAnswersForAPathDiscoveryNeverWalked keeps the boundary of a
// repository one decision. Discovery skips a directory and never reads the
// manifests below it; anything that indexes those files afterwards asks this
// question over a path, and a different answer publishes code from a crate
// this repository does not declare.
func TestCargoExcludesAnswersForAPathDiscoveryNeverWalked(t *testing.T) {
	root := testsupport.TempDir(t)
	tests := map[string]struct {
		path       string
		exclusions []string
		want       bool
	}{
		"source file":            {path: "src/lib.rs"},
		"vendored crate":         {path: "vendor/songbird/src/id.rs", want: true},
		"build output":           {path: "target/debug/build/generated.rs", want: true},
		"nested vendor":          {path: "crates/engine/vendor/fork/src/lib.rs", want: true},
		"configured exclusion":   {path: "examples/generated/src/lib.rs", exclusions: []string{"examples/**"}, want: true},
		"named like a directory": {path: "src/vendor.rs"},
		"outside the repository": {path: "../elsewhere/src/lib.rs"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := filepath.Join(root, filepath.FromSlash(test.path))
			excluded := CargoExcludes(root, candidate, test.exclusions)
			if excluded != test.want {
				t.Fatalf("CargoExcludes(%q, %q) = %t, want %t", test.path, test.exclusions, excluded, test.want)
			}
		})
	}
}

func TestCargoExcludesCheckedRejectsInvalidExclusionPattern(t *testing.T) {
	root := testsupport.TempDir(t)
	candidate := filepath.Join(root, "src", "lib.rs")
	exclusions := []string{"["}
	_, err := CargoExcludesChecked(root, candidate, exclusions)
	if err == nil || !strings.Contains(err.Error(), "exclusions[0]") {
		t.Fatalf("CargoExcludesChecked() candidate=%q exclusions=%q error = %v, want invalid exclusion error", candidate, exclusions, err)
	}
}

func TestCargoExcludesDefaultExcludedDirectoryItself(t *testing.T) {
	root := testsupport.TempDir(t)
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("create Cargo output directory %q: %v", target, err)
	}
	excluded, err := CargoExcludesChecked(root, target, nil)
	if err != nil {
		t.Fatalf("CargoExcludesChecked(%q) error = %v", target, err)
	}
	if !excluded {
		t.Fatalf("CargoExcludesChecked(%q) = false, want default output directory excluded", target)
	}
}

// TestDiscoverCargoAcceptsAVendoredWorkspaceRoot is the shape of the Rust
// standard library: `library/backtrace` declares its own `[workspace]` inside
// `library/`, is not one of its members and is not excluded either. Cargo builds
// that tree every day, so discovery has to see two independent workspaces rather
// than reject the repository.
func TestDiscoverCargoAcceptsAVendoredWorkspaceRoot(t *testing.T) {
	root := testsupport.TempDir(t)
	writeCargoFile(t, filepath.Join(root, "Cargo.toml"), `[workspace]
members = ["std"]
exclude = ["stdarch"]
resolver = "2"
`)
	writeCargoFile(t, filepath.Join(root, "std", "Cargo.toml"), "[package]\nname = \"std\"\nversion = \"0.0.0\"\n")
	writeCargoFile(t, filepath.Join(root, "backtrace", "Cargo.toml"), `[workspace]
members = ["crates/helper"]

[package]
name = "backtrace"
version = "0.3.0"
`)
	writeCargoFile(t, filepath.Join(root, "backtrace", "crates", "helper", "Cargo.toml"),
		"[package]\nname = \"helper\"\nversion = \"0.3.0\"\n")

	discovery, err := DiscoverCargo(context.Background(), Repository{RealPath: root})
	if err != nil {
		t.Fatalf("DiscoverCargo() error = %v", err)
	}
	if len(discovery.Workspaces) != 2 {
		t.Fatalf("Workspaces = %#v, want the outer one and the vendored root", discovery.Workspaces)
	}
	owners := make(map[string]string, len(discovery.Crates))
	for _, crate := range discovery.Crates {
		owners[crate.Name] = crate.WorkspacePath
	}
	outer := filepath.Join(root, "Cargo.toml")
	vendored := filepath.Join(root, "backtrace", "Cargo.toml")
	if owners["std"] != outer {
		t.Fatalf("std belongs to %q, want %q", owners["std"], outer)
	}
	// The vendored root resolves its own members: claiming them from above
	// would publish a membership no build agrees with.
	if owners["backtrace"] != vendored || owners["helper"] != vendored {
		t.Fatalf("vendored crates = %#v, want both under %q", owners, vendored)
	}
}
