package version

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
	"github.com/Luqueee/kivgraph/internal/syntax"
)

// Provenance is the stable machine-readable output of `kivgraph version
// --json`. Fields unavailable outside a distribution bundle are represented as
// null rather than guessed values.
type Provenance struct {
	Kivgraph           string            `json:"kivgraph"`
	Commit             *string           `json:"commit"`
	Dirty              *bool             `json:"dirty"`
	Go                 string            `json:"go"`
	Node               *string           `json:"node"`
	TypeScript         *string           `json:"typescript"`
	TypeScriptFallback *string           `json:"typescript_fallback"`
	Ladybug            string            `json:"ladybug"`
	GoLadybug          string            `json:"go_ladybug"`
	Schema             int               `json:"schema"`
	SnapshotRowFormat  uint32            `json:"snapshot_row_format"`
	Resolver           *string           `json:"resolver"`
	Grammars           GrammarProvenance `json:"grammars"`
	// RustAnalyzer is the engine the bundle carries for Rust. A development
	// binary ships none and reports null: the analyzer it would run comes
	// from the PATH and is not part of this build's provenance.
	RustAnalyzer *string `json:"rust_analyzer"`
}

// GrammarProvenance identifies the pinned grammar manifest and its entries.
type GrammarProvenance struct {
	Manifest string           `json:"manifest"`
	SHA256   *string          `json:"sha256"`
	Versions []GrammarVersion `json:"versions"`
}

// GrammarVersion is the compact grammar identity exposed by provenance.
type GrammarVersion struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

type bundleManifest struct {
	ManifestVersion int    `json:"manifest_version"`
	Product         string `json:"product"`
	Release         string `json:"release"`
	Target          struct {
		OS   string `json:"os"`
		Arch string `json:"arch"`
	} `json:"target"`
	Source struct {
		Commit string `json:"commit"`
		Dirty  bool   `json:"dirty"`
	} `json:"source"`
	Toolchain struct {
		Go         string `json:"go"`
		Node       string `json:"node"`
		PNPM       string `json:"pnpm"`
		TypeScript string `json:"typescript"`
	} `json:"toolchain"`
	LadybugDB struct {
		Core          string `json:"core"`
		Binding       string `json:"binding"`
		ArchiveSHA256 string `json:"archive_sha256"`
		LibrarySHA256 string `json:"library_sha256"`
	} `json:"ladybugdb"`
	Schema struct {
		Canonical         int    `json:"canonical"`
		SnapshotRowFormat uint32 `json:"snapshot_row_format"`
	} `json:"schema"`
	ResolverVersion *string `json:"resolver_version"`
	Grammars        struct {
		Manifest string `json:"manifest"`
		SHA256   string `json:"sha256"`
	} `json:"grammars"`
	Tools struct {
		Manifest     string `json:"manifest"`
		SHA256       string `json:"sha256"`
		RustAnalyzer struct {
			Version string `json:"version"`
			Release string `json:"release"`
			Binary  string `json:"binary"`
			SHA256  string `json:"sha256"`
		} `json:"rust_analyzer"`
	} `json:"tools"`
	Artifacts []json.RawMessage `json:"artifacts"`
}

// Collect describes the running process, and never the directory it was invoked
// from. Provenance comes from the manifest of the bundle this executable lives
// in -- resolved from the executable path, not from workingDir, which only
// locates the grammar manifest of a development checkout. A manifest found
// there that names another release or another target does not describe this
// process and is refused rather than preferred.
//
// The version is always the compiled Value: a manifest can enrich provenance,
// never rename the binary. Without a bundle manifest the unavailable values are
// explicit nulls, because a guess read off a neighbouring directory is what this
// output exists to avoid.
func Collect(executablePath, workingDir string) (Provenance, error) {
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return Provenance{}, fmt.Errorf("get working directory: %w", err)
		}
	}
	workingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return Provenance{}, fmt.Errorf("resolve working directory: %w", err)
	}

	fallbackRoot := findGrammarRoot(workingDir)
	fallback, err := fallbackProvenance(fallbackRoot)
	if err != nil {
		return Provenance{}, err
	}

	manifestPath, bundleRoot, found, err := findBundleManifest(executablePath)
	if err != nil {
		return Provenance{}, err
	}
	if !found {
		return fallback, nil
	}
	return loadBundleProvenance(manifestPath, bundleRoot)
}

