package indexing

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// gitRepository builds a checkout the registry can read: it resolves HEAD, the
// branch and the working tree state with real git, so a bare directory is not a
// repository as far as this service is concerned.
func gitRepository(t *testing.T) string {
	t.Helper()
	directory := testsupport.TempDir(t)
	for _, arguments := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "user.name", "Kivgraph Test"},
		{"-c", "commit.gpgsign=false", "commit", "--quiet", "--allow-empty", "-m", "fixture"},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = directory
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", arguments, err, strings.TrimSpace(string(output)))
		}
	}
	return directory
}

// childService builds a service over a registry on disk with the index step
// substituted, so the routing can be tested without running a pass.
func childService(
	t *testing.T,
	repositories []config.Repository,
	run func(context.Context, DetachedOptions) (FullDocument, error),
) (*Service, string) {
	t.Helper()
	root := testsupport.TempDir(t)
	registryPath := filepath.Join(root, "repositories.yaml")
	if repositories == nil {
		repositories = []config.Repository{}
	}
	registry := config.RepositoriesFile{Version: config.CurrentSchemaVersion, Repositories: repositories}
	if err := config.SaveRepositories(registryPath, registry); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	configuration := config.DefaultConfig()
	configuration.Storage.DatabasePath = filepath.Join(root, "state", "graph.db")
	loaded := config.Loaded{
		Config:           configuration,
		Repositories:     registry,
		ConfigPath:       filepath.Join(root, "config.yaml"),
		RepositoriesPath: registryPath,
	}
	service := NewService(loaded, hotsnapshot.NewSnapshotStore(nil), "test-resolver", root)
	service.index = run
	return service, registryPath
}

// TestReindexRunsThePassOutOfThisProcess is the whole point of ADR 0042: the
// process that answers queries must not be the one holding a type universe per
// Go module. The recorded options are the other half -- a child told nothing
// about the configuration would index the default state directory.
func TestReindexRunsThePassOutOfThisProcess(t *testing.T) {
	var recorded DetachedOptions
	calls := 0
	service, _ := childService(t,
		[]config.Repository{{Name: "one", Path: gitRepository(t), Languages: []string{"go"}}},
		func(_ context.Context, options DetachedOptions) (FullDocument, error) {
			calls++
			recorded = options
			return FullDocument{Passed: true, GenerationID: "000003"}, nil
		})

	// Publication needs a generation store this test does not build, so the
	// error that follows is expected; what it proves is the order, and that
	// the pass itself never ran in this process.
	err := service.Reindex(context.Background())
	if err == nil || !strings.Contains(err.Error(), "publish reindexed snapshot") {
		t.Fatalf("Reindex() error = %v, want the publication step to be what failed", err)
	}
	if calls != 1 {
		t.Fatalf("child ran %d times, want exactly 1", calls)
	}
	if recorded.ConfigPath != service.loaded.ConfigPath ||
		recorded.RepositoriesPath != service.loaded.RepositoriesPath {
		t.Fatalf("child options = %#v, want this service's configuration", recorded)
	}
	if recorded.ResolverVersion != "test-resolver" {
		t.Fatalf("child resolver version = %q, want the service's own", recorded.ResolverVersion)
	}
	if recorded.WorkingDirectory == "" {
		t.Fatal("child got no working directory, so a relative repository path would resolve elsewhere")
	}
}

