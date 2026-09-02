package sourceobservation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/topology"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestCaptureRejectsIncompleteOrUnavailableInputs(t *testing.T) {
	if _, err := Capture(context.Background(), "profile/invalid", "resolver", "analyzer", nil); err == nil ||
		!strings.Contains(err.Error(), "profile") {
		t.Fatalf("Capture() error = %v, want invalid profile refusal", err)
	}
	if _, err := Capture(context.Background(), "default", "", "analyzer", nil); err == nil ||
		!strings.Contains(err.Error(), "resolver version") {
		t.Fatalf("Capture() error = %v, want missing resolver version", err)
	}
	if _, err := Capture(context.Background(), "default", "resolver", "", nil); err == nil ||
		!strings.Contains(err.Error(), "analyzer fingerprint") {
		t.Fatalf("Capture() error = %v, want missing analyzer fingerprint", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Capture(cancelled, "default", "resolver", "analyzer", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Capture() error = %v, want cancellation", err)
	}

	repository := sourceFixtureRepository(t)
	if _, err := Capture(context.Background(), "default", "resolver", "analyzer", []workspace.Repository{repository, repository}); err == nil ||
		!strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Capture() error = %v, want duplicate repository refusal", err)
	}
	repository.Worktree = "bad/worktree"
	if _, err := Capture(context.Background(), "default", "resolver", "analyzer", []workspace.Repository{repository}); err == nil ||
		!errors.Is(err, topology.ErrInvalidID) {
		t.Fatalf("Capture() error = %v, want invalid selected worktree refusal", err)
	}
	repository.Worktree = ""

	if err := os.RemoveAll(repository.RealPath); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(context.Background(), "default", "resolver", "analyzer", []workspace.Repository{repository}); err == nil ||
		!errors.Is(err, ErrAbsent) || !strings.Contains(err.Error(), "source") {
		t.Fatalf("Capture() error = %v, want unavailable source named as absent", err)
	}
}

func TestCaptureTracksDirtyAndCommittedSourceState(t *testing.T) {
	repository := sourceFixtureRepository(t)
	repository.Worktree = "source-main"
	before, err := Capture(context.Background(), "feature", "resolver-1", "analyzer-1", []workspace.Repository{repository})
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Sources) != 1 {
		t.Fatalf("sources = %#v, want one source", before.Sources)
	}
	observed := before.Sources[0]
	if observed.Observation.Worktree != "source-main" || observed.Observation.Dirty {
		t.Fatalf("observation = %#v, want the selected clean worktree", observed.Observation)
	}
	if len(observed.Policy.Languages) != 1 || observed.Policy.Languages[0] != "go" ||
		len(observed.Policy.Exclusions) != 1 || observed.Policy.Exclusions[0] != "generated" {
		t.Fatalf("policy = %#v, want provider configuration retained", observed.Policy)
	}

	path := filepath.Join(repository.RealPath, "main.go")
	if err := os.WriteFile(path, []byte("package source\nconst Value = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dirty, err := Capture(context.Background(), "feature", "resolver-1", "analyzer-1", []workspace.Repository{repository})
	if err != nil {
		t.Fatal(err)
	}
	if !dirty.Sources[0].Observation.Dirty {
		t.Fatalf("dirty observation = %#v, want dirty state", dirty.Sources[0].Observation)
	}
	if err := Compare(before, dirty); !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), "source") {
		t.Fatalf("Compare() error = %v, want named dirty source change", err)
	}

	runGit(t, repository.RealPath, "add", "main.go")
	runGit(t, repository.RealPath, "-c", "user.email=source@example.test", "-c", "user.name=Source Fixture", "commit", "-qm", "move source")
	committed, err := Capture(context.Background(), "feature", "resolver-1", "analyzer-1", []workspace.Repository{repository})
	if err != nil {
		t.Fatal(err)
	}
	if committed.Sources[0].Observation.Dirty {
		t.Fatalf("committed observation = %#v, want clean state", committed.Sources[0].Observation)
	}
	if committed.Sources[0].Observation.Commit == before.Sources[0].Observation.Commit {
		t.Fatalf("commit = %q, want movement from %q", committed.Sources[0].Observation.Commit, before.Sources[0].Observation.Commit)
	}
	if err := Compare(before, committed); !errors.Is(err, ErrChanged) {
		t.Fatalf("Compare() error = %v, want committed source change", err)
	}
}

