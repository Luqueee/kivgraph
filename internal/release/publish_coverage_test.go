package release_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The release workflow builds one archive per matrix row and publishes them in
// a different job, and nothing connected the two: the Windows row produced a
// `.zip`, uploaded it as a build artifact, and the publish job copied only
// `*.tar.gz` into the release. Every step was green. The asset simply was not
// there, and the installer that downloads it could not have worked.
//
// That failure is invisible to the workflow itself -- a glob that matches
// nothing is not an error, it is a glob that matches nothing -- and invisible
// to a reader, because both jobs read as correct on their own. It is only
// visible by comparing them, which is what this does.
//
// It checks the extension rather than the file name, so adding an architecture
// to an existing format does not need a change here, and adding a *format*
// does.
var archiveName = regexp.MustCompile(`(?m)^\s*archive_name:\s*\S+?(\.[a-z.]+)\s*$`)

func workflow(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(data)
}

// job returns one top-level job's text. Jobs are indented two spaces under
// `jobs:`, so the next line at that indentation ends it.
func job(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, "\n  "+name+":\n")
	if start < 0 {
		t.Fatalf("release.yml has no %q job", name)
	}
	rest := source[start+1:]
	if end := regexp.MustCompile(`(?m)^  [a-z][a-z-]*:$`).FindStringIndex(rest[1:]); end != nil {
		return rest[:end[0]+1]
	}
	return rest
}

func archiveExtensions(t *testing.T, source string) []string {
	t.Helper()
	seen := make(map[string]bool, 4)
	for _, match := range archiveName.FindAllStringSubmatch(source, -1) {
		seen[match[1]] = true
	}
	if len(seen) == 0 {
		t.Fatal("release.yml declares no archive_name, so this gate is asserting nothing")
	}
	found := make([]string, 0, len(seen))
	for extension := range seen {
		found = append(found, extension)
	}
	sort.Strings(found)
	return found
}

func TestPublishCoversEveryArchiveTheMatrixBuilds(t *testing.T) {
	source := workflow(t)
	publish := job(t, source, "publish")

	for _, extension := range archiveExtensions(t, source) {
		glob := "*" + extension
		if !strings.Contains(publish, glob) {
			t.Errorf("the matrix builds %s archives and the publish job never mentions %q:\n"+
				"the build uploads them and the release drops them, with every step green",
				extension, glob)
			continue
		}
		// Reaching the release is not enough: an asset outside SHA256SUMS is
		// published with nothing to verify it against.
		if !strings.Contains(checksumLine(t, publish), glob) {
			t.Errorf("SHA256SUMS does not cover %s archives, so they ship unverifiable", extension)
		}
	}
}

// checksumLine returns the SHA256SUMS command, joined across continuations so
// that a line break inside it does not read as an absence.
func checksumLine(t *testing.T, publish string) string {
	t.Helper()
	start := strings.Index(publish, "sha256sum -- ")
	if start < 0 {
		t.Fatal("the publish job no longer builds SHA256SUMS with sha256sum")
	}
	line := publish[start:]
	if end := strings.Index(line, "SHA256SUMS\n"); end >= 0 {
		line = line[:end]
	}
	return strings.ReplaceAll(line, "\\\n", " ")
}

// The installers and uninstallers are documented entry points, and one of
// them was published while the other was not -- which makes a platform's
// README command point at a URL that returns 404.
func TestPublishShipsInstallersAndUninstallers(t *testing.T) {
	publish := job(t, workflow(t), "publish")
	assets := publishAssetList(t, publish)
	checksums := checksumInputs(t, publish)

	for _, script := range []string{"install.sh", "install.ps1", "uninstall.sh", "uninstall.ps1"} {
		if !assets[script] {
			t.Errorf("the publish job never uploads %s, so the documented command 404s",
				script)
		}
		if !slices.Contains(checksums, script) {
			t.Errorf("SHA256SUMS does not cover %s, so the published script is unverifiable", script)
		}
	}
}

func publishAssetList(t *testing.T, publish string) map[string]bool {
	t.Helper()
	start := strings.Index(publish, "assets=(")
	if start < 0 {
		t.Fatal("the publish job no longer declares its release assets")
	}
	rest := publish[start:]
	end := strings.Index(rest, "\n          )")
	if end < 0 {
		t.Fatal("the publish asset list has no closing parenthesis")
	}
	entries := regexp.MustCompile(`(?m)^\s*"release-assets/([^"]+)"\s*$`).FindAllStringSubmatch(
		rest[:end], -1,
	)
	assets := make(map[string]bool, len(entries))
	for _, entry := range entries {
		assets[entry[1]] = true
	}
	return assets
}

func checksumInputs(t *testing.T, publish string) []string {
	t.Helper()
	line := checksumLine(t, publish)
	line = strings.SplitN(line, "|", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "sha256sum" || fields[1] != "--" {
		t.Fatalf("unexpected SHA256SUMS command: %q", line)
	}
	return fields[2:]
}
