// Package upgrade migrates an incompatible canonical graph by rebuilding it
// from the registered source repositories. LadybugDB schemas are not altered
// in place: the old generation is backed up, a fresh canonical generation is
// validated, and publication remains atomic through generation.Store.
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/indexer"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
	"path/filepath"
	"strconv"
)

var ErrUpgradeFailed = errors.New("schema upgrade failed")

// StageName identifies one observable upgrade checkpoint.
type StageName string

const (
	StageDetection  StageName = "detection"
	StageBackup     StageName = "backup"
	StageMigration  StageName = "migration"
	StageValidation StageName = "validation"
	StageRollback   StageName = "rollback"
)

// Stage records one upgrade checkpoint, including skipped work.
type Stage struct {
	Name   StageName
	Passed bool
	Detail string
}

// Report accounts for detection, backup, migration, validation and rollback.
type Report struct {
	From                generation.Generation
	To                  generation.Generation
	FromSchema          ladybug.SchemaKind
	FromSchemaVersion   int
	TargetSchemaVersion int
	BackupPath          string
	Rebuild             rebuild.Report
	Stages              []Stage
	RolledBack          bool
	Passed              bool
}

// RoleResolver resolves the generation pointers.
type RoleResolver func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error)

// Detector identifies and validates the schema at one database path.
type Detector func(context.Context, string) (ladybug.StorageDiagnosis, error)

// Backuper makes the immutable pre-upgrade copy.
type Backuper func(context.Context, BackupRequest) (string, error)

// FullIndexer extracts authoritative facts from the registered repositories.
type FullIndexer func(context.Context, indexer.FullOptions) (facts.Set, indexer.FullReport, error)

// Rebuilder publishes a candidate generation after its own integrity gates.
type Rebuilder func(context.Context, rebuild.Options) (rebuild.Report, error)

// Restorer switches CURRENT to a retained generation after validation.
type Restorer func(context.Context, string, generation.Config, string, generation.ValidateFunc) error

type Options struct {
	Root            string
	BackupRoot      string
	Store           generation.Config
	Full            indexer.FullOptions
	ResolverVersion string

	Roles   RoleResolver
	Detect  Detector
	Backup  Backuper
	Index   FullIndexer
	Rebuild Rebuilder
	Restore Restorer
}

