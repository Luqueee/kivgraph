package upgrade

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/indexer"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

func TestRunValidationFailureRestoresPreviousGenerationThroughStore(t *testing.T) {
	root := t.TempDir()
	previous := publishUpgradeTestGeneration(t, root, "000001", "previous graph")
	detectCalls := 0

	report, err := Run(context.Background(), Options{
		Root:            root,
		BackupRoot:      filepath.Join(root, "backups"),
		ResolverVersion: "resolver-test",
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
		Rebuild: func(ctx context.Context, options rebuild.Options) (rebuild.Report, error) {
			published := publishUpgradeTestGeneration(t, options.Root, options.GenerationID, "new graph")
			return rebuild.Report{
				GenerationID: options.GenerationID,
				Publication: generation.Publication{
					Generation: published,
					PreviousID: previous.ID,
				},
				Passed: true,
			}, nil
		},
	})
	if err == nil || !errors.Is(err, ErrUpgradeFailed) || !strings.Contains(err.Error(), "rollback completed") {
		t.Fatalf("Run() error = %v, want completed rollback", err)
	}
	if !report.RolledBack || report.To.ID != previous.ID {
		t.Fatalf("report = %#v, want a completed rollback to %s", report, previous.ID)
	}

	store, err := generation.New(root, generation.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != previous.ID {
		t.Fatalf("CURRENT = %q, want %q", current.ID, previous.ID)
	}
	layout, err := rebuild.Roles(context.Background(), rebuild.LayoutOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !layout.HasBackup || layout.Backup.ID != "000002" {
		t.Fatalf("layout backup = %+v (has=%t), want displaced candidate 000002", layout.Backup, layout.HasBackup)
	}
	if err := VerifyBackup(context.Background(), report.BackupPath, BackupRequest{
		GenerationID:      previous.ID,
		FromSchemaVersion: 1,
		ToSchemaVersion:   2,
	}); err != nil {
		t.Fatalf("VerifyBackup() after rollback: %v", err)
	}
	data, err := os.ReadFile(previous.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "previous graph" {
		t.Fatalf("restored graph = %q, want original bytes", data)
	}
}

func TestRunRollbackRefusesChangedPreviousGeneration(t *testing.T) {
	root := t.TempDir()
	previous := publishUpgradeTestGeneration(t, root, "000001", "previous graph")
	detectCalls := 0

	report, err := Run(context.Background(), Options{
		Root:            root,
		BackupRoot:      filepath.Join(root, "backups"),
		ResolverVersion: "resolver-test",
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
		Rebuild: func(ctx context.Context, options rebuild.Options) (rebuild.Report, error) {
			published := publishUpgradeTestGeneration(t, options.Root, options.GenerationID, "new graph")
			if err := os.WriteFile(previous.DatabasePath, []byte("previous grapH"), 0o600); err != nil {
				t.Fatal(err)
			}
			return rebuild.Report{
				GenerationID: options.GenerationID,
				Publication: generation.Publication{
					Generation: published,
					PreviousID: previous.ID,
				},
				Passed: true,
			}, nil
		},
	})
	if err == nil || !errors.Is(err, ErrUpgradeFailed) || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("Run() error = %v, want failed-closed rollback", err)
	}
	if report.RolledBack || report.Passed {
		t.Fatalf("report = %#v, want failed rollback", report)
	}
	if !strings.Contains(stageDetail(report, StageRollback), "digest") {
		t.Fatalf("rollback stage = %q, want digest failure", stageDetail(report, StageRollback))
	}

	store, err := generation.New(root, generation.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != "000002" {
		t.Fatalf("CURRENT = %q, want candidate 000002 after refused rollback", current.ID)
	}
}

func publishUpgradeTestGeneration(t *testing.T, root, id, contents string) generation.Generation {
	t.Helper()
	store, err := generation.New(root, generation.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	publication, err := store.Publish(context.Background(), generation.PublishRequest{
		ID: id,
		Build: func(_ context.Context, path string) error {
			return os.WriteFile(filepath.Join(path, "graph.db"), []byte(contents), 0o600)
		},
		Validate: func(_ context.Context, candidate generation.Generation) error {
			_, err := os.Stat(candidate.DatabasePath)
			return err
		},
	})
	if err != nil {
		t.Fatalf("publish test generation %s: %v", id, err)
	}
	return publication.Generation
}
