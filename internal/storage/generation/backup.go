package generation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// maxGenerationNumber is the largest value a six-digit generation id can
// hold; "000000" is reserved, so the usable range is [1, maxGenerationNumber].
const maxGenerationNumber = 999_999

// Backup returns the generation a rollback would restore. BACKUP is
// interpreted, never repaired: writing BACKUP and CURRENT takes two renames
// that cannot be made atomic together, so a crash between them can leave
// BACKUP pointing at the same id as CURRENT. That state, and a BACKUP
// pointing at an id no longer present under generations/, both mean "no
// backup" rather than a corrupt one.
func (store *Store) Backup(ctx context.Context) (Generation, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Generation{}, err
	}
	return store.backupGeneration()
}

// List returns every published generation, ordered by id.
func (store *Store) List(ctx context.Context) ([]Generation, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return store.listGenerations()
}

// Prune removes every generation that is neither active nor backup and
// returns the ids it removed, ordered. Without a current generation there is
// nothing to protect an active or a backup against, so Prune refuses to
// delete anything rather than guess.
func (store *Store) Prune(ctx context.Context) ([]string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, err := store.currentGeneration()
	if err != nil {
		return nil, err
	}
	backup, err := store.backupGeneration()
	hasBackup := err == nil
	if err != nil && !errors.Is(err, ErrNoBackup) {
		return nil, err
	}
	generations, err := store.listGenerations()
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, candidate := range generations {
		if candidate.ID == current.ID || (hasBackup && candidate.ID == backup.ID) {
			continue
		}
		if err := store.before(OperationRemoveGeneration, candidate.Path); err != nil {
			return removed, err
		}
		if err := os.RemoveAll(candidate.Path); err != nil {
			return removed, fmt.Errorf("remove generation %s: %w", candidate.ID, err)
		}
		removed = append(removed, candidate.ID)
	}
	if err := store.syncDirectory(ctx, store.generations); err != nil {
		return removed, fmt.Errorf("sync pruned generations: %w", err)
	}
	return removed, nil
}

// NextID returns the id a publication would use after the active one: the
// active id plus one, zero-padded to six digits, skipping the reserved
// "000000". Without a current generation it returns the first valid id. Any
// candidate already present on disk is skipped too, so that two rebuilds
// run back to back never collide on the same id.
func (store *Store) NextID(ctx context.Context) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	number := 1
	current, err := store.currentGeneration()
	if err == nil {
		currentNumber, convErr := strconv.Atoi(current.ID)
		if convErr != nil {
			return "", fmt.Errorf("invalid CURRENT: %w", convErr)
		}
		number = nextGenerationNumber(currentNumber)
	} else if !errors.Is(err, ErrNoCurrent) {
		return "", err
	}
	generations, err := store.listGenerations()
	if err != nil {
		return "", err
	}
	occupied := make(map[string]bool, len(generations))
	for _, existing := range generations {
		occupied[existing.ID] = true
	}
	for range maxGenerationNumber {
		id := formatGenerationID(number)
		if !occupied[id] {
			return id, nil
		}
		number = nextGenerationNumber(number)
	}
	return "", errors.New("generation store has no free id")
}

// backupGeneration resolves the BACKUP pointer. It folds every inconsistent
// state - a missing file, a malformed id, an id equal to CURRENT, or an id
// with no generation directory - into ErrNoBackup instead of propagating a
// broken pointer as if it were meaningful data. Callers must hold store.mu.
func (store *Store) backupGeneration() (Generation, error) {
	id, err := readPointer(store.backup)
	if err != nil {
		return Generation{}, err
	}
	if id == "" || validateGenerationID(id) != nil {
		return Generation{}, ErrNoBackup
	}
	current, err := store.currentGeneration()
	if err != nil || id == current.ID {
		return Generation{}, ErrNoBackup
	}
	generation, err := store.generationByID(id)
	if err != nil {
		return Generation{}, ErrNoBackup
	}
	return generation, nil
}

