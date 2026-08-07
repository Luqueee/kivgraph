package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const backupManifestVersion = 1

// BackupRequest identifies one schema-upgrade backup. The backup is a
// byte-for-byte copy of one immutable generation, not a live database copy.
type BackupRequest struct {
	SourcePath        string
	DestinationRoot   string
	GenerationID      string
	FromSchemaVersion int
	ToSchemaVersion   int
}

type backupManifest struct {
	FormatVersion     int          `json:"format_version"`
	GenerationID      string       `json:"generation_id"`
	FromSchemaVersion int          `json:"from_schema_version"`
	ToSchemaVersion   int          `json:"to_schema_version"`
	Files             []backupFile `json:"files"`
}

type backupFile struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// CreateBackup creates or verifies the deterministic backup location for a
// schema upgrade. It writes through a sibling temporary directory and
// atomically renames the completed copy into place.
func CreateBackup(ctx context.Context, request BackupRequest) (string, error) {
	if err := validateBackupRequest(request); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	source, err := filepath.Abs(request.SourcePath)
	if err != nil {
		return "", fmt.Errorf("resolve backup source: %w", err)
	}
	destinationRoot, err := filepath.Abs(request.DestinationRoot)
	if err != nil {
		return "", fmt.Errorf("resolve backup destination: %w", err)
	}
	if pathContains(source, destinationRoot) {
		return "", errors.New("backup destination must not be inside the source generation")
	}
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("stat source generation: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source generation %q is not a directory", source)
	}

	if err := os.MkdirAll(destinationRoot, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	if err := os.Chmod(destinationRoot, 0o700); err != nil {
		return "", fmt.Errorf("secure backup directory: %w", err)
	}
	finalPath := filepath.Join(destinationRoot, backupName(request))
	if existing, statErr := os.Stat(finalPath); statErr == nil {
		if !existing.IsDir() {
			return "", fmt.Errorf("backup path %q exists and is not a directory", finalPath)
		}
		if err := VerifyBackup(ctx, finalPath, request); err != nil {
			return "", fmt.Errorf("verify existing backup: %w", err)
		}
		return finalPath, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect backup path: %w", statErr)
	}

	temporaryPath := fmt.Sprintf("%s.tmp-%d", finalPath, os.Getpid())
	if _, statErr := os.Stat(temporaryPath); statErr == nil {
		return "", fmt.Errorf("temporary backup path already exists: %s", temporaryPath)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect temporary backup path: %w", statErr)
	}
	if err := os.Mkdir(temporaryPath, 0o700); err != nil {
		return "", fmt.Errorf("create temporary backup: %w", err)
	}
	completed := false
	defer func() {
		if !completed {
			_ = os.RemoveAll(temporaryPath)
		}
	}()

	files, err := copyGeneration(ctx, source, temporaryPath)
	if err != nil {
		return "", fmt.Errorf("copy generation: %w", err)
	}
	manifest := backupManifest{
		FormatVersion:     backupManifestVersion,
		GenerationID:      request.GenerationID,
		FromSchemaVersion: request.FromSchemaVersion,
		ToSchemaVersion:   request.ToSchemaVersion,
		Files:             files,
	}
	if err := writeManifest(temporaryPath, manifest); err != nil {
		return "", fmt.Errorf("write backup manifest: %w", err)
	}
	if err := syncDirectory(temporaryPath); err != nil {
		return "", fmt.Errorf("sync temporary backup: %w", err)
	}
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		if _, statErr := os.Stat(finalPath); statErr == nil {
			if verifyErr := VerifyBackup(ctx, finalPath, request); verifyErr == nil {
				completed = true
				return finalPath, nil
			}
		}
		return "", fmt.Errorf("publish backup: %w", err)
	}
	completed = true
	if err := syncDirectory(destinationRoot); err != nil {
		return "", fmt.Errorf("sync backup directory: %w", err)
	}
	return finalPath, nil
}