func TestCaptureTracksDerivedProvidersByContent(t *testing.T) {
	root := testsupport.TempDir(t)
	path := filepath.Join(root, "external.dart")
	if err := os.WriteFile(path, []byte("void external() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := workspace.Repository{
		Name: "dart-package:external:1234", Derived: true, Path: root, RealPath: root,
		Languages: []string{"dart"}, Roots: []string{"lib"},
	}
	before, err := Capture(context.Background(), "default", "resolver-1", "analyzer-1", []workspace.Repository{repository})
	if err != nil {
		t.Fatal(err)
	}
	observed := before.Sources[0]
	if !observed.Derived || observed.Observation.Branch != "" || observed.Observation.Dirty ||
		!strings.HasPrefix(observed.Observation.Commit, "content-") ||
		observed.Observation.Worktree != "derived:dart-package:external:1234" {
		t.Fatalf("derived observation = %#v, want a content-addressed derived source", observed)
	}
	if err := os.WriteFile(path, []byte("void external() { print('changed'); }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := Capture(context.Background(), "default", "resolver-1", "analyzer-1", []workspace.Repository{repository})
	if err != nil {
		t.Fatal(err)
	}
	if err := Compare(before, after); !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), repository.Name) {
		t.Fatalf("Compare() error = %v, want named derived-source change", err)
	}
}

func TestManifestRoundTripsWithTheCandidateGeneration(t *testing.T) {
	repository := sourceFixtureRepository(t)
	manifest, err := Capture(context.Background(), "default", "resolver-1", "analyzer-1", []workspace.Repository{repository})
	if err != nil {
		t.Fatal(err)
	}
	candidate := testsupport.TempDir(t)
	if err := Write(candidate, manifest); err != nil {
		t.Fatal(err)
	}
	loaded, err := Read(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := Compare(manifest, loaded); err != nil {
		t.Fatalf("round trip comparison = %v", err)
	}
	if _, err := os.Stat(filepath.Join(candidate, FileName)); err != nil {
		t.Fatalf("published source observation file: %v", err)
	}
}

func TestReadRejectsASecondSourceObservationDocument(t *testing.T) {
	repository := sourceFixtureRepository(t)
	manifest, err := Capture(context.Background(), "default", "resolver-1", "analyzer-1", []workspace.Repository{repository})
	if err != nil {
		t.Fatal(err)
	}
	candidate := testsupport.TempDir(t)
	if err := Write(candidate, manifest); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(candidate, FileName)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{}\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(candidate); err == nil || !strings.Contains(err.Error(), "multiple documents") {
		t.Fatalf("Read() error = %v, want second-document refusal", err)
	}
}

func TestTreeDigestTracksAnalysedFilesButNotDocumentation(t *testing.T) {
	root := testsupport.TempDir(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := TreeDigest(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if after, err := TreeDigest(context.Background(), root); err != nil || after != before {
		t.Fatalf("documentation digest = %q, %v; want unchanged %q", after, err, before)
	}
	if err := os.WriteFile(path, []byte("package source\nconst Value = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if after, err := TreeDigest(context.Background(), root); err != nil || after == before {
		t.Fatalf("source digest = %q, %v; want a changed digest from %q", after, err, before)
	}
}

func TestTreeDigestFramesFileContentWithoutAmbiguousBoundaries(t *testing.T) {
	combined := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(combined, "a.go"), []byte("X\x00b.go\x00Y"), 0o600); err != nil {
		t.Fatal(err)
	}
	separate := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(separate, "a.go"), []byte("X"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(separate, "b.go"), []byte("Y"), 0o600); err != nil {
		t.Fatal(err)
	}
	combinedDigest, err := TreeDigest(context.Background(), combined)
	if err != nil {
		t.Fatal(err)
	}
	separateDigest, err := TreeDigest(context.Background(), separate)
	if err != nil {
		t.Fatal(err)
	}
	if combinedDigest == separateDigest {
		t.Fatal("TreeDigest() collided for file contents that mimic the old entry boundary")
	}
}

func TestManifestValidateRejectsTamperedObservation(t *testing.T) {
	observation, err := topology.NewSourceObservation("source-main", "0123456789abcdef", "main", false,
		strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Version:             CurrentVersion,
		Profile:             "default",
		ResolverVersion:     "resolver-1",
		AnalyzerFingerprint: "analyzer-1",
		Sources:             []Source{{Repository: "source", Observation: observation}},
	}
	manifest.Sources[0].Observation.ID = "obs-tampered"
	if err := manifest.Validate(); err == nil || !errors.Is(err, topology.ErrInvalidSourceObservation) {
		t.Fatalf("Manifest.Validate() error = %v, want invalid observation", err)
	}
}

func TestCompareNamesEveryChangedDimension(t *testing.T) {
	baseline := validManifest(t)
	cases := []struct {
		name   string
		change func(*Manifest)
		want   string
	}{
		{
			name: "profile",
			change: func(manifest *Manifest) {
				manifest.Profile = "other"
			},
			want: "profile changed",
		},
		{
			name: "resolver",
			change: func(manifest *Manifest) {
				manifest.ResolverVersion = "resolver-2"
			},
			want: "resolver changed",
		},
		{
			name: "analyzer",
			change: func(manifest *Manifest) {
				manifest.AnalyzerFingerprint = "analyzer-2"
			},
			want: "analyzer configuration changed",
		},
		{
			name: "provider",
			change: func(manifest *Manifest) {
				manifest.Sources[0].Repository = "other-source"
			},
			want: "provider set changed",
		},
		{
			name: "policy",
			change: func(manifest *Manifest) {
				manifest.Sources[0].Policy.Languages = []string{"rust"}
			},
			want: "source \"source\"",
		},
		{
			name: "source count",
			change: func(manifest *Manifest) {
				extra := manifest.Sources[0]
				extra.Repository = "second-source"
				manifest.Sources = append(manifest.Sources, extra)
			},
			want: "source count changed",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := baseline
			changed.Sources = append([]Source(nil), baseline.Sources...)
			changed.Sources[0].Policy.Languages = append([]string(nil), baseline.Sources[0].Policy.Languages...)
			test.change(&changed)
			err := Compare(baseline, changed)
			if !errors.Is(err, ErrChanged) || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compare() error = %v, want changed observation naming %q", err, test.want)
			}
		})
	}
}

func TestManifestValidateRejectsIncompleteFields(t *testing.T) {
	baseline := validManifest(t)
	cases := []struct {
		name   string
		change func(*Manifest)
		want   string
	}{
		{
			name: "version",
			change: func(manifest *Manifest) {
				manifest.Version = CurrentVersion + 1
			},
			want: "version",
		},
		{
			name: "profile",
			change: func(manifest *Manifest) {
				manifest.Profile = "bad/profile"
			},
			want: "profile",
		},
		{
			name: "resolver",
			change: func(manifest *Manifest) {
				manifest.ResolverVersion = ""
			},
			want: "resolver version",
		},
		{
			name: "analyzer",
			change: func(manifest *Manifest) {
				manifest.AnalyzerFingerprint = ""
			},
			want: "analyzer fingerprint",
		},
		{
			name: "source name",
			change: func(manifest *Manifest) {
				manifest.Sources[0].Repository = ""
			},
			want: "repository",
		},
		{
			name: "duplicate source",
			change: func(manifest *Manifest) {
				manifest.Sources = append(manifest.Sources, manifest.Sources[0])
			},
			want: "duplicate",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			changed := baseline
			changed.Sources = append([]Source(nil), baseline.Sources...)
			test.change(&changed)
			if err := changed.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Manifest.Validate() error = %v, want %q refusal", err, test.want)
			}
		})
	}
}

