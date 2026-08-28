package scratchtree

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// entry is one thing to put in a hostile archive.
type entry struct {
	name     string
	kind     byte
	linkname string
	body     string
}

func archiveOf(t *testing.T, entries []entry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, e := range entries {
		header := &tar.Header{
			Name:     e.name,
			Typeflag: e.kind,
			Linkname: e.linkname,
			Mode:     0o644,
			Size:     int64(len(e.body)),
		}
		if e.kind != tar.TypeReg {
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if e.kind == tar.TypeReg {
			if _, err := writer.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// TestExtractRefusesATraversalName is the obvious escape, and the only one the
// first version of this package defended against.
func TestExtractRefusesATraversalName(t *testing.T) {
	root := testsupport.TempDir(t)
	outside := filepath.Join(testsupport.TempDir(t), "stolen.txt")

	for _, name := range []string{
		"../stolen.txt",
		"../../stolen.txt",
		"a/../../stolen.txt",
		"/etc/stolen.txt",
	} {
		data := archiveOf(t, []entry{{name: name, kind: tar.TypeReg, body: "owned"}})
		if err := extractTar(bytes.NewReader(data), root); err == nil {
			t.Errorf("entry %q was accepted", name)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Error("a file was written outside the tree")
	}
}

// TestExtractRefusesAnAbsoluteSymlinkTarget is the hole CodeQL found and the
// reason this file exists.
//
// The check used to be `securePath(root, filepath.Join(dir, linkname))`, and
// filepath.Join cleans an absolute operand away: Join("a", "/etc") is "a/etc",
// which passes every containment test there is. The link was then created
// pointing at /etc, and the next entry wrote through it.
func TestExtractRefusesAnAbsoluteSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture relies on unix symlink semantics")
	}
	root := testsupport.TempDir(t)
	victim := testsupport.TempDir(t)

	data := archiveOf(t, []entry{
		{name: "a/evil", kind: tar.TypeSymlink, linkname: victim},
		{name: "a/evil/owned.txt", kind: tar.TypeReg, body: "escaped"},
	})
	// The link is skipped rather than refused -- a repository may legitimately
	// contain one that leaves it -- so the entry behind it lands on a plain
	// directory inside the tree, or fails. Either is fine; what must not
	// happen is a write into the victim.
	_ = extractTar(bytes.NewReader(data), root)

	if _, err := os.Stat(filepath.Join(victim, "owned.txt")); err == nil {
		t.Fatal("the archive wrote through an absolute symlink and escaped the tree")
	}
	if link, err := os.Readlink(filepath.Join(root, "a", "evil")); err == nil {
		t.Errorf("an absolute symlink was reproduced, pointing at %q", link)
	}
}

// TestExtractRefusesARelativeSymlinkThatEscapes covers the same shape written
// the other way, which the original check did catch.
func TestExtractRefusesARelativeSymlinkThatEscapes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture relies on unix symlink semantics")
	}
	root := testsupport.TempDir(t)
	data := archiveOf(t, []entry{
		{name: "a/evil", kind: tar.TypeSymlink, linkname: "../../.."},
		{name: "a/evil/owned.txt", kind: tar.TypeReg, body: "escaped"},
	})
	_ = extractTar(bytes.NewReader(data), root)
	if link, err := os.Readlink(filepath.Join(root, "a", "evil")); err == nil {
		t.Errorf("an escaping symlink was reproduced, pointing at %q", link)
	}
}

// TestExtractRefusesAWriteThroughAPreexistingSymlink is the third shape: the
// entry name is clean and no test on it can see the problem, because the
// escape is in a directory that was already a link before extraction started.
func TestExtractRefusesAWriteThroughAPreexistingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture relies on unix symlink semantics")
	}
	root := testsupport.TempDir(t)
	victim := testsupport.TempDir(t)
	if err := os.Symlink(victim, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}

	data := archiveOf(t, []entry{{name: "linked/owned.txt", kind: tar.TypeReg, body: "escaped"}})
	if err := extractTar(bytes.NewReader(data), root); err == nil {
		t.Error("a write through a symlinked parent was accepted")
	}
	if _, err := os.Stat(filepath.Join(victim, "owned.txt")); err == nil {
		t.Fatal("the archive wrote through a pre-existing symlink and escaped the tree")
	}
}

