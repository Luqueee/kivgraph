package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/executable"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) { return 0, errors.New("body read failed") }
func (failingReadCloser) Close() error             { return nil }

// requireReleasePlatform guards the tests that exercise the install path. They
// need the platform to have a published bundle, because Run refuses everything
// else before it touches the network.
func requireReleasePlatform(t *testing.T) {
	t.Helper()
	if err := checkReleaseTarget(runtime.GOOS, runtime.GOARCH); err != nil {
		t.Skipf("no bundle is published for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func TestRunRefusesAPlatformWithoutAPublishedBundle(t *testing.T) {
	// Pinned rather than read from releaseTargets, so that publishing a
	// platform is a decision taken here as well as there. The release
	// workflow's matrix is the other half of this list; the two drifting is
	// how a reader gets told to update to an asset that was never built.
	supported := []string{"linux/amd64", "darwin/arm64", "windows/amd64"}
	if strings.Join(releaseTargets, ",") != strings.Join(supported, ",") {
		t.Fatalf("releaseTargets = %v, want %v", releaseTargets, supported)
	}
	for _, target := range []string{"windows/arm64", "linux/arm64", "darwin/amd64", "linux/386"} {
		goos, goarch, _ := strings.Cut(target, "/")
		err := checkReleaseTarget(goos, goarch)
		if err == nil {
			t.Fatalf("checkReleaseTarget(%q) = nil, want a refusal", target)
		}
		if !strings.Contains(err.Error(), target) {
			t.Fatalf("checkReleaseTarget(%q) error = %v, want it to name the observed platform", target, err)
		}
		for _, published := range supported {
			if !strings.Contains(err.Error(), published) {
				t.Fatalf("checkReleaseTarget(%q) error = %v, want it to name %s", target, err, published)
			}
		}
	}
	for _, target := range supported {
		goos, goarch, _ := strings.Cut(target, "/")
		if err := checkReleaseTarget(goos, goarch); err != nil {
			t.Fatalf("checkReleaseTarget(%q) error = %v, want nil", target, err)
		}
	}
}

// TestReleaseNamesTrackTheRunningPlatform pins the published asset and bundle
// directory names. The literals are the release contract shared with the build
// script, the installer and the workflows, so they are spelled out here rather
// than recomputed from runtime.GOOS/GOARCH.
func TestReleaseNamesTrackTheRunningPlatform(t *testing.T) {
	// The extension is part of the contract and differs by platform: Windows
	// publishes a zip because that is what a reader there can open, and an
	// update that looked for a .tar.gz would ask the release for an asset
	// that does not exist.
	names := map[string]struct{ directory, archive string }{
		"linux/amd64":   {"kivgraph-linux-amd64", "kivgraph-linux-amd64.tar.gz"},
		"darwin/arm64":  {"kivgraph-darwin-arm64", "kivgraph-darwin-arm64.tar.gz"},
		"windows/amd64": {"kivgraph-windows-amd64", "kivgraph-windows-amd64.zip"},
	}
	want, ok := names[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		t.Skipf("no bundle is published for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if bundleDirName != want.directory {
		t.Fatalf("bundleDirName = %q, want %q", bundleDirName, want.directory)
	}
	if archiveName != want.archive {
		t.Fatalf("archiveName = %q, want %q", archiveName, want.archive)
	}
}

func TestValidateBundleRejectsAForeignTarget(t *testing.T) {
	root := t.TempDir()
	manifest := `{"product":"kivgraph","release":"0.1.1","target":{"os":"plan9","arch":"mips"},"source":{"dirty":false}}`
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	err := validateBundle(root, "0.1.1")
	if err == nil {
		t.Fatal("validateBundle() = nil, want a target mismatch")
	}
	if !strings.Contains(err.Error(), "plan9/mips") || !strings.Contains(err.Error(), runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("validateBundle() error = %v, want it to name plan9/mips and %s/%s", err, runtime.GOOS, runtime.GOARCH)
	}
}

func TestRunRejectsAnUnknownChannelBeforeTheNetwork(t *testing.T) {
	requireReleasePlatform(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()

	_, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0",
		Channel:        "nightly",
		CheckOnly:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "update channel") {
		t.Fatalf("Run() error = %v, want invalid channel", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want no network request for invalid channel", requests)
	}
}

func TestResolveChannelTreatsBuildMetadataAsStable(t *testing.T) {
	channel, err := resolveChannel("0.1.0+build-dev", "")
	if err != nil {
		t.Fatalf("resolveChannel(current=%q, requested=%q) error = %v", "0.1.0+build-dev", "", err)
	}
	if channel != ChannelStable {
		t.Fatalf("resolveChannel(current=%q, requested=%q) = %q, want %q", "0.1.0+build-dev", "", channel, ChannelStable)
	}
}

func TestRunUsesStableEndpointForBuildMetadataVersion(t *testing.T) {
	requireReleasePlatform(t)
	var requestPath string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestPath = request.URL.Path
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(githubRelease{
			TagName: "v0.1.1",
			Assets: []githubAsset{
				{Name: archiveName, BrowserDownloadURL: server.URL + "/archive"},
				{Name: checksumsName, BrowserDownloadURL: server.URL + "/checksums"},
			},
		})
	}))
	defer server.Close()

	result, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0+build-dev",
		CheckOnly:      true,
	})
	if err != nil {
		t.Fatalf("Run(current=%q) error = %v", "0.1.0+build-dev", err)
	}
	if requestPath != "/repos/"+Repository+"/releases/latest" || result.Channel != ChannelStable {
		t.Fatalf("Run(current=%q) path=%q channel=%q, want stable endpoint and channel %q", "0.1.0+build-dev", requestPath, result.Channel, ChannelStable)
	}
}