func TestReadAndWriteRejectMissingOrCorruptCandidates(t *testing.T) {
	candidate := testsupport.TempDir(t)
	if _, err := Read(candidate); err == nil || !strings.Contains(err.Error(), "read source observations") {
		t.Fatalf("Read() error = %v, want missing-file refusal", err)
	}
	path := filepath.Join(candidate, FileName)
	if err := os.WriteFile(path, []byte(`{"version":1,"unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(candidate); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Read() error = %v, want unknown-field refusal", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(candidate); err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("Read() error = %v, want incomplete-manifest refusal", err)
	}
	if err := Write("", validManifest(t)); err == nil || !strings.Contains(err.Error(), "candidate path") {
		t.Fatalf("Write() error = %v, want empty-candidate refusal", err)
	}
	fileCandidate := filepath.Join(testsupport.TempDir(t), "not-a-directory")
	if err := os.WriteFile(fileCandidate, []byte("file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(fileCandidate, validManifest(t)); err == nil || !strings.Contains(err.Error(), "create source observation candidate") {
		t.Fatalf("Write() error = %v, want non-directory candidate refusal", err)
	}
}

func TestFileAndTreeDigestsRejectUnavailableOrCancelledInputs(t *testing.T) {
	root := testsupport.TempDir(t)
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package source\nconst One = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := FileDigest(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package source\nconst Two = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := FileDigest(nil, path)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatalf("FileDigest() = %q after content change, want a new digest", after)
	}
	if fromTree, err := TreeDigest(context.Background(), path); err != nil || fromTree != after {
		t.Fatalf("TreeDigest(file) = %q, %v; want FileDigest() %q", fromTree, err, after)
	}
	missing := filepath.Join(root, "missing.go")
	if _, err := FileDigest(context.Background(), missing); !errors.Is(err, ErrAbsent) {
		t.Fatalf("FileDigest() error = %v, want absent input", err)
	}
	if _, err := TreeDigest(context.Background(), missing); !errors.Is(err, ErrAbsent) {
		t.Fatalf("TreeDigest() error = %v, want absent input", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FileDigest(cancelled, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("FileDigest() error = %v, want cancellation", err)
	}
	if _, err := TreeDigest(cancelled, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("TreeDigest() error = %v, want cancellation", err)
	}
}

func TestIsAnalyzedSourceRecognizesSourceAndBuildInputs(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{name: "main.go", want: true},
		{name: "Cargo.lock", want: true},
		{name: "requirements-dev.txt", want: true},
		{name: "README.md", want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := IsAnalyzedSource(test.name); got != test.want {
				t.Fatalf("IsAnalyzedSource(%q) = %t, want %t", test.name, got, test.want)
			}
		})
	}
}

func validManifest(t *testing.T) Manifest {
	t.Helper()
	observation, err := topology.NewSourceObservation("source-main", "0123456789abcdef", "main", false,
		strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return Manifest{
		Version:             CurrentVersion,
		Profile:             "default",
		ResolverVersion:     "resolver-1",
		AnalyzerFingerprint: "analyzer-1",
		Sources: []Source{{
			Repository:  "source",
			Observation: observation,
			Policy:      Policy{Languages: []string{"go"}},
		}},
	}
}

func sourceFixtureRepository(t *testing.T) workspace.Repository {
	t.Helper()
	root := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package source\nconst Value = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-q")
	runGit(t, root, "add", "main.go")
	runGit(t, root, "-c", "user.email=source@example.test", "-c", "user.name=Source Fixture", "commit", "-qm", "initial")
	registry, err := workspace.NewRegistry(context.Background(), config.RepositoriesFile{Repositories: []config.Repository{{
		Name: "source", Path: root, Languages: []string{"go"}, Exclusions: []string{"generated"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return registry.List()[0]
}

func runGit(t *testing.T, directory string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), err, output)
	}
}
