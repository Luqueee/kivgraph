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

// A viewer whose log never says where it listens is a viewer you cannot open,
// and with a port of zero nobody outside Run can resolve it.
func TestRunReportsTheAddressItBound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bound := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, "127.0.0.1:0", NewHandler(nil), OnListen(func(address net.Addr) {
			bound <- address
		}))
	}()

	select {
	case address := <-bound:
		host, port, err := net.SplitHostPort(address.String())
		if err != nil {
			t.Fatalf("SplitHostPort(%q) error = %v", address, err)
		}
		if host != "127.0.0.1" || port == "0" || port == "" {
			t.Fatalf("reported address = %q, want the resolved port", address)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() never reported the address it bound")
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}