// TestReindexWithNothingRegisteredRunsNothing keeps a resynchronisation from
// paying for a process that has no repository to read.
func TestReindexWithNothingRegisteredRunsNothing(t *testing.T) {
	calls := 0
	service, _ := childService(t, nil, func(context.Context, DetachedOptions) (FullDocument, error) {
		calls++
		return FullDocument{Passed: true}, nil
	})

	if err := service.Reindex(context.Background()); err != nil {
		t.Fatalf("Reindex() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("child ran %d times, want 0", calls)
	}
}

func TestReindexMarksFreshnessOnlyAfterOwningPublication(t *testing.T) {
	for name, test := range map[string]struct {
		currentID         string
		currentSnapshotID uint64
		documentID        string
		servedID          uint64
		want              freshness.Status
	}{
		"owns publication": {
			currentID: "000076", currentSnapshotID: 76, documentID: "000076",
			want: freshness.Status{Generation: 76, State: "fresh"},
		},
		"another publisher serves the same generation": {
			currentID: "000076", currentSnapshotID: 76, documentID: "000076", servedID: 76,
			want: freshness.Status{
				Generation: 76,
				State:      "unverified",
				Detail:     "cached content freshness belongs to another generation",
			},
		},
		"another publisher changed CURRENT": {
			currentID: "000077", currentSnapshotID: 77, documentID: "000076", servedID: 77,
			want: freshness.Status{
				Generation: 77,
				State:      "unverified",
				Detail:     "cached content freshness belongs to another generation",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			service, _ := childService(t,
				[]config.Repository{{Name: "one", Path: gitRepository(t), Languages: []string{"go"}}},
				func(context.Context, DetachedOptions) (FullDocument, error) {
					return FullDocument{Passed: true, GenerationID: test.documentID}, nil
				})
			stateRoot := filepath.Dir(service.loaded.Config.Storage.DatabasePath)
			snapshot, err := hotsnapshot.BuildGraphSnapshot(
				hotsnapshot.LadybugSnapshotRows{}, test.currentSnapshotID,
				time.Unix(int64(test.currentSnapshotID), 0).UTC(), 1,
			)
			if err != nil {
				t.Fatalf("build current generation %s fixture: %v", test.currentID, err)
			}
			generations, err := generation.New(stateRoot, generation.DefaultConfig())
			if err != nil {
				t.Fatalf("generation.New(root=%q, current=%s) error = %v", stateRoot, test.currentID, err)
			}
			_, err = generations.Publish(t.Context(), generation.PublishRequest{
				ID:                     test.currentID,
				EstimatedSnapshotBytes: 1,
				Build: func(_ context.Context, directory string) error {
					if err := os.WriteFile(filepath.Join(directory, generation.DefaultConfig().DatabaseFile), []byte("graph"), 0o600); err != nil {
						return err
					}
					return writePublishedSnapshotFixture(directory, snapshot)
				},
				Validate: func(context.Context, generation.Generation) error { return nil },
			})
			if errors.Is(err, generation.ErrInsufficientSpace) {
				t.Skipf("this filesystem cannot satisfy the publish space policy: %v", err)
			}
			if err != nil {
				t.Fatalf("publish current generation %s fixture: %v", test.currentID, err)
			}
			if test.servedID > 0 {
				served, err := hotsnapshot.BuildGraphSnapshot(
					hotsnapshot.LadybugSnapshotRows{}, test.servedID,
					time.Unix(int64(test.servedID), 0).UTC(), 1,
				)
				if err != nil {
					t.Fatalf("build served generation %d fixture: %v", test.servedID, err)
				}
				service.snapshotStore = hotsnapshot.NewSnapshotStore(served)
			} else {
				service.snapshotStore = hotsnapshot.NewSnapshotStore(nil)
			}
			defer service.snapshotStore.Close()

			if err := service.Reindex(t.Context()); err != nil {
				t.Fatalf("Reindex() current=%s document=%s served=%d: %v", test.currentID, test.documentID, test.servedID, err)
			}
			got := service.ContentFreshness(t.Context())
			if got != test.want {
				t.Fatalf("Reindex() current=%s document=%s served=%d: freshness = %+v, want %+v", test.currentID, test.documentID, test.servedID, got, test.want)
			}
		})
	}
}

func writePublishedSnapshotFixture(directory string, snapshot *hotsnapshot.GraphSnapshot) error {
	contentDigest := sha256.Sum256([]byte("test content"))
	var data bytes.Buffer
	if _, err := hotsnapshot.WriteSnapshot(&data, snapshot, contentDigest); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, rebuild.PublishedSnapshotFileName), data.Bytes(), 0o600); err != nil {
		return err
	}
	return os.WriteFile(
		filepath.Join(directory, rebuild.PublishedSnapshotDigestFileName),
		[]byte(hex.EncodeToString(contentDigest[:])+"\n"), 0o600,
	)
}

// TestIndexProjectsRestoresTheRegistryWhenTheChildFails keeps the registry and
// the graph consistent across the new process boundary: a project whose index
// failed is not registered, exactly as when the pass ran in this process.
func TestIndexProjectsRestoresTheRegistryWhenTheChildFails(t *testing.T) {
	service, registryPath := childService(t,
		[]config.Repository{{Name: "one", Path: gitRepository(t), Languages: []string{"go"}}},
		func(context.Context, DetachedOptions) (FullDocument, error) {
			return FullDocument{}, errors.New("the child could not load a module")
		})

	_, err := service.IndexProjects(context.Background(), []Project{
		{Name: "two", Path: gitRepository(t), Languages: []string{"go"}},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "could not load a module") {
		t.Fatalf("IndexProjects() error = %v, want the child's reason", err)
	}

	saved, err := config.LoadRepositories(registryPath)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if len(saved.Repositories) != 1 || saved.Repositories[0].Name != "one" {
		t.Fatalf("registry = %#v, want the failed project removed", saved.Repositories)
	}
	if len(service.loaded.Repositories.Repositories) != 1 {
		t.Fatalf("in-memory registry = %#v, want it restored too", service.loaded.Repositories.Repositories)
	}
}

// TestIndexProjectsRegistersTheBatchBeforeIndexing fixes the handover: the child
// reads the registry from disk, so the file must already name every project of
// the batch by the time it starts.
func TestIndexProjectsRegistersTheBatchBeforeIndexing(t *testing.T) {
	registryPath := ""
	var observed []string
	service, path := childService(t, nil, func(context.Context, DetachedOptions) (FullDocument, error) {
		saved, err := config.LoadRepositories(registryPath)
		if err != nil {
			return FullDocument{}, err
		}
		for _, repository := range saved.Repositories {
			observed = append(observed, repository.Name)
		}
		return FullDocument{}, errors.New("stop after the handover")
	})
	registryPath = path

	if _, err := service.IndexProjects(context.Background(), []Project{
		{Name: "first", Path: gitRepository(t), Languages: []string{"go"}},
		{Name: "second", Path: gitRepository(t), Languages: []string{"typescript"}},
	}, nil); err == nil {
		t.Fatal("IndexProjects() error = nil, want the injected failure")
	}
	if len(observed) != 2 || observed[0] != "first" || observed[1] != "second" {
		t.Fatalf("child saw %v, want the whole batch registered before it ran", observed)
	}
	if _, err := os.Stat(registryPath); err != nil {
		t.Fatalf("stat registry: %v", err)
	}
}
