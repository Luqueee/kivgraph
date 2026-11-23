//go:build darwin || linux

package sourceobservation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// A tagged directory is stamped even though it is skipped, so a tag that
// appears or disappears after the capture changes a stamp and the signature
// stops vouching for a digest that was taken under the other answer.
func TestSignatureDoesNotVouchAcrossATagAppearingOrGoing(t *testing.T) {
	appearing := testsupport.TempDir(t)
	testsupport.WriteCacheTagTree(t, appearing)
	going := testsupport.TempDir(t)
	skipped := testsupport.WriteCacheTagTree(t, going)

	time.Sleep(racyWindow + 150*time.Millisecond)

	for name, test := range map[string]struct {
		root  string
		apply func() error
	}{
		"a tag appears in a walked directory": {appearing, func() error {
			return os.WriteFile(filepath.Join(appearing, "plain", "CACHEDIR.TAG"), []byte(testsupport.CacheTagSignature+"\n"), 0o600)
		}},
		"the tag of a skipped directory goes": {going, func() error {
			return os.Remove(filepath.Join(skipped[0], "CACHEDIR.TAG"))
		}},
	} {
		t.Run(name, func(t *testing.T) {
			before, signature, err := TreeDigestSigned(context.Background(), test.root)
			if err != nil {
				t.Fatal(err)
			}
			if !signature.Unchanged() {
				t.Fatal("control: a quiet tree older than the racy window was not reported unchanged")
			}
			if err := test.apply(); err != nil {
				t.Fatal(err)
			}
			after, err := TreeDigest(context.Background(), test.root)
			if err != nil {
				t.Fatal(err)
			}
			if before == after {
				t.Fatal("control: the change did not move the digest, so it proves nothing")
			}
			if signature.Unchanged() {
				t.Fatal("the signature vouched for a digest taken under the other tag answer")
			}
		})
	}
}

// Whether a directory with a `.git` is another checkout depends on the text of
// a pointer and of the `.gitmodules` above it. Either can be rewritten in
// place without moving a stamp the signature holds, so a tree that holds one
// is never vouched for.
func TestSignatureDoesNotVouchForATreeThatHoldsAGitEntry(t *testing.T) {
	plain := testsupport.TempDir(t)
	withSubmodule := testsupport.TempDir(t)
	for _, root := range []string{plain, withSubmodule} {
		if err := os.MkdirAll(filepath.Join(root, "sub"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "sub", "a.go"), []byte("package sub\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(withSubmodule, "sub", ".git"), []byte("gitdir: ../.git/modules/sub\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(racyWindow + 150*time.Millisecond)

	_, signature, err := TreeDigestSigned(context.Background(), plain)
	if err != nil {
		t.Fatal(err)
	}
	if !signature.Unchanged() {
		t.Fatal("control: a quiet tree with no `.git` below it was not reported unchanged")
	}
	_, signature, err = TreeDigestSigned(context.Background(), withSubmodule)
	if err != nil {
		t.Fatal(err)
	}
	if signature.Unchanged() {
		t.Fatal("the signature vouched for a tree whose skip decision it cannot see change")
	}
}
