//go:build unix

package indexing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/watcher"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// One source file that cannot be read used to stop the watcher before it
// started, and with it every later invalidation of the profile. It is now
// reported, and the files around it are still watched.
func TestWatchSourcesWatchesAroundAnUnreadableSource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file without permission, so nothing is unreadable")
	}
	root := testsupport.TempDir(t)
	path := filepath.Join(root, "main.go")
	locked := filepath.Join(root, "locked.go")
	for _, file := range []string{path, locked} {
		if err := writeSourceWatchFile(file, "package source\n"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes := make(chan watcher.ReconciliationResult, 1)
	reported := make(chan error, 16)
	done := make(chan error, 1)
	go func() {
		done <- WatchSources(ctx, SourceWatchOptions{
			Repositories: []workspace.Repository{{
				Name: "repo", Path: root, RealPath: root, Languages: []string{"go"},
			}},
			Debounce:               10 * time.Millisecond,
			MaximumBatch:           50 * time.Millisecond,
			ReconciliationInterval: time.Hour,
			OnChange: func(_ context.Context, result watcher.ReconciliationResult) error {
				select {
				case changes <- result:
				default:
				}
				return nil
			},
			OnError: func(err error) {
				select {
				case reported <- err:
				default:
				}
			},
		})
	}()

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for write := 0; ; write++ {
		if err := writeSourceWatchFile(path, fmt.Sprintf("package source\n\nconst changed = %d\n", write)); err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-changes:
			if len(result.Modified) != 1 || result.Modified[0].Path != path {
				t.Fatalf("source change = %#v, want one modification of %q", result, path)
			}
			goto changed
		case err := <-done:
			t.Fatalf("WatchSources() stopped over one unreadable file: %v", err)
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("WatchSources did not report a change to %q beside an unreadable file", path)
		}
	}

changed:
	select {
	case err := <-reported:
		if !strings.Contains(err.Error(), "locked.go") {
			t.Fatalf("reported error = %v, want it to name the unreadable file", err)
		}
	default:
		t.Fatal("the unreadable file was watched around and never reported")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WatchSources() after cancellation = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WatchSources did not stop after cancellation")
	}
}
