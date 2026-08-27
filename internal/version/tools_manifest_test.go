package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// toolManifest is the pinned list of executables a bundle carries besides the
// ones this repository builds.
type toolManifest struct {
	SchemaVersion int `json:"schema_version"`
	Tools         []struct {
		Name         string `json:"name"`
		Version      string `json:"version"`
		Release      string `json:"release"`
		Repository   string `json:"repository"`
		Commit       string `json:"commit"`
		License      string `json:"license"`
		LicenseFiles []struct {
			Name   string `json:"name"`
			URL    string `json:"url"`
			SHA256 string `json:"sha256"`
		} `json:"license_files"`
		Platforms []struct {
			Target        string `json:"target"`
			Asset         string `json:"asset"`
			URL           string `json:"url"`
			SHA256        string `json:"sha256"`
			ArchiveFormat string `json:"archive_format"`
		} `json:"platforms"`
	} `json:"tools"`
}

// TestToolManifestPinsEveryDistributionTarget is the guard a workstation can
// run for a platform it cannot build.
//
// Only a Linux host can build the Linux bundle -- cgo links the native library
// and the project does not cross-compile -- so a missing or malformed Linux
// entry would first be noticed by a release job. This check costs nothing and
// runs everywhere.
func TestToolManifestPinsEveryDistributionTarget(t *testing.T) {
	path := filepath.Join("..", "..", "tools", "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	var manifest toolManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode %q: %v", path, err)
	}
	if manifest.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2", manifest.SchemaVersion)
	}
	if len(manifest.Tools) == 0 {
		t.Fatal("the manifest pins no tool")
	}

	digest := regexp.MustCompile(`^[0-9a-f]{64}$`)
	commit := regexp.MustCompile(`^[0-9a-f]{40}$`)
	// The distribution targets of the project, and only those.
	wanted := map[string]bool{"linux/amd64": false, "darwin/arm64": false, "windows/amd64": false}

	for _, tool := range manifest.Tools {
		if tool.Name == "" || tool.Version == "" || tool.Release == "" {
			t.Fatalf("tool %#v is not identified", tool)
		}
		if !commit.MatchString(tool.Commit) {
			t.Errorf("%s: commit %q is not a full hash", tool.Name, tool.Commit)
		}
		if tool.License == "" {
			t.Errorf("%s: no license declared", tool.Name)
		}
		if len(tool.LicenseFiles) == 0 {
			t.Errorf("%s: the bundle would distribute a binary with no license text", tool.Name)
		}
		for _, license := range tool.LicenseFiles {
			if license.Name == "" || license.URL == "" || !digest.MatchString(license.SHA256) {
				t.Errorf("%s: license %#v is not pinned", tool.Name, license)
			}
		}
		seen := make(map[string]struct{}, len(tool.Platforms))
		for _, platform := range tool.Platforms {
			if _, known := wanted[platform.Target]; !known {
				t.Errorf("%s: %q is not a distribution target of this project", tool.Name, platform.Target)
				continue
			}
			if _, duplicate := seen[platform.Target]; duplicate {
				t.Errorf("%s: %q is pinned twice", tool.Name, platform.Target)
			}
			seen[platform.Target] = struct{}{}
			wanted[platform.Target] = true
			if !digest.MatchString(platform.SHA256) {
				t.Errorf("%s %s: sha256 %q is not a digest", tool.Name, platform.Target, platform.SHA256)
			}
			// The archive format is per platform because the platforms
			// disagree, so every one has to state its own: a missing field
			// would be read as an unknown format and stop the fetch, and a
			// field that disagreed with the asset's own suffix would stop it
			// later and less clearly.
			switch platform.ArchiveFormat {
			case "gz", "zip":
				if suffix := "." + platform.ArchiveFormat; !strings.HasSuffix(platform.Asset, suffix) {
					t.Errorf("%s %s: asset %q is not a %s", tool.Name, platform.Target, platform.Asset, platform.ArchiveFormat)
				}
			default:
				t.Errorf("%s %s: archive_format %q is not one this project can extract", tool.Name, platform.Target, platform.ArchiveFormat)
			}
			if platform.Asset == "" || platform.URL == "" {
				t.Errorf("%s %s: asset or URL missing", tool.Name, platform.Target)
			}
			// A release URL that does not carry the pinned version resolves
			// to whatever the project publishes next.
			if !regexp.MustCompile(regexp.QuoteMeta(tool.Version)).MatchString(platform.URL) {
				t.Errorf("%s %s: URL %q does not name the pinned version %q",
					tool.Name, platform.Target, platform.URL, tool.Version)
			}
		}
	}
	for target, pinned := range wanted {
		if !pinned {
			t.Errorf("no tool is pinned for %s: that bundle would ship without a Rust engine", target)
		}
	}
}
