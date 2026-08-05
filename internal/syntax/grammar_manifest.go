package syntax

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

const (
	// DefaultManifestPath is the repository-relative grammar manifest path.
	DefaultManifestPath   = "grammars/manifest.json"
	grammarManifestSchema = 1
)

var (
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)
)

// GrammarManifest pins every grammar source used by Luque.
type GrammarManifest struct {
	SchemaVersion int             `json:"schema_version"`
	ArchiveFormat string          `json:"archive_format"`
	Grammars      []GrammarSource `json:"grammars"`
}

// GrammarSource identifies one language grammar inside a pinned source archive.
type GrammarSource struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
	Version    string `json:"version"`
	Commit     string `json:"commit"`
	SourcePath string `json:"source_path"`
	ArchiveURL string `json:"archive_url"`
	SHA256     string `json:"sha256"`
	License    string `json:"license"`
	LicenseURL string `json:"license_url"`
}

// LoadManifest reads and validates a grammar manifest from path.
func LoadManifest(manifestPath string) (GrammarManifest, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return GrammarManifest{}, fmt.Errorf("read grammar manifest %q: %w", manifestPath, err)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest GrammarManifest
	if err := decoder.Decode(&manifest); err != nil {
		return GrammarManifest{}, fmt.Errorf("decode grammar manifest %q: %w", manifestPath, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return GrammarManifest{}, fmt.Errorf("decode grammar manifest %q: trailing JSON value", manifestPath)
		}
		return GrammarManifest{}, fmt.Errorf("decode grammar manifest %q: trailing data: %w", manifestPath, err)
	}
	if err := manifest.Validate(); err != nil {
		return GrammarManifest{}, fmt.Errorf("validate grammar manifest %q: %w", manifestPath, err)
	}
	manifest.Grammars = append([]GrammarSource(nil), manifest.Grammars...)
	return manifest, nil
}

// Validate checks the schema and reproducibility fields of a grammar manifest.
func (manifest GrammarManifest) Validate() error {
	if manifest.SchemaVersion != grammarManifestSchema {
		return fmt.Errorf("schema_version: want %d, got %d", grammarManifestSchema, manifest.SchemaVersion)
	}
	if manifest.ArchiveFormat != "tar.gz" {
		return fmt.Errorf("archive_format: want %q, got %q", "tar.gz", manifest.ArchiveFormat)
	}
	if len(manifest.Grammars) == 0 {
		return errors.New("grammars: must not be empty")
	}

	seen := make(map[string]struct{}, len(manifest.Grammars))
	for index, grammar := range manifest.Grammars {
		if err := validateGrammarSource(grammar); err != nil {
			return fmt.Errorf("grammars[%d] %q: %w", index, grammar.Name, err)
		}
		if _, exists := seen[grammar.Name]; exists {
			return fmt.Errorf("grammars[%d] %q: duplicate name", index, grammar.Name)
		}
		seen[grammar.Name] = struct{}{}
	}
	for _, required := range []string{"typescript", "tsx", "javascript", "go"} {
		if _, exists := seen[required]; !exists {
			return fmt.Errorf("grammars: missing %q", required)
		}
	}
	return nil
}

func validateGrammarSource(grammar GrammarSource) error {
	if strings.TrimSpace(grammar.Name) == "" {
		return errors.New("name: must not be empty")
	}
	const githubRepositoryPrefix = "https://github.com/tree-sitter/"
	if !strings.HasPrefix(grammar.Repository, githubRepositoryPrefix) || !strings.HasSuffix(grammar.Repository, ".git") {
		return fmt.Errorf("repository: unsupported URL %q", grammar.Repository)
	}
	if !versionPattern.MatchString(grammar.Version) {
		return fmt.Errorf("version: invalid semver tag %q", grammar.Version)
	}
	if !commitPattern.MatchString(grammar.Commit) {
		return errors.New("commit: want 40 lowercase hexadecimal characters")
	}
	if grammar.SourcePath == "" || path.IsAbs(grammar.SourcePath) || path.Clean(grammar.SourcePath) != grammar.SourcePath || grammar.SourcePath == ".." || strings.HasPrefix(grammar.SourcePath, "../") {
		return fmt.Errorf("source_path: must be a relative normalized path, got %q", grammar.SourcePath)
	}

	repositoryPath := strings.TrimPrefix(strings.TrimSuffix(grammar.Repository, ".git"), "https://github.com/")
	expectedArchivePrefix := "https://codeload.github.com/" + repositoryPath + "/tar.gz/"
	if !strings.HasPrefix(grammar.ArchiveURL, expectedArchivePrefix) || !strings.HasSuffix(grammar.ArchiveURL, grammar.Commit) {
		return errors.New("archive_url: must point to the repository archive at the pinned commit")
	}
	if !sha256Pattern.MatchString(grammar.SHA256) {
		return errors.New("sha256: want 64 lowercase hexadecimal characters")
	}
	if grammar.License != "MIT" {
		return fmt.Errorf("license: want %q, got %q", "MIT", grammar.License)
	}
	expectedLicenseURL := "https://raw.githubusercontent.com/" + repositoryPath + "/" + grammar.Commit + "/LICENSE"
	if grammar.LicenseURL != expectedLicenseURL {
		return errors.New("license_url: must point to LICENSE at the pinned commit")
	}
	return nil
}
