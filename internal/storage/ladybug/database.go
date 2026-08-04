package ladybug

import "context"

// Database is the Luque-owned abstraction over one LadybugDB database.
type Database interface {
	Close() error
	Health(context.Context) error
	OpenReader(context.Context) (Reader, error)
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
