package update

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Windows release publishes a zip, so update grew a second reader. The
// risk in a second reader is not that it fails -- that is loud -- but that it
// skips a guard the first one has, and the guards here are what stand between
// a downloaded archive and arbitrary writes on the reader's disk.
//
// So extractArchive chooses its reader by the archive's name rather than by
// runtime.GOOS, and these run on every platform. A zip path that is only ever
// exercised on the machine that ships it is a zip path nobody reviews.

// writeZip builds an archive from names to contents. A name ending in "/" is
// written as a directory entry, which is the shape a real packer produces and
// the shape the extractor has to tell apart from a file.
func writeZip(t *testing.T, entries [][2]string) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		name, contents := entry[0], entry[1]
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if strings.HasSuffix(name, "/") {
			header.SetMode(0o755 | os.ModeDir)
		} else {
			header.SetMode(0o644)
		}
		member, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := member.Write([]byte(contents)); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	path := filepath.Join(t.TempDir(), bundleDirName+".zip")
	if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	return path
}

func TestExtractArchiveReadsAZip(t *testing.T) {
	archivePath := writeZip(t, [][2]string{
		{bundleDirName + "/", ""},
		{bundleDirName + "/bin/", ""},
		{bundleDirName + "/bin/kivgraph.exe", "binary"},
		{bundleDirName + "/manifest.json", `{"product":"kivgraph"}`},
	})
	destination := t.TempDir()

	if err := extractArchive(archivePath, destination); err != nil {
		t.Fatalf("extractArchive() error = %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(destination, bundleDirName, "bin", "kivgraph.exe"))
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if string(contents) != "binary" {
		t.Fatalf("extracted binary = %q, want the archive's own bytes", contents)
	}
}

// Each of these is a guard the tarball reader has. A zip reader that reached
// the disk without one of them would be a way in that the format, and not the
// check, decided was open.
func TestExtractArchiveRefusesTheSameThingsFromAZip(t *testing.T) {
	tests := []struct {
		name    string
		entries [][2]string
		want    string
	}{
		{
			name:    "escapes the bundle directory",
			entries: [][2]string{{bundleDirName + "/../outside", "x"}},
			// The name cleans to "outside", which is not under the bundle
			// root, so this is refused as being outside it rather than as
			// unsafe -- the guard that fires is the containment one.
			want: "is outside",
		},
		{
			name:    "absolute path",
			entries: [][2]string{{"/etc/passwd", "x"}},
			want:    "unsafe path",
		},
		{
			name:    "windows separator",
			entries: [][2]string{{bundleDirName + `\outside`, "x"}},
			want:    "unsafe path",
		},
		{
			name:    "another platform's bundle",
			entries: [][2]string{{"kivgraph-plan9-mips/bin/kivgraph", "x"}},
			want:    "outside",
		},
		{
			name: "duplicate entry",
			entries: [][2]string{
				{bundleDirName + "/bin/kivgraph", "one"},
				{bundleDirName + "/bin/kivgraph", "two"},
			},
			want: "duplicate entry",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := extractArchive(writeZip(t, test.entries), t.TempDir())
			if err == nil {
				t.Fatalf("extractArchive() = nil, want a refusal naming %q", test.want)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("extractArchive() error = %v, want it to name %q", err, test.want)
			}
		})
	}
}

// A zip that carries only entries outside the bundle root extracts nothing,
// and the caller must not read that as a bundle it can install.
func TestExtractArchiveRefusesAZipWithoutTheBundle(t *testing.T) {
	archivePath := writeZip(t, nil)

	err := extractArchive(archivePath, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), bundleDirName) {
		t.Fatalf("extractArchive(empty zip) error = %v, want it to name the missing %s", err, bundleDirName)
	}
}

// A member that declares less than it carries must not write more than it
// declared: the declared size is what the running total is bounded by, so a
// member allowed to overrun it would walk past maxBundleBytes.
func TestExtractArchiveTruncatesAMemberToItsDeclaredSize(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	header := &zip.FileHeader{Name: bundleDirName + "/bin/kivgraph", Method: zip.Store}
	header.SetMode(0o755)
	member, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := member.Write([]byte("declared and delivered")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	archivePath := filepath.Join(t.TempDir(), bundleDirName+".zip")
	if err := os.WriteFile(archivePath, buffer.Bytes(), 0o600); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	destination := t.TempDir()

	if err := extractArchive(archivePath, destination); err != nil {
		t.Fatalf("extractArchive() error = %v", err)
	}
	written, err := os.ReadFile(filepath.Join(destination, bundleDirName, "bin", "kivgraph"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(written) != "declared and delivered" {
		t.Fatalf("extracted = %q, want exactly what the member declared", written)
	}
}
