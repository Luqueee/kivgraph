package generation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sync"
)

const (
	MinimumReserveBytes = int64(512 << 20)
	MinimumMarginBytes  = int64(1 << 30)
	MinimumFreePermille = uint64(150)
)

var (
	ErrNoCurrent         = errors.New("generation store has no current generation")
	ErrNoBackup          = errors.New("generation store has no backup generation")
	ErrInvalidID         = errors.New("invalid generation id")
	ErrInsufficientSpace = errors.New("insufficient space for candidate generation")
)

type Operation string

const (
	OperationAllocateReserve  Operation = "allocate_reserve"
	OperationSyncFile         Operation = "sync_file"
	OperationSyncDirectory    Operation = "sync_directory"
	OperationRenameGeneration Operation = "rename_generation"
	OperationWriteCurrent     Operation = "write_current"
	OperationRenameCurrent    Operation = "rename_current"
	OperationWriteBackup      Operation = "write_backup"
	OperationRenameBackup     Operation = "rename_backup"
	OperationRemoveGeneration Operation = "remove_generation"
)

type FaultInjector func(Operation, string) error

type Config struct {
	ReserveBytes  int64
	MarginBytes   int64
	FreePermille  uint64
	DatabaseFile  string
	FaultInjector FaultInjector
}

// IsZero reports whether the caller left the configuration untouched, so a
// component can fall back to DefaultConfig. Config carries a func field, so
// it cannot be compared with ==.
func (config Config) IsZero() bool {
	return config.ReserveBytes == 0 &&
		config.MarginBytes == 0 &&
		config.FreePermille == 0 &&
		config.DatabaseFile == "" &&
		config.FaultInjector == nil
}

func DefaultConfig() Config {
	return Config{
		ReserveBytes: MinimumReserveBytes,
		MarginBytes:  MinimumMarginBytes,
		FreePermille: MinimumFreePermille,
		DatabaseFile: "graph.db",
	}
}

type BuildFunc func(context.Context, string) error

type ValidateFunc func(context.Context, Generation) error

type PublishRequest struct {
	ID                     string
	EstimatedSnapshotBytes int64
	Build                  BuildFunc
	Validate               ValidateFunc
}

type Generation struct {
	ID           string
	Path         string
	DatabasePath string
}

type Publication struct {
	Generation Generation
	PreviousID string
	Space      SpaceAssessment
}

type SpaceAssessment struct {
	FilesystemBytes     uint64
	AvailableBytes      uint64
	ActiveDatabaseBytes uint64
	SnapshotBytes       uint64
	RequiredBytes       uint64
}

type Store struct {
	root        string
	generations string
	current     string
	backup      string
	reserve     string
	failure     string
	config      Config
	mu          sync.Mutex
}

var generationIDPattern = regexp.MustCompile(`^[0-9]{6}$`)

func New(root string, config Config) (*Store, error) {
	return newStore(root, config, true)
}

func newStore(root string, config Config, enforceMinimums bool) (*Store, error) {
	if root == "" {
		return nil, errors.New("generation store root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if config.DatabaseFile == "" || filepath.Base(config.DatabaseFile) != config.DatabaseFile {
		return nil, errors.New("generation database filename must be a base name")
	}
	if config.ReserveBytes < 0 || config.MarginBytes < 0 || config.FreePermille > 1_000 {
		return nil, errors.New("invalid generation space policy")
	}
	if enforceMinimums {
		if config.ReserveBytes < MinimumReserveBytes {
			return nil, fmt.Errorf("reserve bytes %d: minimum is %d", config.ReserveBytes, MinimumReserveBytes)
		}
		if config.MarginBytes < MinimumMarginBytes {
			return nil, fmt.Errorf("margin bytes %d: minimum is %d", config.MarginBytes, MinimumMarginBytes)
		}
		if config.FreePermille < MinimumFreePermille {
			return nil, fmt.Errorf("free space permille %d: minimum is %d", config.FreePermille, MinimumFreePermille)
		}
	}
	return &Store{
		root:        absolute,
		generations: filepath.Join(absolute, "generations"),
		current:     filepath.Join(absolute, "CURRENT"),
		backup:      filepath.Join(absolute, "BACKUP"),
		reserve:     filepath.Join(absolute, "space-reserve"),
		failure:     filepath.Join(absolute, "LAST_FAILURE.json"),
		config:      config,
	}, nil
}

func validateGenerationID(id string) error {
	if !generationIDPattern.MatchString(id) || id == "000000" {
		return fmt.Errorf("%w %q", ErrInvalidID, id)
	}
	return nil
}
