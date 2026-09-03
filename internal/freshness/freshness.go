// Package freshness binds a conservative source inventory to a generation.
// It never infers graph completeness from filesystem stability.
package freshness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
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
	repos := slices.Clone(repositories)
	slices.SortFunc(repos, func(a, b workspace.Repository) int { return strings.Compare(a.Name, b.Name) })
	digest := sha256.New()
	extensions := config.SourceExtensionSet(config.SupportedLanguages())
	for _, repo := range repos {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		root := repo.RealPath
		if root == "" {
			root = repo.Path
		}
		stat, err := os.Stat(root)
		if err != nil {
			return "", fmt.Errorf("registered repository %s: %w", repo.Name, err)
		}
		if !stat.IsDir() {
			return "", fmt.Errorf("registered repository %s is not a directory", repo.Name)
		}
		identity, _ := json.Marshal(struct {
			Name, Path, Commit, Branch string
			Languages, Exclusions      []string
		}{repo.Name, root, repo.Commit, repo.Branch, repo.Languages, repo.Exclusions})
		digest.Write(identity)
		err = filepath.WalkDir(root, func(filename string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			relative, err := filepath.Rel(root, filename)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			excluded := false
			for _, pattern := range repo.Exclusions {
				match, err := path.Match(pattern, relative)
				if err != nil {
					return err
				}
				excluded = excluded || match || relative == pattern || strings.HasPrefix(relative, strings.TrimSuffix(pattern, "/")+"/")
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
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			manifest := slices.Contains([]string{".json", ".yaml", ".yml", ".toml", ".mod", ".sum", ".lock", ".xml", ".props", ".targets", ".csproj", ".sln", ".cfg"}, ext)
			if !manifest && !config.HasSourceExtension(extensions, entry.Name()) && entry.Name() != "requirements.txt" {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("source inventory refuses symlink %s", filename)
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("source inventory refuses non-regular file %s", filename)
			}
			file, err := os.Open(filename)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, readErr := io.Copy(hash, file)
			closeErr := file.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			fmt.Fprintf(digest, "%s\x00%x\x00", relative, hash.Sum(nil))
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("inventory %s: %w", repo.Name, err)
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func recordPath(root string, generation uint64) string {
	return filepath.Join(root, "freshness", fmt.Sprintf("%020d.json", generation))
}

// Save is deliberately separate from CURRENT. A missing attestation makes a
// generation unverified, never corrupt, and cannot make a stale graph fresh.
func Save(root string, generation uint64, digest string) error {
	filename := recordPath(root, generation)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return err
	}
	body, err := json.Marshal(Record{Version: 1, Generation: generation, Digest: digest})
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(filename), ".freshness-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(body); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), filename)
}

func Check(ctx context.Context, root string, generation uint64, repositories []workspace.Repository) Status {
	status := Status{Generation: generation, State: "unverified"}
	body, err := os.ReadFile(recordPath(root, generation))
	if err != nil {
		status.Detail = "generation has no readable content inventory"
		return status
	}
	var record Record
	if json.Unmarshal(body, &record) != nil || record.Version != 1 || record.Generation != generation || len(record.Digest) != 64 {
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
