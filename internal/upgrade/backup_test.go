package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateBackupWritesManifestAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	source := filepath.Join(root, "generation")
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "graph.db"), []byte("canonical-v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "snapshot.sha256"), []byte("digest\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := BackupRequest{
		SourcePath:        source,
		DestinationRoot:   backupRoot,
		GenerationID:      "000007",
		FromSchemaVersion: 1,
		ToSchemaVersion:   2,
	}

	first, err := CreateBackup(ctx, request)
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if err := VerifyBackup(ctx, first, request); err != nil {
		t.Fatalf("VerifyBackup() error = %v", err)
	}
	second, err := CreateBackup(ctx, request)
	if err != nil {
		t.Fatalf("idempotent CreateBackup() error = %v", err)
	}
	if first != second {
		t.Fatalf("backup paths differ: first=%q second=%q", first, second)
	}
	if _, err := os.Stat(filepath.Join(first, "manifest.json")); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
}

func TestVerifyBackupRejectsChangedFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "generation")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "graph.db"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := BackupRequest{
		SourcePath:        source,
		DestinationRoot:   filepath.Join(root, "backups"),
		GenerationID:      "000001",
		FromSchemaVersion: 1,
		ToSchemaVersion:   2,
	}
	backupPath, err := CreateBackup(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupPath, "graph.db"), []byte("after!"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBackup(context.Background(), backupPath, request); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("VerifyBackup() error = %v, want digest failure", err)
	}
}

func TestVerifyGenerationAgainstBackupRejectsUnexpectedFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "generation")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "graph.db"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := BackupRequest{
		SourcePath:        source,
		DestinationRoot:   filepath.Join(root, "backups"),
		GenerationID:      "000001",
		FromSchemaVersion: 1,
		ToSchemaVersion:   2,
	}
	backupPath, err := CreateBackup(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "unexpected.tmp"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyGenerationAgainstBackup(context.Background(), source, backupPath); err == nil || !strings.Contains(err.Error(), "unexpected file") {
		t.Fatalf("VerifyGenerationAgainstBackup() error = %v, want unexpected-file failure", err)
	}
}

func TestCreateBackupRejectsBackupInsideGeneration(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "generation")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	request := BackupRequest{
		SourcePath:        source,
		DestinationRoot:   filepath.Join(source, "backups"),
		GenerationID:      "000001",
		FromSchemaVersion: 1,
		ToSchemaVersion:   2,
	}
	if _, err := CreateBackup(context.Background(), request); err == nil || !strings.Contains(err.Error(), "inside the source") {
		t.Fatalf("CreateBackup() error = %v, want source containment failure", err)
	}
}
