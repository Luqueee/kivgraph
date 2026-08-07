//go:build !webassets

package webassets

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewReportsUnavailableBundle(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	New().ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(response.Body.String(), "Web bundle unavailable") {
		t.Fatalf("body = %q", response.Body.String())
	}
}
