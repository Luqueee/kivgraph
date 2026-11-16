package freshness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func capture(t *testing.T, root string) string {
	t.Helper()
	digest, err := Capture(t.Context(), []workspace.Repository{{Name: "test", Path: root}})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	return digest
}

// The inventory equals a walk that applies the rule by hand: with tagged
// directories present it is the digest of the tree with them deleted. Control:
// the directories that carry no valid tag do change it, so the rule is not
// hiding everything.
func TestInventoryEqualsTheTreeWithTheTaggedDirectoriesDeleted(t *testing.T) {
	root := testsupport.TempDir(t)
	skipped := testsupport.WriteCacheTagTree(t, root)
	got := capture(t, root)

	if err := os.WriteFile(filepath.Join(skipped[0], "gen.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if capture(t, root) != got {
		t.Fatal("an edit below a tagged directory reached the inventory")
	}
	for _, directory := range skipped {
		if err := os.RemoveAll(directory); err != nil {
			t.Fatal(err)
		}
	}
	if capture(t, root) != got {
		t.Fatal("the inventory with tagged directories differs from the tree without them")
	}
	for _, name := range []string{"plain", "wrongsig", "emptytag", "dirtag", "linktag", "src"} {
		before := capture(t, root)
		if err := os.WriteFile(filepath.Join(root, name, "gen.go"), []byte("package changed\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if capture(t, root) == before {
			t.Errorf("an edit in %q, which carries no valid tag, is invisible to the inventory", name)
		}
	}
}

// A generation published before this rule hashed the files of a tagged
// directory. The tree has not changed, but the digest it would give now does,
// so the generation is stale -- rebuilt once -- and never reported as damaged
// or unavailable.
func TestAGenerationPublishedBeforeTheRuleIsStaleNotBroken(t *testing.T) {
	root := testsupport.TempDir(t)
	skipped := testsupport.WriteCacheTagTree(t, root)
	repositories := []workspace.Repository{{Name: "test", Path: root}}

	// What the previous digest saw: the same tree with no tag to honour.
	tag := filepath.Join(skipped[0], "CACHEDIR.TAG")
	tagBody, err := os.ReadFile(tag)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tag); err != nil {
		t.Fatal(err)
	}
	stored := capture(t, root)
	state := t.TempDir()
	if err := Save(t.Context(), state, 7, stored); err != nil {
		t.Fatal(err)
	}
	if status := Check(t.Context(), state, 7, repositories); status.State != "fresh" {
		t.Fatalf("control: Check() = %+v, want fresh for the tree the digest was taken from", status)
	}

	if err := os.WriteFile(tag, tagBody, 0o600); err != nil {
		t.Fatal(err)
	}
	status := Check(t.Context(), state, 7, repositories)
	if status.State != "stale" || status.Detail != "registered source inventory changed" {
		t.Fatalf("Check() = %+v, want stale with the ordinary changed-inventory detail", status)
	}
}
