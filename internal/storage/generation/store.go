package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (store *Store) Prepare(ctx context.Context) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.prepare(ctx)
}

func (store *Store) Current(ctx context.Context) (Generation, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Generation{}, err
	}
	return store.currentGeneration()
}

func (store *Store) CheckSpace(ctx context.Context, estimatedSnapshotBytes int64) (SpaceAssessment, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.prepare(ctx); err != nil {
		return SpaceAssessment{}, err
	}
	return store.checkSpace(ctx, estimatedSnapshotBytes)
}

func (store *Store) Publish(ctx context.Context, request PublishRequest) (publication Publication, returnErr error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validatePublishRequest(request); err != nil {
		return Publication{}, err
	}
	if err := store.prepare(ctx); err != nil {
		return Publication{}, err
	}
	space, err := store.checkSpace(ctx, request.EstimatedSnapshotBytes)
	if err != nil {
		return Publication{}, err
	}
	previous, err := store.currentGeneration()
	if errors.Is(err, ErrNoCurrent) {
		previous = Generation{}
	} else if err != nil {
		return Publication{}, err
	}

	candidatePath := filepath.Join(store.generations, request.ID+".tmp")
	finalPath := filepath.Join(store.generations, request.ID)
	if err := requireAbsent(candidatePath); err != nil {
		return Publication{}, err
	}
	if err := requireAbsent(finalPath); err != nil {
		return Publication{}, err
	}
	if err := os.Mkdir(candidatePath, 0o700); err != nil {
		return Publication{}, fmt.Errorf("create candidate generation: %w", err)
	}
	candidateCreated := true
	finalCreated := false
	currentChanged := false
	defer func() {
		if returnErr == nil || !candidateCreated {
			return
		}
		cleanupErr := store.abortPublication(request.ID, previous.ID, candidatePath, finalPath, finalCreated, currentChanged, returnErr)
		returnErr = errors.Join(returnErr, cleanupErr)
	}()

	candidate := store.generation(request.ID, candidatePath)
	if err := request.Build(ctx, candidatePath); err != nil {
		return Publication{}, fmt.Errorf("build candidate generation: %w", err)
	}
	if err := requireRegular(candidate.DatabasePath); err != nil {
		return Publication{}, fmt.Errorf("candidate database: %w", err)
	}
	if err := store.syncTree(ctx, candidatePath); err != nil {
		return Publication{}, fmt.Errorf("sync candidate generation: %w", err)
	}
	if err := request.Validate(ctx, candidate); err != nil {
		return Publication{}, fmt.Errorf("validate candidate generation: %w", err)
	}
	if err := store.syncTree(ctx, candidatePath); err != nil {
		return Publication{}, fmt.Errorf("sync validated generation: %w", err)
	}
	if err := store.before(OperationRenameGeneration, candidatePath); err != nil {
		return Publication{}, err
	}
	if err := os.Rename(candidatePath, finalPath); err != nil {
		return Publication{}, fmt.Errorf("publish generation directory: %w", err)
	}
	finalCreated = true
	if err := store.syncDirectory(ctx, store.generations); err != nil {
		return Publication{}, fmt.Errorf("sync generations directory: %w", err)
	}
	changed, err := store.writeCurrent(ctx, request.ID)
	currentChanged = changed
	if err != nil {
		return Publication{}, err
	}
	if err := store.syncDirectory(ctx, store.root); err != nil {
		return Publication{}, fmt.Errorf("sync state directory: %w", err)
	}
	if err := store.clearFailure(); err != nil {
		return Publication{}, fmt.Errorf("clear previous failure: %w", err)
	}
	candidateCreated = false
	return Publication{
		Generation: store.generation(request.ID, finalPath),
		PreviousID: previous.ID,
		Space:      space,
	}, nil
}

func (store *Store) Restore(ctx context.Context, id string, validate ValidateFunc) (returnErr error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := validateGenerationID(id); err != nil {
		return err
	}
	if validate == nil {
		return errors.New("generation validator is required")
	}
	if err := store.prepare(ctx); err != nil {
		return err
	}
	previous, err := store.currentGeneration()
	if err != nil {
		return err
	}
	target, err := store.generationByID(id)
	if err != nil {
		return err
	}
	if err := validate(ctx, target); err != nil {
		return fmt.Errorf("validate restored generation: %w", err)
	}
	if target.ID == previous.ID {
		return nil
	}
	currentChanged := false
	defer func() {
		if returnErr == nil || !currentChanged {
			return
		}
		_ = store.releaseReserve()
		returnErr = errors.Join(returnErr, store.restoreCurrent(previous.ID), store.recordFailure(id, returnErr))
	}()
	changed, err := store.writeCurrent(ctx, id)
	currentChanged = changed
	if err != nil {
		return err
	}
	if err := store.syncDirectory(ctx, store.root); err != nil {
		return fmt.Errorf("sync restored CURRENT: %w", err)
	}
	return store.clearFailure()
}

