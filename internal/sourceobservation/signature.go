package sourceobservation

import (
	"io/fs"
	"os"
	"time"
)

// racyWindow is how close to the moment a signature was captured an entry's
// modification or change time has to be for the entry not to be trusted.
//
// A file written in the same timestamp tick as the capture can be written again
// in that tick without its stat moving. Git calls such entries racily clean and
// reads them again; so does this, for every entry newer than the window. The
// window has to be at least the granularity of the filesystem's timestamps, which
// is why it is two seconds, the coarsest local one (FAT).
const racyWindow = 2 * time.Second

// stamp is what one stat says about an entry.
//
// The change time is part of it and is not optional. A modification time can be
// set by a program, so an edit followed by a restore of the old modification time
// leaves everything else a stamp holds equal; the inode change time cannot be
// set, and moves on every write.
type stamp struct {
	size         int64
	mtime, ctime int64
	inode        uint64
	mode         uint32
	// racy is decided once, at capture: the entry is too new to be vouched for.
	racy bool
}

// TreeSignature is the stat of every directory a digest's walk entered and of
// every analysed file it hashed, taken as the walk went, together with the digest
// that walk produced. It lives for the length of one pass and is never persisted:
// the manifest and its digest are the identity of a generation, and this only
// lets a later observation skip reading a tree it can prove it would read the same.
//
// The stamp of a directory is taken before the directory is listed and the stamp
// of a file before it is opened, so a change that lands in between leaves a stamp
// older than the content the digest saw, and the signature reports it.
type TreeSignature struct {
	root        string
	captured    time.Time
	digest      string
	directories map[string]stamp
	files       map[string]stamp
	// trusted is false once anything makes the signature unable to vouch for
	// its tree: a symlinked source, an entry that vanished mid-walk, a platform
	// whose stat does not carry a change time.
	trusted bool
}

func newTreeSignature(root string) *TreeSignature {
	return &TreeSignature{
		root:        root,
		captured:    time.Now(),
		directories: make(map[string]stamp),
		files:       make(map[string]stamp),
		trusted:     true,
	}
}

// distrust is safe on a nil signature: a walk that records nothing has nothing to
// distrust. It is called for an entry that vanished mid-walk, which shows the tree
// was moving while it was captured, and for a source reached through a link.
func (signature *TreeSignature) distrust() {
	if signature != nil {
		signature.trusted = false
	}
}

func (signature *TreeSignature) recordDirectory(path string, info fs.FileInfo) {
	recorded, ok := signature.stampOf(info)
	if !ok {
		signature.distrust()
		return
	}
	signature.directories[path] = recorded
}

func (signature *TreeSignature) recordFile(path string, info fs.FileInfo) {
	recorded, ok := signature.stampOf(info)
	if !ok {
		signature.distrust()
		return
	}
	signature.files[path] = recorded
}

func (signature *TreeSignature) stampOf(info fs.FileInfo) (stamp, bool) {
	changed, inode, mode, ok := statChange(info)
	if !ok {
		return stamp{}, false
	}
	recorded := stamp{
		size: info.Size(), mtime: info.ModTime().UnixNano(),
		ctime: changed, inode: inode, mode: mode,
	}
	newest := info.ModTime()
	if when := time.Unix(0, changed); when.After(newest) {
		newest = when
	}
	// An entry stamped in the future, or within the window of the capture, cannot
	// be told apart from one a later write in the same tick would leave alike.
	recorded.racy = signature.captured.Sub(newest) < racyWindow
	return recorded, true
}

// Unchanged reports whether every directory and file the signature recorded
// still has the stamp it had. It reads no directory and opens no file.
//
// It answers true only when it can vouch for the tree, and false for everything
// else: a differing stamp, a missing entry, a failed stat, an entry too new to be
// trusted, a signature that was never able to vouch. False means "read the tree",
// never "the tree changed", so it costs time and cannot cost correctness.
func (signature *TreeSignature) Unchanged() bool {
	if signature == nil || !signature.trusted {
		return false
	}
	for _, entries := range []map[string]stamp{signature.directories, signature.files} {
		for path, before := range entries {
			if before.racy {
				return false
			}
			info, err := os.Lstat(path)
			if err != nil {
				return false
			}
			now, ok := signature.stampOf(info)
			if !ok || now.size != before.size || now.mtime != before.mtime || now.ctime != before.ctime ||
				now.inode != before.inode || now.mode != before.mode {
				return false
			}
		}
	}
	return true
}
