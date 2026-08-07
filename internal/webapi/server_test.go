package webapi

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeServesHealthAndStopsOnCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	address := listener.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, listener, NewHandler(nil))
	}()

	client := &http.Client{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(time.Second)
	for {
		response, requestErr := client.Get("http://" + address + "/healthz")
		if requestErr == nil {
			if response.Body.Close() != nil {
				t.Fatal("closing health response failed")
			}
			if response.StatusCode != http.StatusOK {
				t.Fatalf("health status = %d, want %d", response.StatusCode, http.StatusOK)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("health request did not succeed: %v", requestErr)
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve() after cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve() did not stop after cancellation")
	}
}

func TestRunRejectsInvalidInputs(t *testing.T) {
	if err := Run(nil, "127.0.0.1:0", NewHandler(nil)); err == nil {
		t.Fatal("Run() accepted nil context")
	}
	if err := Run(context.Background(), "127.0.0.1:0", nil); err == nil {
		t.Fatal("Run() accepted nil handler")
	}
	if err := Run(context.Background(), "not-an-address", NewHandler(nil)); err == nil {
		t.Fatal("Run() accepted invalid listen address")
	}
}
func TestRunRejectsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	if err := Run(context.Background(), listener.Addr().String(), NewHandler(nil)); err == nil {
		t.Fatal("Run() accepted an occupied port")
	}
}
