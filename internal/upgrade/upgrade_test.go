package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/indexer"
	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
)

func TestRunCurrentSchemaIsExplicitNoOp(t *testing.T) {
	root := t.TempDir()
	active := generation.Generation{ID: "000004", Path: filepath.Join(root, "generations", "000004"), DatabasePath: filepath.Join(root, "generations", "000004", "graph.db")}
	roles := staticRoles(active, "000005")
	indexCalled := false
	rebuildCalled := false
	report, err := Run(context.Background(), Options{
		Root:            root,
		ResolverVersion: "resolver-test",
		Roles:           roles,
		Detect: func(context.Context, string) (ladybug.StorageDiagnosis, error) {
			return healthyDiagnosis(ladybug.CanonicalSchemaVersion), nil
		},
		Index: func(context.Context, indexer.FullOptions) (facts.Set, indexer.FullReport, error) {
			indexCalled = true
			return facts.Set{}, indexer.FullReport{}, nil
		},
		Rebuild: func(context.Context, rebuild.Options) (rebuild.Report, error) {
			rebuildCalled = true
			return rebuild.Report{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Passed || report.To.ID != active.ID {
		t.Fatalf("report = %#v, want passing no-op on %s", report, active.ID)
	}
	if indexCalled || rebuildCalled {
		t.Fatal("current schema invoked migration work")
	}
	if got := stageDetail(report, StageMigration); !strings.Contains(got, "not required") {
		t.Fatalf("migration stage detail = %q, want explicit no-op", got)
	}
}

func TestRunOlderCanonicalSchemaBacksUpAndRebuilds(t *testing.T) {
	root := t.TempDir()
	activePath := filepath.Join(root, "generations", "000001")
	if err := os.MkdirAll(activePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activePath, "graph.db"), []byte("old graph"), 0o600); err != nil {
		t.Fatal(err)
	}
	active := generation.Generation{ID: "000001", Path: activePath, DatabasePath: filepath.Join(activePath, "graph.db")}
	newGeneration := generation.Generation{ID: "000002", Path: filepath.Join(root, "generations", "000002"), DatabasePath: filepath.Join(root, "generations", "000002", "graph.db")}
	current := active
	roles := func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
		return rebuild.Layout{Active: current, NextID: "000002"}, nil
	}
	detectCalls := 0
	indexCalled := false
	var indexedOptions indexer.FullOptions
	report, err := Run(context.Background(), Options{
		Root:            root,
		BackupRoot:      filepath.Join(root, "backups"),
		ResolverVersion: "resolver-test",
		Roles:           roles,
		Detect: func(context.Context, string) (ladybug.StorageDiagnosis, error) {
			detectCalls++
			if detectCalls == 1 {
				return unhealthyDiagnosis(1), nil
			}
			return healthyDiagnosis(ladybug.CanonicalSchemaVersion), nil
		},
		Index: func(_ context.Context, options indexer.FullOptions) (facts.Set, indexer.FullReport, error) {
			indexCalled = true
			indexedOptions = options
			return facts.Set{}, indexer.FullReport{GoRepositories: 1}, nil
		},
		Rebuild: func(_ context.Context, options rebuild.Options) (rebuild.Report, error) {
			if options.GenerationID != newGeneration.ID {
				t.Fatalf("rebuild generation = %q, want %q", options.GenerationID, newGeneration.ID)
			}
			current = newGeneration
			return rebuild.Report{
				GenerationID: options.GenerationID,
				Publication:  generation.Publication{Generation: newGeneration, PreviousID: active.ID},
				Passed:       true,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Passed || !indexCalled || report.FromSchemaVersion != 1 || report.To.ID != newGeneration.ID {
		t.Fatalf("report = %#v, want successful migration", report)
	}
	if indexedOptions.ResolverVersion != "resolver-test" {
		t.Fatalf("upgrade index resolver version = %q, want resolver-test", indexedOptions.ResolverVersion)
	}
	if report.BackupPath == "" {
		t.Fatal("backup path is empty")
	}
	request := BackupRequest{
		GenerationID:      active.ID,
		FromSchemaVersion: 1,
		ToSchemaVersion:   ladybug.CanonicalSchemaVersion,
	}
	if err := VerifyBackup(context.Background(), report.BackupPath, request); err != nil {
		t.Fatalf("VerifyBackup() error = %v", err)
	}
	if got := stageDetail(report, StageRollback); !strings.Contains(got, "graph.backup") {
		t.Fatalf("rollback stage detail = %q, want graph.backup retention", got)
	}
}

func TestRunRejectsSyntheticSchemaWithoutMutation(t *testing.T) {
	root := t.TempDir()
	active := generation.Generation{ID: "000001", Path: filepath.Join(root, "generation"), DatabasePath: filepath.Join(root, "generation", "graph.db")}
	backupCalled := false
	indexCalled := false
	_, err := Run(context.Background(), Options{
		Root:            root,
		BackupRoot:      filepath.Join(root, "backups"),
		ResolverVersion: "resolver-test",
		Roles:           staticRoles(active, "000002"),
		Detect: func(context.Context, string) (ladybug.StorageDiagnosis, error) {
			return ladybug.StorageDiagnosis{Schema: ladybug.SchemaSynthetic}, nil
		},
		Backup: func(context.Context, BackupRequest) (string, error) {
			backupCalled = true
			return "", nil
		},
		Index: func(context.Context, indexer.FullOptions) (facts.Set, indexer.FullReport, error) {
			indexCalled = true
			return facts.Set{}, indexer.FullReport{}, nil
		},
	})
	if err == nil || !errors.Is(err, ErrUpgradeFailed) || !strings.Contains(err.Error(), "cannot be upgraded") {
		t.Fatalf("Run() error = %v, want synthetic schema rejection", err)
	}
	if backupCalled || indexCalled {
		t.Fatal("unsupported schema invoked mutation work")
	}
}

func TestRunFailurePreservesCurrentAfterBackup(t *testing.T) {
	root := t.TempDir()
	activePath := filepath.Join(root, "generations", "000001")
	if err := os.MkdirAll(activePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activePath, "graph.db"), []byte("old graph"), 0o600); err != nil {
		t.Fatal(err)
	}
	active := generation.Generation{ID: "000001", Path: activePath, DatabasePath: filepath.Join(activePath, "graph.db")}
	current := active
	report, err := Run(context.Background(), Options{
		Root:            root,
		BackupRoot:      filepath.Join(root, "backups"),
		ResolverVersion: "resolver-test",
		Roles: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return rebuild.Layout{Active: current, NextID: "000002"}, nil
		},
		Detect: func(context.Context, string) (ladybug.StorageDiagnosis, error) {
			return unhealthyDiagnosis(1), nil
		},
		Index: func(context.Context, indexer.FullOptions) (facts.Set, indexer.FullReport, error) {
			return facts.Set{}, indexer.FullReport{}, fmt.Errorf("source repository unavailable")
		},
	})
	if err == nil || !errors.Is(err, ErrUpgradeFailed) {
		t.Fatalf("Run() error = %v, want upgrade failure", err)
	}
	if report.Passed || current.ID != active.ID {
		t.Fatalf("report/current = %#v/%s, want failed upgrade with current preserved", report, current.ID)
	}
	if report.BackupPath == "" {
		t.Fatal("failed migration did not retain backup path")
	}
	if got := stageDetail(report, StageRollback); !strings.Contains(got, "remained generation=000001") {
		t.Fatalf("rollback stage detail = %q, want unchanged CURRENT", got)
	}
}

func TestRunValidationFailureRestoresPreviousGeneration(t *testing.T) {
	root := t.TempDir()
	activePath := filepath.Join(root, "generations", "000001")
	if err := os.MkdirAll(activePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activePath, "graph.db"), []byte("old graph"), 0o600); err != nil {
		t.Fatal(err)
	}
	active := generation.Generation{ID: "000001", Path: activePath, DatabasePath: filepath.Join(activePath, "graph.db")}
	published := generation.Generation{ID: "000002", Path: filepath.Join(root, "generations", "000002"), DatabasePath: filepath.Join(root, "generations", "000002", "graph.db")}
	current := active
	detectCalls := 0
	report, err := Run(context.Background(), Options{
		Root:            root,
		BackupRoot:      filepath.Join(root, "backups"),
		ResolverVersion: "resolver-test",
		Roles: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return rebuild.Layout{Active: current, NextID: "000002"}, nil
		},
		Detect: func(context.Context, string) (ladybug.StorageDiagnosis, error) {
			detectCalls++
			if detectCalls == 1 {
				return unhealthyDiagnosis(1), nil
			}
			return unhealthyDiagnosis(999), nil
		},
		Index: func(context.Context, indexer.FullOptions) (facts.Set, indexer.FullReport, error) {
			return facts.Set{}, indexer.FullReport{}, nil
		},
		Rebuild: func(context.Context, rebuild.Options) (rebuild.Report, error) {
			current = published
			return rebuild.Report{
				Publication: generation.Publication{Generation: published, PreviousID: active.ID},
				Passed:      true,
			}, nil
		},
		Restore: func(_ context.Context, _ string, _ generation.Config, id string, validate generation.ValidateFunc) error {
			if id != active.ID {
				t.Fatalf("restore id = %q, want %q", id, active.ID)
			}
			if err := validate(context.Background(), active); err != nil {
				t.Fatalf("rollback validation = %v", err)
			}
			current = active
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "rollback completed") {
		t.Fatalf("Run() error = %v, want completed rollback", err)
	}
	if !report.RolledBack || current.ID != active.ID {
		t.Fatalf("report/current = %#v/%s, want rollback to %s", report, current.ID, active.ID)
	}
}

func healthyDiagnosis(version int) ladybug.StorageDiagnosis {
	return ladybug.StorageDiagnosis{Schema: ladybug.SchemaCanonical, SchemaVersion: version, Healthy: true}
}

func unhealthyDiagnosis(version int) ladybug.StorageDiagnosis {
	return ladybug.StorageDiagnosis{
		Schema:        ladybug.SchemaCanonical,
		SchemaVersion: version,
		Checks:        []ladybug.DiagnosticCheck{{Name: "schema", Detail: "schema version mismatch"}},
	}
}

func staticRoles(active generation.Generation, next string) RoleResolver {
	return func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
		return rebuild.Layout{Active: active, NextID: next}, nil
	}
}

func stageDetail(report Report, name StageName) string {
	for _, stage := range report.Stages {
		if stage.Name == name {
			return stage.Detail
		}
	}
	return ""
}
