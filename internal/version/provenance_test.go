package version

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCollectReadsBundleManifest(t *testing.T) {
	root := t.TempDir()
	executable := writeBundleFixture(t, root)

	provenance, err := Collect(executable, t.TempDir())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if provenance.Kivgraph != "0.1.0" || provenance.Go != "go1.24.4" {
		t.Fatalf("release/toolchain = %#v", provenance)
	}
	if provenance.Commit == nil || *provenance.Commit != strings.Repeat("a", 40) {
		t.Fatalf("commit = %v", provenance.Commit)
	}
	if provenance.Dirty == nil || *provenance.Dirty {
		t.Fatalf("dirty = %v, want false", provenance.Dirty)
	}
	if provenance.Node == nil || *provenance.Node != "v25.9.0" {
		t.Fatalf("node = %v", provenance.Node)
	}
	if provenance.TypeScript == nil || *provenance.TypeScript != "7.0.2" {
		t.Fatalf("typescript = %v", provenance.TypeScript)
	}
	if provenance.Ladybug != "v0.13.1" || provenance.GoLadybug != "v0.13.1" {
		t.Fatalf("LadybugDB versions = %q/%q", provenance.Ladybug, provenance.GoLadybug)
	}
	if provenance.Schema != 3 || provenance.SnapshotRowFormat != 3 {
		t.Fatalf("schema = %d/%d, want 3/3", provenance.Schema, provenance.SnapshotRowFormat)
	}
	if provenance.Resolver == nil || *provenance.Resolver != "resolver-v9" {
		t.Fatalf("resolver = %v", provenance.Resolver)
	}
	if len(provenance.Grammars.Versions) != 5 {
		t.Fatalf("grammar versions = %d, want 5", len(provenance.Grammars.Versions))
	}
	if !slices.ContainsFunc(provenance.Grammars.Versions, func(grammar GrammarVersion) bool {
		return grammar.Name == "rust"
	}) {
		t.Fatalf("grammar versions = %#v, want the Rust grammar", provenance.Grammars.Versions)
	}
	if provenance.Grammars.SHA256 == nil || *provenance.Grammars.SHA256 == "" {
		t.Fatalf("grammar sha256 = %v", provenance.Grammars.SHA256)
	}
	// The bundle carries the Rust engine, so the provenance names it: two
	// installations of the same release must index Rust with the same one.
	if provenance.RustAnalyzer == nil || *provenance.RustAnalyzer != "0.3.3008-standalone" {
		t.Fatalf("rust analyzer = %v", provenance.RustAnalyzer)
	}

	encoded, err := json.Marshal(provenance)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if decoded["resolver"] != "resolver-v9" {
		t.Fatalf("encoded resolver = %#v", decoded["resolver"])
	}
}

func TestCollectFallsBackWithoutBundleManifest(t *testing.T) {
	provenance, err := Collect(filepath.Join(t.TempDir(), "bin", "kivgraph"), t.TempDir())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if provenance.Kivgraph != Value {
		t.Fatalf("Kivgraph = %q, want %q", provenance.Kivgraph, Value)
	}
	if provenance.Go != runtime.Version() {
		t.Fatalf("Go = %q, want %q", provenance.Go, runtime.Version())
	}
	if provenance.Ladybug != "v0.13.1" || provenance.GoLadybug != "v0.13.1" {
		t.Fatalf("LadybugDB versions = %q/%q", provenance.Ladybug, provenance.GoLadybug)
	}
	if provenance.Schema != 3 || provenance.SnapshotRowFormat != 3 {
		t.Fatalf("schema = %d/%d, want 3/3", provenance.Schema, provenance.SnapshotRowFormat)
	}
	if provenance.Node != nil || provenance.TypeScript != nil || provenance.Resolver != nil {
		t.Fatalf("unavailable fallback values = node %v/typescript %v/resolver %v", provenance.Node, provenance.TypeScript, provenance.Resolver)
	}
	if provenance.Grammars.Manifest != "grammars/manifest.json" || len(provenance.Grammars.Versions) != 0 {
		t.Fatalf("fallback grammars = %#v", provenance.Grammars)
	}
}

func TestCollectRejectsBundleGrammarDigestMismatch(t *testing.T) {
	root := t.TempDir()
	executable := writeBundleFixture(t, root)
	manifestPath := filepath.Join(root, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read fixture manifest: %v", err)
	}
	grammarData, err := os.ReadFile(filepath.Join(root, "grammars", "manifest.json"))
	if err != nil {
		t.Fatalf("read grammar fixture: %v", err)
	}
	digest := sha256.Sum256(grammarData)
	data = []byte(strings.Replace(string(data), hex.EncodeToString(digest[:]), strings.Repeat("0", 64), 1))
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatalf("write fixture manifest: %v", err)
	}

	_, err = Collect(executable, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("Collect() error = %v, want grammar sha256 mismatch", err)
	}
}

