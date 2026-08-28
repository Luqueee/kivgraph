package javaloader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const recordedIndex = "../../testdata/java/index/basic.scip"

// recordedPayload converts the checked-in index through the same Convert the
// loader uses, so a hermetic assertion and a live one describe the same code
// path rather than two that happen to agree.
func recordedPayload(t *testing.T) facts.SemanticPayload {
	t.Helper()
	data, err := os.ReadFile(recordedIndex)
	if err != nil {
		t.Fatalf("read recorded index: %v", err)
	}
	index, err := scipwire.Decode(data)
	if err != nil {
		t.Fatalf("decode recorded index: %v", err)
	}
	root, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := Convert(index, Options{
		Repository: workspace.Repository{Name: "basic", Path: root, RealPath: root},
	}, root)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return payload
}

func TestRecordedPayloadIsAuthoritativeAndJavaStamped(t *testing.T) {
	payload := recordedPayload(t)
	if payload.Language != facts.LanguageJava {
		t.Fatalf("language = %q", payload.Language)
	}
	if !payload.Authoritative {
		t.Fatal("scip-java resolves through javac, so its payload is authoritative")
	}
	if payload.Analyzer != DefaultCommand {
		t.Errorf("analyzer = %q, want %q", payload.Analyzer, DefaultCommand)
	}
	if len(payload.Symbols) == 0 {
		t.Fatal("the recorded payload has no symbols")
	}
}

// TestPackageIdentityNamesTheManifest is what puts a Java repository's symbols
// under one package. The package name reaches every stable key, so deriving it
// from a manifest that is on disk beats deriving it from the index, whose
// package field varies per module in a multi-module build.
func TestPackageIdentityNamesTheManifest(t *testing.T) {
	root, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatal(err)
	}
	name, manifest := packageIdentity(root)
	if name != "basic" {
		t.Errorf("package name = %q, want basic", name)
	}
	if filepath.Base(manifest) != "pom.xml" {
		t.Errorf("manifest = %q, want the fixture's pom.xml", manifest)
	}

	// A directory with no manifest still names a package rather than an empty
	// string: facts.PackageKey would otherwise derive one key for every
	// manifest-less Java repository.
	empty := t.TempDir()
	name, manifest = packageIdentity(empty)
	if name == "" {
		t.Error("a repository with no manifest produced no package name")
	}
	if manifest != "" {
		t.Errorf("manifest = %q, want empty", manifest)
	}
}