func fallbackProvenance(root string) (Provenance, error) {
	provenance := Provenance{
		Kivgraph:          Value,
		Go:                runtime.Version(),
		Ladybug:           ladybug.CoreVersion,
		GoLadybug:         ladybug.GoBindingVersion,
		Schema:            ladybug.CanonicalSchemaVersion,
		SnapshotRowFormat: rebuild.SnapshotRowFormatVersion,
		Grammars: GrammarProvenance{
			Manifest: "grammars/manifest.json",
			Versions: []GrammarVersion{},
		},
	}
	applyBuildInfo(&provenance)

	if typeScriptVersion, err := readTypeScriptVersion(root); err != nil {
		return Provenance{}, err
	} else if typeScriptVersion != "" {
		provenance.TypeScriptFallback = stringPointer(typeScriptVersion)
	}

	grammarPath := filepath.Join(root, filepath.FromSlash(provenance.Grammars.Manifest))
	if _, err := os.Stat(grammarPath); err == nil {
		loaded, loadErr := loadGrammarProvenance(root, provenance.Grammars.Manifest, "", true)
		if loadErr != nil {
			return Provenance{}, loadErr
		}
		provenance.Grammars = loaded
	} else if !errors.Is(err, os.ErrNotExist) {
		return Provenance{}, fmt.Errorf("stat grammar manifest %q: %w", grammarPath, err)
	}
	return provenance, nil
}

func applyBuildInfo(provenance *Provenance) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if commit := settings["vcs.revision"]; commit != "" {
		provenance.Commit = stringPointer(commit)
	}
	if modified, exists := settings["vcs.modified"]; exists {
		value := modified == "true"
		provenance.Dirty = &value
	}
}

func readTypeScriptVersion(root string) (string, error) {
	if root == "" {
		return "", nil
	}
	data, err := os.ReadFile(filepath.Join(root, "ts-worker", "package.json"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read TypeScript package manifest: %w", err)
	}
	var packageManifest struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &packageManifest); err != nil {
		return "", fmt.Errorf("decode TypeScript package manifest: %w", err)
	}
	return packageManifest.Version, nil
}

func findGrammarRoot(start string) string {
	current := start
	for {
		manifestPath := filepath.Join(current, filepath.FromSlash(syntax.DefaultManifestPath))
		if info, err := os.Stat(manifestPath); err == nil && !info.IsDir() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return start
		}
		current = parent
	}
}

func findBundleManifest(executablePath string) (path, root string, found bool, err error) {
	candidates := make([]struct{ path, root string }, 0, 1)
	if executablePath != "" {
		executable, absErr := filepath.Abs(executablePath)
		if absErr != nil {
			return "", "", false, fmt.Errorf("resolve executable path: %w", absErr)
		}
		if resolved, evalErr := filepath.EvalSymlinks(executable); evalErr == nil {
			executable = resolved
		} else if !errors.Is(evalErr, os.ErrNotExist) {
			return "", "", false, fmt.Errorf("resolve executable symlinks: %w", evalErr)
		}
		bundleRoot := filepath.Dir(filepath.Dir(executable))
		candidates = append(candidates, struct{ path, root string }{
			path: filepath.Join(bundleRoot, "manifest.json"), root: bundleRoot,
		})
	}
	for _, candidate := range candidates {
		info, statErr := os.Stat(candidate.path)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return "", "", false, fmt.Errorf("stat bundle manifest %q: %w", candidate.path, statErr)
		}
		if info.IsDir() {
			return "", "", false, fmt.Errorf("bundle manifest path %q is a directory", candidate.path)
		}
		return candidate.path, candidate.root, true, nil
	}
	return "", "", false, nil
}

