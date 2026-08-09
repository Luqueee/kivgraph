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
	"sort"
	"strings"
	"testing"
)

func TestRunCheckReportsAvailableReleaseWithoutDownloading(t *testing.T) {
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
	root := t.TempDir()
	binaryPath := filepath.Join(root, "bin", "ladygraph")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{"product":"ladygraph"}`), 0o644); err != nil {
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
	root := t.TempDir()
	oldBinary := filepath.Join(root, "ladygraph", "bin", "ladygraph")
	if err := os.MkdirAll(filepath.Dir(oldBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ladygraph", "manifest.json"), []byte(`{"product":"ladygraph"}`), 0o644); err != nil {
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
	if _, err := os.Stat(filepath.Join(root, "ladygraph.previous")); !os.IsNotExist(err) {
		t.Fatalf("previous bundle stat error = %v, want absent", err)
	}
}

func TestArchivePathNameRejectsTraversal(t *testing.T) {
	for _, name := range []string{"/tmp/ladygraph", "ladygraph-linux-amd64/../../outside", "ladygraph-linux-amd64\\outside"} {
		if _, err := archivePathName(name); err == nil {
			t.Fatalf("archivePathName(%q) accepted unsafe path", name)
		}
	}
	if got, err := archivePathName("ladygraph-linux-amd64/bin/ladygraph"); err != nil || got != "ladygraph-linux-amd64/bin/ladygraph" {
		t.Fatalf("archivePathName(valid) = %q, %v", got, err)
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
		bundleDirName + "/manifest.json":           []byte(fmt.Sprintf(`{"product":"ladygraph","release":"%s","target":{"os":"linux","arch":"amd64"},"source":{"dirty":false}}`, version)),
		bundleDirName + "/bin/ladygraph":           []byte("#!/bin/sh\nprintf '%s\\n' " + version + "\n"),
		bundleDirName + "/bin/ladygraph-ts-worker": []byte("#!/bin/sh\nexit 0\n"),
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
		if strings.HasSuffix(name, "/ladygraph") || strings.HasSuffix(name, "/ladygraph-ts-worker") {
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
