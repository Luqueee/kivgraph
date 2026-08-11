package workspace

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const maxCargoManifestBytes = 16 << 20

// defaultCargoEdition is the edition Cargo assumes when a manifest declares
// none. It is recorded explicitly so a crate never carries an empty edition
// that a later reader would have to interpret again.
const defaultCargoEdition = "2015"

// defaultCargoVersion is the version Cargo assumes when a package declares
// none. It travels into the crate identity, so it is resolved here rather
// than left empty.
const defaultCargoVersion = "0.0.0"

// CargoDiscovery contains the Cargo workspaces, crates and lockfiles found in
// a registered repository.
//
// Every crate belongs to exactly one workspace: Cargo treats a package with no
// [workspace] section and no parent workspace as a workspace of one, and so
// does this discovery. That is what makes Workspaces the list of units an
// indexing pass can schedule without a second rule.
type CargoDiscovery struct {
	Workspaces []CargoWorkspace
	Crates     []CargoCrate
	LockFiles  []string
}

// CargoWorkspace is one root that Cargo resolves as a whole.
type CargoWorkspace struct {
	ManifestPath string
	RootPath     string
	// LockPath is the Cargo.lock beside the manifest, empty when absent.
	LockPath string
	// Virtual reports a manifest that declares [workspace] and no [package].
	Virtual bool
	// Members are the manifest paths of the crates this workspace resolves,
	// sorted, including the root crate when it has one.
	Members []string
}

// CargoCrate is one package declared by a Cargo.toml.
type CargoCrate struct {
	ManifestPath string
	RootPath     string
	// WorkspacePath is the manifest of the workspace that resolves this
	// crate. A standalone crate names its own manifest.
	WorkspacePath string
	Name          string
	// Version and Edition are already resolved: a member that inherits them
	// from [workspace.package] carries the inherited value, because the
	// version is part of the identity every cross-repository edge depends
	// on.
	Version string
	Edition string
}

// cargoManifest is one parsed Cargo.toml, before membership is resolved.
type cargoManifest struct {
	path      string
	directory string
	pkg       *cargoPackageSection
	workspace *cargoWorkspaceSection
}

type cargoManifestFile struct {
	Package   *cargoPackageSection   `toml:"package"`
	Workspace *cargoWorkspaceSection `toml:"workspace"`
}

type cargoPackageSection struct {
	Name    string           `toml:"name"`
	Version cargoInheritable `toml:"version"`
	Edition cargoInheritable `toml:"edition"`
	// Workspace names the directory of the workspace this package joins.
	Workspace string `toml:"workspace"`
}

type cargoWorkspaceSection struct {
	Members []string                   `toml:"members"`
	Exclude []string                   `toml:"exclude"`
	Package *cargoWorkspacePackageKeys `toml:"package"`
}

type cargoWorkspacePackageKeys struct {
	Version string `toml:"version"`
	Edition string `toml:"edition"`
}

// cargoInheritable is a manifest value spelled either as a literal string or
// as the table `{ workspace = true }`.
type cargoInheritable struct {
	Value     string
	Inherited bool
}

// UnmarshalTOML accepts the two spellings Cargo allows and rejects everything
// else. A malformed manifest is an error, never a silently empty value.
func (value *cargoInheritable) UnmarshalTOML(data any) error {
	switch typed := data.(type) {
	case string:
		value.Value = strings.TrimSpace(typed)
		return nil
	case map[string]any:
		inherited, ok := typed["workspace"].(bool)
		if !ok || !inherited {
			return fmt.Errorf("expected a string or { workspace = true }")
		}
		value.Inherited = true
		return nil
	default:
		return fmt.Errorf("expected a string or { workspace = true }")
	}
}

func readCargoManifest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCargoManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCargoManifestBytes {
		return nil, fmt.Errorf("manifest exceeds %d bytes", maxCargoManifestBytes)
	}
	return data, nil
}

func parseCargoManifest(manifestPath string) (cargoManifest, error) {
	data, err := readCargoManifest(manifestPath)
	if err != nil {
		return cargoManifest{}, err
	}
	var file cargoManifestFile
	if err := toml.Unmarshal(data, &file); err != nil {
		return cargoManifest{}, err
	}
	if file.Package == nil && file.Workspace == nil {
		return cargoManifest{}, fmt.Errorf("manifest declares neither [package] nor [workspace]")
	}
	if file.Package != nil && strings.TrimSpace(file.Package.Name) == "" {
		return cargoManifest{}, fmt.Errorf("[package] declares no name")
	}
	return cargoManifest{
		path:      manifestPath,
		directory: filepath.Dir(manifestPath),
		pkg:       file.Package,
		workspace: file.Workspace,
	}, nil
}

// resolveCargoCrate answers the crate a manifest declares, with the version
// and edition its workspace supplies when the manifest inherits them.
func resolveCargoCrate(manifest cargoManifest, workspaceManifest cargoManifest) (CargoCrate, error) {
	crate := CargoCrate{
		ManifestPath:  manifest.path,
		RootPath:      manifest.directory,
		WorkspacePath: workspaceManifest.path,
		Name:          strings.TrimSpace(manifest.pkg.Name),
	}
	var owner *cargoWorkspacePackageKeys
	if workspaceManifest.workspace != nil {
		owner = workspaceManifest.workspace.Package
	}

	version, err := resolveInheritable(manifest.pkg.Version, owner, "version")
	if err != nil {
		return CargoCrate{}, err
	}
	if version == "" {
		version = defaultCargoVersion
	}
	crate.Version = version

	edition, err := resolveInheritable(manifest.pkg.Edition, owner, "edition")
	if err != nil {
		return CargoCrate{}, err
	}
	if edition == "" {
		edition = defaultCargoEdition
	}
	crate.Edition = edition
	return crate, nil
}

func resolveInheritable(value cargoInheritable, owner *cargoWorkspacePackageKeys, field string) (string, error) {
	if !value.Inherited {
		return value.Value, nil
	}
	if owner == nil {
		return "", fmt.Errorf("[package] %s inherits from the workspace, which declares no [workspace.package]", field)
	}
	inherited := ""
	switch field {
	case "version":
		inherited = strings.TrimSpace(owner.Version)
	case "edition":
		inherited = strings.TrimSpace(owner.Edition)
	}
	if inherited == "" {
		return "", fmt.Errorf("[package] %s inherits from the workspace, which declares no %s", field, field)
	}
	return inherited, nil
}

func sortedCargoWorkspaces(workspaces map[string]CargoWorkspace) []CargoWorkspace {
	result := make([]CargoWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		workspace.Members = append([]string(nil), workspace.Members...)
		sort.Strings(workspace.Members)
		result = append(result, workspace)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].ManifestPath < result[right].ManifestPath
	})
	return result
}

func sortedCargoCrates(crates map[string]CargoCrate) []CargoCrate {
	result := make([]CargoCrate, 0, len(crates))
	for _, crate := range crates {
		result = append(result, crate)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].ManifestPath < result[right].ManifestPath
	})
	return result
}
