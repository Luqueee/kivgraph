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

// Discard removes every published generation and both pointers, and returns
// the ids it removed, ordered.
//
// The pointers go first. A crash between the two halves must leave a store
// that reads as empty and rebuilds cleanly, never one whose CURRENT names a
// directory that is gone: NextID skips the ids still on disk, and a later
// Discard removes what this one did not reach. The space reserve is released
// too, because it exists to complete a publication and there is none left to
// complete.
func (store *Store) Discard(ctx context.Context) ([]string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	generations, err := store.listGenerations()
	if err != nil {
		return nil, err
	}
	for _, pointer := range []string{store.current, store.backup} {
		if err := os.Remove(pointer); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove %s: %w", filepath.Base(pointer), err)
		}
	}
	removed := make([]string, 0, len(generations))
	for _, candidate := range generations {
		if err := store.before(OperationRemoveGeneration, candidate.Path); err != nil {
			return removed, err
		}
		if err := os.RemoveAll(candidate.Path); err != nil {
			return removed, fmt.Errorf("remove generation %s: %w", candidate.ID, err)
		}
		removed = append(removed, candidate.ID)
	}
	if err := store.releaseReserve(); err != nil {
		return removed, err
	}
	if err := store.clearFailure(); err != nil {
		return removed, fmt.Errorf("clear last failure: %w", err)
	}
	// The pointers live in the root and the generations in their own
	// directory, so both removals have to reach the disk. A store that never
	// published has no generations directory to sync.
	for _, directory := range []string{store.root, store.generations} {
		if err := store.syncDirectory(ctx, directory); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return removed, fmt.Errorf("sync discarded generations: %w", err)
		}
	}
	return removed, nil
}

// DiscardExcept removes every generation but id, and returns the ids it
// removed, ordered.
//
// The BACKUP pointer goes with them: what it named is being removed, and a
// pointer that survives its generation is the one state this store refuses to
// keep. A rollback has nothing to restore afterwards, which is the whole point
// of asking for one generation.
func (store *Store) DiscardExcept(ctx context.Context, id string) ([]string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateGenerationID(id); err != nil {
		return nil, err
	}
	generations, err := store.listGenerations()
	if err != nil {
		return nil, err
	}
	kept := false
	for _, candidate := range generations {
		if candidate.ID == id {
			kept = true
		}
	}
	if !kept {
		return nil, fmt.Errorf("generation %s: %w", id, os.ErrNotExist)
	}
	if err := os.Remove(store.backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove BACKUP: %w", err)
	}
	removed := make([]string, 0, len(generations))
	for _, candidate := range generations {
		if candidate.ID == id {
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
	for _, directory := range []string{store.root, store.generations} {
		if err := store.syncDirectory(ctx, directory); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return removed, fmt.Errorf("sync discarded generations: %w", err)
		}
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