func TestCollectRejectsMalformedBundleManifest(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte("{"), 0o600); err != nil {
		t.Fatalf("write malformed manifest: %v", err)
	}

	_, err := Collect(filepath.Join(root, "bin", "kivgraph"), t.TempDir())
	if err == nil {
		t.Fatal("Collect() succeeded for malformed manifest")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Collect() error = %v, want unexpected EOF", err)
	}
}

func TestCollectRejectsBundleManifestBuiltForAnotherPlatform(t *testing.T) {
	root := t.TempDir()
	executable := writeBundleFixture(t, root)
	manifestPath := filepath.Join(root, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read fixture manifest: %v", err)
	}
	foreign := strings.Replace(
		string(data),
		fmt.Sprintf(`"target": {"os": "%s", "arch": "%s"}`, runtime.GOOS, runtime.GOARCH),
		`"target": {"os": "plan9", "arch": "mips"}`,
		1,
	)
	if foreign == string(data) {
		t.Fatal("fixture manifest does not carry the running platform target")
	}
	if err := os.WriteFile(manifestPath, []byte(foreign), 0o600); err != nil {
		t.Fatalf("write fixture manifest: %v", err)
	}

	_, err = Collect(executable, t.TempDir())
	if err == nil {
		t.Fatal("Collect() = nil error, want a target mismatch")
	}
	if !strings.Contains(err.Error(), "plan9/mips") || !strings.Contains(err.Error(), runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("Collect() error = %v, want it to name plan9/mips and %s/%s", err, runtime.GOOS, runtime.GOARCH)
	}
}

func TestCollectReadsDistBundleForTheRunningPlatform(t *testing.T) {
	workingDir := t.TempDir()
	bundleRoot := filepath.Join(workingDir, "dist", "kivgraph-"+runtime.GOOS+"-"+runtime.GOARCH)
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		t.Fatalf("mkdir dist bundle: %v", err)
	}
	writeBundleFixture(t, bundleRoot)
	// A bundle for another platform sitting next to it must be ignored.
	otherRoot := filepath.Join(workingDir, "dist", "kivgraph-plan9-mips")
	if err := os.MkdirAll(otherRoot, 0o755); err != nil {
		t.Fatalf("mkdir foreign dist bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherRoot, "manifest.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("write foreign manifest: %v", err)
	}

	provenance, err := Collect("", workingDir)
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if provenance.Kivgraph != "0.1.0" || provenance.Resolver == nil || *provenance.Resolver != "resolver-v9" {
		t.Fatalf("provenance = %#v, want the dist bundle manifest", provenance)
	}
}

func writeBundleFixture(t *testing.T, root string) string {
	t.Helper()
	grammarPath := filepath.Join(root, "grammars", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(grammarPath), 0o755); err != nil {
		t.Fatalf("mkdir grammar fixture: %v", err)
	}
	grammarData, err := os.ReadFile(filepath.Join("..", "..", "grammars", "manifest.json"))
	if err != nil {
		t.Fatalf("read grammar manifest: %v", err)
	}
	if err := os.WriteFile(grammarPath, grammarData, 0o600); err != nil {
		t.Fatalf("write grammar manifest: %v", err)
	}
	digest := sha256.Sum256(grammarData)
	grammarSHA := hex.EncodeToString(digest[:])
	manifest := fmt.Sprintf(`{
  "manifest_version": 1,
  "product": "kivgraph",
  "release": "0.1.0",
  "target": {"os": "%s", "arch": "%s"},
  "source": {"commit": "%s", "dirty": false},
  "toolchain": {"go": "go1.24.4", "node": "v25.9.0", "pnpm": "11.5.1", "typescript": "7.0.2"},
  "ladybugdb": {"core": "v0.13.1", "binding": "v0.13.1", "archive_sha256": "%s", "library_sha256": "%s"},
  "schema": {"canonical": 3, "snapshot_row_format": 3},
  "resolver_version": "resolver-v9",
  "tools": {
    "manifest": "tools/manifest.json",
    "sha256": "%s",
    "rust_analyzer": {
      "version": "2026-08-10.1",
      "release": "0.3.3008-standalone",
      "binary": "bin/rust-analyzer",
      "sha256": "%s"
    }
  },
  "grammars": {"manifest": "grammars/manifest.json", "sha256": "grammar-sha-placeholder"},
  "artifacts": []
}`,
		runtime.GOOS, runtime.GOARCH, strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 64),
		strings.Repeat("d", 64), strings.Repeat("e", 64))
	manifest = strings.Replace(manifest, "grammar-sha-placeholder", grammarSHA, 1)
	manifestPath := filepath.Join(root, "manifest.json")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write bundle manifest: %v", err)
	}
	return filepath.Join(root, "bin", "kivgraph")
}
