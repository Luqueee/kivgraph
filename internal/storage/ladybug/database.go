// Package ladybug is the canonical graph store: the LadybugDB schema, the
// native bindings that read and write it, and the stubs that report the
// feature unavailable when those bindings were not linked in.
package ladybug

import "context"

// Database is the Kivgraph-owned abstraction over one LadybugDB database.
type Database interface {
	Close() error
	Health(context.Context) error
	OpenReader(context.Context) (Reader, error)
	OpenWriter(context.Context) (Writer, error)
}

// Config controls the native LadybugDB database configuration.
type Config struct {
	BufferPoolSize    uint64
	MaxNumThreads     uint64
	EnableCompression bool
	ReadOnly          bool
	MaxDatabaseSize   uint64
}

// DefaultConfig returns conservative defaults for a writable database.
func DefaultConfig() Config {
	return Config{EnableCompression: true}
}

func validatePath(path string) error {
	if path == "" {
		return &Error{Op: "open", Err: ErrInvalidPath}
	}
	return nil
}
