package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Luqueee/kivgraph/internal/checkout"
)

// cacheDirectoryTag and cacheDirectorySignature are the Cache Directory Tagging
// Specification (https://bford.info/cachedir/): a tool that fills a directory
// with reproducible cache says so by writing this file, whose first bytes are
// the signature. Cargo writes one in every target/ it creates.
const (
	cacheDirectoryTag       = "CACHEDIR.TAG"
	cacheDirectorySignature = "Signature: 8a477f597d28d172789f06886806bc55"
)

// IsCacheDirectory reports whether directory holds a valid cache directory tag
// (ADR 0122). Every walk that decides what a repository's sources are asks it
// before entering a directory, so they cannot disagree about what is skipped.
//
// The tag must be a regular file -- a symlink is not one, so a tagged-looking
// link cannot hide a tree -- directly inside directory, and start with the
// signature. Anything else, including a tag that cannot be read, is not a tag
// and the directory is walked. Callers never ask about the repository root.
func IsCacheDirectory(directory string) bool {
	tag := filepath.Join(directory, cacheDirectoryTag)
	info, err := os.Lstat(tag)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	file, err := os.Open(tag)
	if err != nil {
		return false
	}
	defer file.Close()
	prefix := make([]byte, len(cacheDirectorySignature))
	if _, err := io.ReadFull(file, prefix); err != nil {
		return false
	}
	return string(prefix) == cacheDirectorySignature
}

// IsSkippedDirectory is the one question every walk asks before entering a
// directory below the root: a tagged cache, or another checkout
// (checkout.IsOtherCheckout).
func IsSkippedDirectory(directory string) bool {
	return IsCacheDirectory(directory) || checkout.IsOtherCheckout(directory)
}

// OtherCheckouts lists the directories below the repository's root that the
// walks skip because they are another checkout, as CacheDirectories lists the
// tagged ones.
func OtherCheckouts(ctx context.Context, repository Repository) ([]string, error) {
	return skippedBelow(ctx, repository, checkout.IsOtherCheckout)
}

// CacheDirectories lists, as absolute sorted paths, the directories below the
// repository's root that the walks skip because they hold a cache directory tag.
// A tagged directory below another one is not listed: it is skipped with
// everything under it. Directories no walk reaches anyway -- `.git`,
// `node_modules`, and what the repository's exclusions name -- are not listed
// either, so the list is what the tag changed and nothing else.
//
// It exists so that a skip that decides what the graph holds is never silent.
func CacheDirectories(ctx context.Context, repository Repository) ([]string, error) {
	return skippedBelow(ctx, repository, IsCacheDirectory)
}

func skippedBelow(ctx context.Context, repository Repository, skipped func(string) bool) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rootInput := repository.RealPath
	if strings.TrimSpace(rootInput) == "" {
		rootInput = repository.Path
	}
	_, root, err := inspectRepositoryPath(rootInput)
	if err != nil {
		return nil, fmt.Errorf("list cache directories root: %w", err)
	}
	if err := validateExclusionPatterns(root, repository.Exclusions); err != nil {
		return nil, fmt.Errorf("validate exclusions: %w", err)
	}
	var found []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// A directory that vanished mid-walk holds no tag; the root going
			// away is not that.
			if errors.Is(walkErr, fs.ErrNotExist) && path != root {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || path == root {
			return nil
		}
		if name := entry.Name(); name == ".git" || name == "node_modules" {
			return fs.SkipDir
		}
		excluded, err := MatchesExclusion(root, path, repository.Exclusions)
		if err != nil {
			return err
		}
		if excluded {
			return fs.SkipDir
		}
		if skipped(path) {
			found = append(found, path)
			return fs.SkipDir
		}
		if IsSkippedDirectory(path) {
			// Skipped for the other reason: not listed, and nothing under it.
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list cache directories below %q: %w", root, err)
	}
	return found, nil
}
