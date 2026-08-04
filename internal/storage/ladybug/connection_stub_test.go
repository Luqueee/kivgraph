//go:build !ladybug || !cgo

package ladybug

import (
	"context"
	"errors"
	"testing"
)

func TestOpenWithoutNativeSupportReturnsLuqueError(t *testing.T) {
	_, err := Open(context.Background(), ":memory:", DefaultConfig())
	if err == nil {
		t.Fatal("Open() error = nil, want unavailable error")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Open() error = %v, want ErrUnavailable", err)
	}
	var wrapped *Error
	if !errors.As(err, &wrapped) || wrapped.Op != "open" {
		t.Fatalf("Open() error = %#v, want Luque open error", err)
	}
}

func TestOpenRejectsCanceledContextBeforeNativeAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Open(ctx, ":memory:", DefaultConfig())
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("Open() error = %v, want context.Canceled", err)
	}
}
