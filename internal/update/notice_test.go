package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckNoticeCachesSuccessfulLookup(t *testing.T) {
	var requests atomic.Int64
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/repos/"+Repository+"/releases/latest" {
			t.Fatalf("request path = %q, want latest release", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"tag_name": "v0.4.0",
			"assets": []map[string]string{
				{"name": archiveName, "browser_download_url": server.URL + "/archive"},
				{"name": checksumsName, "browser_download_url": server.URL + "/checksums"},
			},
		})
	}))
	defer server.Close()

	cachePath := filepath.Join(t.TempDir(), "cache", "update.json")
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	first, err := CheckNotice(context.Background(), NoticeOptions{
		APIBaseURL:     server.URL,
		CurrentVersion: "0.3.0",
		CachePath:      cachePath,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("first CheckNotice() error = %v", err)
	}
	if !first.UpdateAvailable || first.FromCache {
		t.Fatalf("first result = %#v, want an update from the network", first)
	}

	second, err := CheckNotice(context.Background(), NoticeOptions{
		APIBaseURL:     server.URL,
		CurrentVersion: "0.3.0",
		CachePath:      cachePath,
		Now:            func() time.Time { return now.Add(time.Hour) },
	})
	if err != nil {
		t.Fatalf("second CheckNotice() error = %v", err)
	}
	if !second.UpdateAvailable || !second.FromCache || second.LatestVersion != "0.4.0" {
		t.Fatalf("second result = %#v, want cached 0.4.0 update", second)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("latest release requests = %d, want 1", got)
	}
}

func TestCheckNoticeRefreshesExpiredCache(t *testing.T) {
	var version atomic.Int64
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		current := version.Add(1)
		tag := "v0.4.0"
		if current > 1 {
			tag = "v0.5.0"
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"tag_name": tag,
			"assets": []map[string]string{
				{"name": archiveName, "browser_download_url": server.URL + "/archive"},
				{"name": checksumsName, "browser_download_url": server.URL + "/checksums"},
			},
		})
	}))
	defer server.Close()

	cachePath := filepath.Join(t.TempDir(), "update.json")
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	options := NoticeOptions{
		APIBaseURL:     server.URL,
		CurrentVersion: "0.3.0",
		CachePath:      cachePath,
		CacheTTL:       time.Hour,
		Now:            func() time.Time { return now },
	}
	if _, err := CheckNotice(context.Background(), options); err != nil {
		t.Fatalf("initial CheckNotice() error = %v", err)
	}
	options.Now = func() time.Time { return now.Add(2 * time.Hour) }
	result, err := CheckNotice(context.Background(), options)
	if err != nil {
		t.Fatalf("expired CheckNotice() error = %v", err)
	}
	if result.FromCache || result.LatestVersion != "0.5.0" {
		t.Fatalf("expired result = %#v, want refreshed 0.5.0", result)
	}
	if got := version.Load(); got != 2 {
		t.Fatalf("latest release requests = %d, want 2", got)
	}
}
