// Package freshness binds a conservative source inventory to a generation.
// It never infers graph completeness from filesystem stability.
package freshness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

type Status struct {
	Generation uint64 `json:"generation"`
	State      string `json:"state"`
	Detail     string `json:"detail,omitempty"`
}

type Record struct {
	Version    int    `json:"version"`
	Generation uint64 `json:"generation"`
	Digest     string `json:"digest"`
}

// Capture includes names as well as contents, including uncommitted and
// untracked sources. Configured exclusions are honored; source extensions come
// from the same language registry as the indexer.
func Capture(ctx context.Context, repositories []workspace.Repository) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	repos := slices.Clone(repositories)
	slices.SortFunc(repos, func(a, b workspace.Repository) int { return strings.Compare(a.Name, b.Name) })
	digest := sha256.New()
	for _, repo := range repos {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		root, err := inventoryRoot(repo)
		if err != nil {
			return "", err
		}
		stat, err := os.Stat(root)
		if err != nil {
			return "", fmt.Errorf("inspect registered repository %s at %q: %w", repo.Name, root, err)
		}
		if !stat.IsDir() {
			return "", fmt.Errorf("inspect registered repository %s at %q: not a directory", repo.Name, root)
		}
		manifests, err := resolveRepositoryPaths(root, repo.Manifests)
		if err != nil {
			return "", fmt.Errorf("resolve repository %s manifests: %w", repo.Name, err)
		}
		roots, err := resolveRepositoryPaths(root, repo.Roots)
		if err != nil {
			return "", fmt.Errorf("resolve repository %s roots: %w", repo.Name, err)
		}
		identity, err := json.Marshal(struct {
			Name, Path, Commit, Branch string
			Languages, Roots           []string
			Manifests, Exclusions      []string
		}{repo.Name, root, repo.Commit, repo.Branch, repo.Languages, roots, manifests, repo.Exclusions})
		if err != nil {
			return "", fmt.Errorf("encode repository %s inventory identity: %w", repo.Name, err)
		}
		if _, err := digest.Write(identity); err != nil {
			return "", fmt.Errorf("write repository %s inventory identity: %w", repo.Name, err)
		}
		explicitManifests := make(map[string]struct{}, len(manifests))
		for _, manifest := range manifests {
			explicitManifests[manifest] = struct{}{}
		}
		hashedFiles := make(map[string]struct{})
		languages := repo.Languages
		if len(languages) == 0 {
			languages = config.SupportedLanguages()
		}
		extensions := config.SourceExtensionSet(languages)
		err = filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("walk %q: %w", filename, walkErr)
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			relative, err := filepath.Rel(root, filename)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			excluded, err := workspace.MatchesExclusion(root, filename, repo.Exclusions)
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if relative != "." && (excluded || entry.Name() == ".git" || entry.Name() == "node_modules") {
					return fs.SkipDir
				}
				return nil
			}
			if excluded {
				return nil
			}
			if _, explicit := explicitManifests[filepath.Clean(filename)]; !explicit &&
				!config.IsBuildConfigurationFile(relative) &&
				!config.HasSourceExtension(extensions, entry.Name()) {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("inventory %q: refuses symlink", filename)
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("inventory %q: refuses non-regular file", filename)
			}
			file, err := os.Open(filename)
			if err != nil {
				return fmt.Errorf("open inventory file %q: %w", filename, err)
			}
			hash := sha256.New()
			_, readErr := io.Copy(hash, file)
			closeErr := file.Close()
			if readErr != nil || closeErr != nil {
				return fmt.Errorf("read inventory file %q: %w", filename, errors.Join(readErr, closeErr))
			}
			hashedFiles[filepath.Clean(filename)] = struct{}{}
			if _, err := fmt.Fprintf(digest, "%s\x00%x\x00", relative, hash.Sum(nil)); err != nil {
				return fmt.Errorf("write inventory digest for %q: %w", filename, err)
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("inventory %s: %w", repo.Name, err)
		}
		for _, manifest := range manifests {
			if _, hashed := hashedFiles[filepath.Clean(manifest)]; hashed {
				continue
			}
			withinRoot, err := inventoryPathWithin(root, manifest)
			if err != nil {
				return "", fmt.Errorf("resolve explicit manifest %q: %w", manifest, err)
			}
			if withinRoot {
				excluded, err := workspace.MatchesExclusion(root, manifest, repo.Exclusions)
				if err != nil {
					return "", fmt.Errorf("check explicit manifest %q exclusion: %w", manifest, err)
				}
				if excluded {
					continue
				}
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			info, err := os.Lstat(manifest)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return "", fmt.Errorf("inspect explicit manifest %q: %w", manifest, err)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("inventory %q: refuses symlink", manifest)
			}
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("inventory %q: refuses non-regular file", manifest)
			}
			file, err := os.Open(manifest)
			if err != nil {
				return "", fmt.Errorf("open explicit manifest %q: %w", manifest, err)
			}
			hash := sha256.New()
			_, readErr := io.Copy(hash, file)
			closeErr := file.Close()
			if readErr != nil || closeErr != nil {
				return "", fmt.Errorf("read explicit manifest %q: %w", manifest, errors.Join(readErr, closeErr))
			}
			relative, err := filepath.Rel(root, manifest)
			if err != nil {
				return "", fmt.Errorf("resolve explicit manifest %q identity: %w", manifest, err)
			}
			if _, err := fmt.Fprintf(digest, "%s\x00%x\x00", filepath.ToSlash(relative), hash.Sum(nil)); err != nil {
				return "", fmt.Errorf("write explicit manifest digest for %q: %w", manifest, err)
			}
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func inventoryPathWithin(root, candidate string) (bool, error) {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false, err
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative), nil
}

