package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/audit"
	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// auditCheckout builds a repository that holds a commit and no module.
//
// The commit is real because the registry reads the commit and the dirty
// state by running git, which rejects a HEAD whose object does not exist; a
// fixture of loose reference files satisfies `rev-parse` and then fails on
// `status`. The repository holds no go.mod, which is the blocking shape the
// audit has to name.
func auditCheckout(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is required to register a repository: %v", err)
	}
	root := testsupport.TempDir(t)
	gitTestCommand(t, "-C", root, "init", "-q")
	gitTestCommand(t, "-C", root, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("initial\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	gitTestCommand(t, "-C", root, "add", "README.md")
	gitTestCommand(t, "-C", root, "-c", "user.name=Kivgraph Test",
		"-c", "user.email=kivgraph-test@example.invalid", "commit", "-qm", "initial")
	return root
}

// gitTestCommand runs git and fails the test with its output, which is the
// only way a fixture failure names its own cause.
func gitTestCommand(t *testing.T, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, strings.TrimSpace(string(output)))
	}
}

// auditWorkspace writes an isolated configuration and registers the given
// repositories in it, returning the configuration path the command reads.
//
// The registry is written through the same call the `register` command uses,
// so a test cannot register a shape the product would reject.
func auditWorkspace(t *testing.T, repositories ...config.Repository) string {
	t.Helper()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	initConfig(t, configPath)
	if len(repositories) == 0 {
		return configPath
	}
	repositoriesPath := filepath.Join(directory, "repositories.yaml")
	if err := config.RegisterRepositories(repositoriesPath, repositories); err != nil {
		t.Fatalf("config.RegisterRepositories() error = %v", err)
	}
	return configPath
}

// A request the parser cannot use is rejected before anything is loaded: an
// audit that ran on a misspelled flag would answer for settings nobody asked
// for.
func TestDoctorRepositoriesRejectsAnUnusableRequest(t *testing.T) {
	configPath := auditWorkspace(t)
	for _, args := range [][]string{
		{"--config", configPath, "--nonexistent"},
		{"--config", configPath, "unexpected"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runDoctorRepositories(args, &stdout, &stderr); code != 2 {
			t.Fatalf("runDoctorRepositories(%v) = %d, want 2 (stderr=%q)", args, code, stderr.String())
		}
	}
}

// A configuration that cannot be read is a failure, not an empty audit: a
// silent pass would read as "every repository is fine".
func TestDoctorRepositoriesFailsOnAnUnreadableConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"--config", filepath.Join(t.TempDir(), "absent.yaml")}
	if code := runDoctorRepositories(args, &stdout, &stderr); code != 1 {
		t.Fatalf("runDoctorRepositories() = %d, want 1 (stderr=%q)", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing written before the configuration loaded", stdout.String())
	}
}

