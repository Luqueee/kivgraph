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
			if err == nil {
				t.Fatalf("%s: request after worker exit returned no error", protocol)
			}
			for _, want := range []string{"exit status 23", "fixture analyzer crash"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("%s: diagnosis missing %q: %v", protocol, want, err)
				}
			}
			if size := len(err.Error()); size > 17000 {
				t.Errorf("%s: diagnosis is %d bytes, want at most 17000", protocol, size)
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
		t.Fatal("analyzer reader did not stop after done closed while output was blocked")
	}
}

func TestDartReadersDeliverDecodedFrameBeforeObservedShutdown(t *testing.T) {
	done := make(chan struct{})
	close(done)

	analyzerOutput := make(chan analyzerMessage, 1)
	readAnalyzerMessages(strings.NewReader(`{"event":"server.status"}`+"\n"), analyzerOutput, done)
	if message, ok := <-analyzerOutput; !ok || message.Event != "server.status" {
		t.Fatalf("analyzer final frame = %#v, open = %v", message, ok)
	}

	lspOutput := make(chan rpcMessage, 1)
	body := `{"jsonrpc":"2.0","id":1,"result":{"ready":true}}`
	framed := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	readMessages(strings.NewReader(framed), lspOutput, done)
	if message, ok := <-lspOutput; !ok || string(message.ID) != "1" {
		t.Fatalf("LSP final frame = %#v, open = %v", message, ok)
	}
}
