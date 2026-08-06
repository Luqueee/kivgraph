package watcher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	// ContentHashAlgorithm is the stable digest algorithm used for source bytes.
	ContentHashAlgorithm  = "sha256"
	contentHashBufferSize = 32 * 1024
)

// FileHash seeds a ContentHasher with the hash already associated with a file
// in the indexed graph.
type FileHash struct {
	Repository  string
	Path        string
	ContentHash string
}

// FileState is the current hash observation for one event path.
type FileState struct {
	Repository  string
	Path        string
	Operations  Operation
	ContentHash string
	Size        int64
}

// HashResult separates files that need reindexing from files whose bytes are
// unchanged, paths removed from the tracked set, and non-regular paths such as
// directories. Only Changed should trigger source reindexing.
type HashResult struct {
	Changed   []FileState
	Unchanged []FileState
	Removed   []FileState
	Skipped   []FileState
}

// ContentHasher compares filesystem content against the last indexed hash.
// Process is serialized so a failed batch cannot partially update the cache.
type ContentHasher struct {
	mu    sync.Mutex
	known map[FileKey]string
}

// NewContentHasher creates a content hasher seeded with known indexed hashes.
// Hashes must be lowercase or uppercase hexadecimal SHA-256 digests.
func NewContentHasher(known []FileHash) (*ContentHasher, error) {
	hasher := &ContentHasher{known: make(map[FileKey]string, len(known))}
	for index, record := range known {
		key, err := newFileKey(record.Repository, record.Path)
		if err != nil {
			return nil, fmt.Errorf("known hash[%d]: %w", index, err)
		}
		if !validContentHash(record.ContentHash) {
			return nil, fmt.Errorf("known hash[%d] %s: invalid %s digest", index, key, ContentHashAlgorithm)
		}
		if _, exists := hasher.known[key]; exists {
			return nil, fmt.Errorf("known hash[%d] %s: duplicate file", index, key)
		}
		hasher.known[key] = strings.ToLower(record.ContentHash)
	}
	return hasher, nil
}

// KnownHash returns the currently indexed hash for a repository path.
func (hasher *ContentHasher) KnownHash(repository, path string) (string, bool) {
	if hasher == nil {
		return "", false
	}
	key, err := newFileKey(repository, path)
	if err != nil {
		return "", false
	}
	hasher.mu.Lock()
	defer hasher.mu.Unlock()
	hash, exists := hasher.known[key]
	return hash, exists
}

// KnownFiles returns a sorted copy of all hashes currently in the cache.
func (hasher *ContentHasher) KnownFiles() []FileHash {
	if hasher == nil {
		return nil
	}
	hasher.mu.Lock()
	defer hasher.mu.Unlock()
	records := make([]FileHash, 0, len(hasher.known))
	for key, hash := range hasher.known {
		records = append(records, FileHash{
			Repository:  key.Repository,
			Path:        key.Path,
			ContentHash: hash,
		})
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].Repository != records[right].Repository {
			return records[left].Repository < records[right].Repository
		}
		return records[left].Path < records[right].Path
	})
	return records
}

// Process hashes every unique event path in batch. A regular file with no
// known hash or a different hash is Changed; a matching hash is Unchanged. A
// missing or non-regular path is Removed only when it was previously known,
// otherwise it is Skipped so directory notifications cannot masquerade as
// source-file deletions.
func (hasher *ContentHasher) Process(ctx context.Context, batch Batch) (HashResult, error) {
	if hasher == nil {
		return HashResult{}, errors.New("process nil content hasher")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	events, err := normalizeHashEvents(batch.Events)
	if err != nil {
		return HashResult{}, err
	}
	hasher.mu.Lock()
	defer hasher.mu.Unlock()

	result := HashResult{}
	updates := make(map[FileKey]string, len(events))
	deletes := make(map[FileKey]struct{})
	for _, event := range events {
		if err := ctx.Err(); err != nil {
			return HashResult{}, err
		}
		key := FileKey{Repository: event.Repository, Path: event.Path}
		knownHash, known := hasher.known[key]
		state, regular, exists, err := observeFile(ctx, event)
		if err != nil {
			return HashResult{}, fmt.Errorf("hash %s: %w", key, err)
		}
		if !exists || !regular {
			if known {
				result.Removed = append(result.Removed, state)
				deletes[key] = struct{}{}
			} else {
				result.Skipped = append(result.Skipped, state)
			}
			continue
		}
		if knownHash == state.ContentHash {
			result.Unchanged = append(result.Unchanged, state)
			continue
		}
		result.Changed = append(result.Changed, state)
		updates[key] = state.ContentHash
	}
	for key, hash := range updates {
		hasher.known[key] = hash
	}
	for key := range deletes {
		delete(hasher.known, key)
	}
	return result, nil
}

// FileKey is the stable in-memory key for a repository-qualified path.
type FileKey struct {
	Repository string
	Path       string
}

func (key FileKey) String() string {
	return key.Repository + ":" + key.Path
}

func newFileKey(repository, path string) (FileKey, error) {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return FileKey{}, errors.New("repository must not be empty")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return FileKey{}, errors.New("path must not be empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return FileKey{}, fmt.Errorf("make path absolute: %w", err)
	}
	return FileKey{Repository: repository, Path: filepath.Clean(absolute)}, nil
}

func normalizeHashEvents(events []Event) ([]Event, error) {
	result := make([]Event, 0, len(events))
	indices := make(map[FileKey]int, len(events))
	for index, event := range events {
		key, err := newFileKey(event.Repository, event.Path)
		if err != nil {
			return nil, fmt.Errorf("event[%d]: %w", index, err)
		}
		event.Repository = key.Repository
		event.Path = key.Path
		if previous, exists := indices[key]; exists {
			result[previous].Operations |= event.Operations
			continue
		}
		indices[key] = len(result)
		result = append(result, event)
	}
	return result, nil
}

func observeFile(ctx context.Context, event Event) (state FileState, regular, exists bool, err error) {
	state = FileState{
		Repository: event.Repository,
		Path:       event.Path,
		Operations: event.Operations,
	}
	if err := ctx.Err(); err != nil {
		return state, false, false, err
	}
	info, err := os.Lstat(event.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, false, false, nil
		}
		return state, false, false, fmt.Errorf("inspect: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		state.Size = info.Size()
		return state, false, true, nil
	}
	state.Size, state.ContentHash, err = hashRegularFile(ctx, event.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FileState{Repository: event.Repository, Path: event.Path, Operations: event.Operations}, false, false, nil
		}
		return state, false, false, err
	}
	return state, true, true, nil
}

func hashRegularFile(ctx context.Context, path string) (size int64, digest string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close: %w", closeErr)
		}
	}()

	hasher := sha256.New()
	buffer := make([]byte, contentHashBufferSize)
	for {
		if err := ctx.Err(); err != nil {
			return 0, "", err
		}
		read, readErr := file.Read(buffer)
		if read > 0 {
			if _, err := hasher.Write(buffer[:read]); err != nil {
				return 0, "", fmt.Errorf("digest: %w", err)
			}
			size += int64(read)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return 0, "", readErr
		}
	}
	return size, hex.EncodeToString(hasher.Sum(nil)), nil
}

func validContentHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