// Narrowing to a repository that is not registered is a mistake in the
// request, not an empty result: reporting "no findings" would assert that a
// repository nobody audited is healthy. This is the third emptiness -- it
// could not be looked at -- and it must not be rendered as the first.
func TestDoctorRepositoriesRejectsAnUnregisteredRepository(t *testing.T) {
	configPath := auditWorkspace(t, config.Repository{
		Name: "registered", Path: auditCheckout(t), Languages: []string{"go"},
	})
	var stdout, stderr bytes.Buffer
	args := []string{"--config", configPath, "--repository", "absent"}
	if code := runDoctorRepositories(args, &stdout, &stderr); code != 2 {
		t.Fatalf("runDoctorRepositories() = %d, want 2 (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), `no repository named "absent" is registered`) {
		t.Fatalf("stderr = %q, want it to name the repository that is missing", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no report for an audit that never ran", stdout.String())
	}
}

// An empty registry is the first emptiness -- there is nothing -- and it
// passes. The line has to say that nothing was audited rather than that
// everything is healthy.
func TestDoctorRepositoriesPassesOnAnEmptyRegistry(t *testing.T) {
	configPath := auditWorkspace(t)
	var stdout, stderr bytes.Buffer
	if code := runDoctorRepositories([]string{"--config", configPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("runDoctorRepositories() = %d, want 0 (stderr=%q)", code, stderr.String())
	}
	want := "audit: 0 repositories, 0 finding(s), 0 blocking\n" +
		"audit: every registered repository declares what a pass needs to read it\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

// A registered directory that holds no module contributes nothing, which is
// blocking, and the command exits non-zero so a pipeline cannot read it as a
// pass.
//
// The failure branch of audit.Run is the one statement pair this file leaves
// uncovered. Run turns an unreadable repository into a finding on purpose, so
// it only returns an error when a language discovery pass itself fails or the
// context is cancelled -- neither of which a test can fix without a toolchain
// that misbehaves on demand or a cancellation the command does not accept
// from a caller.
func TestDoctorRepositoriesExitsNonZeroOnABlockingFinding(t *testing.T) {
	configPath := auditWorkspace(t, config.Repository{
		Name: "empty", Path: auditCheckout(t), Languages: []string{"go"},
	})
	var stdout, stderr bytes.Buffer
	if code := runDoctorRepositories([]string{"--config", configPath}, &stdout, &stderr); code != 1 {
		t.Fatalf("runDoctorRepositories() = %d, want 1 (stderr=%q)", code, stderr.String())
	}
	want := "audit: 1 repositories, 1 finding(s), 1 blocking\n" +
		"audit.blocking: empty [GO_NO_MODULE] no go.mod in the tree, so there is no module to load\n" +
		"  remedy: run `go mod init` in the module root, or take the repository out of the registry if it holds no Go module\n" +
		"  remedy.command: go mod init <module path>\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

// The JSON form is the same audit, not a summary of it: a client that reads
// it must see every finding the text form printed.
func TestDoctorRepositoriesEmitsTheWholeReportAsJSON(t *testing.T) {
	configPath := auditWorkspace(t, config.Repository{
		Name: "empty", Path: auditCheckout(t), Languages: []string{"go"},
	})
	var stdout, stderr bytes.Buffer
	if code := runDoctorRepositories([]string{"--config", configPath, "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("runDoctorRepositories() = %d, want 1 (stderr=%q)", code, stderr.String())
	}
	var got audit.Report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	want := audit.Report{
		Repositories: 1,
		Findings: []audit.Finding{{
			Repository: "empty",
			Language:   "go",
			Code:       audit.CodeGoNoModule,
			Severity:   audit.SeverityBlocking,
			Detail:     "no go.mod in the tree, so there is no module to load",
			Remedy: audit.Remedy{
				Summary: "run `go mod init` in the module root, or take the repository out of the registry if it holds no Go module",
				Command: "go mod init <module path>",
			},
		}},
	}
	if !reportsEqual(got, want) {
		t.Fatalf("report = %+v, want %+v", got, want)
	}
}

// A remedy carries whatever the check could establish, and every field it
// carries has to reach the reader: a path or a configuration key printed
// nowhere is a remedy the operator cannot apply.
func TestWriteAuditReportPrintsEveryRemedyFieldThatIsSet(t *testing.T) {
	report := audit.Report{
		Repositories: 2,
		Findings: []audit.Finding{
			{
				Repository: "full", Language: "typescript", Code: "TS_NO_CONFIG",
				Severity: audit.SeverityBlocking, Detail: "no tsconfig.json",
				Remedy: audit.Remedy{
					Summary:     "add a tsconfig.json",
					Path:        "tsconfig.json",
					ConfigKey:   "typescript.include_unclaimed_sources",
					ConfigValue: "true",
					Command:     "pnpm tsc --init",
				},
			},
			{
				Repository: "bare", Language: "go", Code: "GO_PARTIAL",
				Severity: audit.SeverityPartial, Detail: "one package is excluded",
				Remedy: audit.Remedy{Summary: "grant the build tag"},
			},
		},
	}
	var stdout bytes.Buffer
	writeAuditReport(&stdout, report)
	want := "audit: 2 repositories, 2 finding(s), 1 blocking\n" +
		"audit.blocking: full [TS_NO_CONFIG] no tsconfig.json\n" +
		"  remedy: add a tsconfig.json\n" +
		"  remedy.path: tsconfig.json\n" +
		"  remedy.config: typescript.include_unclaimed_sources: true\n" +
		"  remedy.command: pnpm tsc --init\n" +
		"audit.partial: bare [GO_PARTIAL] one package is excluded\n" +
		"  remedy: grant the build tag\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
}

// reportsEqual compares two reports field by field because Finding holds no
// pointer or map, so the comparison is total.
func reportsEqual(left, right audit.Report) bool {
	if left.Repositories != right.Repositories || len(left.Findings) != len(right.Findings) {
		return false
	}
	for index := range left.Findings {
		if left.Findings[index] != right.Findings[index] {
			return false
		}
	}
	return true
}

// A registered path that is not a repository stops the audit: the registry
// records the commit it audited, so a directory without one cannot be audited
// at all. Reporting no findings would assert that it is healthy.
func TestDoctorRepositoriesFailsWhenARegisteredPathIsNotARepository(t *testing.T) {
	configPath := auditWorkspace(t, config.Repository{
		Name: "loose", Path: testsupport.TempDir(t), Languages: []string{"go"},
	})
	var stdout, stderr bytes.Buffer
	if code := runDoctorRepositories([]string{"--config", configPath}, &stdout, &stderr); code != 1 {
		t.Fatalf("runDoctorRepositories() = %d, want 1 (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "register repositories:") {
		t.Fatalf("stderr = %q, want it to name the registration that failed", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no report for an audit that never ran", stdout.String())
	}
}

// Narrowing to a repository that is registered audits that one and leaves the
// rest alone: a report that counted the others would answer for repositories
// the caller excluded.
func TestDoctorRepositoriesNarrowsToTheNamedRepository(t *testing.T) {
	configPath := auditWorkspace(t,
		config.Repository{Name: "first", Path: auditCheckout(t), Languages: []string{"go"}},
		config.Repository{Name: "second", Path: auditCheckout(t), Languages: []string{"go"}},
	)
	var stdout, stderr bytes.Buffer
	args := []string{"--config", configPath, "--repository", "second", "--json"}
	if code := runDoctorRepositories(args, &stdout, &stderr); code != 1 {
		t.Fatalf("runDoctorRepositories() = %d, want 1 (stderr=%q)", code, stderr.String())
	}
	var got audit.Report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", stdout.String(), err)
	}
	want := audit.Report{
		Repositories: 1,
		Findings: []audit.Finding{{
			Repository: "second",
			Language:   "go",
			Code:       audit.CodeGoNoModule,
			Severity:   audit.SeverityBlocking,
			Detail:     "no go.mod in the tree, so there is no module to load",
			Remedy: audit.Remedy{
				Summary: "run `go mod init` in the module root, or take the repository out of the registry if it holds no Go module",
				Command: "go mod init <module path>",
			},
		}},
	}
	if !reportsEqual(got, want) {
		t.Fatalf("report = %+v, want %+v", got, want)
	}
}

// A report that could not be written is a failure. Exiting zero after a
// truncated write would tell a pipeline the audit passed when nobody read it.
func TestDoctorRepositoriesFailsWhenTheReportCannotBeWritten(t *testing.T) {
	configPath := auditWorkspace(t, config.Repository{
		Name: "empty", Path: auditCheckout(t), Languages: []string{"go"},
	})
	var stderr bytes.Buffer
	args := []string{"--config", configPath, "--json"}
	if code := runDoctorRepositories(args, refusingWriter{}, &stderr); code != 1 {
		t.Fatalf("runDoctorRepositories() = %d, want 1 (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "write report:") {
		t.Fatalf("stderr = %q, want it to name the write that failed", stderr.String())
	}
}

// refusingWriter is the destination that cannot take the report: a closed
// pipe, or a full disk.
type refusingWriter struct{}

func (refusingWriter) Write([]byte) (int, error) {
	return 0, errors.New("no space left on device")
}