func TestRunSelectsTheHighestPublishedDevelopmentRelease(t *testing.T) {
	requireReleasePlatform(t)
	var releaseBase string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/"+Repository+"/releases" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		assets := func() []githubAsset {
			return []githubAsset{
				{Name: archiveName, BrowserDownloadURL: releaseBase + "/archive"},
				{Name: checksumsName, BrowserDownloadURL: releaseBase + "/checksums"},
			}
		}
		_ = json.NewEncoder(writer).Encode([]githubRelease{
			{TagName: "v0.1.1-dev.1", Prerelease: true, Assets: assets()},
			{TagName: "v0.1.0", Assets: assets()},
			{TagName: "v0.1.2-dev.1", Prerelease: true, Assets: assets()},
			// GitHub's flag alone is not enough: a malformed stable tag marked
			// prerelease must not enter the development stream.
			{TagName: "v0.9.0+build-dev", Prerelease: true, Assets: assets()},
			{TagName: "v0.1.3-dev.1", Prerelease: true, Draft: true, Assets: assets()},
		})
	}))
	defer server.Close()
	releaseBase = server.URL

	result, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.1-dev.1",
		CheckOnly:      true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.UpdateAvailable || result.LatestVersion != "0.1.2-dev.1" || result.Channel != ChannelDevelopment {
		t.Fatalf("result = %#v, want the highest published dev release", result)
	}
}

func TestRunReportsWhenNoDevelopmentReleaseExists(t *testing.T) {
	requireReleasePlatform(t)
	releaseList := []githubRelease{{TagName: "v0.1.0", Assets: nil}}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/"+Repository+"/releases" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(releaseList)
	}))
	defer server.Close()

	_, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0-dev.1",
		CheckOnly:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "no published development release") {
		t.Fatalf("Run(current=%q, release-list=%#v) error = %v, want no-development-release error", "0.1.0-dev.1", releaseList, err)
	}
}

func TestRunRejectsAStableEndpointMarkedPrerelease(t *testing.T) {
	requireReleasePlatform(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/"+Repository+"/releases/latest" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(githubRelease{
			TagName: "v0.1.1", Prerelease: true,
		})
	}))
	defer server.Close()

	_, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0",
		CheckOnly:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "marked prerelease") {
		t.Fatalf("Run() error = %v, want stable-channel rejection", err)
	}
}

func TestRunRejectsADevelopmentReleaseWithoutThePlatformAsset(t *testing.T) {
	requireReleasePlatform(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/"+Repository+"/releases" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode([]githubRelease{{TagName: "v0.1.1-dev.1", Prerelease: true}})
	}))
	defer server.Close()

	_, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0-dev.1",
		CheckOnly:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "missing asset") {
		t.Fatalf("Run() error = %v, want missing-asset rejection", err)
	}
}

