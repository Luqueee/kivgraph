// Package audit answers one question about a registered repository without
// indexing it: is it structured so that a pass can produce a graph of it, and
// when it is not, what has to change.
//
// Every check asks the code the pass itself asks -- the same discovery, the
// same package registry, the same source resolution, the same `go list`, the
// same `cargo metadata`. A check that reimplemented any of them would answer
// for a pass nobody runs, which is worse than not answering: it would clear a
// repository the pass then drops.
//
// Nothing here writes. A remedy is a proposal, spelled out precisely enough
// for a reader -- or an agent -- to apply it, and Kivgraph does not write
// inside the code it indexes.
package audit

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// Severity says what a finding costs the graph.
type Severity string

const (
	// SeverityBlocking means the repository, or a package of it,
	// contributes nothing at all: there is no program to read it.
	SeverityBlocking Severity = "blocking"
	// SeverityPartial means the repository is indexed and part of it is
	// invisible.
	SeverityPartial Severity = "partial"
)

// Finding codes. Stable: a caller may branch on them.
const (
	CodeTypeScriptNoPackage        = "TS_NO_PACKAGE"
	CodeTypeScriptNoProject        = "TS_NO_PROJECT"
	CodeTypeScriptProjectClaimsNo  = "TS_PROJECT_CLAIMS_NOTHING"
	CodeTypeScriptDuplicateName    = "TS_DUPLICATE_PACKAGE_NAME"
	CodeTypeScriptUnclaimedSources = "TS_UNCLAIMED_SOURCES"
	CodeGoNoModule                 = "GO_NO_MODULE"
	CodeGoBuildConstraints         = "GO_BUILD_CONSTRAINTS_EXCLUDE_PACKAGE"
	CodeGoPackageError             = "GO_PACKAGE_ERROR"
	CodeRustNoManifest             = "RUST_NO_MANIFEST"
	CodeRustMetadataFailed         = "RUST_METADATA_FAILED"
)

// Remedy is what to do about a finding. Every field is optional except
// Summary; a caller applies what it understands and prints the rest.
type Remedy struct {
	// Summary is one imperative line.
	Summary string `json:"summary"`
	// Path is the repository-relative file a remedy creates or edits.
	Path string `json:"path,omitempty"`
	// Content is the exact text to write at Path.
	Content string `json:"content,omitempty"`
	// ConfigKey and ConfigValue name a Kivgraph configuration change.
	ConfigKey   string `json:"config_key,omitempty"`
	ConfigValue string `json:"config_value,omitempty"`
	// Command runs inside the repository.
	Command string `json:"command,omitempty"`
}

// Finding is one observed reason a repository does not produce the graph its
// registration promises.
type Finding struct {
	Repository string   `json:"repository"`
	Language   string   `json:"language"`
	Code       string   `json:"code"`
	Severity   Severity `json:"severity"`
	// Detail is what was observed, with the paths that were read.
	Detail string `json:"detail"`
	Remedy Remedy `json:"remedy"`
}

// Report is the audit of every repository that was asked about.
type Report struct {
	Repositories int       `json:"repositories"`
	Findings     []Finding `json:"findings"`
}

// Blocking counts the findings that leave a repository or package out of the
// graph entirely.
func (report Report) Blocking() int {
	count := 0
	for _, finding := range report.Findings {
		if finding.Severity == SeverityBlocking {
			count++
		}
	}
	return count
}

// Options carries the configuration the checks have to honour: auditing under
// settings other than the ones a pass would use answers for a pass nobody
// runs.
type Options struct {
	// GoBuildTags, GoAllowNetwork and the Rust fields mirror the keys of the
	// same name. A repository excluded by a tag the configuration does grant
	// is not a finding.
	GoBuildTags             []string
	GoAllowNetwork          bool
	RustAllowNetwork        bool
	RustTargetDirectory     string
	IncludeUnclaimedSources bool
	// Repository, when set, restricts the audit to that registry name.
	Repository string
}

// OptionsFromConfig reads the settings a pass would run under.
func OptionsFromConfig(configuration config.Config) Options {
	return Options{
		GoBuildTags:             append([]string(nil), configuration.Go.BuildTags...),
		GoAllowNetwork:          configuration.Go.AllowNetwork,
		RustAllowNetwork:        configuration.Rust.AllowNetwork,
		RustTargetDirectory:     configuration.Rust.TargetDirectory,
		IncludeUnclaimedSources: configuration.TypeScript.IncludeUnclaimedSources,
	}
}

// Run audits every repository, in registry order.
//
// A repository whose toolchain cannot be reached produces a finding rather
// than an error: an audit that stops at the first unreadable repository says
// nothing about the other fifty.
func Run(ctx context.Context, repositories []workspace.Repository, options Options) (Report, error) {
	report := Report{Findings: make([]Finding, 0)}
	for _, repository := range repositories {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		if options.Repository != "" && repository.Name != options.Repository {
			continue
		}
		report.Repositories++
		for _, language := range repository.Languages {
			var findings []Finding
			var err error
			switch strings.ToLower(strings.TrimSpace(language)) {
			case "typescript", "javascript":
				findings, err = auditTypeScript(ctx, repository, options)
			case "go":
				findings, err = auditGo(ctx, repository, options)
			case "rust":
				findings, err = auditRust(ctx, repository, options)
			default:
				continue
			}
			if err != nil {
				return Report{}, fmt.Errorf("audit %q: %w", repository.Name, err)
			}
			report.Findings = append(report.Findings, findings...)
		}
	}
	sort.SliceStable(report.Findings, func(left, right int) bool {
		if report.Findings[left].Repository != report.Findings[right].Repository {
			return report.Findings[left].Repository < report.Findings[right].Repository
		}
		return report.Findings[left].Code < report.Findings[right].Code
	})
	return report, nil
}

// relativePath names a path the way a reader of the repository would.
func relativePath(root, path string) string {
	if relative, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(relative)
	}
	return path
}
