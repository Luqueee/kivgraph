package resilience

import (
	"context"
	"errors"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/app"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	mcpserver "github.com/Luqueee/kivgraph/internal/mcp"
	"github.com/Luqueee/kivgraph/internal/tsworker"
	"github.com/Luqueee/kivgraph/internal/watcher"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestLifecycleClosesMCPWatcherWorkerAndSnapshot(t *testing.T) {
	ctx := context.Background()
	store := hotsnapshot.NewSnapshotStore(publishedSnapshot(t))
	server := mcpserver.NewServerWithSnapshotStore(store)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "shutdown-test", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		serverSession.Close()
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Close()
	})

	root := t.TempDir()
	fileWatcher, err := watcher.New([]workspace.Repository{{Name: "repo", RealPath: root}})
	if err != nil {
		clientSession.Close()
		serverSession.Close()
		t.Fatalf("watcher.New() error = %v", err)
	}
	watcherDone := make(chan error, 1)

	supervisor := startWorker(t, func(options *tsworker.Options) {
		options.ShutdownGrace = 2 * time.Second
	})

	lifecycle := app.NewLifecycle(ctx)
	for _, resource := range []app.Resource{
		{Name: "MCP client connection", Close: func(context.Context) error { return clientSession.Close() }},
		{Name: "MCP server connection", Close: func(context.Context) error { return serverSession.Close() }},
		{Name: "HotSnapshot", Close: func(context.Context) error { store.Close(); return nil }},
		{Name: "watcher", Close: func(context.Context) error { return fileWatcher.Close() }},
		{Name: "worker", Close: supervisor.Close},
	} {
		if err := lifecycle.Add(resource); err != nil {
			t.Fatalf("Add(%s) error = %v", resource.Name, err)
		}
	}
	if err := lifecycle.Go("watcher loop", func(ctx context.Context) error {
		err := fileWatcher.Run(ctx)
		watcherDone <- err
		return err
	}); err != nil {
		t.Fatalf("Go(watcher) error = %v", err)
	}

	if err := lifecycle.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case err := <-watcherDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("watcher Run() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watcher Run() did not stop")
	}
	if status := supervisor.Status(); status.State != tsworker.StateClosed {
		t.Fatalf("worker state = %q, want %q", status.State, tsworker.StateClosed)
	}
	if store.Load() != nil {
		t.Fatal("HotSnapshot remained readable after shutdown")
	}
	if _, ok := <-fileWatcher.Events(); ok {
		t.Fatal("watcher events channel remained open")
	}
	if _, ok := <-fileWatcher.Errors(); ok {
		t.Fatal("watcher errors channel remained open")
	}

	if _, err := clientSession.ListTools(context.Background(), nil); err == nil {
		t.Fatal("MCP client connection remained usable after shutdown")
	}
}
