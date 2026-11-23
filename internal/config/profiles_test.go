package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestProfilePointerCannotDangle(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if _, err := Initialize(InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	if err := UseProfile(configPath, "missing"); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("UseProfile() error = %v, want ErrProfileNotFound", err)
	}
	if err := RemoveProfile(configPath, "default"); !errors.Is(err, ErrDefaultProfile) {
		t.Fatalf("RemoveProfile(default) error = %v, want ErrDefaultProfile", err)
	}
	if err := CreateProfile(configPath, "other"); err != nil {
		t.Fatal(err)
	}
	if err := UseProfile(configPath, "other"); err != nil {
		t.Fatal(err)
	}
	configuration, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(profileAt(configuration, "other").StateDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(configPath, ""); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("LoadProfile() dangling default error = %v, want ErrProfileNotFound", err)
	}
}

func TestProfilesCreateListUseAndRemove(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if _, err := Initialize(InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	if err := CreateProfile(configPath, "frontend"); err != nil {
		t.Fatalf("CreateProfile() error = %v", err)
	}
	profiles, err := ListProfiles(configPath)
	if err != nil {
		t.Fatalf("ListProfiles() error = %v", err)
	}
	if len(profiles) != 2 || profiles[0].Name != "default" || !profiles[0].Default || profiles[1].Name != "frontend" {
		t.Fatalf("profiles = %#v", profiles)
	}
	if err := UseProfile(configPath, "frontend"); err != nil {
		t.Fatalf("UseProfile() error = %v", err)
	}
	if err := RemoveProfile(configPath, "default"); err != nil {
		t.Fatalf("RemoveProfile(old default) error = %v", err)
	}
	profiles, err = ListProfiles(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Name != "frontend" || !profiles[0].Default {
		t.Fatalf("profiles after remove = %#v", profiles)
	}
}

func TestCreateProfileRejectsReservedName(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if _, err := Initialize(InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	if err := CreateProfile(configPath, "*"); err == nil {
		t.Fatal("CreateProfile(*) error = nil")
	}
}

func TestLoadProfileScopesDerivedStateAndRegistry(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if _, err := Initialize(InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	if err := CreateProfile(configPath, "frontend"); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProfile(configPath, "frontend")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	wantRoot := filepath.Join(root, "state", "profiles", "frontend")
	if loaded.Profile != "frontend" {
		t.Errorf("loaded profile = %q, want frontend", loaded.Profile)
	}
	if got := filepath.Dir(loaded.Config.Storage.DatabasePath); got != wantRoot {
		t.Errorf("loaded graph root = %q, want %q", got, wantRoot)
	}
	wantRepositories := filepath.Join(wantRoot, "repositories.yaml")
	if loaded.RepositoriesPath != wantRepositories {
		t.Errorf("loaded repositories path = %q, want %q", loaded.RepositoriesPath, wantRepositories)
	}
	cachePath := filepath.Clean(loaded.Config.Indexing.FactCachePath)
	if cachePath == "." || cachePath == wantRoot || strings.HasPrefix(cachePath, wantRoot+string(filepath.Separator)) {
		t.Errorf("loaded fact cache path = %q, want a non-profile-specific path", loaded.Config.Indexing.FactCachePath)
	}
	defaultLoaded, err := LoadProfile(configPath, "default")
	if err != nil {
		t.Fatalf("LoadProfile(default) error = %v", err)
	}
	if defaultLoaded.Config.Indexing.FactCachePath != loaded.Config.Indexing.FactCachePath {
		t.Fatalf("profile caches differ: default=%q frontend=%q", defaultLoaded.Config.Indexing.FactCachePath, loaded.Config.Indexing.FactCachePath)
	}
}

func TestInitializeMigratesLegacyFactCacheAtInstallationScope(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	state := filepath.Join(root, "state")
	legacyEntry := filepath.Join(state, "factcache", "entry.json")
	if err := os.MkdirAll(filepath.Dir(legacyEntry), 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigFixture(t, configPath, "version: 1\nworkspace:\n  repositories_file: "+repositoriesPath+"\nstorage:\n  database_path: "+filepath.Join(state, "graph.lbdb")+"\n")
	writeConfigFixture(t, repositoriesPath, "version: 1\nrepositories: []\n")
	if err := os.WriteFile(legacyEntry, []byte("cached"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Initialize(InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("Initialize(%q) migration error = %v", configPath, err)
	}
	retainedEntry := filepath.Join(state, "factcache", "entry.json")
	content, err := os.ReadFile(retainedEntry)
	if err != nil {
		t.Fatalf("ReadFile(%q) retained installation fact cache: %v", retainedEntry, err)
	}
	if string(content) != "cached" {
		t.Fatalf("retained installation fact cache %q = %q, want cached", retainedEntry, content)
	}
	profileFactCache := filepath.Join(state, "profiles", "default", "factcache")
	if _, err := os.Stat(profileFactCache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("profile-local fact cache %q exists after migration: %v", profileFactCache, err)
	}
}

func TestInitializeMigratesLegacyGenerationAndRetainsRollback(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(state, "generations", "000007"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigFixture(t, configPath, "version: 1\nworkspace:\n  repositories_file: "+repositoriesPath+"\nstorage:\n  database_path: "+filepath.Join(state, "graph.lbdb")+"\n")
	writeConfigFixture(t, repositoriesPath, "version: 1\nrepositories: []\n")
	writeConfigFixture(t, filepath.Join(state, "CURRENT"), "000007\n")
	writeConfigFixture(t, filepath.Join(state, "generations", "000007", "snapshot.kvsnap"), "published")

	if _, err := Initialize(InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("Initialize() migration error = %v", err)
	}
	for _, path := range []string{
		filepath.Join(state, "profiles", "default", "CURRENT"),
		filepath.Join(state, "profiles", "default", "generations", "000007", "snapshot.kvsnap"),
		filepath.Join(state+".pre-profiles", "CURRENT"),
		filepath.Join(state+".pre-profiles", "generations", "000007", "snapshot.kvsnap"),
	} {
		if _, err := filepath.Abs(path); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("migrated path %q: %v", path, err)
		}
	}
}

func TestLoadProfileMigratesLegacyInstallationWithoutReindexing(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(state, "generations", "000009"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigFixture(t, configPath, "version: 1\nworkspace:\n  repositories_file: "+repositoriesPath+"\nstorage:\n  database_path: "+filepath.Join(state, "graph.lbdb")+"\n")
	writeConfigFixture(t, repositoriesPath, "version: 1\nrepositories: []\n")
	writeConfigFixture(t, filepath.Join(state, "CURRENT"), "000009\n")

	loaded, err := LoadProfile(configPath, "")
	if err != nil {
		t.Fatalf("LoadProfile() migration error = %v", err)
	}
	if loaded.Profile != "default" {
		t.Fatalf("Profile = %q, want default", loaded.Profile)
	}
	for _, path := range []string{
		filepath.Join(state, "profiles", "default", "CURRENT"),
		filepath.Join(state+".pre-profiles", "CURRENT"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("migrated path %q: %v", path, err)
		}
	}
}

func TestLegacyMigrationRejectsDanglingCurrentWithoutTouchingState(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigFixture(t, configPath, "version: 1\nworkspace:\n  repositories_file: "+repositoriesPath+"\nstorage:\n  database_path: "+filepath.Join(state, "graph.lbdb")+"\n")
	writeConfigFixture(t, repositoriesPath, "version: 1\nrepositories: []\n")
	writeConfigFixture(t, filepath.Join(state, "CURRENT"), "000404\n")

	if _, err := ListProfiles(configPath); err == nil {
		t.Fatal("ListProfiles() dangling CURRENT error = nil")
	}
	if _, err := os.Stat(filepath.Join(state, "CURRENT")); err != nil {
		t.Fatalf("legacy CURRENT changed after refused migration: %v", err)
	}
	if _, err := os.Stat(state + ".pre-profiles"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rollback backup exists after validation refusal: %v", err)
	}
}

func TestLegacyMigrationRecoversBackupAfterInterruptedRename(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	state := filepath.Join(root, "state")
	backup := state + ".pre-profiles"
	if err := os.MkdirAll(filepath.Join(backup, "generations", "000011"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeConfigFixture(t, configPath, "version: 1\nworkspace:\n  repositories_file: "+repositoriesPath+"\nstorage:\n  database_path: "+filepath.Join(state, "graph.lbdb")+"\n")
	writeConfigFixture(t, repositoriesPath, "version: 1\nrepositories: []\n")
	writeConfigFixture(t, filepath.Join(backup, "CURRENT"), "000011\n")

	profiles, err := ListProfiles(configPath)
	if err != nil {
		t.Fatalf("ListProfiles() recovery error = %v", err)
	}
	if len(profiles) != 1 || profiles[0].Name != "default" {
		t.Fatalf("profiles = %#v", profiles)
	}
	if _, err := os.Stat(filepath.Join(state, "profiles", "default", "CURRENT")); err != nil {
		t.Fatalf("recovered migrated CURRENT: %v", err)
	}
}

// `kivgraph index` keeps a project's configuration and state in `.kivgraph`,
// inside the repository it indexes. The indexers refuse a synthetic `go.work`
// or a build directory there, so the profile has to point them elsewhere --
// and it pointed the workspace back inside after the configuration had moved
// it out.
func TestLoadProfileKeepsWhatIndexersRefuseOutOfTheRepository(t *testing.T) {
	testsupport.SetHome(t, t.TempDir())
	project := t.TempDir()
	configPath := filepath.Join(project, ".kivgraph", "config.yaml")
	if _, err := Initialize(InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	inside := func(path string) bool {
		relative, err := filepath.Rel(project, path)
		return err == nil && !strings.HasPrefix(relative, "..")
	}
	paths := func(loaded Loaded) map[string]string {
		return map[string]string{
			"go.work": loaded.Config.Go.SyntheticWorkFile,
			"rust":    loaded.Config.Rust.TargetDirectory,
			"java":    loaded.Config.Java.TargetDirectory,
			"csharp":  loaded.Config.CSharp.TargetDirectory,
		}
	}

	// Nothing registered yet: no repository to be inside of, nothing moves.
	before, err := LoadProfile(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if !inside(before.Config.Rust.TargetDirectory) {
		t.Fatalf("the fixture is not the case: rust target = %q", before.Config.Rust.TargetDirectory)
	}

	registry := "version: 1\nrepositories:\n  - name: project\n    path: " + project + "\n    languages: [go]\n"
	if err := os.WriteFile(before.RepositoriesPath, []byte(registry), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProfile(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for name, path := range paths(loaded) {
		if inside(path) {
			t.Errorf("%s = %q, inside the repository %q", name, path, project)
		}
		if other, taken := seen[path]; taken {
			t.Errorf("%s and %s share %q", name, other, path)
		}
		seen[path] = name
	}
	// And the directory it moved to is there, which is what `doctor` checks.
	if _, err := os.Stat(filepath.Dir(loaded.Config.Go.SyntheticWorkFile)); err != nil {
		t.Errorf("the parent of the moved go.work is not there: %v", err)
	}
	// The graph stays where the project keeps it.
	if !inside(loaded.Config.Storage.DatabasePath) {
		t.Errorf("the graph moved out of the project: %q", loaded.Config.Storage.DatabasePath)
	}

	// Two projects do not meet outside, and neither do two profiles of one.
	other := t.TempDir()
	otherConfig := filepath.Join(other, ".kivgraph", "config.yaml")
	if _, err := Initialize(InitOptions{ConfigPath: otherConfig}); err != nil {
		t.Fatal(err)
	}
	otherLoaded, err := LoadProfile(otherConfig, "")
	if err != nil {
		t.Fatal(err)
	}
	otherRegistry := "version: 1\nrepositories:\n  - name: other\n    path: " + other + "\n    languages: [go]\n"
	if err := os.WriteFile(otherLoaded.RepositoriesPath, []byte(otherRegistry), 0o600); err != nil {
		t.Fatal(err)
	}
	if otherLoaded, err = LoadProfile(otherConfig, ""); err != nil {
		t.Fatal(err)
	}
	for name, path := range paths(otherLoaded) {
		if path == paths(loaded)[name] {
			t.Errorf("two projects share %s = %q", name, path)
		}
	}

	// A registered repository that holds none of the state moves nothing.
	elsewhere := "version: 1\nrepositories:\n  - name: elsewhere\n    path: " + t.TempDir() + "\n    languages: [go]\n"
	if err := os.WriteFile(before.RepositoriesPath, []byte(elsewhere), 0o600); err != nil {
		t.Fatal(err)
	}
	untouched, err := LoadProfile(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths(untouched), paths(before); !reflect.DeepEqual(got, want) {
		t.Errorf("paths = %v, want them as configured: %v", got, want)
	}
}

// An empty backups directory is what Initialize makes before the default
// profile is built; it is not state from before profiles, so a fresh
// installation keeps no recovery point.
func TestInitializeKeepsNoRecoveryPointForEmptyBackups(t *testing.T) {
	testsupport.SetHome(t, testsupport.TempDir(t))
	configPath := filepath.Join(testsupport.TempDir(t), ".kivgraph", "config.yaml")
	if _, err := Initialize(InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProfile(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Dir(loaded.Config.Storage.DatabasePath)
	if _, err := os.Stat(state + ".pre-profiles"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a fresh installation kept a recovery point (%v)", err)
	}
}

// Java and C# both name their target `target`. Inside one repository they are
// both moved outside it, and must not be sent to the same directory: each
// build would overwrite the other's output. A name nobody else took is kept.
func TestKeepOutOfRepositoriesGivesEachTargetItsOwnDirectory(t *testing.T) {
	repository := testsupport.TempDir(t)
	outside := testsupport.TempDir(t)
	configuration := Config{}
	configuration.Rust.TargetDirectory = filepath.Join(repository, "rust-out")
	configuration.Java.TargetDirectory = filepath.Join(repository, "target")
	configuration.CSharp.TargetDirectory = filepath.Join(repository, "nested", "target")
	keepOutOfRepositories(&configuration, "default", outside, []Repository{{Name: "project", Path: repository}})

	got := []string{configuration.Rust.TargetDirectory, configuration.Java.TargetDirectory, configuration.CSharp.TargetDirectory}
	want := []string{
		filepath.Join(outside, "rust-out"),
		filepath.Join(outside, "target"),
		filepath.Join(outside, "csharp-target"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("targets = %q, want %q", got, want)
	}
}

// A workspace split in several is written as `<name>.<n>.work` beside the
// first, and a profile may be called `default.1`. The file of one profile
// must never be a later file of another.
func TestKeepOutOfRepositoriesNamesAWorkFileNoOtherProfileSplitsInto(t *testing.T) {
	repository := testsupport.TempDir(t)
	outside := testsupport.TempDir(t)
	nameOf := func(profile string) string {
		configuration := Config{}
		configuration.Go.SyntheticWorkFile = filepath.Join(repository, ".kivgraph", "go.work")
		keepOutOfRepositories(&configuration, profile, outside, []Repository{{Name: "project", Path: repository}})
		return configuration.Go.SyntheticWorkFile
	}
	names := map[string]string{}
	for _, profile := range []string{"default", "default.1", "default.1.2", "default.profile", "default.profile.1"} {
		name := nameOf(profile)
		if filepath.Dir(name) != outside {
			t.Fatalf("profile %s: %q is not directly in %q", profile, name, outside)
		}
		for other, taken := range names {
			if taken == name {
				t.Errorf("profiles %s and %s share %q", profile, other, name)
			}
		}
		names[profile] = name
	}
	// The rule the indexer splits by: `<name minus .work>.<n>.work`.
	for profile, name := range names {
		for index := 1; index <= 3; index++ {
			split := strings.TrimSuffix(name, ".work") + "." + strconv.Itoa(index) + ".work"
			for other, taken := range names {
				if taken == split {
					t.Errorf("file %d of profile %s is the file of profile %s: %q", index, profile, other, split)
				}
			}
		}
	}
}