func TestRunRejectsADevelopmentReleaseWithoutChecksums(t *testing.T) {
	requireReleasePlatform(t)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/"+Repository+"/releases" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode([]githubRelease{{
			TagName: "v0.1.1-dev.1", Prerelease: true,
			Assets: []githubAsset{{Name: archiveName, BrowserDownloadURL: server.URL + "/archive"}},
		}})
	}))
	defer server.Close()

	_, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0-dev.1",
		CheckOnly:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "missing asset") {
		t.Fatalf("Run() error = %v, want missing-checksum rejection", err)
	}
}

func TestRunRejectsMalformedDevelopmentResponse(t *testing.T) {
	requireReleasePlatform(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/"+Repository+"/releases" {
			http.NotFound(writer, request)
			return
		}
		_, _ = io.WriteString(writer, "not-json")
	}))
	defer server.Close()

	_, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0-dev.1",
		CheckOnly:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "decode development releases") {
		t.Fatalf("Run() error = %v, want malformed-response error", err)
	}
}

func TestRunReportsDevelopmentRequestErrors(t *testing.T) {
	requireReleasePlatform(t)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network unavailable")
	})}
	_, err := Run(context.Background(), Options{
		APIBaseURL:     "https://example.invalid",
		Client:         client,
		CurrentVersion: "0.1.0-dev.1",
		CheckOnly:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "query development releases") {
		t.Fatalf("Run() error = %v, want request error", err)
	}
}

func TestRunReportsMalformedDevelopmentURL(t *testing.T) {
	requireReleasePlatform(t)
	_, err := Run(context.Background(), Options{
		APIBaseURL:     "://invalid",
		CurrentVersion: "0.1.0-dev.1",
		CheckOnly:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "create development release request") {
		t.Fatalf("Run() error = %v, want malformed-URL error", err)
	}
}

func TestRunReportsDevelopmentResponseBodyErrors(t *testing.T) {
	requireReleasePlatform(t)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Status:     "502 Bad Gateway",
			Body:       failingReadCloser{},
		}, nil
	})}
	_, err := Run(context.Background(), Options{
		APIBaseURL:     "https://example.invalid",
		Client:         client,
		CurrentVersion: "0.1.0-dev.1",
		CheckOnly:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "read response") {
		t.Fatalf("Run() error = %v, want response-body error", err)
	}
}

func TestRunReportsDevelopmentHTTPStatus(t *testing.T) {
	requireReleasePlatform(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/"+Repository+"/releases" {
			http.NotFound(writer, request)
			return
		}
		http.Error(writer, "GitHub is unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	_, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0-dev.1",
		CheckOnly:      true,
	})
	if err == nil || !strings.Contains(err.Error(), "GitHub is unavailable") {
		t.Fatalf("Run() error = %v, want HTTP response detail", err)
	}
}

// TestVerifyChecksumFileSelectsThePlatformAsset pins the release SHA256SUMS
// layout: one file listing every published artifact, lexicographic, no paths.
func TestVerifyChecksumFileSelectsThePlatformAsset(t *testing.T) {
	dir := t.TempDir()
	payload := []byte("bundle payload")
	archivePath := filepath.Join(dir, archiveName)
	if err := os.WriteFile(archivePath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	other := "kivgraph-linux-amd64.tar.gz"
	if other == archiveName {
		other = "kivgraph-darwin-arm64.tar.gz"
	}
	// The release file is sorted by artifact name, exactly as the workflow
	// publishes it, and never lists itself.
	lines := []string{
		strings.Repeat("1", 64) + "  install.sh",
		hex.EncodeToString(digest[:]) + "  " + archiveName,
		strings.Repeat("2", 64) + "  " + other,
	}
	sort.Slice(lines, func(i, j int) bool { return lines[i][66:] < lines[j][66:] })
	sums := strings.Join(lines, "\n") + "\n"
	checksumsPath := filepath.Join(dir, checksumsName)
	if err := os.WriteFile(checksumsPath, []byte(sums), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyChecksumFile(archivePath, checksumsPath, archiveName); err != nil {
		t.Fatalf("verifyChecksumFile() error = %v, want nil", err)
	}

	withoutPlatform := strings.Join([]string{
		strings.Repeat("1", 64) + "  install.sh",
		strings.Repeat("2", 64) + "  " + other,
	}, "\n") + "\n"
	if err := os.WriteFile(checksumsPath, []byte(withoutPlatform), 0o600); err != nil {
		t.Fatal(err)
	}
	err := verifyChecksumFile(archivePath, checksumsPath, archiveName)
	if err == nil {
		t.Fatal("verifyChecksumFile() = nil, want a missing entry error")
	}
	if !strings.Contains(err.Error(), archiveName) {
		t.Fatalf("verifyChecksumFile() error = %v, want it to name %s", err, archiveName)
	}
}

func TestRunCheckReportsAvailableReleaseWithoutDownloading(t *testing.T) {
	requireReleasePlatform(t)
	downloads := 0
	var releaseBase string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/repos/"+Repository+"/releases/latest" {
			writeRelease(t, writer, releaseBase, "v0.1.1")
			return
		}
		downloads++
		http.NotFound(writer, request)
	}))
	defer server.Close()
	releaseBase = server.URL

	result, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0",
		CheckOnly:      true,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.UpdateAvailable || result.LatestVersion != "0.1.1" {
		t.Fatalf("result = %#v, want available 0.1.1", result)
	}
	if downloads != 0 {
		t.Fatalf("download requests = %d, want 0", downloads)
	}
}

