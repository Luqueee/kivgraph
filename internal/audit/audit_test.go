package audit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func repositoryAt(root string, languages ...string) workspace.Repository {
	return workspace.Repository{Name: "probe", Path: root, RealPath: root, Languages: languages}
}

// only returns the single finding of the report, so a test that expects one
// says so instead of indexing into a slice of unknown length.
func only(t *testing.T, report Report) Finding {
	t.Helper()
	if len(report.Findings) != 1 {
		t.Fatalf("findings = %#v, want exactly one", report.Findings)
	}
	return report.Findings[0]
}

// TestAuditProposesAJsconfigForAJavaScriptPackage is the remedy that matters
// most: a repository of .mjs files must not be told to declare itself
// TypeScript, and the same check on a TypeScript tree must not propose a
// jsconfig.
func TestAuditProposesTheProjectTheSourcesAskFor(t *testing.T) {
	for _, test := range []struct {
		name         string
		sources      map[string]string
		wantPath     string
		wantSubstr   string
		wantSeverity Severity
	}{
		{
			name: "javascript",
			sources: map[string]string{
				"tool.mjs":  "export const tool = 1\n",
				"other.mjs": "export const other = 1\n",
			},
			wantPath:     "jsconfig.json",
			wantSubstr:   "jsconfig",
			wantSeverity: SeverityBlocking,
		},
		{
			name: "typescript",
			sources: map[string]string{
				"src/index.ts": "export const index = 1\n",
				"src/other.ts": "export const other = 1\n",
			},
			wantPath:     "tsconfig.json",
			wantSubstr:   "declare a project",
			wantSeverity: SeverityPartial,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			write(t, filepath.Join(root, "package.json"), `{"name":"probe","version":"1.0.0"}`)
			for name, contents := range test.sources {
				write(t, filepath.Join(root, name), contents)
			}

			report, err := Run(context.Background(),
				[]workspace.Repository{repositoryAt(root, "typescript")}, Options{})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			finding := only(t, report)
			// JavaScript is a source only if a package says so, and without a
			// project nothing reads it. TypeScript is read all the same, under
			// options the repository did not choose.
			if finding.Code != CodeTypeScriptNoProject || finding.Severity != test.wantSeverity {
				t.Fatalf("finding = %#v, want a %s TS_NO_PROJECT", finding, test.wantSeverity)
			}
			if finding.Remedy.Path != test.wantPath {
				t.Fatalf("remedy path = %q, want %q", finding.Remedy.Path, test.wantPath)
			}
			if !strings.Contains(finding.Remedy.Summary, test.wantSubstr) {
				t.Fatalf("remedy summary = %q, want it to mention %q", finding.Remedy.Summary, test.wantSubstr)
			}
		})
	}
}

