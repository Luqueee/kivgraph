package indexing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/watcher"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestWatchSourcesReportsAChangedSourceAndStops(t *testing.T) {
	root := testsupport.TempDir(t)
	path := filepath.Join(root, "main.go")
	if err := writeSourceWatchFile(path, "package source\n"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes := make(chan watcher.ReconciliationResult, 1)
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
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("WatchSources did not report a change to %q in repository root %q", path, root)
		}
	}

changed:
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WatchSources() after cancellation = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("WatchSources(%q) did not stop after cancellation", root)
	}
}

func TestWatchSourcesRetriesARejectedChange(t *testing.T) {
	root := testsupport.TempDir(t)
	path := filepath.Join(root, "main.go")
	if err := writeSourceWatchFile(path, "package source\n"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstAttempt := make(chan struct{}, 1)
	delivered := make(chan watcher.ReconciliationResult, 1)
	done := make(chan error, 1)
	attempt := 0
	go func() {
		done <- WatchSources(ctx, SourceWatchOptions{
			Repositories: []workspace.Repository{{
				Name: "repo", Path: root, RealPath: root, Languages: []string{"go"},
			}},
			Debounce:               10 * time.Millisecond,
			MaximumBatch:           50 * time.Millisecond,
			ReconciliationInterval: time.Hour,
			OnChange: func(_ context.Context, result watcher.ReconciliationResult) error {
				attempt++
				if attempt == 1 {
					firstAttempt <- struct{}{}
					return errors.New("temporary change handler failure")
				}
				select {
				case delivered <- result:
				default:
				}
				return nil
			},
		})
	}()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for attempt := 0; ; attempt++ {
		if err := writeSourceWatchFile(path, fmt.Sprintf("package source\n\nconst retry = %d\n", attempt)); err != nil {
			t.Fatal(err)
		}
		select {
		case <-firstAttempt:
			goto rejected
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("WatchSources(%q) did not call the change handler", root)
		}
	}

rejected:
	if err := writeSourceWatchFile(path, "package source\n\nconst retry = 2\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-delivered:
		if len(result.Modified) != 1 || result.Modified[0].Path != path {
			t.Fatalf("retried source change = %#v, want one modification of %q", result, path)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("WatchSources(%q) did not deliver rejected change", root)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WatchSources() after cancellation = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("WatchSources(%q) did not stop after cancellation", root)
	}
}

func TestCoalesceReconciliationResultsRetainsLatestStatePerPath(t *testing.T) {
	first := watcher.ReconciliationResult{
		Modified: []watcher.FileState{{
			Repository:  "repo",
			Path:        "/repo/a.go",
			Operations:  watcher.OperationWrite,
			ContentHash: "first",
			Size:        1,
		}},
	}
	next := watcher.ReconciliationResult{
		Modified: []watcher.FileState{
			{Repository: "repo", Path: "/repo/a.go", Operations: watcher.OperationWrite, ContentHash: "latest", Size: 2},
			{Repository: "repo", Path: "/repo/b.go", Operations: watcher.OperationWrite, ContentHash: "other", Size: 3},
		},
		Removed: []watcher.FileState{{Repository: "repo", Path: "/repo/removed.go", Operations: watcher.OperationRemove}},
	}

	got := coalesceReconciliationResults(first, next)
	want := watcher.ReconciliationResult{
		Modified: []watcher.FileState{
			{Repository: "repo", Path: "/repo/a.go", Operations: watcher.OperationWrite, ContentHash: "latest", Size: 2},
			{Repository: "repo", Path: "/repo/b.go", Operations: watcher.OperationWrite, ContentHash: "other", Size: 3},
		},
		Removed: []watcher.FileState{{Repository: "repo", Path: "/repo/removed.go", Operations: watcher.OperationRemove}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("coalesceReconciliationResults() = %#v, want %#v", got, want)
	}
}

func TestWatchSourcesRequiresAChangeHandler(t *testing.T) {
	err := WatchSources(context.Background(), SourceWatchOptions{})
	if !errors.Is(err, ErrChangeHandlerRequired) {
		t.Fatalf("WatchSources(SourceWatchOptions{}) error = %v, want required change handler", err)
	}
}

func writeSourceWatchFile(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o600)
}
