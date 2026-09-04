package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
