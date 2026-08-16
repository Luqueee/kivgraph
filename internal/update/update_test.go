package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

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
	supported := []string{"linux/amd64", "darwin/arm64"}
	for _, target := range []string{"windows/amd64", "linux/arm64", "darwin/amd64", "linux/386"} {
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
	names := map[string]string{
		"linux/amd64":  "kivgraph-linux-amd64",
		"darwin/arm64": "kivgraph-darwin-arm64",
	}
	wantDir, ok := names[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		t.Skipf("no bundle is published for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if bundleDirName != wantDir {
		t.Fatalf("bundleDirName = %q, want %q", bundleDirName, wantDir)
	}
	if archiveName != wantDir+".tar.gz" {
		t.Fatalf("archiveName = %q, want %q", archiveName, wantDir+".tar.gz")
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
	oldBinary := filepath.Join(root, "kivgraph", "bin", "kivgraph")
	if err := os.MkdirAll(filepath.Dir(oldBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "kivgraph", "manifest.json"), []byte(`{"product":"kivgraph"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldBinary, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

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
	newOutput, err := os.ReadFile(oldBinary)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(newOutput) != "#!/bin/sh\nprintf '%s\\n' 0.1.1\n" {
		t.Fatalf("installed binary = %q, want new release script", newOutput)
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

func testBundleArchive(t *testing.T, version string) []byte {
	t.Helper()
	files := map[string][]byte{
		bundleDirName + "/manifest.json":          []byte(fmt.Sprintf(`{"product":"kivgraph","release":"%s","target":{"os":"%s","arch":"%s"},"source":{"dirty":false}}`, version, runtime.GOOS, runtime.GOARCH)),
		bundleDirName + "/bin/kivgraph":           []byte("#!/bin/sh\nprintf '%s\\n' " + version + "\n"),
		bundleDirName + "/bin/kivgraph-ts-worker": []byte("#!/bin/sh\nexit 0\n"),
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
		if strings.HasSuffix(name, "/kivgraph") || strings.HasSuffix(name, "/kivgraph-ts-worker") {
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
