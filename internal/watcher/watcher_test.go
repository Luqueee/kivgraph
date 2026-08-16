package watcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/workspace"
	"github.com/fsnotify/fsnotify"
)

func TestOperationFromFsnotify(t *testing.T) {
	operation := operationFrom(fsnotify.Create | fsnotify.Write | fsnotify.Rename)
	for _, flag := range []Operation{OperationCreate, OperationWrite, OperationRename} {
		if !operation.Has(flag) {
			t.Fatalf("operation %s does not contain %s", operation, flag)
		}
	}
	if operation.Has(OperationRemove) || operation.Has(OperationChmod) {
		t.Fatalf("operation = %s, contains an unrelated operation", operation)
	}
	if got, want := operation.String(), "CREATE|WRITE|RENAME"; got != want {
		t.Fatalf("operation.String() = %q, want %q", got, want)
	}
}

func TestNewRecursivelyWatchesRepositoriesAndExclusions(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{
		filepath.Join(root, "src", "nested"),
		filepath.Join(root, "ignored", "nested"),
		filepath.Join(root, "node_modules", "dependency"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("MkdirAll(%q): %v", directory, err)
		}
	}

	watcher, err := New([]workspace.Repository{{
		Name:       "repo",
		RealPath:   root,
		Exclusions: []string{"ignored/**"},
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = watcher.Close() })

	paths := watcher.WatchedPaths()
	for _, want := range []string{root, filepath.Join(root, "src"), filepath.Join(root, "src", "nested")} {
		if !containsPath(paths, want) {
			t.Fatalf("WatchedPaths() = %v, missing %q", paths, want)
		}
	}
	for _, excluded := range []string{
		filepath.Join(root, "ignored"),
		filepath.Join(root, "ignored", "nested"),
		filepath.Join(root, "node_modules"),
		filepath.Join(root, "node_modules", "dependency"),
	} {
		if containsPath(paths, excluded) {
			t.Fatalf("WatchedPaths() = %v, unexpectedly contains excluded %q", paths, excluded)
		}
	}
}

// TestWatcherReportsEventsAndWatchesCreatedDirectories pins the contract that
// holds on every backend: a new path is announced, a directory created after
// the watch is watched too, and a file that already existed reports its
// modification. The write that fills a brand-new file is deliberately not
// required, because kqueue cannot see it: the file is only watched once it
// exists, by which time the write is over.
func TestWatcherReportsEventsAndWatchesCreatedDirectories(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q): %v", source, err)
	}
	existingFile := filepath.Join(source, "existing.go")
	if err := os.WriteFile(existingFile, []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", existingFile, err)
	}
	watcher, err := New([]workspace.Repository{{Name: "repo", RealPath: root}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- watcher.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-runDone; !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	}()

	firstFile := filepath.Join(source, "main.go")
	if err := os.WriteFile(firstFile, []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", firstFile, err)
	}
	firstEvent := waitForEvent(t, watcher, func(event Event) bool {
		return event.Path == firstFile && event.Operations.Has(OperationCreate|OperationWrite)
	})
	if firstEvent.Repository != "repo" {
		t.Fatalf("first event repository = %q, want repo", firstEvent.Repository)
	}

	if err := os.WriteFile(existingFile, []byte("package main // touched\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", existingFile, err)
	}
	waitForEvent(t, watcher, func(event Event) bool {
		return event.Path == existingFile && event.Operations.Has(OperationWrite)
	})

	newDirectory := filepath.Join(root, "new")
	if err := os.Mkdir(newDirectory, 0o700); err != nil {
		t.Fatalf("Mkdir(%q): %v", newDirectory, err)
	}
	waitForEvent(t, watcher, func(event Event) bool {
		return event.Path == newDirectory && event.Operations.Has(OperationCreate)
	})

	createdFile := filepath.Join(newDirectory, "created.go")
	if err := os.WriteFile(createdFile, []byte("package created\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", createdFile, err)
	}
	waitForEvent(t, watcher, func(event Event) bool {
		return event.Path == createdFile && event.Operations.Has(OperationCreate|OperationWrite)
	})
}

func TestDescriptorLimitExplainsWhichLimitToRaise(t *testing.T) {
	wrapped := descriptorLimit(fmt.Errorf("add watch: %w", syscall.EMFILE))
	if !errors.Is(wrapped, syscall.EMFILE) {
		t.Fatalf("descriptorLimit() = %v, want it to keep EMFILE", wrapped)
	}
	if !strings.Contains(wrapped.Error(), "ulimit -n") {
		t.Fatalf("descriptorLimit() = %v, want it to name the limit to raise", wrapped)
	}
	if runtime.GOOS == "darwin" && !strings.Contains(wrapped.Error(), "kern.maxfilesperproc") {
		t.Fatalf("descriptorLimit() = %v, want the darwin ceiling named", wrapped)
	}
	other := errors.New("permission denied")
	if got := descriptorLimit(other); got != other {
		t.Fatalf("descriptorLimit(other) = %v, want it untouched", got)
	}
}

func TestWatcherReturnsCancellationAndClosesChannels(t *testing.T) {
	watcher, err := New([]workspace.Repository{{Name: "repo", RealPath: t.TempDir()}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- watcher.Run(ctx) }()
	cancel()

	select {
	case err := <-runDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
	if _, ok := <-watcher.Events(); ok {
		t.Fatal("Events channel is still open")
	}
	if _, ok := <-watcher.Errors(); ok {
		t.Fatal("Errors channel is still open")
	}
}

func waitForEvent(t *testing.T, watcher *Watcher, matches func(Event) bool) Event {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event, ok := <-watcher.Events():
			if !ok {
				t.Fatal("Events channel closed before expected event")
			}
			if matches(event) {
				return event
			}
		case err, ok := <-watcher.Errors():
			if ok {
				t.Fatalf("watcher error: %v", err)
			}
			t.Fatal("Errors channel closed before expected event")
		case <-deadline.C:
			t.Fatal("timed out waiting for filesystem event")
		}
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