// VerifyBackup validates the manifest and every file digest in a backup.
// expected.SourcePath is intentionally ignored: the manifest is portable,
// while the generation and schema identities are checked.
func VerifyBackup(ctx context.Context, path string, expected BackupRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	manifestPath := filepath.Join(path, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest backupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.FormatVersion != backupManifestVersion {
		return fmt.Errorf("manifest format version %d, want %d", manifest.FormatVersion, backupManifestVersion)
	}
	if expected.GenerationID != "" && manifest.GenerationID != expected.GenerationID {
		return fmt.Errorf("manifest generation %q, want %q", manifest.GenerationID, expected.GenerationID)
	}
	if expected.FromSchemaVersion != 0 && manifest.FromSchemaVersion != expected.FromSchemaVersion {
		return fmt.Errorf("manifest source schema %d, want %d", manifest.FromSchemaVersion, expected.FromSchemaVersion)
	}
	if expected.ToSchemaVersion != 0 && manifest.ToSchemaVersion != expected.ToSchemaVersion {
		return fmt.Errorf("manifest target schema %d, want %d", manifest.ToSchemaVersion, expected.ToSchemaVersion)
	}
	if len(manifest.Files) == 0 {
		return errors.New("backup manifest contains no files")
	}

	seen := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		clean, err := cleanRelativePath(file.Path)
		if err != nil {
			return fmt.Errorf("manifest file %q: %w", file.Path, err)
		}
		if _, exists := seen[clean]; exists {
			return fmt.Errorf("manifest contains duplicate file %q", clean)
		}
		seen[clean] = struct{}{}
		actualPath := filepath.Join(path, filepath.FromSlash(clean))
		info, err := os.Stat(actualPath)
		if err != nil {
			return fmt.Errorf("stat backup file %q: %w", clean, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup file %q is not regular", clean)
		}
		if info.Size() != file.Bytes {
			return fmt.Errorf("backup file %q has %d bytes, want %d", clean, info.Size(), file.Bytes)
		}
		digest, err := fileSHA256(ctx, actualPath)
		if err != nil {
			return fmt.Errorf("hash backup file %q: %w", clean, err)
		}
		if digest != file.SHA256 {
			return fmt.Errorf("backup file %q has digest %s, want %s", clean, digest, file.SHA256)
		}
	}
	return nil
}

// VerifyGenerationAgainstBackup proves that a retained generation still
// equals the immutable pre-upgrade backup before a pointer rollback.
func VerifyGenerationAgainstBackup(ctx context.Context, generationPath, backupPath string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(backupPath, "manifest.json"))
	if err != nil {
		return fmt.Errorf("read backup manifest: %w", err)
	}
	var manifest backupManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode backup manifest: %w", err)
	}
	if manifest.FormatVersion != backupManifestVersion {
		return fmt.Errorf("manifest format version %d, want %d", manifest.FormatVersion, backupManifestVersion)
	}
	return verifyFilesAtPath(ctx, generationPath, manifest.Files)
}

func validateBackupRequest(request BackupRequest) error {
	if strings.TrimSpace(request.SourcePath) == "" {
		return errors.New("backup source path is required")
	}
	if strings.TrimSpace(request.DestinationRoot) == "" {
		return errors.New("backup destination root is required")
	}
	if strings.TrimSpace(request.GenerationID) == "" {
		return errors.New("backup generation id is required")
	}
	if request.FromSchemaVersion <= 0 || request.ToSchemaVersion <= 0 {
		return errors.New("backup schema versions must be positive")
	}
	if request.FromSchemaVersion >= request.ToSchemaVersion {
		return fmt.Errorf("backup source schema %d must be older than target schema %d", request.FromSchemaVersion, request.ToSchemaVersion)
	}
	return nil
}

func backupName(request BackupRequest) string {
	return fmt.Sprintf("schema-upgrade-%s-%03d-to-%03d", request.GenerationID, request.FromSchemaVersion, request.ToSchemaVersion)
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return true
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func copyGeneration(ctx context.Context, source, destination string) ([]backupFile, error) {
	var files []backupFile
	err := filepath.WalkDir(source, func(sourcePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(source, sourcePath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		targetPath := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.Mkdir(targetPath, 0o700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not allowed: %s", relative)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported generation entry %s", relative)
		}
		if err := copyFile(ctx, sourcePath, targetPath, info.Mode().Perm()); err != nil {
			return err
		}
		digest, err := fileSHA256(ctx, targetPath)
		if err != nil {
			return err
		}
		files = append(files, backupFile{Path: filepath.ToSlash(relative), Bytes: info.Size(), SHA256: digest})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Path < files[right].Path })
	return files, nil
}

func copyFile(ctx context.Context, source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return errors.Join(err, input.Close())
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeOutputErr := output.Close()
	closeInputErr := input.Close()
	return errors.Join(copyErr, syncErr, closeOutputErr, closeInputErr, ctx.Err())
}

func writeManifest(directory string, manifest backupManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(directory, "manifest.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}

func cleanRelativePath(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) {
		return "", errors.New("path must be relative")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path escapes backup root")
	}
	return clean, nil
}

func verifyFilesAtPath(ctx context.Context, root string, files []backupFile) error {
	if info, err := os.Stat(root); err != nil {
		return fmt.Errorf("stat generation root: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("generation root %q is not a directory", root)
	}
	expected := make(map[string]backupFile, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		clean, err := cleanRelativePath(file.Path)
		if err != nil {
			return err
		}
		if _, exists := expected[clean]; exists {
			return fmt.Errorf("manifest contains duplicate file %q", clean)
		}
		expected[clean] = file
		path := filepath.Join(root, filepath.FromSlash(clean))
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("stat generation file %q: %w", clean, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != file.Bytes {
			return fmt.Errorf("generation file %q does not match backup", clean)
		}
		digest, err := fileSHA256(ctx, path)
		if err != nil {
			return fmt.Errorf("hash generation file %q: %w", clean, err)
		}
		if digest != file.SHA256 {
			return fmt.Errorf("generation file %q has digest %s, want %s", clean, digest, file.SHA256)
		}
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		clean := filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("generation contains symbolic link %q", clean)
		}
		if entry.IsDir() {
			return nil
		}
		if _, exists := expected[clean]; !exists {
			return fmt.Errorf("generation contains unexpected file %q", clean)
		}
		return nil
	})
	return err
}

func fileSHA256(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if err := errors.Join(copyErr, ctx.Err(), closeErr); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
}
