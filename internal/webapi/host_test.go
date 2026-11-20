package webapi

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestLoopbackHostNamesOnlyThisMachine(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1:7777":      true,
		"127.0.0.1":           true,
		"127.1.2.3:80":        true,
		"[::1]:7777":          true,
		"[::1]":               true,
		"localhost":           true,
		"localhost:7777":      true,
		"LOCALHOST:7777":      true,
		"localhost.:7777":     true,
		"app.localhost:7777":  true,
		"":                    false,
		":7777":               false,
		"evil.example":        false,
		"evil.example:7777":   false,
		"127.0.0.1.evil.test": false,
		"localhost.evil.test": false,
		"notlocalhost":        false,
		"192.168.1.4:7777":    false,
		"0.0.0.0:7777":        false,
		"[2001:db8::1]:7777":  false,
		"user@127.0.0.1:7777": false,
		"127.0.0.1:7777@evil": false,
	} {
		if got := loopbackHost(host); got != want {
			t.Errorf("loopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// startViewer runs the real Run on an address and returns where it bound.
func startViewer(t *testing.T, address string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	bound := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, address, NewHandler(nil), OnListen(func(addr net.Addr) { bound <- addr }))
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(shutdownTimeout + 3*time.Second):
			t.Error("the viewer did not stop")
		}
	})
	select {
	case addr := <-bound:
		return addr.String()
	case err := <-done:
		t.Fatalf("Run(%q) returned %v", address, err)
	case <-time.After(3 * time.Second):
		t.Fatal("the viewer never reported its address")
	}
	return ""
}

func statusFor(t *testing.T, address, host string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "http://"+address+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		request.Host = host
	}
	response, err := viewerClient().Do(request)
	if err != nil {
		t.Fatalf("GET %s as %q: %v", address, host, err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

// DNS rebinding: a page on a hostname the attacker controls, resolved to
// 127.0.0.1, reaches the viewer with that hostname as Host -- and, being the
// same origin to the browser, with a matching Origin. Only Host gives it away.
func TestLoopbackViewerRefusesAForeignHostEvenWithAMatchingOrigin(t *testing.T) {
	address := startViewer(t, "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(address)

	for _, host := range []string{"evil.example:" + port, "evil.example", "192.168.1.4:" + port} {
		request, err := http.NewRequest(http.MethodGet, "http://"+address+"/healthz", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Host = host
		request.Header.Set("Origin", "http://"+host)
		response, err := viewerClient().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Errorf("Host %q with a matching Origin: status %d, want 403", host, response.StatusCode)
		}
	}
	for _, host := range []string{"", "localhost:" + port, "127.0.0.1:" + port} {
		if got := statusFor(t, address, host); got != http.StatusOK {
			t.Errorf("Host %q: status %d, want 200", host, got)
		}
	}
}

// A viewer the operator chose to expose is reached by names this process
// cannot know, so the bind is the decision there and Host is not second-guessed.
func TestExposedViewerDoesNotJudgeTheHost(t *testing.T) {
	address := startViewer(t, "0.0.0.0:0")
	_, port, _ := net.SplitHostPort(address)
	if got := statusFor(t, "127.0.0.1:"+port, "viewer.internal.example:"+port); got != http.StatusOK {
		t.Fatalf("an exposed viewer answered a named host with %d, want 200", got)
	}
}

// A client that leaves no idle connection behind, so shutdown has nothing to wait for.
func viewerClient() *http.Client {
	return &http.Client{Timeout: time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
}