// TestAuditReportsAProjectThatClaimsNothing covers the hole that has no
// warning of its own in a pass: the package is in the registry, its project
// exists, and its file selection reaches nothing.
func TestAuditReportsAProjectThatClaimsNothing(t *testing.T) {
	root := testsupport.TempDir(t)
	write(t, filepath.Join(root, "package.json"), `{"name":"probe","version":"1.0.0"}`)
	write(t, filepath.Join(root, "tsconfig.json"), `{"include":["sources"]}`)
	write(t, filepath.Join(root, "src", "index.ts"), "export const index = 1\n")

	report, err := Run(context.Background(),
		[]workspace.Repository{repositoryAt(root, "typescript")}, Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	finding := report.Findings[0]
	if finding.Code != CodeTypeScriptProjectClaimsNo || finding.Severity != SeverityBlocking {
		t.Fatalf("finding = %#v, want a blocking TS_PROJECT_CLAIMS_NOTHING", finding)
	}
	if !strings.Contains(finding.Detail, "1 .ts") {
		t.Fatalf("detail = %q, want it to say what the tree holds", finding.Detail)
	}
}

// TestAuditIsSilentOnARepositoryThatDeclaresEverything is the negative half of
// the whole command: a repository a pass can read in full produces no finding,
// so a report is a list of real holes rather than a list of repositories.
func TestAuditIsSilentOnARepositoryThatDeclaresEverything(t *testing.T) {
	root := testsupport.TempDir(t)
	write(t, filepath.Join(root, "package.json"), `{"name":"probe","version":"1.0.0"}`)
	write(t, filepath.Join(root, "tsconfig.json"), `{"include":["src"]}`)
	write(t, filepath.Join(root, "src", "index.ts"), "export const index = 1\n")

	report, err := Run(context.Background(),
		[]workspace.Repository{repositoryAt(root, "typescript")}, Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %#v, want none", report.Findings)
	}
	if report.Repositories != 1 || report.Blocking() != 0 {
		t.Fatalf("report = %#v, want one repository audited and nothing blocking", report)
	}
}

// TestAuditNamesTheBuildTagAGoPackageIsGuardedBy is what makes the Go finding
// actionable: a remedy that says "grant the tag" without naming it cannot be
// applied by anyone, and the tag is in the files the go command refused.
func TestAuditNamesTheBuildTagAGoPackageIsGuardedBy(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go command is not installed")
	}
	root := testsupport.TempDir(t)
	write(t, filepath.Join(root, "go.mod"), "module example.com/probe\n\ngo 1.24\n")
	write(t, filepath.Join(root, "tools.go"), "//go:build tools\n\npackage probe\n")
	write(t, filepath.Join(root, "internal", "carried", "carried.go"), "package carried\n\nconst Carried = 1\n")

	report, err := Run(context.Background(),
		[]workspace.Repository{repositoryAt(root, "go")}, Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	finding := only(t, report)
	if finding.Code != CodeGoBuildConstraints || finding.Severity != SeverityPartial {
		t.Fatalf("finding = %#v, want a partial GO_BUILD_CONSTRAINTS_EXCLUDE_PACKAGE", finding)
	}
	if finding.Remedy.ConfigKey != "go.build_tags" || finding.Remedy.ConfigValue != "[tools]" {
		t.Fatalf("remedy = %#v, want the tag named", finding.Remedy)
	}
	if !strings.Contains(finding.Detail, "example.com/probe") {
		t.Fatalf("detail = %q, want the excluded package named", finding.Detail)
	}
}

// TestAuditReportsACargoWorkspaceThatResolvesNothing covers the failure that
// costs a whole dependency graph and shows as one warning among hundreds: a
// lockfile pinning a version the local registry cache does not hold, with the
// network closed.
func TestAuditReportsACargoWorkspaceThatResolvesNothing(t *testing.T) {
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skip("cargo is not installed")
	}
	root := testsupport.TempDir(t)
	write(t, filepath.Join(root, "Cargo.toml"), `[package]
name = "probe"
version = "0.1.0"
edition = "2021"

[dependencies]
kivgraph-probe-crate-that-does-not-exist = "9999.0.0"
`)
	write(t, filepath.Join(root, "src", "main.rs"), "fn main() {}\n")

	report, err := Run(context.Background(),
		[]workspace.Repository{repositoryAt(root, "rust")},
		Options{RustTargetDirectory: filepath.Join(testsupport.TempDir(t), "target")})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	finding := only(t, report)
	if finding.Code != CodeRustMetadataFailed || finding.Severity != SeverityPartial {
		t.Fatalf("finding = %#v, want a partial RUST_METADATA_FAILED", finding)
	}
	if finding.Remedy.Command != "cargo fetch --locked" {
		t.Fatalf("remedy = %#v, want the command that populates the cache", finding.Remedy)
	}
}

