package webapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

const shutdownTimeout = 5 * time.Second

// Option configures Run.
type Option func(*settings)

type settings struct {
	onListen func(net.Addr)
}

// OnListen reports the address the viewer bound, before it serves anything.
//
// Run owns the listener, so the caller cannot resolve the address itself --
// and with a port of zero nobody can know it at all. A viewer whose log never
// says where it is listening is a viewer you cannot open.
func OnListen(report func(net.Addr)) Option {
	return func(target *settings) { target.onListen = report }
}

// Run serves the read-only viewer on address until ctx is canceled.
// Cancellation performs a bounded graceful shutdown and returns nil.
func Run(ctx context.Context, address string, handler http.Handler, options ...Option) error {
	if ctx == nil {
		return errors.New("webapi: nil context")
	}
	if handler == nil {
		return errors.New("webapi: nil handler")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	resolved := settings{}
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %q: %w", address, err)
	}
	if resolved.onListen != nil {
		resolved.onListen(listener.Addr())
	}
	return serve(ctx, listener, handler)
}

func serve(ctx context.Context, listener net.Listener, handler http.Handler) error {
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	shutdownDone := make(chan error, 1)
	go func() {
		<-runContext.Done()
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()
		shutdownDone <- server.Shutdown(shutdownContext)
	}()

	serveErr := server.Serve(listener)
	cancel()
	shutdownErr := <-shutdownDone

	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return fmt.Errorf("serve web viewer: %w", serveErr)
	}
	if shutdownErr != nil {
		return fmt.Errorf("shutdown web viewer: %w", shutdownErr)
	}
	return nil
}