func validatePublishRequest(request PublishRequest) error {
	if err := validateGenerationID(request.ID); err != nil {
		return err
	}
	if request.EstimatedSnapshotBytes < 0 {
		return errors.New("estimated snapshot bytes cannot be negative")
	}
	if request.Build == nil {
		return errors.New("generation builder is required")
	}
	if request.Validate == nil {
		return errors.New("generation validator is required")
	}
	return nil
}

func (store *Store) prepare(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(store.generations, 0o700); err != nil {
		return fmt.Errorf("create generation layout: %w", err)
	}
	if err := os.Chmod(store.root, 0o700); err != nil {
		return fmt.Errorf("secure state directory: %w", err)
	}
	if err := os.Chmod(store.generations, 0o700); err != nil {
		return fmt.Errorf("secure generations directory: %w", err)
	}
	info, err := os.Stat(store.reserve)
	if err == nil && info.Mode().IsRegular() && info.Size() == store.config.ReserveBytes {
		return os.Chmod(store.reserve, 0o600)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat space reserve: %w", err)
	}
	if err == nil {
		if err := os.Remove(store.reserve); err != nil {
			return fmt.Errorf("replace space reserve: %w", err)
		}
	}
	if err := store.before(OperationAllocateReserve, store.reserve); err != nil {
		return err
	}
	if err := preallocate(store.reserve, store.config.ReserveBytes); err != nil {
		return fmt.Errorf("preallocate space reserve: %w", err)
	}
	if err := os.Chmod(store.reserve, 0o600); err != nil {
		return err
	}
	if err := store.syncFile(ctx, store.reserve); err != nil {
		return err
	}
	return store.syncDirectory(ctx, store.root)
}

func (store *Store) checkSpace(ctx context.Context, estimatedSnapshotBytes int64) (SpaceAssessment, error) {
	if err := ctx.Err(); err != nil {
		return SpaceAssessment{}, err
	}
	filesystemBytes, availableBytes, err := filesystemCapacity(store.root)
	if err != nil {
		return SpaceAssessment{}, err
	}
	var activeBytes uint64
	current, err := store.currentGeneration()
	if err == nil {
		info, statErr := os.Stat(current.DatabasePath)
		if statErr != nil {
			return SpaceAssessment{}, fmt.Errorf("stat active database: %w", statErr)
		}
		activeBytes = uint64(info.Size())
	} else if !errors.Is(err, ErrNoCurrent) {
		return SpaceAssessment{}, err
	}
	snapshotBytes := uint64(estimatedSnapshotBytes)
	required, err := requiredCandidateBytes(activeBytes, snapshotBytes, uint64(store.config.MarginBytes), filesystemBytes, store.config.FreePermille)
	if err != nil {
		return SpaceAssessment{}, err
	}
	assessment := SpaceAssessment{
		FilesystemBytes:     filesystemBytes,
		AvailableBytes:      availableBytes,
		ActiveDatabaseBytes: activeBytes,
		SnapshotBytes:       snapshotBytes,
		RequiredBytes:       required,
	}
	if availableBytes < required {
		return assessment, fmt.Errorf("%w: available=%d required=%d", ErrInsufficientSpace, availableBytes, required)
	}
	return assessment, nil
}

func requiredCandidateBytes(active, snapshot, margin, filesystem, freePermille uint64) (uint64, error) {
	if active > math.MaxUint64/2 {
		return 0, errors.New("active database size overflows space calculation")
	}
	required := active * 2
	if snapshot > math.MaxUint64-required {
		return 0, errors.New("snapshot size overflows space calculation")
	}
	required += snapshot
	if margin > math.MaxUint64-required {
		return 0, errors.New("margin overflows space calculation")
	}
	required += margin
	fraction := filesystem/1_000*freePermille + filesystem%1_000*freePermille/1_000
	if fraction > required {
		required = fraction
	}
	return required, nil
}

