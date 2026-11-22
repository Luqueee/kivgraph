package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// stampContentHashes records the digest of every indexed file that has none.
//
// `get_source` serves a body from the lines the graph recorded only when the
// file still has the digest it was analysed with (ADR 0040), and no loader
// ever wrote one: every body was served as "file changed", re-anchored by
// name, on files nobody had touched.
//
// The digest has to be of the bytes that were analysed, and this runs after
// the analysis. So a file modified since the pass started gets none: it can
// only be reported as not fresh, never as fresh, which is the direction ADR
// 0040 allows. A file that cannot be read gets none either. A cancelled pass
// stops before the next read and returns the context's error.
func stampContentHashes(ctx context.Context, set *facts.Set, started time.Time) error {
	roots := make(map[string]string, len(set.Repositories))
	for _, repository := range set.Repositories {
		roots[repository.Key] = repository.RootPath
	}
	for index := range set.Files {
		file := &set.Files[index]
		root := roots[file.RepositoryKey]
		if file.ContentHash != "" || root == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		digest, ok := analysedDigest(filepath.Join(root, filepath.FromSlash(file.Path)), started)
		if ok {
			file.ContentHash = digest
		}
	}
	return nil
}

// analysedDigest hashes a file only when it provably did not change while it
// was read: its metadata agrees before and after the read, and it predates the
// pass. A write that keeps the modification time is not detected.
func analysedDigest(filename string, started time.Time) (string, bool) {
	before, err := os.Stat(filename)
	if err != nil || !before.Mode().IsRegular() || !before.ModTime().Before(started) {
		return "", false
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", false
	}
	after, err := os.Stat(filename)
	if err != nil || !after.Mode().IsRegular() ||
		after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return "", false
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), true
}