func inventoryRoot(repository workspace.Repository) (string, error) {
	root := strings.TrimSpace(repository.RealPath)
	if root == "" {
		root = strings.TrimSpace(repository.Path)
	}
	if root == "" {
		return "", fmt.Errorf("registered repository %s: root path is empty", repository.Name)
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve registered repository %s root %q: %w", repository.Name, root, err)
	}
	return filepath.Clean(absolute), nil
}

func resolveRepositoryPaths(root string, paths []string) ([]string, error) {
	resolved := make([]string, 0, len(paths))
	for index, value := range paths {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(root, value)
		}
		absolute, err := filepath.Abs(value)
		if err != nil {
			return nil, fmt.Errorf("resolve path %q: %w", paths[index], err)
		}
		resolved = append(resolved, filepath.Clean(absolute))
	}
	return resolved, nil
}

func recordPath(root string, generation uint64) string {
	return filepath.Join(root, "freshness", fmt.Sprintf("%020d.json", generation))
}

// Save is deliberately separate from CURRENT. A missing attestation makes a
// generation unverified, never corrupt, and cannot make a stale graph fresh.
func Save(ctx context.Context, root string, generation uint64, digest string) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	filename := recordPath(root, generation)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return fmt.Errorf("create freshness directory %q: %w", filepath.Dir(filename), err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	body, err := json.Marshal(Record{Version: 1, Generation: generation, Digest: digest})
	if err != nil {
		return fmt.Errorf("encode freshness record for generation %d: %w", generation, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".freshness-*")
	if err != nil {
		return fmt.Errorf("create temporary freshness record for %q: %w", filename, err)
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		if !removeTemporary {
			return
		}
		if removeErr := os.Remove(temporaryName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove temporary freshness record %q: %w", temporaryName, removeErr))
		}
	}()
	if err := ctx.Err(); err != nil {
		return errors.Join(err, temporary.Close())
	}
	if _, err := temporary.Write(body); err != nil {
		closeErr := temporary.Close()
		return fmt.Errorf("write freshness record %q: %w", filename, errors.Join(err, closeErr))
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary freshness record %q: %w", temporaryName, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("replace freshness record %q: %w", filename, err)
	}
	removeTemporary = false
	return nil
}

func Check(ctx context.Context, root string, generation uint64, repositories []workspace.Repository) Status {
	status := Status{Generation: generation, State: "unverified"}
	body, err := os.ReadFile(recordPath(root, generation))
	if err != nil {
		status.Detail = "generation has no readable content inventory"
		return status
	}
	var record Record
	if err := json.Unmarshal(body, &record); err != nil || record.Version != 1 || record.Generation != generation || len(record.Digest) != 64 {
		status.Detail = "generation content inventory is invalid"
		return status
	}
	if _, err := hex.DecodeString(record.Digest); err != nil {
		status.Detail = "generation content inventory is invalid"
		return status
	}
	digest, err := Capture(ctx, repositories)
	if err != nil {
		status.State = "unavailable"
		status.Detail = err.Error()
		return status
	}
	status.State = "fresh"
	if digest != record.Digest {
		status.State = "stale"
		status.Detail = "registered source inventory changed"
	}
	return status
}
