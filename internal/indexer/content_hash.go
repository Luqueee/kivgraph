package indexer

import (
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
// 0040 allows. A file that cannot be read gets none either.
func stampContentHashes(set *facts.Set, started time.Time) {
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
		filename := filepath.Join(root, filepath.FromSlash(file.Path))
		info, err := os.Stat(filename)
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().Before(started) {
			continue
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			continue
		}
		digest := sha256.Sum256(data)
		file.ContentHash = hex.EncodeToString(digest[:])
	}
}
