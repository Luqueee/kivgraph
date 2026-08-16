//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestOpenHealthCloseAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	ctx := context.Background()

	database, err := Open(ctx, path, DefaultConfig())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := database.Health(ctx); err != nil {
		t.Fatalf("Health() before close error = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := database.Health(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("Health() after close error = %v, want ErrClosed", err)
	}

	reopened, err := Open(ctx, path, DefaultConfig())
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer reopened.Close()
	if err := reopened.Health(ctx); err != nil {
		t.Fatalf("Health() after reopen error = %v", err)
	}
}

func TestOpenConvertsNativeError(t *testing.T) {
	_, err := Open(context.Background(), t.TempDir(), DefaultConfig())
	if err == nil {
		t.Fatal("Open() error = nil, want native error")
	}
	var wrapped *Error
	if !errors.As(err, &wrapped) || wrapped.Op != "open" {
		t.Fatalf("Open() error = %#v, want Kivgraph open error", err)
	}
}
