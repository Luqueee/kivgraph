package dartloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestDartWorkerCrashHelper(t *testing.T) {
	if os.Getenv("KIVGRAPH_DART_CRASH_FIXTURE") != "1" {
		return
	}
	fmt.Fprint(os.Stderr, strings.Repeat("x", 20000)+"\nfixture analyzer crash\n")
	os.Exit(23)
}

func TestDartWorkerFailurePreservesExitAndBoundedStderr(t *testing.T) {
	t.Setenv("KIVGRAPH_DART_CRASH_FIXTURE", "1")
	command := os.Args[0] + " -test.run=^TestDartWorkerCrashHelper$"
	for _, protocol := range []string{"lsp", "analyzer"} {
		t.Run(protocol, func(t *testing.T) {
			var err error
			if protocol == "lsp" {
				c, e := start(t.Context(), command, "", testsupport.TempDir(t))
				if e != nil {
					t.Fatal(e)
				}
				defer c.close()
				<-c.worker.done
				err = c.notify("textDocument/didOpen", map[string]any{})
			} else {
				c, e := startAnalyzer(t.Context(), command, "", testsupport.TempDir(t))
				if e != nil {
					t.Fatal(e)
				}
				defer c.close()
				<-c.worker.done
				_, err = c.call(t.Context(), "analysis.getNavigation", map[string]any{})
			}
			if err == nil || !strings.Contains(err.Error(), "exit status 23") || !strings.Contains(err.Error(), "fixture analyzer crash") || len(err.Error()) > 17000 {
				t.Fatalf("missing or unbounded worker diagnosis: %v", err)
			}
		})
	}
}

func TestDartWorkerCancellationKeepsCallerCause(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w := &workerProcess{ctx: ctx, done: make(chan struct{})}
	if err := w.failure("request", errors.New("broken pipe")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDartReaderStopsWhenNobodyDrainsNotifications(t *testing.T) {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		readAnalyzerMessages(strings.NewReader(`{"event":"server.status"}`+"\n"), make(chan analyzerMessage), done)
		close(finished)
	}()
	close(done)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("reader leaked on a full output channel")
	}
}
