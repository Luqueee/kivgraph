//go:build ladybug && cgo && linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
	"github.com/Luqueee/luque/internal/storage/ladybug"
)

const createRecoverySymbolQuery = `CREATE (:Symbol {
stable_key: $stable_key,
repository_key: $repository_key,
file_key: $file_key,
name: $name,
qualified_name: $qualified_name,
kind: $kind,
signature: $signature,
start_line: $start_line,
end_line: $end_line
})`

func runWorker(ctx context.Context, cfg config) error {
	switch cfg.Worker {
	case "insert-loop":
		return workerInsertLoop(cfg.DatabasePath, cfg.MarkerPath)
	case "before-commit":
		return workerBeforeCommit(cfg.DatabasePath, cfg.MarkerPath)
	case "bulk-copy":
		return workerBulkCopy(ctx, cfg.DatabasePath, cfg.MarkerPath, cfg.GatePath, cfg.BulkCSVPath)
	case "health":
		return workerHealth(ctx, cfg.DatabasePath)
	case "disk-full":
		return workerDiskFull(ctx, cfg.DatabasePath, cfg.MarkerPath)
	default:
		return fmt.Errorf("unknown worker mode %q", cfg.Worker)
	}
}

func workerInsertLoop(databasePath, markerPath string) error {
	database, connection, err := openNative(databasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	defer connection.Close()
	if err := executeNative(connection, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	statement, err := connection.Prepare(createRecoverySymbolQuery)
	if err != nil {
		return err
	}
	defer statement.Close()
	for index := 0; index < 100_000; index++ {
		key := fmt.Sprintf("recovery-insert-%08d", index)
		if err := executeInsert(connection, statement, recoverySymbol(key, index)); err != nil {
			return err
		}
		if index == 31 {
			if err := writeMarker(markerPath, "inserting"); err != nil {
				return err
			}
		}
	}
	return errors.New("insert worker completed without SIGKILL")
}

func workerBeforeCommit(databasePath, markerPath string) error {
	database, connection, err := openNative(databasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	defer connection.Close()
	if err := executeNative(connection, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	statement, err := connection.Prepare(createRecoverySymbolQuery)
	if err != nil {
		return err
	}
	defer statement.Close()
	if err := executeInsert(connection, statement, recoverySymbol("recovery-before-commit", 0)); err != nil {
		return err
	}
	if err := writeMarker(markerPath, "before_commit"); err != nil {
		return err
	}
	for {
		time.Sleep(time.Hour)
	}
}

func workerBulkCopy(ctx context.Context, databasePath, markerPath, gatePath, csvPath string) error {
	if csvPath == "" || gatePath == "" {
		return errors.New("bulk worker requires CSV and gate paths")
	}
	database, connection, err := openNative(databasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	defer connection.Close()
	if err := executeNative(connection, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	if err := writeMarker(markerPath, "bulk_ready"); err != nil {
		return err
	}
	if err := waitForGate(ctx, gatePath); err != nil {
		return err
	}
	query := fmt.Sprintf("COPY Symbol FROM %s", cypherString(csvPath))
	if err := executeNative(connection, query); err != nil {
		return err
	}
	if err := writeMarker(markerPath+".complete", "bulk_complete"); err != nil {
		return err
	}
	for {
		time.Sleep(time.Hour)
	}
}

func workerHealth(ctx context.Context, databasePath string) error {
	database, err := ladybug.Open(ctx, databasePath, ladybug.DefaultConfig())
	if err != nil {
		return err
	}
	defer database.Close()
	return database.Health(ctx)
}

func workerDiskFull(ctx context.Context, databasePath, markerPath string) error {
	database, err := ladybug.Open(ctx, databasePath, ladybug.DefaultConfig())
	if err != nil {
		return err
	}
	defer database.Close()
	writer, err := database.OpenWriter(ctx)
	if err != nil {
		return err
	}
	defer writer.Close()
	if markerPath != "" {
		if err := writeMarker(markerPath, "disk_full_ready"); err != nil {
			return err
		}
	}
	symbols := make([]ladybug.Symbol, 1_000)
	for index := range symbols {
		symbols[index] = recoverySymbol(fmt.Sprintf("recovery-enospc-%04d", index), index)
	}
	if err := os.Setenv("LUQUE_ENOSPC_ARMED", "1"); err != nil {
		return err
	}
	if err := os.Setenv("LUQUE_ENOSPC_PHASE", "apply"); err != nil {
		return err
	}
	_, applyErr := writer.Apply(ctx, ladybug.Delta{AddSymbols: symbols})
	if err := os.Setenv("LUQUE_ENOSPC_PHASE", "after_apply"); err != nil {
		return err
	}
	statusPath := os.Getenv("LUQUE_ENOSPC_STATUS")
	status, statusErr := os.ReadFile(statusPath)
	injectedDuringApply := statusErr == nil && strings.TrimSpace(string(status)) == "ENOSPC apply"
	if applyErr != nil {
		return fmt.Errorf("injected write failure returned by Apply: %w", applyErr)
	}
	if injectedDuringApply {
		return errors.New("Apply returned success after ENOSPC was injected during the transaction")
	}
	return errors.New("ENOSPC was not injected during Apply")
}

func openNative(databasePath string) (*lbug.Database, *lbug.Connection, error) {
	configuration := lbug.DefaultSystemConfig()
	database, err := lbug.OpenDatabase(databasePath, configuration)
	if err != nil {
		return nil, nil, err
	}
	connection, err := lbug.OpenConnection(database)
	if err != nil {
		database.Close()
		return nil, nil, err
	}
	return database, connection, nil
}

func executeInsert(connection *lbug.Connection, statement *lbug.PreparedStatement, symbol ladybug.Symbol) error {
	result, err := connection.Execute(statement, map[string]any{
		"stable_key": symbol.StableKey, "repository_key": symbol.RepositoryKey,
		"file_key": symbol.FileKey, "name": symbol.Name, "qualified_name": symbol.QualifiedName,
		"kind": symbol.Kind, "signature": symbol.Signature,
		"start_line": symbol.StartLine, "end_line": symbol.EndLine,
	})
	if result != nil {
		result.Close()
	}
	return err
}

func executeNative(connection *lbug.Connection, query string) error {
	result, err := connection.Query(query)
	if result != nil {
		result.Close()
	}
	return err
}

func writeMarker(path, value string) error {
	if path == "" {
		return errors.New("marker path is empty")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(value + "\n"); err != nil {
		_ = file.Close()
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

func waitForGate(ctx context.Context, path string) error {
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

func cypherString(value string) string {
	absolute, err := filepath.Abs(value)
	if err == nil {
		value = absolute
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