// listGenerations returns every generation directory under generations/,
// ordered by id. In-progress "<id>.tmp" candidates and any entry that is not
// a valid, non-reserved six-digit id are ignored. Callers must hold store.mu.
func (store *Store) listGenerations() ([]Generation, error) {
	entries, err := os.ReadDir(store.generations)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list generations: %w", err)
	}
	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() || validateGenerationID(entry.Name()) != nil {
			continue
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)
	generations := make([]Generation, len(ids))
	for i, id := range ids {
		generations[i] = store.generation(id, filepath.Join(store.generations, id))
	}
	return generations, nil
}

// writeCurrent atomically repoints CURRENT at id.
func (store *Store) writeCurrent(ctx context.Context, id string) (bool, error) {
	return store.writePointer(ctx, store.current, id, OperationWriteCurrent, OperationRenameCurrent)
}

// writeBackup atomically repoints BACKUP at id.
func (store *Store) writeBackup(ctx context.Context, id string) (bool, error) {
	return store.writePointer(ctx, store.backup, id, OperationWriteBackup, OperationRenameBackup)
}

// writePointer atomically updates a single-line pointer file (CURRENT or
// BACKUP) to id: write a sibling ".next" file, fsync it, then rename it over
// the target, fsync-ing the file at every step so the rename is the only
// thing a crash can catch mid-flight. The FaultInjector is consulted before
// the write, the fsync, and the rename, so tests can force a crash at any of
// those points. It reports whether the rename actually happened.
func (store *Store) writePointer(ctx context.Context, path, id string, writeOp, renameOp Operation) (bool, error) {
	name := filepath.Base(path)
	next := path + ".next"
	_ = os.Remove(next)
	if err := store.before(writeOp, next); err != nil {
		return false, err
	}
	file, err := os.OpenFile(next, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false, fmt.Errorf("create %s.next: %w", name, err)
	}
	if _, err := file.WriteString(id + "\n"); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("write %s.next: %w", name, err)
	}
	if err := store.before(OperationSyncFile, next); err != nil {
		_ = file.Close()
		return false, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("sync %s.next: %w", name, err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("close %s.next: %w", name, err)
	}
	if err := store.before(renameOp, path); err != nil {
		return false, err
	}
	if err := os.Rename(next, path); err != nil {
		return false, fmt.Errorf("rename %s.next: %w", name, err)
	}
	return true, nil
}

// restoreCurrent reverts CURRENT to id (or removes it, when id is empty)
// during abort/rollback recovery.
func (store *Store) restoreCurrent(id string) error {
	return restorePointer(store.current, id)
}

// restoreBackup reverts BACKUP to id (or removes it, when id is empty)
// during abort/rollback recovery.
func (store *Store) restoreBackup(id string) error {
	return restorePointer(store.backup, id)
}

// restorePointer reverts a pointer file to a previously captured raw value.
// It deliberately bypasses the FaultInjector: recovery must not be
// interrupted by the same faults being exercised against the forward path,
// or the store could never reliably return to a known state.
func restorePointer(path, id string) error {
	if id == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return rawSyncDirectory(filepath.Dir(path))
	}
	temporary := path + ".rollback"
	_ = os.Remove(temporary)
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(id + "\n"); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return rawSyncDirectory(filepath.Dir(path))
}

// readPointer returns the trimmed contents of a pointer file, or "" if the
// file does not exist. It never validates the id; callers decide whether the
// content is meaningful.
func readPointer(path string) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return strings.TrimSpace(string(data)), nil
}

// nextGenerationNumber returns the id number after number, wrapping from
// maxGenerationNumber back to 1 - never to the reserved 0.
func nextGenerationNumber(number int) int {
	number++
	if number > maxGenerationNumber {
		return 1
	}
	return number
}

func formatGenerationID(number int) string {
	return fmt.Sprintf("%06d", number)
}