// Run upgrades an old canonical schema without mutating its database in
// place. The old schema is not converted by guessing column meanings: source
// repositories are re-indexed into a new canonical generation, which is the
// only authoritative reconstruction path for Ladygraph facts.
func Run(ctx context.Context, options Options) (Report, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Report{}, fmt.Errorf("%w: %v", ErrUpgradeFailed, err)
	}
	if options.Root == "" {
		return Report{}, fmt.Errorf("%w: root is required", ErrUpgradeFailed)
	}
	if options.ResolverVersion == "" {
		return Report{}, fmt.Errorf("%w: resolver version is required", ErrUpgradeFailed)
	}

	roles := options.Roles
	if roles == nil {
		roles = rebuild.Roles
	}
	detect := options.Detect
	if detect == nil {
		detect = ladybug.DiagnoseStorage
	}
	backup := options.Backup
	if backup == nil {
		backup = CreateBackup
	}
	index := options.Index
	if index == nil {
		index = func(indexCtx context.Context, full indexer.FullOptions) (facts.Set, indexer.FullReport, error) {
			return indexer.Full(indexCtx, full)
		}
	}
	rebuildGraph := options.Rebuild
	if rebuildGraph == nil {
		rebuildGraph = rebuild.Run
	}
	restore := options.Restore
	if restore == nil {
		restore = restoreGeneration
	}
	storeConfig := options.Store
	if storeConfigIsZero(storeConfig) {
		storeConfig = generation.DefaultConfig()
	}
	backupRoot := options.BackupRoot
	if backupRoot == "" {
		backupRoot = filepath.Join(options.Root, "backups")
	}

	layout, err := roles(ctx, rebuild.LayoutOptions{Root: options.Root, Store: storeConfig})
	if err != nil {
		return Report{}, fmt.Errorf("%w: resolve active generation: %v", ErrUpgradeFailed, err)
	}
	if layout.Active.ID == "" {
		return Report{}, fmt.Errorf("%w: no published generation to upgrade", ErrUpgradeFailed)
	}
	report := Report{
		From:                layout.Active,
		TargetSchemaVersion: ladybug.CanonicalSchemaVersion,
		Stages:              []Stage{{Name: StageDetection}},
	}

	diagnosis, err := detect(ctx, layout.Active.DatabasePath)
	if err != nil {
		report.Stages[0].Detail = err.Error()
		return report, fmt.Errorf("%w: detect schema: %v", ErrUpgradeFailed, err)
	}
	report.FromSchema = diagnosis.Schema
	report.FromSchemaVersion = diagnosis.SchemaVersion
	if err := validateSourceSchema(diagnosis); err != nil {
		report.Stages[0].Detail = err.Error()
		return report, fmt.Errorf("%w: %v", ErrUpgradeFailed, err)
	}
	if diagnosis.SchemaVersion > ladybug.CanonicalSchemaVersion {
		report.Stages[0].Detail = fmt.Sprintf("schema=%d is newer than supported schema=%d", diagnosis.SchemaVersion, ladybug.CanonicalSchemaVersion)
		return report, fmt.Errorf("%w: %s", ErrUpgradeFailed, report.Stages[0].Detail)
	}
	report.Stages[0].Passed = true
	report.Stages[0].Detail = fmt.Sprintf("canonical schema version=%d; target=%d", diagnosis.SchemaVersion, ladybug.CanonicalSchemaVersion)

	if diagnosis.SchemaVersion == ladybug.CanonicalSchemaVersion {
		if !diagnosis.Healthy {
			return failWithPreservedCurrent(report, fmt.Errorf("current canonical generation is unhealthy: %s", schemaCheckDetail(diagnosis)))
		}
		report.Stages = append(report.Stages,
			Stage{Name: StageBackup, Passed: true, Detail: "not required; schema is already current"},
			Stage{Name: StageMigration, Passed: true, Detail: "not required; schema is already current"},
			Stage{Name: StageValidation, Passed: true, Detail: fmt.Sprintf("generation=%s is healthy", layout.Active.ID)},
			Stage{Name: StageRollback, Passed: true, Detail: "not required"},
		)
		report.To = layout.Active
		report.Passed = true
		return report, nil
	}

	backupRequest := BackupRequest{
		SourcePath:        layout.Active.Path,
		DestinationRoot:   backupRoot,
		GenerationID:      layout.Active.ID,
		FromSchemaVersion: diagnosis.SchemaVersion,
		ToSchemaVersion:   ladybug.CanonicalSchemaVersion,
	}
	backupPath, err := backup(ctx, backupRequest)
	if err != nil {
		report.Stages = append(report.Stages, Stage{Name: StageBackup, Detail: err.Error()})
		return failWithPreservedCurrent(report, fmt.Errorf("%w: create backup: %v", ErrUpgradeFailed, err))
	}
	report.BackupPath = backupPath
	report.Stages = append(report.Stages, Stage{
		Name: StageBackup, Passed: true,
		Detail: fmt.Sprintf("generation=%s backup=%s", layout.Active.ID, backupPath),
	})

	generationNumber, err := strconv.ParseInt(layout.NextID, 10, 64)
	if err != nil {
		return failWithPreservedCurrent(report, fmt.Errorf("%w: parse next generation %q: %v", ErrUpgradeFailed, layout.NextID, err))
	}
	set, fullReport, err := index(ctx, options.Full)
	if err != nil {
		report.Stages = append(report.Stages, Stage{Name: StageMigration, Detail: err.Error()})
		report.Stages = append(report.Stages, preservedRollbackStage(layout.Active.ID))
		return report, fmt.Errorf("%w: extract source facts: %v", ErrUpgradeFailed, err)
	}
	report.Stages = append(report.Stages, Stage{
		Name: StageMigration, Passed: true,
		Detail: fmt.Sprintf("re-indexed %d Go repositories and %d TypeScript repositories", fullReport.GoRepositories, fullReport.TypeScriptRepositories),
	})

	rebuildReport, err := rebuildGraph(ctx, rebuild.Options{
		Root:            options.Root,
		GenerationID:    layout.NextID,
		Facts:           set,
		ResolverVersion: options.ResolverVersion,
		SnapshotID:      generationNumber,
		Store:           storeConfig,
	})
	report.Rebuild = rebuildReport
	if err != nil || !rebuildReport.Passed {
		detail := "rebuild did not publish a verified generation"
		if err != nil {
			detail = err.Error()
		}
		report.Stages = append(report.Stages, Stage{Name: StageValidation, Detail: detail})
		report.Stages = append(report.Stages, preservedRollbackStage(layout.Active.ID))
		return report, fmt.Errorf("%w: migrate schema: %s", ErrUpgradeFailed, detail)
	}

	newLayout, err := roles(ctx, rebuild.LayoutOptions{Root: options.Root, Store: storeConfig})
	if err != nil {
		report.Stages = append(report.Stages, Stage{Name: StageValidation, Detail: err.Error()})
		return rollbackAfterValidationFailure(ctx, report, layout.Active, options.Root, storeConfig, restore, fmt.Errorf("resolve published generation: %w", err))
	}
	if newLayout.Active.ID != rebuildReport.Publication.Generation.ID || newLayout.Active.ID == "" {
		detail := fmt.Sprintf("CURRENT=%q, rebuild published=%q", newLayout.Active.ID, rebuildReport.Publication.Generation.ID)
		report.Stages = append(report.Stages, Stage{Name: StageValidation, Detail: detail})
		return rollbackAfterValidationFailure(ctx, report, layout.Active, options.Root, storeConfig, restore, errors.New(detail))
	}
	postDiagnosis, err := detect(ctx, newLayout.Active.DatabasePath)
	if err != nil {
		report.Stages = append(report.Stages, Stage{Name: StageValidation, Detail: err.Error()})
		return rollbackAfterValidationFailure(ctx, report, layout.Active, options.Root, storeConfig, restore, fmt.Errorf("validate published schema: %w", err))
	}
	if postDiagnosis.Schema != ladybug.SchemaCanonical || postDiagnosis.SchemaVersion != ladybug.CanonicalSchemaVersion || !postDiagnosis.Healthy {
		detail := fmt.Sprintf("published generation schema=%s version=%d healthy=%t", postDiagnosis.Schema, postDiagnosis.SchemaVersion, postDiagnosis.Healthy)
		report.Stages = append(report.Stages, Stage{Name: StageValidation, Detail: detail})
		return rollbackAfterValidationFailure(ctx, report, layout.Active, options.Root, storeConfig, restore, errors.New(detail))
	}

	report.To = newLayout.Active
	report.Stages = append(report.Stages,
		Stage{Name: StageValidation, Passed: true, Detail: fmt.Sprintf("generation=%s schema=%d is healthy", newLayout.Active.ID, postDiagnosis.SchemaVersion)},
		Stage{Name: StageRollback, Passed: true, Detail: fmt.Sprintf("not needed; previous generation=%s retained as graph.backup", layout.Active.ID)},
	)
	report.Passed = true
	return report, nil
}

