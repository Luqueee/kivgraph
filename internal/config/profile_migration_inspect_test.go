package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyProfileFixture is an installation from before profiles: one published
// generation directly under the state directory.
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

// stateListing names every entry under the state root and beside it, so a test
// can show that a diagnostic wrote nothing.
func stateListing(t *testing.T, state string) []string {
	t.Helper()
	var listing []string
	for _, root := range []string{state, state + ".pre-profiles", state + ".profile-migration.lock"} {
		err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			listing = append(listing, path)
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	return listing
}

// A recovery point that is not a copy of the state is what a workstation is left
// with when something recreates the state directory after an old backup: every
// command fails and the diagnostic has to be the one that says so. It must name
// the directory, agree with what the migration itself answers, and touch nothing.
func TestInspectProfileMigrationNamesABackupThatIsNotACopyOfTheState(t *testing.T) {
	path, state := legacyProfileFixture(t)
	backup := state + ".pre-profiles"
	if err := os.Mkdir(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigFixture(t, filepath.Join(backup, "CURRENT"), "000003\n")
	before := stateListing(t, state)

	err := InspectProfileMigration(path)
	if !errors.Is(err, ErrProfileMigrationBackup) || !strings.Contains(err.Error(), backup) {
		t.Fatalf("InspectProfileMigration() = %v, want ErrProfileMigrationBackup naming %s", err, backup)
	}
	if after := stateListing(t, state); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("the inspection changed the state:\nbefore %v\nafter  %v", before, after)
	}
	if _, migrateErr := LoadProfile(path, ""); migrateErr == nil || migrateErr.Error() != err.Error() {
		t.Fatalf("LoadProfile() = %v, want the refusal the inspection predicted: %v", migrateErr, err)
	}
}

// The three states that are not a blocked migration. The middle one is the one a
// cruder check gets wrong: an interrupted migration leaves a backup too, and it
// resumes.
func TestInspectProfileMigrationAcceptsWhatTheMigrationAccepts(t *testing.T) {
	t.Run("never migrated, no backup", func(t *testing.T) {
		path, _ := legacyProfileFixture(t)
		if err := InspectProfileMigration(path); err != nil {
			t.Fatalf("InspectProfileMigration() = %v, want nil", err)
		}
	})
	t.Run("interrupted after the backup was published", func(t *testing.T) {
		path, state := legacyProfileFixture(t)
		if _, err := LoadProfile(path, ""); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(state, "profiles")); err != nil {
			t.Fatal(err)
		}
		if err := InspectProfileMigration(path); err != nil {
			t.Fatalf("InspectProfileMigration() = %v, want nil: the backup is a copy of the state", err)
		}
		if _, err := LoadProfile(path, ""); err != nil {
			t.Fatalf("LoadProfile() = %v after an inspection that passed", err)
		}
	})
	t.Run("already migrated, backup since edited", func(t *testing.T) {
		path, state := legacyProfileFixture(t)
		if _, err := LoadProfile(path, ""); err != nil {
			t.Fatal(err)
		}
		writeConfigFixture(t, filepath.Join(state+".pre-profiles", "CURRENT"), "different\n")
		if err := InspectProfileMigration(path); err != nil {
			t.Fatalf("InspectProfileMigration() = %v, want nil: a migrated profile no longer reads the backup", err)
		}
	})
	t.Run("backup with no state to compare it to", func(t *testing.T) {
		path, state := legacyProfileFixture(t)
		if err := os.Rename(state, state+".pre-profiles"); err != nil {
			t.Fatal(err)
		}
		if err := InspectProfileMigration(path); err != nil {
			t.Fatalf("InspectProfileMigration() = %v, want nil: the migration renames it back", err)
		}
	})
}

func TestInspectProfileMigrationReportsAnUnreadableConfiguration(t *testing.T) {
	if err := InspectProfileMigration(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Fatal("InspectProfileMigration() accepted a configuration that does not exist")
	}
}

// The digest read in place has to be the digest of the copy a migration makes,
// byte for byte: it exists only to answer early what that comparison will
// answer, and a second implementation that drifts does not fail, it answers.
func TestLegacyProfileDigestIsTheDigestOfTheMigratedCopy(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	registry := filepath.Join(root, "registry.yaml")
	writeConfigFixture(t, registry, "version: 1\nrepositories: []\n")
	for name, content := range map[string]string{
		"CURRENT":                             "000007\n",
		"BACKUP":                              "000006\n",
		"generations/000007/snapshot.kvsnap":  "snapshot",
		"generations/000006/graph.lbdb":       "",
		"backups/000005/CURRENT":              "000005\n",
		"freshness/00000000000000000007.json": `{"version":1}`,
		"go.work":                             "go 1.26\n",
		"graph.lbdb":                          "graph",
		// Not artifacts of the graph: a migration leaves them where they are.
		"events.jsonl":          "{}\n",
		"factcache/aa/entry":    "fact",
		"repositories.yaml":     "not the configured registry\n",
		"analyzer-targets.lock": "",
	} {
		path := filepath.Join(state, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		writeConfigFixture(t, path, content)
	}
	if err := os.MkdirAll(filepath.Join(state, "generations", "empty"), 0o700); err != nil {
		t.Fatal(err)
	}

	copied := filepath.Join(root, "copy")
	if err := os.Mkdir(copied, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyProfileRegistry(registry, filepath.Join(copied, "repositories.yaml")); err != nil {
		t.Fatal(err)
	}
	for _, name := range legacyProfileArtifacts {
		source := filepath.Join(state, name)
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err := copyProfileArtifact(source, filepath.Join(copied, name)); err != nil {
			t.Fatal(err)
		}
	}
	want, err := profileArtifactDigest(copied)
	if err != nil {
		t.Fatal(err)
	}
	got, err := legacyProfileDigest(state, registry)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("legacyProfileDigest() = %x, want the digest of the copy %x", got, want)
	}

	// And it is a digest of the contents, not of the names alone.
	writeConfigFixture(t, filepath.Join(state, "generations", "000007", "snapshot.kvsnap"), "snapshoT")
	changed, err := legacyProfileDigest(state, registry)
	if err != nil {
		t.Fatal(err)
	}
	if changed == want {
		t.Fatal("legacyProfileDigest() did not change with the contents of an artifact")
	}
}

func TestLegacyProfileDigestRefusesWhatAMigrationRefuses(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	registry := filepath.Join(root, "registry.yaml")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := legacyProfileDigest(state, registry); err == nil || !strings.Contains(err.Error(), "read repository registry") {
		t.Fatalf("missing registry: %v, want a read error naming it", err)
	}
	writeConfigFixture(t, registry, "version: 1\nrepositories: []\n")
	if err := os.Symlink(registry, filepath.Join(state, "graph.lbdb")); err != nil {
		t.Skipf("symbolic links are not available here: %v", err)
	}
	if _, err := legacyProfileDigest(state, registry); err == nil || !strings.Contains(err.Error(), "unexpected recovery artifact") {
		t.Fatalf("symbolic link as an artifact: %v, want a refusal", err)
	}
}
