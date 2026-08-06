//go:build ladybug && cgo

package ladybug

import (
	"context"
	"fmt"
	"sync"

	lbug "github.com/LadybugDB/go-ladybug"
)

type database struct {
	mu       sync.RWMutex
	path     string
	native   *lbug.Database
	readOnly bool
	readers  map[*reader]struct{}
	writer   *writer
	closed   bool
}

// Open opens one LadybugDB database owned by the returned wrapper.
func Open(ctx context.Context, path string, config Config) (Database, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: "open", Err: err}
	}

	nativeConfig := lbug.DefaultSystemConfig()
	if config.BufferPoolSize != 0 {
		nativeConfig.BufferPoolSize = config.BufferPoolSize
	}
	if config.MaxNumThreads != 0 {
		nativeConfig.MaxNumThreads = config.MaxNumThreads
	}
	nativeConfig.EnableCompression = config.EnableCompression
	nativeConfig.ReadOnly = config.ReadOnly
	if config.MaxDatabaseSize != 0 {
		nativeConfig.MaxDbSize = config.MaxDatabaseSize
	}

	native, err := lbug.OpenDatabase(path, nativeConfig)
	if err != nil {
		return nil, &Error{Op: "open", Err: classifyOpenFailure(path, err)}
	}
	if err := ctx.Err(); err != nil {
		native.Close()
		return nil, &Error{Op: "open", Err: err}
	}
	return &database{path: path, native: native, readOnly: config.ReadOnly}, nil
}

// classifyOpenFailure tells a duplicate process apart from a damaged database.
// The engine reports both as the same status, so the distinction has to come
// from outside it: if another live process holds a lock on this file, that is
// the reason, and saying so is the difference between an operator restarting
// the right thing and an operator restoring a backup they did not need.
func classifyOpenFailure(path string, cause error) error {
	pids, supported, lockErr := externalStorageLocks(path)
	if !supported || lockErr != nil || len(pids) == 0 {
		return cause
	}
	return fmt.Errorf("%w (pids %v): %w", ErrDatabaseLocked, pids, cause)
}

func (db *database) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed {
		return nil
	}
	for reader := range db.readers {
		reader.mu.Lock()
		reader.closeNative()
		reader.mu.Unlock()
	}
	clear(db.readers)
	if db.writer != nil {
		db.writer.mu.Lock()
		db.writer.closeNative()
		db.writer.mu.Unlock()
		db.writer = nil
	}
	db.native.Close()
	db.closed = true
	return nil
}

func (db *database) Health(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return &Error{Op: "health", Err: err}
	}

	db.mu.RLock()
	defer db.mu.RUnlock()
	if db.closed {
		return &Error{Op: "health", Err: ErrClosed}
	}
	connection, err := openConnection(db)
	if err != nil {
		return err
	}
	if err := connection.Close(); err != nil {
		return &Error{Op: "health", Err: err}
	}
	if err := ctx.Err(); err != nil {
		return &Error{Op: "health", Err: err}
	}
	return nil
}

type connection struct {
	mu     sync.Mutex
	native *lbug.Connection
	closed bool
}

func openConnection(db *database) (*connection, error) {
	native, err := lbug.OpenConnection(db.native)
	if err != nil {
		return nil, &Error{Op: "connect", Err: err}
	}
	return &connection{native: native}, nil
}

func (connection *connection) Close() error {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if connection.closed {
		return nil
	}
	connection.native.Close()
	connection.closed = true
	return nil
}
