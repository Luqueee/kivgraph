package watcher

import (
	"os"
	"path/filepath"
	"testing"
)

// `vendor` is a dependency tree only where a package manager put it, which is
// beside that manager's manifest. Anywhere else it is a directory somebody
// named, and ignoring it left every edit under it out of the graph for good:
// the watcher never fired and the reconciler never hashed it.
func TestIgnoredTreatsVendorAsDependenciesOnlyBesideAManifest(t *testing.T) {
	root := t.TempDir()
	for _, file := range []string{
		"go.mod",
		"crates/Cargo.toml",
		"web/composer.json",
		"plain/README.md",
	} {
		path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	matcher, err := newIgnoreMatcher(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		want bool
	}{
		{"vendor", true},
		{"vendor/example.com/dep/dep.go", true},
		{"crates/vendor/serde/lib.rs", true},
		{"web/vendor/autoload.php", true},
		// Source that happens to be called vendor: no manifest beside it,
		// even though one sits higher up.
		{"src/vendor/vendor.service.ts", false},
		{"plain/vendor/notes.py", false},
		{"plain/vendor", false},
		// The manifest has to be beside the directory, not beside the file.
		{"crates/src/vendor/inner.rs", false},
		// A dependency tree that contains a source-looking vendor stays ignored.
		{"vendor/example.com/dep/vendor/x.go", true},
		// The names that are dependencies wherever they appear.
		{"node_modules/pkg/index.js", true},
		{"src/node_modules/pkg/index.js", true},
		{".git/HEAD", true},
		{"main.go", false},
	}
	for _, test := range cases {
		if got := matcher.ignored(filepath.Join(root, filepath.FromSlash(test.path))); got != test.want {
			t.Errorf("ignored(%q) = %v, want %v", test.path, got, test.want)
		}
	}
	if matcher.ignored(root) {
		t.Error("the repository root itself is ignored")
	}
	if !matcher.ignored(filepath.Dir(root)) {
		t.Error("a path outside the repository is watched")
	}
}

// An exclusion the user wrote still wins: it is how a repository says its own
// vendor directory is not source, whatever is or is not beside it.
func TestIgnoredHonoursAConfiguredVendorExclusion(t *testing.T) {
	root := t.TempDir()
	matcher, err := newIgnoreMatcher(root, []string{"src/vendor"})
	if err != nil {
		t.Fatal(err)
	}
	if !matcher.ignored(filepath.Join(root, "src", "vendor", "a.ts")) {
		t.Error("a configured exclusion of src/vendor was not applied")
	}
	if matcher.ignored(filepath.Join(root, "lib", "vendor", "a.ts")) {
		t.Error("an exclusion of src/vendor ignored lib/vendor")
	}
}
