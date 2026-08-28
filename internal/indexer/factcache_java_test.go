package indexer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// repositoryNamed is the one repository every unit of the identity test shares,
// which is what makes a collision between two kinds visible.
func repositoryNamed(name string) workspace.Repository {
	return workspace.Repository{Name: name, Path: "/repositories/" + name, RealPath: "/repositories/" + name}
}

// TestAnalyzerFingerprintWatchesTheJavaIndexerBinary is the Java half of what
// TestAnalyzerFingerprintWatchesThePythonProducerThatRuns pins for Python.
//
// Java's producer is an executable this repository does not ship, so what
// identifies it is the binary the PATH resolves. A scip-java upgraded in place
// emits a different index from the same sources, and an entry written by the
// old one describes a graph this pass would not produce. Naming only the
// command string would miss that entirely: the string does not change.
func TestAnalyzerFingerprintWatchesTheJavaIndexerBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture relies on a unix executable bit")
	}
	directory := testsupport.TempDir(t)
	indexer := filepath.Join(directory, "scip-java-fixture")
	if err := os.WriteFile(indexer, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))

	options := FullOptions{JavaIndexerCommand: "scip-java-fixture"}
	base := analyzerFingerprint(options)
	if analyzerFingerprint(options) != base {
		t.Fatal("fingerprint is not stable across two reads")
	}

	if err := os.WriteFile(indexer, []byte("#!/bin/sh\necho changed\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if analyzerFingerprint(options) == base {
		t.Fatal("fingerprint ignored a change to the indexer binary this pass runs")
	}
}

// TestAnalyzerFingerprintSeparatesTheJavaOptions guards the options that change
// what the indexer is asked to do. Two passes that disagree on any of them
// observe different code, so neither may be served the other's entry.
func TestAnalyzerFingerprintSeparatesTheJavaOptions(t *testing.T) {
	base := FullOptions{JavaIndexerCommand: "scip-java", JavaTargetDirectory: "/tmp/a"}
	reference := analyzerFingerprint(base)

	for name, mutate := range map[string]func(*FullOptions){
		"build tool":        func(o *FullOptions) { o.JavaBuildTool = "gradle" },
		"include tests":     func(o *FullOptions) { o.JavaIncludeTests = true },
		"include generated": func(o *FullOptions) { o.JavaIncludeGenerated = true },
		"target directory":  func(o *FullOptions) { o.JavaTargetDirectory = "/tmp/b" },
		"indexer command":   func(o *FullOptions) { o.JavaIndexerCommand = "scip-java --verbose" },
	} {
		options := base
		mutate(&options)
		if analyzerFingerprint(options) == reference {
			t.Errorf("changing the %s did not change the fingerprint", name)
		}
	}
}

// TestUnitIdentitySeparatesEveryKind is the regression for a defect the kind
// refactor uncovered: unitIdentity had no branch for Rust, so every Rust
// workspace was keyed as a TypeScript package with an empty name. One
// workspace per repository hid it; two shared an entry.
func TestUnitIdentitySeparatesEveryKind(t *testing.T) {
	identities := map[string]string{}
	for name, unit := range map[string]analysisUnit{
		"go":         {kind: unitGo, repository: repositoryNamed("shared")},
		"typescript": {kind: unitTypeScript, repository: repositoryNamed("shared")},
		"rust":       {kind: unitRust, repository: repositoryNamed("shared")},
		"python":     {kind: unitSemantic, language: "python", repository: repositoryNamed("shared")},
		"dart":       {kind: unitSemantic, language: "dart", repository: repositoryNamed("shared")},
		"java":       {kind: unitSemantic, language: "java", repository: repositoryNamed("shared")},
	} {
		identity := unitIdentity(unit)
		if previous, clash := identities[identity]; clash {
			t.Errorf("%s and %s share the cache identity %q", name, previous, identity)
		}
		identities[identity] = name
	}
}
