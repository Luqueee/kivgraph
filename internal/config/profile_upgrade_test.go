//go:build unix

package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/filelock"
)

func legacyProfileFixture(t *testing.T) (string, string) {
	t.Helper()
	// Unix socket addresses must fit Darwin's 104-byte limit.
	root, err := os.MkdirTemp("", "kg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(state, "generations", "000007"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.yaml")
	registry := filepath.Join(root, "repositories.yaml")
	writeConfigFixture(t, path, "version: 1\nworkspace:\n  repositories_file: "+registry+"\nstorage:\n  database_path: "+filepath.Join(state, "graph.lbdb")+"\n")
	writeConfigFixture(t, registry, "version: 1\nrepositories: []\n")
	writeConfigFixture(t, filepath.Join(state, "CURRENT"), "000007\n")
	return path, state
}

func TestProfileUpgradePreservesRuntimeAndFreshness(t *testing.T) {
	path, state := legacyProfileFixture(t)
	// A real socket cannot be copied as an ordinary file. It must retain its
	// identity, as must the lock inode protecting this installation.
	listener, err := net.Listen("unix", filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	listener.(*net.UnixListener).SetUnlinkOnClose(false)
	if _, err := LoadProfile(path, ""); err == nil || !strings.Contains(err.Error(), "running daemon") {
		t.Fatalf("live daemon refusal = %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	socketBefore, err := os.Lstat(filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(state, "freshness"), 0700); err != nil {
		t.Fatal(err)
	}
	writeConfigFixture(t, filepath.Join(state, "freshness", "00000000000000000007.json"), `{"version":1,"generation":7,"digest":"attestation"}`)
	writeConfigFixture(t, filepath.Join(state, "publish.lock"), "")
	before, err := os.Stat(filepath.Join(state, "publish.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(path, ""); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filepath.Join(state, "publish.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("migration replaced live lock inode")
	}
	for _, root := range []string{filepath.Join(state, "profiles", "default"), state + ".pre-profiles"} {
		if _, err := os.Stat(filepath.Join(root, "freshness", "00000000000000000007.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(filepath.Join(root, "daemon.sock")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("copied runtime socket into %q: %v", root, err)
		}
	}
	socketAfter, err := os.Lstat(filepath.Join(state, "daemon.sock"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(socketBefore, socketAfter) {
		t.Fatal("replaced runtime socket")
	}
	if _, err := LoadProfile(path, ""); err != nil {
		t.Fatalf("idempotent load: %v", err)
	}
}

func TestProfileUpgradeRefusesActiveWritersAndCanRetry(t *testing.T) {
	for _, name := range []string{"publish.lock", "resync.lock", "analyzer-targets.lock", "profile-migration.lock"} {
		t.Run(name, func(t *testing.T) {
			path, state := legacyProfileFixture(t)
			lockPath := filepath.Join(state, name)
			if name == "profile-migration.lock" {
				lockPath = state + ".profile-migration.lock"
			}
			lock, acquired, err := filelock.Acquire(lockPath)
			if err != nil || !acquired {
				t.Fatalf("lock: %v %v", acquired, err)
			}
			t.Cleanup(func() { _ = lock.Release() })
			expected := name
			if name == "profile-migration.lock" {
				expected = "profile migration"
			}
			if _, err := LoadProfile(path, ""); err == nil || !strings.Contains(err.Error(), expected) {
				t.Fatalf("refusal while %q was held = %v", name, err)
			}
			if _, err := os.Stat(filepath.Join(state, "profiles", "default")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("published on refusal: %v", err)
			}
			if err := lock.Release(); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadProfile(path, ""); err != nil {
				t.Fatalf("retry: %v", err)
			}
		})
	}
}

func TestProfileUpgradeRejectsPartialDestination(t *testing.T) {
	path, state := legacyProfileFixture(t)
	if err := os.MkdirAll(filepath.Join(state, "profiles", "default"), 0700); err != nil {
		t.Fatal(err)
	}
	configuration, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureDefaultProfile(configuration, configuration.Workspace.RepositoriesFile); err == nil || !strings.Contains(err.Error(), "repositories.yaml") {
		t.Fatalf("incomplete destination refusal = %v", err)
	}
}

func TestProfileUpgradeRefusesSpecialGraphArtifact(t *testing.T) {
	path, state := legacyProfileFixture(t)
	listener, err := net.Listen("unix", filepath.Join(state, "generations", "unexpected.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if _, err := LoadProfile(path, ""); err == nil || !strings.Contains(err.Error(), "unexpected.sock") {
		t.Fatalf("special graph refusal = %v", err)
	}
	if _, err := os.Stat(state + ".pre-profiles"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published backup on failure: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(path, ""); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestProfileUpgradeResumesOnlyAnIdenticalBackup(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			path, state := legacyProfileFixture(t)
			if _, err := LoadProfile(path, ""); err != nil {
				t.Fatal(err)
			}
			// Simulate interruption after publishing backup but before profile.
			if err := os.RemoveAll(filepath.Join(state, "profiles")); err != nil {
				t.Fatal(err)
			}
			if changed {
				writeConfigFixture(t, filepath.Join(state+".pre-profiles", "CURRENT"), "different\n")
			}
			_, err := LoadProfile(path, "")
			if changed && err == nil {
				t.Fatalf("changed=%t: overwrote mismatched recovery point", changed)
			}
			if changed {
				contents, readErr := os.ReadFile(filepath.Join(state+".pre-profiles", "CURRENT"))
				if readErr != nil || string(contents) != "different\n" {
					t.Fatalf("changed=%t: recovery CURRENT = %q, %v", changed, contents, readErr)
				}
			}
			if !changed && err != nil {
				t.Fatalf("changed=%t: resume: %v", changed, err)
			}
		})
	}
}

func TestProfileUpgradeResumesAcrossPermissionNarrowing(t *testing.T) {
	path, state := legacyProfileFixture(t)
	if _, err := LoadProfile(path, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(state, "profiles")); err != nil {
		t.Fatal(err)
	}
	backupCurrent := filepath.Join(state+".pre-profiles", "CURRENT")
	if err := os.Chmod(backupCurrent, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(path, ""); err != nil {
		t.Fatalf("resume after permission-only backup difference: %v", err)
	}
}

func TestReadProfileCannotMigrateLegacyState(t *testing.T) {
	path, state := legacyProfileFixture(t)
	if _, err := ReadProfile(path, ""); err == nil {
		t.Fatalf("ReadProfile(config=%q) accepted unmigrated state", path)
	}
	for _, candidate := range []string{
		filepath.Join(state, "profiles"),
		state + ".pre-profiles",
		state + ".profile-migration.lock",
	} {
		if _, err := os.Stat(candidate); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("ReadProfile(config=%q) created %q: %v", path, candidate, err)
		}
	}
}

func TestProfileUpgradeRejectsUnsafeArtifactsWithoutPublication(t *testing.T) {
	expectedErrors := map[string]string{
		"current-parent":    "invalid generation",
		"current-file":      "000008",
		"graph-symlink":     "symbolic link",
		"unexpected-socket": "unknown.sock",
		"unreadable-graph":  "unreadable",
	}
	for kind, expected := range expectedErrors {
		t.Run(kind, func(t *testing.T) {
			path, state := legacyProfileFixture(t)
			switch kind {
			case "current-parent":
				writeConfigFixture(t, filepath.Join(state, "CURRENT"), "..\n")
			case "current-file":
				writeConfigFixture(t, filepath.Join(state, "generations", "000008"), "not a directory")
				writeConfigFixture(t, filepath.Join(state, "CURRENT"), "000008\n")
			case "graph-symlink":
				if err := os.Symlink(path, filepath.Join(state, "generations", "link")); err != nil {
					t.Fatal(err)
				}
			case "unexpected-socket":
				listener, err := net.Listen("unix", filepath.Join(state, "unknown.sock"))
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
			case "unreadable-graph":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses read permissions")
				}
				file := filepath.Join(state, "generations", "unreadable")
				writeConfigFixture(t, file, "private")
				if err := os.Chmod(file, 0000); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := LoadProfile(path, ""); err == nil || !strings.Contains(err.Error(), expected) {
				t.Fatalf("unsafe graph refusal for %q = %v", kind, err)
			}
			if _, err := os.Stat(filepath.Join(state, "profiles", "default")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("published invalid state: %v", err)
			}
		})
	}
}

// OS failures after successful staging (close/unlock or rename failure during
// a concurrent filesystem change) have no deterministic injection seam here.
// Tests exercise real read failures and locks rather than adding test-only
// filesystem hooks to the migration.