func validateSourceSchema(diagnosis ladybug.StorageDiagnosis) error {
	if diagnosis.Schema != ladybug.SchemaCanonical {
		return fmt.Errorf("schema %q cannot be upgraded; only canonical schema versions are supported", diagnosis.Schema)
	}
	if diagnosis.SchemaVersion <= 0 {
		return errors.New("canonical schema has no positive schema version")
	}
	return nil
}

func schemaCheckDetail(diagnosis ladybug.StorageDiagnosis) string {
	if check, ok := diagnosis.Check("schema"); ok {
		return check.Detail
	}
	return "schema diagnostic is unavailable"
}

func preservedRollbackStage(id string) Stage {
	return Stage{Name: StageRollback, Passed: true, Detail: fmt.Sprintf("CURRENT remained generation=%s; no pointer was changed", id)}
}

func failWithPreservedCurrent(report Report, err error) (Report, error) {
	report.Stages = append(report.Stages, preservedRollbackStage(report.From.ID))
	return report, err
}

func rollbackAfterValidationFailure(
	ctx context.Context,
	report Report,
	previous generation.Generation,
	root string,
	storeConfig generation.Config,
	restore Restorer,
	cause error,
) (Report, error) {
	restoreErr := restore(ctx, root, storeConfig, previous.ID, func(validateCtx context.Context, candidate generation.Generation) error {
		return VerifyGenerationAgainstBackup(validateCtx, candidate.Path, report.BackupPath)
	})
	if restoreErr != nil {
		report.Stages = append(report.Stages, Stage{Name: StageRollback, Detail: restoreErr.Error()})
		return report, fmt.Errorf("%w: %v; rollback failed: %v", ErrUpgradeFailed, cause, restoreErr)
	}
	report.RolledBack = true
	report.To = previous
	report.Stages = append(report.Stages, Stage{Name: StageRollback, Passed: true, Detail: fmt.Sprintf("restored generation=%s from backup=%s", previous.ID, report.BackupPath)})
	return report, fmt.Errorf("%w: %v; rollback completed", ErrUpgradeFailed, cause)
}

func restoreGeneration(ctx context.Context, root string, storeConfig generation.Config, id string, validate generation.ValidateFunc) error {
	if storeConfigIsZero(storeConfig) {
		storeConfig = generation.DefaultConfig()
	}
	store, err := generation.New(root, storeConfig)
	if err != nil {
		return err
	}
	return store.Restore(ctx, id, validate)
}

func storeConfigIsZero(config generation.Config) bool {
	return config.ReserveBytes == 0 && config.MarginBytes == 0 && config.FreePermille == 0 && config.DatabaseFile == "" && config.FaultInjector == nil
}

// An upgrade always uses a filesystem path for its state, never an in-memory
// or alternate source of graph facts.