func TestRunRejectsMismatchedReleaseChecksum(t *testing.T) {
	requireReleasePlatform(t)
	root := t.TempDir()
	binaryPath := filepath.Join(root, "bin", "kivgraph")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"product":"kivgraph"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	var releaseBase string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/" + Repository + "/releases/latest":
			writeRelease(t, writer, releaseBase, "v0.1.1")
		case "/archive":
			_, _ = io.WriteString(writer, "not a valid archive")
		case "/checksums":
			_, _ = io.WriteString(writer, strings.Repeat("0", 64)+"  "+archiveName+"\n")
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	releaseBase = server.URL

	_, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0",
		ExecutablePath: binaryPath,
	})
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Run() error = %v, want checksum mismatch", err)
	}
}

func TestRunInstallsValidatedBundleAtomically(t *testing.T) {
	requireReleasePlatform(t)
	root := t.TempDir()
	oldBinary := filepath.Join(root, "kivgraph", "bin", executable.Name("kivgraph"))
	if err := os.MkdirAll(filepath.Dir(oldBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "kivgraph", "manifest.json"), []byte(`{"product":"kivgraph"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldBinary, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	// validateBundle runs the bundled binary and compares what it prints
	// against the release. The bundled binary is this test binary, so the
	// variable is what makes it answer as a kivgraph of that version instead
	// of running the suite again.
	t.Setenv(bundleVersionVariable, "0.1.1")

	archive := testBundleArchive(t, "0.1.1")
	digest := sha256.Sum256(archive)
	checksum := hex.EncodeToString(digest[:]) + "  " + archiveName + "\n"
	var releaseBase string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/repos/" + Repository + "/releases/latest":
			writeRelease(t, writer, releaseBase, "v0.1.1")
		case "/archive":
			_, _ = writer.Write(archive)
		case "/checksums":
			_, _ = io.WriteString(writer, checksum)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	releaseBase = server.URL

	result, err := Run(context.Background(), Options{
		APIBaseURL:     server.URL,
		Client:         server.Client(),
		CurrentVersion: "0.1.0",
		ExecutablePath: oldBinary,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Updated || result.LatestVersion != "0.1.1" {
		t.Fatalf("result = %#v, want updated 0.1.1", result)
	}
	// The assertion is on behaviour rather than bytes: the installed binary is
	// asked what version it is, which is the question a reader asks after an
	// update and the one validateBundle asked before installing it.
	answer, err := exec.Command(oldBinary, "version").Output()
	if err != nil {
		t.Fatalf("run the installed binary: %v", err)
	}
	if got := strings.TrimSpace(string(answer)); got != "0.1.1" {
		t.Fatalf("installed binary reports %q, want the release that was installed", got)
	}
	if _, err := os.Stat(filepath.Join(root, "kivgraph.previous")); !os.IsNotExist(err) {
		t.Fatalf("previous bundle stat error = %v, want absent", err)
	}
}

func TestArchivePathNameRejectsTraversal(t *testing.T) {
	unsafe := []string{
		"/tmp/kivgraph",
		bundleDirName + "/../../outside",
		bundleDirName + "\\outside",
		// A bundle built for another platform must not be extracted here.
		"kivgraph-plan9-mips/bin/kivgraph",
	}
	for _, name := range unsafe {
		if _, err := archivePathName(name); err == nil {
			t.Fatalf("archivePathName(%q) accepted unsafe path", name)
		}
	}
	valid := bundleDirName + "/bin/kivgraph"
	if got, err := archivePathName(valid); err != nil || got != valid {
		t.Fatalf("archivePathName(%q) = %q, %v", valid, got, err)
	}
}

func writeRelease(t *testing.T, writer http.ResponseWriter, baseURL, tag string) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(githubRelease{
		TagName: tag,
		Assets: []githubAsset{
			{Name: archiveName, BrowserDownloadURL: baseURL + "/archive"},
			{Name: checksumsName, BrowserDownloadURL: baseURL + "/checksums"},
		},
	}); err != nil {
		t.Fatalf("encode release: %v", err)
	}
}

// bundleWorkerName is the shim the distribution builder writes: a `.cmd` on
// Windows and an extensionless script everywhere else. The fixture carries the
// same name the real bundle does, because validateBundle looks for what the
// platform would run and a fixture that named it differently would be testing
// a bundle nobody ships.
func bundleWorkerName() string {
	if runtime.GOOS == "windows" {
		return "kivgraph-ts-worker.cmd"
	}
	return "kivgraph-ts-worker"
}

// bundleProgram returns the bytes of the fake installed binary. validateBundle
// runs it and compares what it prints against the release, so it has to be a
// program the platform can actually execute -- which rules out the `#!/bin/sh`
// script this fixture used to carry, because Windows has no interpreter for a
// `#!` line.
//
// This test binary is that program. It prints the version and exits when
// bundleVersionVariable is set, which the caller sets around the update, and
// runs the suite otherwise. Eleven megabytes through an in-memory archive is
// the price of exercising the install path on both platforms rather than one.
func bundleProgram(t *testing.T) []byte {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve the test binary: %v", err)
	}
	contents, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read the test binary: %v", err)
	}
	return contents
}

func testBundleArchive(t *testing.T, version string) []byte {
	t.Helper()
	files := map[string][]byte{
		bundleDirName + "/manifest.json":                      []byte(fmt.Sprintf(`{"product":"kivgraph","release":"%s","target":{"os":"%s","arch":"%s"},"source":{"dirty":false}}`, version, runtime.GOOS, runtime.GOARCH)),
		bundleDirName + "/bin/" + executable.Name("kivgraph"): bundleProgram(t),
		bundleDirName + "/bin/" + bundleWorkerName():          []byte("#!/bin/sh\nexit 0\n"),
	}
	var checksumLines []string
	for name, contents := range files {
		digest := sha256.Sum256(contents)
		checksumLines = append(checksumLines, hex.EncodeToString(digest[:])+"  "+strings.TrimPrefix(name, bundleDirName+"/"))
	}
	// The checksums are relative to the bundle root and must be sorted like the
	// distribution builder's output.
	sort.Strings(checksumLines)
	files[bundleDirName+"/"+checksumsName] = []byte(strings.Join(checksumLines, "\n") + "\n")

	// The container follows the platform, because that is what the release
	// publishes and what archiveName tells Run to ask for.
	if archiveExtension == ".zip" {
		return testZipArchive(t, files)
	}

	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, name := range sortedFileNames(files) {
		contents := files[name]
		header := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(contents)),
		}
		if isBundleProgram(name) {
			header.Mode = 0o755
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

func sortedFileNames(files map[string][]byte) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// isBundleProgram reports whether a bundle entry has to come out executable.
// On Windows nothing does -- there are no mode bits -- but the archive is
// written the same way on both so that one fixture describes one bundle.
func isBundleProgram(name string) bool {
	base := path.Base(name)
	return base == executable.Name("kivgraph") || base == bundleWorkerName()
}

func testZipArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, name := range sortedFileNames(files) {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if isBundleProgram(name) {
			header.SetMode(0o755)
		} else {
			header.SetMode(0o644)
		}
		member, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := member.Write(files[name]); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return archive.Bytes()
}
