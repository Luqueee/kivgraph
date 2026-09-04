package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	kivmcp "github.com/Luqueee/kivgraph/internal/mcp"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestConfiguredProfileIndexerServesGenerationBoundFreshness(t *testing.T) {
	root := testsupport.TempDir(t)
	path := filepath.Join(root, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: path}); err != nil {
		t.Fatal(err)
	}
	if err := config.CreateProfile(path, "other"); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadProfile(path, "default")
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-qm", "fixture"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, output)
		}
	}
	source := filepath.Join(repo, "source.go")
	if err := os.WriteFile(source, []byte("package fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	repositories := config.RepositoriesFile{Version: 1, Repositories: []config.Repository{{Name: "fixture", Path: repo, Languages: []string{"go"}}}}
	if err := config.SaveRepositories(loaded.RepositoriesPath, repositories); err != nil {
		t.Fatal(err)
	}
	registry, err := workspace.NewRegistry(t.Context(), repositories)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := freshness.Capture(t.Context(), registry.List())
	if err != nil {
		t.Fatal(err)
	}
	if err := freshness.Save(filepath.Dir(loaded.Config.Storage.DatabasePath), 7, digest); err != nil {
		t.Fatal(err)
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{SchemaVersion: 2, ResolverVersion: "test"}, 7, time.Now(), 1)
	if err != nil {
		t.Fatal(err)
	}
	store, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{"default": hotsnapshot.NewSnapshotStore(snapshot), "other": hotsnapshot.NewSnapshotStore(snapshot)})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	indexer := newProfileProjectIndexer(path, store)
	indexer.setProfileWatcher(func(string, config.Loaded, *hotsnapshot.SnapshotStore) { t.Error("freshness query started a watcher") })
	server := kivmcp.NewServerWithSnapshotStoreAndIndexer(store, indexer)
	a, b := sdkmcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	check := func(profile, want string) {
		t.Helper()
		result, err := cs.CallTool(t.Context(), &sdkmcp.CallToolParams{Name: "graph_status", Arguments: map[string]any{"profile": []string{profile}}})
		if err != nil || result.IsError {
			t.Fatalf("graph_status: %v %v", result, err)
		}
		var response struct {
			Results struct {
				ContentFreshness *freshness.Status `json:"content_freshness"`
			} `json:"results"`
		}
		text, ok := result.Content[0].(*sdkmcp.TextContent)
		if !ok {
			t.Fatalf("unexpected content: %T", result.Content[0])
		}
		if err := json.Unmarshal([]byte(text.Text), &response); err != nil {
			t.Fatal(err)
		}
		got := response.Results.ContentFreshness
		if want == "" {
			if got != nil {
				t.Fatalf("borrowed freshness: %+v", got)
			}
			return
		}
		if got == nil || got.State != want || got.Generation != 7 {
			t.Fatalf("freshness = %+v, want %s generation 7", got, want)
		}
	}
	check("default", "fresh")
	if got := indexer.ContentFreshness(nil); got.State != "fresh" {
		t.Fatalf("nil context: %+v", got)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if got := indexer.ContentFreshness(cancelled); got.State != "unavailable" {
		t.Fatalf("cancelled: %+v", got)
	}
	attestation := filepath.Join(filepath.Dir(loaded.Config.Storage.DatabasePath), "freshness", "00000000000000000007.json")
	if err := os.Remove(attestation); err != nil {
		t.Fatal(err)
	}
	check("default", "unverified")
	if err := freshness.Save(filepath.Dir(loaded.Config.Storage.DatabasePath), 7, digest); err != nil {
		t.Fatal(err)
	}
	check("other", "")
	check("*", "")
	// Changing the configuration pointer must not attest a different profile
	// than the running server's actual default, even at the same generation.
	if err := config.UseProfile(path, "other"); err != nil {
		t.Fatal(err)
	}
	check("default", "fresh")
	if err := os.WriteFile(source, []byte("package changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check("default", "stale")
	if err := os.WriteFile(source, []byte("package fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check("default", "fresh")
	added := filepath.Join(repo, "new.go")
	if err := os.WriteFile(added, []byte("package fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	check("default", "stale")
	if err := os.Remove(added); err != nil {
		t.Fatal(err)
	}
	check("default", "fresh")
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	check("default", "stale")
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	check("default", "unavailable")
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	if got := indexer.ContentFreshness(t.Context()); got.State != "unavailable" {
		t.Fatalf("removed configuration: %+v", got)
	}
	if err := os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(root, "other-config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: newPath}); err != nil {
		t.Fatal(err)
	}
	newConfiguration, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite storage only: the runtime must not combine its old snapshot with
	// an attestation found at a newly configured path.
	newConfiguration = []byte(strings.ReplaceAll(string(newConfiguration), filepath.Join(root, "state"), filepath.Join(root, "other-state")))
	if err := os.WriteFile(path, newConfiguration, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "other-state"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadProfile(path, "default"); err != nil {
		t.Fatal(err)
	}
	if got := indexer.ContentFreshness(t.Context()); got.State != "unavailable" || !strings.Contains(got.Detail, "storage changed") {
		t.Fatalf("moved storage: %+v", got)
	}
}

func TestProfileIndexerFreshnessFailsClosedWithoutConfiguration(t *testing.T) {
	indexer := newProfileProjectIndexer(filepath.Join(t.TempDir(), "missing.yaml"), hotsnapshot.NewSnapshotStore(nil))
	verifier, ok := any(indexer).(interface {
		ContentFreshness(context.Context) freshness.Status
	})
	if !ok {
		t.Fatal("configured indexer does not expose content freshness")
	}
	if got := verifier.ContentFreshness(t.Context()); got.State != "unavailable" {
		t.Fatalf("missing configuration: %+v", got)
	}
	var absent *profileProjectIndexer
	verifier = any(absent).(interface {
		ContentFreshness(context.Context) freshness.Status
	})
	if got := verifier.ContentFreshness(t.Context()); got.State != "unverified" {
		t.Fatalf("nil indexer: %+v", got)
	}
}