// TestAuditRestrictsItselfToOneRepository keeps the flag meaning what it says:
// the other repositories are not audited, not merely filtered out of the
// report.
func TestAuditRestrictsItselfToOneRepository(t *testing.T) {
	first := testsupport.TempDir(t)
	write(t, filepath.Join(first, "package.json"), `{"name":"first","version":"1.0.0"}`)
	second := testsupport.TempDir(t)
	write(t, filepath.Join(second, "package.json"), `{"name":"second","version":"1.0.0"}`)

	repositories := []workspace.Repository{
		{Name: "first", Path: first, RealPath: first, Languages: []string{"typescript"}},
		{Name: "second", Path: second, RealPath: second, Languages: []string{"typescript"}},
	}
	report, err := Run(context.Background(), repositories, Options{Repository: "second"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if report.Repositories != 1 {
		t.Fatalf("repositories audited = %d, want 1", report.Repositories)
	}
	if finding := only(t, report); finding.Repository != "second" {
		t.Fatalf("finding = %#v, want it to be about the requested repository", finding)
	}
}

// TypeScript and JavaScript are one audit. `kivgraph index` registers both for
// a repository that holds both, and every finding was reported twice.
func TestAuditRunsOnceForTypeScriptAndJavaScript(t *testing.T) {
	root := testsupport.TempDir(t)
	write(t, filepath.Join(root, "package.json"), `{"name":"probe","version":"1.0.0"}`)
	write(t, filepath.Join(root, "tsconfig.json"), `{"include":["sources"]}`)
	write(t, filepath.Join(root, "src", "index.ts"), "export const index = 1\n")

	alone, err := Run(context.Background(), []workspace.Repository{repositoryAt(root, "typescript")}, Options{})
	if err != nil || len(alone.Findings) == 0 {
		t.Fatalf("Run(typescript) = %#v, %v, want findings to count", alone, err)
	}
	for _, languages := range [][]string{
		{"typescript", "javascript"}, {"javascript", "typescript"}, {"ts", "js", "typescript"},
	} {
		report, err := Run(context.Background(), []workspace.Repository{repositoryAt(root, languages...)}, Options{})
		if err != nil {
			t.Fatalf("Run(%v) error = %v", languages, err)
		}
		if !reflect.DeepEqual(report.Findings, alone.Findings) {
			t.Errorf("languages %v: %d findings, want the %d of one audit", languages, len(report.Findings), len(alone.Findings))
		}
	}
}

// What no project claims is three things, and the audit tells them apart. A
// file of a package that declares no project is read through one written for
// the pass; one a project leaves out waits for the key; one in no package at
// all is read by nobody, key or no key.
func TestAuditTellsApartTheSourcesNoProjectClaims(t *testing.T) {
	root := testsupport.TempDir(t)
	write(t, filepath.Join(root, "apps", "web", "package.json"), `{"name":"web","version":"1.0.0"}`)
	write(t, filepath.Join(root, "apps", "web", "tsconfig.json"), `{"include":["src"]}`)
	write(t, filepath.Join(root, "apps", "web", "src", "index.ts"), "export const index = 1\n")
	write(t, filepath.Join(root, "apps", "web", "e2e", "spec.ts"), "export const spec = 1\n")
	write(t, filepath.Join(root, "packages", "api", "package.json"), `{"name":"api","version":"1.0.0"}`)
	write(t, filepath.Join(root, "packages", "api", "src", "index.ts"), "export const api = 1\n")
	repositories := []workspace.Repository{repositoryAt(root, "typescript")}

	off, err := Run(context.Background(), repositories, Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if finding := only(t, off); finding.Code != CodeTypeScriptUnclaimedSources || !strings.Contains(finding.Detail, "1 source file(s)") ||
		!strings.Contains(finding.Detail, "apps/web/e2e/spec.ts") || finding.Remedy.ConfigKey != "typescript.include_unclaimed_sources" {
		t.Fatalf("with the key off: %#v", finding)
	}
	on, err := Run(context.Background(), repositories, Options{IncludeUnclaimedSources: true})
	if err != nil || len(on.Findings) != 0 {
		t.Fatalf("with the key on: %#v, %v", on.Findings, err)
	}

	// In no package: the key does not reach it.
	write(t, filepath.Join(root, "scripts", "tool.ts"), "export const tool = 1\n")
	orphaned, err := Run(context.Background(), repositories, Options{IncludeUnclaimedSources: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	finding := only(t, orphaned)
	if finding.Code != CodeTypeScriptUnclaimedSources || finding.Severity != SeverityPartial ||
		!strings.Contains(finding.Detail, "scripts/tool.ts") || finding.Remedy.ConfigKey != "" ||
		!strings.Contains(finding.Remedy.Summary, "package.json") {
		t.Fatalf("a file in no package: %#v", finding)
	}
}
