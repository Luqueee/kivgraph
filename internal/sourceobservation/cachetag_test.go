package sourceobservation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func digestOf(t *testing.T, root string) string {
	t.Helper()
	digest, err := TreeDigest(context.Background(), root)
	if err != nil {
		t.Fatalf("TreeDigest() error = %v", err)
	}
	return digest
}

// The digest equals a walk that applies the rule by hand: with tagged
// directories present it is the digest of the tree with them deleted, and an
// edit below one never reaches it. A directory without a valid tag still does.
func TestTreeDigestEqualsTheTreeWithTheTaggedDirectoriesDeleted(t *testing.T) {
	root := testsupport.TempDir(t)
	skipped := testsupport.WriteCacheTagTree(t, root)
	got := digestOf(t, root)

	if err := os.WriteFile(filepath.Join(skipped[0], "gen.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if digestOf(t, root) != got {
		t.Fatal("an edit below a tagged directory reached the digest")
	}
	for _, directory := range skipped {
		if err := os.RemoveAll(directory); err != nil {
			t.Fatal(err)
		}
	}
	if digestOf(t, root) != got {
		t.Fatal("the digest with tagged directories differs from the tree without them")
	}
	for _, name := range []string{"plain", "wrongsig", "emptytag", "dirtag", "linktag", "src"} {
		before := digestOf(t, root)
		if err := os.WriteFile(filepath.Join(root, name, "gen.go"), []byte("package changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if digestOf(t, root) == before {
			t.Errorf("an edit in %q, which carries no valid tag, is invisible to the digest", name)
		}
	}
}

// The root of the walk is never skipped, whatever it holds: a repository that
// is itself tagged is the one the user asked to index.
func TestTreeDigestNeverSkipsItsOwnRoot(t *testing.T) {
	root := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "CACHEDIR.TAG"), []byte(testsupport.CacheTagSignature+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := digestOf(t, root)
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if digestOf(t, root) == before {
		t.Fatal("a tag at the root hid the whole tree")
	}
}