// TestExtractMaterialisesAnInnerSymlinkAsItsContent is the other half of the
// contract. A link that stays inside the tree is legitimate content -- this
// repository carries one, CLAUDE.md pointing at AGENTS.md -- so a tree without
// it is not the repository.
//
// It arrives as a **copy of the bytes** rather than as a link, and that is the
// design: nothing in this package calls os.Symlink, so the whole class of
// archive-symlink escape cannot occur instead of being defended against. A
// build reads the bytes behind a path; the one thing it costs is a build that
// inspects link-ness, which is not a thing Maven or dotnet do.
func TestExtractMaterialisesAnInnerSymlinkAsItsContent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture relies on unix symlink semantics")
	}
	root := testsupport.TempDir(t)
	data := archiveOf(t, []entry{
		{name: "AGENTS.md", kind: tar.TypeReg, body: "instructions\n"},
		{name: "CLAUDE.md", kind: tar.TypeSymlink, linkname: "AGENTS.md"},
		{name: "docs/nested.md", kind: tar.TypeReg, body: "nested\n"},
		{name: "docs/link.md", kind: tar.TypeSymlink, linkname: "../AGENTS.md"},
	})
	if err := extractTar(bytes.NewReader(data), root); err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, path := range []string{"CLAUDE.md", "docs/link.md"} {
		full := filepath.Join(root, filepath.FromSlash(path))
		content, err := os.ReadFile(full)
		if err != nil || string(content) != "instructions\n" {
			t.Errorf("%s does not carry the target's content: %q %v", path, content, err)
		}
		info, err := os.Lstat(full)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is a symlink; nothing here may create one", path)
		}
	}
}

// TestExtractCallsNoSymlink is the property the design rests on, stated
// directly: whatever an archive contains, the tree comes out with no links in
// it at all.
func TestExtractCallsNoSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fixture relies on unix symlink semantics")
	}
	root := testsupport.TempDir(t)
	data := archiveOf(t, []entry{
		{name: "real.txt", kind: tar.TypeReg, body: "content\n"},
		{name: "inner", kind: tar.TypeSymlink, linkname: "real.txt"},
		{name: "outer", kind: tar.TypeSymlink, linkname: "../../etc/passwd"},
		{name: "absolute", kind: tar.TypeSymlink, linkname: "/etc/passwd"},
		{name: "dangling", kind: tar.TypeSymlink, linkname: "missing.txt"},
	})
	if err := extractTar(bytes.NewReader(data), root); err != nil {
		t.Fatalf("extract: %v", err)
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("the tree contains a symlink: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A link onto something absent stays absent rather than becoming an empty
	// file, so a build cannot mistake it for a source it can read.
	if _, err := os.Stat(filepath.Join(root, "dangling")); !os.IsNotExist(err) {
		t.Error("a dangling link produced a file")
	}
}

func TestSymlinkStaysInsideRejectsEveryAbsoluteShape(t *testing.T) {
	root := "/tmp/tree"
	target := filepath.Join(root, "a", "link")
	for _, linkname := range []string{
		"/etc",
		"/",
		`C:\Windows`,
		`\\server\share`,
		"",
		"../../..",
		"../../outside",
	} {
		if symlinkStaysInside(root, target, linkname) {
			t.Errorf("linkname %q was accepted", linkname)
		}
	}
	for _, linkname := range []string{"sibling", "./sibling", "../other/file", "nested/deep"} {
		if !symlinkStaysInside(root, target, linkname) {
			t.Errorf("linkname %q was refused, but it stays inside", linkname)
		}
	}
}