func loadBundleProvenance(manifestPath, bundleRoot string) (Provenance, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Provenance{}, fmt.Errorf("read bundle manifest %q: %w", manifestPath, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest bundleManifest
	if err := decoder.Decode(&manifest); err != nil {
		return Provenance{}, fmt.Errorf("decode bundle manifest %q: %w", manifestPath, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Provenance{}, fmt.Errorf("decode bundle manifest %q: trailing JSON value", manifestPath)
		}
		return Provenance{}, fmt.Errorf("decode bundle manifest %q: trailing data: %w", manifestPath, err)
	}
	if err := validateBundleManifest(manifest); err != nil {
		return Provenance{}, fmt.Errorf("validate bundle manifest %q: %w", manifestPath, err)
	}
	grammars, err := loadGrammarProvenance(bundleRoot, manifest.Grammars.Manifest, manifest.Grammars.SHA256, true)
	if err != nil {
		return Provenance{}, err
	}
	return Provenance{
		Kivgraph:           Value,
		Commit:             stringPointer(manifest.Source.Commit),
		Dirty:              boolPointer(manifest.Source.Dirty),
		Go:                 manifest.Toolchain.Go,
		Node:               optionalStringPointer(manifest.Toolchain.Node),
		TypeScript:         optionalStringPointer(manifest.Toolchain.TypeScript),
		TypeScriptFallback: optionalStringPointer(manifest.Toolchain.TypeScript),
		Ladybug:            manifest.LadybugDB.Core,
		GoLadybug:          manifest.LadybugDB.Binding,
		Schema:             manifest.Schema.Canonical,
		SnapshotRowFormat:  manifest.Schema.SnapshotRowFormat,
		Resolver:           manifest.ResolverVersion,
		Grammars:           grammars,
		RustAnalyzer:       optionalStringPointer(manifest.Tools.RustAnalyzer.Release),
	}, nil
}

func validateBundleManifest(manifest bundleManifest) error {
	if manifest.ManifestVersion != 1 {
		return fmt.Errorf("manifest_version: want 1, got %d", manifest.ManifestVersion)
	}
	if manifest.Product != "kivgraph" {
		return fmt.Errorf("product: want %q, got %q", "kivgraph", manifest.Product)
	}
	if manifest.Release == "" || manifest.Source.Commit == "" {
		return errors.New("release and source.commit are required")
	}
	if manifest.Target.OS != runtime.GOOS || manifest.Target.Arch != runtime.GOARCH {
		return fmt.Errorf("target: want %s/%s, got %s/%s", runtime.GOOS, runtime.GOARCH, manifest.Target.OS, manifest.Target.Arch)
	}
	if manifest.Release != Value {
		return fmt.Errorf("release: want %s, got %s", Value, manifest.Release)
	}
	if manifest.Toolchain.Go == "" || manifest.LadybugDB.Core == "" || manifest.LadybugDB.Binding == "" {
		return errors.New("toolchain.go, ladybugdb.core and ladybugdb.binding are required")
	}
	if manifest.Schema.Canonical <= 0 || manifest.Schema.SnapshotRowFormat == 0 {
		return errors.New("schema versions must be positive")
	}
	if manifest.Grammars.Manifest == "" {
		return errors.New("grammars.manifest is required")
	}
	if len(manifest.Grammars.SHA256) != sha256.Size*2 {
		return errors.New("grammars.sha256 must be a 64-character hexadecimal digest")
	}
	if _, err := hex.DecodeString(manifest.Grammars.SHA256); err != nil {
		return fmt.Errorf("grammars.sha256: %w", err)
	}
	return nil
}

func loadGrammarProvenance(root, manifestReference, expectedSHA256 string, required bool) (GrammarProvenance, error) {
	base := GrammarProvenance{Manifest: manifestReference, Versions: []GrammarVersion{}}
	// The reference comes out of a JSON manifest, so it is slash-separated by
	// contract and travels between platforms unchanged. The containment check
	// is therefore made in slash terms: filepath.Clean rewrites
	// "grammars/manifest.json" to "grammars\\manifest.json" on Windows, finds
	// it differs from what it was handed, and reports a correctly written
	// manifest as an attempt to escape the bundle. A backslash is refused
	// outright for the same reason -- it is not a separator in this format, so
	// a reference carrying one is not one this bundle wrote.
	//
	// The host's separator enters one line down, where the reference stops
	// being a name in a document and becomes a path on a disk.
	if manifestReference == "" || path.IsAbs(manifestReference) || filepath.IsAbs(manifestReference) ||
		strings.ContainsRune(manifestReference, '\\') ||
		path.Clean(manifestReference) != manifestReference ||
		manifestReference == ".." || strings.HasPrefix(manifestReference, "../") {
		return GrammarProvenance{}, fmt.Errorf("grammar manifest path %q escapes bundle root", manifestReference)
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(manifestReference))
	data, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) && !required {
		return base, nil
	}
	if err != nil {
		return GrammarProvenance{}, fmt.Errorf("read grammar manifest %q: %w", manifestPath, err)
	}
	digest := sha256.Sum256(data)
	observedSHA256 := hex.EncodeToString(digest[:])
	if expectedSHA256 != "" && expectedSHA256 != observedSHA256 {
		return GrammarProvenance{}, fmt.Errorf("grammar manifest %q: sha256 mismatch: want %s, got %s", manifestPath, expectedSHA256, observedSHA256)
	}
	loaded, err := syntax.LoadManifest(manifestPath)
	if err != nil {
		return GrammarProvenance{}, err
	}
	versions := make([]GrammarVersion, 0, len(loaded.Grammars))
	for _, grammar := range loaded.Grammars {
		versions = append(versions, GrammarVersion{Name: grammar.Name, Version: grammar.Version, Commit: grammar.Commit})
	}
	return GrammarProvenance{Manifest: manifestReference, SHA256: stringPointer(observedSHA256), Versions: versions}, nil
}

func stringPointer(value string) *string {
	return &value
}

func optionalStringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return stringPointer(value)
}

func boolPointer(value bool) *bool {
	return &value
}
