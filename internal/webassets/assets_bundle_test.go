//go:build webassets

package webassets

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewServesVersionedBundle(t *testing.T) {
	root := t.TempDir()
	webRoot := filepath.Join(root, "web", "dist")
	if err := os.MkdirAll(filepath.Join(webRoot, "assets"), 0o755); err != nil {
		t.Fatalf("create web bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>viewer</html>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webRoot, "favicon.svg"), []byte("<svg/>"), 0o644); err != nil {
		t.Fatalf("write root asset: %v", err)
	}
	if err := os.WriteFile(filepath.Join(webRoot, "assets", "app-abc.js"), []byte("console.log('viewer')"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	t.Chdir(root)

	handler := New()

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "<html>viewer</html>" {
		t.Fatalf("index response = %d %q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/favicon.svg", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "<svg/>" {
		t.Fatalf("root asset response = %d %q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/assets/app-abc.js", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "viewer") {
		t.Fatalf("asset response = %d %q", response.Code, response.Body.String())
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "public, max-age=31536000, immutable" {
		t.Fatalf("asset cache control = %q", cacheControl)
	}
}

// The viewer loads from its own origin and nowhere else, and the browser is
// told so on every response: the page, an asset, a miss and a refusal.
func TestEveryResponseCarriesAPolicyThatNamesNoOtherOrigin(t *testing.T) {
	root := t.TempDir()
	webRoot := filepath.Join(root, "web", "dist")
	if err := os.MkdirAll(filepath.Join(webRoot, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webRoot, "index.html"), []byte("<html>viewer</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webRoot, "assets", "app.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	handler := New()
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/"}, {http.MethodGet, "/assets/app.js"},
		{http.MethodGet, "/missing.js"}, {http.MethodPost, "/"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(call.method, call.path, nil))
		policy := response.Header().Get("Content-Security-Policy")
		if policy != contentSecurityPolicy {
			t.Errorf("%s %s: policy = %q", call.method, call.path, policy)
		}
		if response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s %s: no nosniff", call.method, call.path)
		}
	}
	if !strings.HasPrefix(contentSecurityPolicy, "default-src 'self';") {
		t.Errorf("policy does not default to its own origin: %q", contentSecurityPolicy)
	}
	for _, word := range []string{"http:", "https:", "*", "unsafe-eval"} {
		if strings.Contains(contentSecurityPolicy, word) {
			t.Errorf("policy admits %q: %q", word, contentSecurityPolicy)
		}
	}
}
