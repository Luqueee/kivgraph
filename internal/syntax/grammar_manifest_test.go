package syntax

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadManifestLoadsPinnedInitialGrammars(t *testing.T) {
	manifestPath := filepath.Join("..", "..", DefaultManifestPath)
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if manifest.SchemaVersion != 1 || manifest.ArchiveFormat != "tar.gz" {
		t.Fatalf("manifest header = %#v", manifest)
	}
	if len(manifest.Grammars) != 4 {
		t.Fatalf("grammar count = %d, want 4", len(manifest.Grammars))
	}

	byName := make(map[string]GrammarSource, len(manifest.Grammars))
	for _, grammar := range manifest.Grammars {
		byName[grammar.Name] = grammar
	}
	for _, name := range []string{"typescript", "tsx", "javascript", "go"} {
		grammar, ok := byName[name]
		if !ok {
			t.Fatalf("manifest is missing %q", name)
		}
		if grammar.Commit == "" || grammar.SHA256 == "" || grammar.License != "MIT" {
			t.Fatalf("grammar %q is not pinned completely: %#v", name, grammar)
		}
	}
	if byName["typescript"].Commit != byName["tsx"].Commit || byName["typescript"].SHA256 != byName["tsx"].SHA256 {
		t.Fatal("TypeScript and TSX must come from the same pinned archive")
	}
}

func TestGrammarManifestValidateRejectsInvalidPins(t *testing.T) {
	valid := GrammarManifest{
		SchemaVersion: 1,
		ArchiveFormat: "tar.gz",
		Grammars: []GrammarSource{
			{
				Name:       "typescript",
				Repository: "https://github.com/tree-sitter/tree-sitter-typescript.git",
				Version:    "v0.23.2",
				Commit:     strings.Repeat("a", 40),
				SourcePath: "typescript",
				ArchiveURL: "https://codeload.github.com/tree-sitter/tree-sitter-typescript/tar.gz/" + strings.Repeat("a", 40),
				SHA256:     strings.Repeat("b", 64),
				License:    "MIT",
				LicenseURL: "https://raw.githubusercontent.com/tree-sitter/tree-sitter-typescript/" + strings.Repeat("a", 40) + "/LICENSE",
			},
		},
	}

	for _, required := range []string{"schema_version", "archive_format", "empty grammars", "missing grammar", "invalid commit", "invalid checksum", "invalid license"} {
		t.Run(required, func(t *testing.T) {
			candidate := valid
			candidate.Grammars = append([]GrammarSource(nil), valid.Grammars...)
			switch required {
			case "schema_version":
				candidate.SchemaVersion = 2
			case "archive_format":
				candidate.ArchiveFormat = "zip"
			case "empty grammars":
				candidate.Grammars = nil
			case "missing grammar":
				candidate.Grammars[0].Name = "javascript"
			case "invalid commit":
				candidate.Grammars[0].Commit = "not-a-commit"
			case "invalid checksum":
				candidate.Grammars[0].SHA256 = "not-a-checksum"
			case "invalid license":
				candidate.Grammars[0].License = "Apache-2.0"
			}
			if err := candidate.Validate(); err == nil {
				t.Fatalf("Validate() accepted %s", required)
			}
		})
	}
}