func (store *Store) currentGeneration() (Generation, error) {
	data, err := os.ReadFile(store.current)
	if errors.Is(err, os.ErrNotExist) {
		return Generation{}, ErrNoCurrent
	}
	if err != nil {
		return Generation{}, fmt.Errorf("read CURRENT: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if err := validateGenerationID(id); err != nil {
		return Generation{}, fmt.Errorf("invalid CURRENT: %w", err)
	}
	return store.generationByID(id)
}

func (store *Store) generationByID(id string) (Generation, error) {
	if err := validateGenerationID(id); err != nil {
		return Generation{}, err
	}
	generation := store.generation(id, filepath.Join(store.generations, id))
	info, err := os.Stat(generation.Path)
	if err != nil {
		return Generation{}, fmt.Errorf("stat generation %s: %w", id, err)
	}
	if !info.IsDir() {
		return Generation{}, fmt.Errorf("generation %s is not a directory", id)
	}
	if err := requireRegular(generation.DatabasePath); err != nil {
		return Generation{}, fmt.Errorf("generation %s database: %w", id, err)
	}
	return generation, nil
}

func (store *Store) generation(id, path string) Generation {
	return Generation{ID: id, Path: path, DatabasePath: filepath.Join(path, store.config.DatabaseFile)}
}

func (store *Store) writeCurrent(ctx context.Context, id string) (bool, error) {
	next := store.current + ".next"
	_ = os.Remove(next)
	if err := store.before(OperationWriteCurrent, next); err != nil {
		return false, err
	}
	file, err := os.OpenFile(next, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false, fmt.Errorf("create CURRENT.next: %w", err)
	}
	if _, err := file.WriteString(id + "\n"); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("write CURRENT.next: %w", err)
	}
	if err := store.before(OperationSyncFile, next); err != nil {
		_ = file.Close()
		return false, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return false, fmt.Errorf("sync CURRENT.next: %w", err)
	}
	if err := file.Close(); err != nil {
		return false, fmt.Errorf("close CURRENT.next: %w", err)
	}
	if err := store.before(OperationRenameCurrent, store.current); err != nil {
		return false, err
	}
	if err := os.Rename(next, store.current); err != nil {
		return false, fmt.Errorf("rename CURRENT.next: %w", err)
	}
	return true, nil
}

func (store *Store) abortPublication(id, previousID, candidatePath, finalPath string, finalCreated, currentChanged bool, cause error) error {
	var cleanupErrs []error
	cleanupErrs = append(cleanupErrs, store.releaseReserve())
	currentRestored := true
	if currentChanged {
		if err := store.restoreCurrent(previousID); err != nil {
			currentRestored = false
			cleanupErrs = append(cleanupErrs, fmt.Errorf("restore CURRENT: %w", err))
		}
	}
	if err := os.RemoveAll(candidatePath); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("remove candidate: %w", err))
	}
	if finalCreated && currentRestored {
		if err := os.RemoveAll(finalPath); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("remove unpublished generation: %w", err))
		}
	}
	if err := rawSyncDirectory(store.generations); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("sync generation cleanup: %w", err))
	}
	cleanupErrs = append(cleanupErrs, store.recordFailure(id, cause))
	return errors.Join(cleanupErrs...)
}

func (store *Store) restoreCurrent(id string) error {
	if id == "" {
		if err := os.Remove(store.current); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return rawSyncDirectory(store.root)
	}
	temporary := store.current + ".rollback"
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
	if err := os.Rename(temporary, store.current); err != nil {
		return err
	}
	return rawSyncDirectory(store.root)
}

func (store *Store) releaseReserve() error {
	if err := os.Remove(store.reserve); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("release space reserve: %w", err)
	}
	return rawSyncDirectory(store.root)
}

func (store *Store) recordFailure(id string, cause error) error {
	record := struct {
		Generation string    `json:"generation"`
		OccurredAt time.Time `json:"occurred_at"`
		Error      string    `json:"error"`
	}{Generation: id, OccurredAt: time.Now().UTC(), Error: cause.Error()}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	next := store.failure + ".next"
	if err := os.WriteFile(next, data, 0o600); err != nil {
		return fmt.Errorf("write failure record: %w", err)
	}
	if err := rawSyncFile(next); err != nil {
		return err
	}
	if err := os.Rename(next, store.failure); err != nil {
		return err
	}
	return rawSyncDirectory(store.root)
}

func (store *Store) clearFailure() error {
	if err := os.Remove(store.failure); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (store *Store) syncTree(ctx context.Context, root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("candidate contains symlink %s", path)
		}
		if entry.IsDir() {
			directories = append(directories, path)
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("candidate contains non-regular file %s", path)
		}
		return store.syncFile(ctx, path)
	})
	if err != nil {
		return err
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if err := store.syncDirectory(ctx, directory); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) syncFile(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.before(OperationSyncFile, path); err != nil {
		return err
	}
	return rawSyncFile(path)
}

func (store *Store) syncDirectory(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.before(OperationSyncDirectory, path); err != nil {
		return err
	}
	return rawSyncDirectory(path)
}

func (store *Store) before(operation Operation, path string) error {
	if store.config.FaultInjector == nil {
		return nil
	}
	if err := store.config.FaultInjector(operation, path); err != nil {
		return fmt.Errorf("%s %s: %w", operation, path, err)
	}
	return nil
}

func requireAbsent(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("path already exists: %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func requireRegular(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", path)
	}
	return nil
}

func rawSyncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func rawSyncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
