package testsupport

import (
	"os"
	"path/filepath"
	"testing"
)

// CacheTagSignature is the first line a valid CACHEDIR.TAG starts with.
const CacheTagSignature = "Signature: 8a477f597d28d172789f06886806bc55"

// WriteCacheTagTree writes, below root, a small repository whose directories
// answer the cache-tag rule every way it can be answered, and returns the
// directories the rule must skip. Each holds a Go and a TypeScript file, so
// every walk that sees it has something to report.
//
//	src                  plain source, kept
//	tagged               a valid tag: skipped, with everything below it
//	src/nested/target    a valid tag below a kept directory: skipped
//	plain                no tag, kept
//	wrongsig             a tag whose signature is one byte off, kept
//	emptytag             an empty tag file, kept
//	dirtag               a directory named CACHEDIR.TAG, kept
//	linktag              a tag that is a symlink to a valid one, kept
//
// linktag is left out where symlinks are unavailable.
func WriteCacheTagTree(t testing.TB, root string) (skipped []string) {
	t.Helper()
	put := func(name, body string) {
		t.Helper()
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	valid := CacheTagSignature + "\n# a cache directory tag\n"
	put("go.mod", "module example.test/tagged\n\ngo 1.22\n")
	put("package.json", `{"name": "tagged", "private": true}`)
	put("tsconfig.json", `{"compilerOptions": {}, "include": ["src"]}`)
	for _, directory := range []string{"src", "tagged", "tagged/deep", "src/nested/target", "plain", "wrongsig", "emptytag", "dirtag", "linktag"} {
		put(directory+"/gen.go", "package gen\n")
		put(directory+"/gen.ts", "export const gen = 1\n")
	}
	put("tagged/CACHEDIR.TAG", valid)
	put("src/nested/target/CACHEDIR.TAG", valid)
	put("wrongsig/CACHEDIR.TAG", "Signature: 8a477f597d28d172789f06886806bc56\n")
	put("emptytag/CACHEDIR.TAG", "")
	if err := os.MkdirAll(filepath.Join(root, "dirtag", "CACHEDIR.TAG"), 0o700); err != nil {
		t.Fatal(err)
	}
	put("store/real.tag", valid)
	if err := os.Symlink(filepath.Join(root, "store", "real.tag"), filepath.Join(root, "linktag", "CACHEDIR.TAG")); err != nil {
		t.Logf("symlinks unavailable, linktag keeps no tag: %v", err)
	}
	return []string{filepath.Join(root, "tagged"), filepath.Join(root, "src", "nested", "target")}
}
