package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Luqueee/kivgraph/internal/durable"
	"github.com/Luqueee/kivgraph/internal/filelock"
	"gopkg.in/yaml.v3"
)

var (
	ErrProfileNotFound = errors.New("profile does not exist")
	ErrProfileExists   = errors.New("profile already exists")
	ErrDefaultProfile  = errors.New("default profile cannot be removed")
)

// Profile describes one independently indexed repository registry.
type Profile struct {
	Name             string
	Default          bool
	StateDirectory   string
	RepositoriesPath string
	TopologyPath     string
}

func profilesRoot(configuration Config) string {
	return filepath.Join(filepath.Dir(configuration.Storage.DatabasePath), "profiles")
}

func profileAt(configuration Config, name string) Profile {
	directory := filepath.Join(profilesRoot(configuration), name)
	return Profile{
		Name:             name,
		Default:          name == configuration.Profiles.Default,
		StateDirectory:   directory,
		RepositoriesPath: filepath.Join(directory, "repositories.yaml"),
		TopologyPath:     filepath.Join(directory, "topology.yaml"),
	}
}

// ensureDefaultProfile builds the initial profile beside the legacy layout and
// publishes it with one rename. Existing state is copied, never moved, so an
// interrupted upgrade still has the graph it started with.
func ensureDefaultProfile(configuration Config, repositoriesPath string) (resultErr error) {
	profile := profileAt(configuration, configuration.Profiles.Default)
	if _, err := os.Stat(profile.StateDirectory); err == nil {
		return validateMigratedProfile(profile.StateDirectory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect default profile %q: %w", profile.StateDirectory, err)
	}
	stateRoot := filepath.Dir(configuration.Storage.DatabasePath)
	// Keep the installation directory and every lock inode in place. Moving
	// the root would detach running processes from both their sockets and locks.
	lock, acquired, err := filelock.Acquire(stateRoot + ".profile-migration.lock")
	if err != nil {
		return fmt.Errorf("lock profile migration: %w", err)
	}
	if !acquired {
		return errors.New("profile migration is in progress; retry after it completes")
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Release()) }()
	if _, err := os.Stat(profile.StateDirectory); err == nil {
		return validateMigratedProfile(profile.StateDirectory)
	}
	backupRoot := stateRoot + ".pre-profiles"
	if _, err := os.Stat(stateRoot); errors.Is(err, os.ErrNotExist) {
		if _, backupErr := os.Stat(backupRoot); backupErr == nil {
			if renameErr := os.Rename(backupRoot, stateRoot); renameErr != nil {
				return fmt.Errorf("recover legacy profile state: %w", renameErr)
			}
		} else {
			return fmt.Errorf("inspect legacy profile state %q: %w", stateRoot, err)
		}
	} else if err != nil {
		return fmt.Errorf("inspect legacy profile state %q: %w", stateRoot, err)
	}
	if err := inspectLegacyRuntime(stateRoot); err != nil {
		return err
	}
	for _, name := range []string{"analyzer-targets.lock", "resync.lock", "publish.lock"} {
		lock, acquired, err := filelock.Acquire(filepath.Join(stateRoot, name))
		if err != nil {
			return fmt.Errorf("lock legacy state %s: %w", name, err)
		}
		if !acquired {
			return fmt.Errorf("profile migration blocked by active writer (%s); stop writers and retry", name)
		}
		defer func() { resultErr = errors.Join(resultErr, lock.Release()) }()
	}
	root := profilesRoot(configuration)
	if entries, err := os.ReadDir(root); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || ValidateProfileName(entry.Name()) != nil {
				continue
			}
			if _, err := os.Stat(profileAt(configuration, entry.Name()).RepositoriesPath); err == nil {
				return fmt.Errorf("profiles.default %q: %w", configuration.Profiles.Default, ErrProfileNotFound)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect profiles directory: %w", err)
	}
	temporaryParent, err := os.MkdirTemp(filepath.Dir(stateRoot), ".kivgraph-profile-migration-*")
	if err != nil {
		return fmt.Errorf("prepare default profile migration: %w", err)
	}
	defer os.RemoveAll(temporaryParent)
	temporaryProfile := filepath.Join(temporaryParent, "profile")
	if err := os.MkdirAll(temporaryProfile, 0o700); err != nil {
		return fmt.Errorf("create temporary default profile: %w", err)
	}
	if err := copyProfileRegistry(repositoriesPath, filepath.Join(temporaryProfile, "repositories.yaml")); err != nil {
		return err
	}
	for _, name := range []string{
		"generations", "CURRENT", "BACKUP", "backups", "freshness",
		"go.work", "go.work.sum", "graph.lbdb",
	} {
		source := filepath.Join(stateRoot, name)
		if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect copied profile artifact %q: %w", source, err)
		}
		if err := copyProfileArtifact(source, filepath.Join(temporaryProfile, name)); err != nil {
			return fmt.Errorf("place copied profile artifact %q: %w", name, err)
		}
	}
	if err := validateMigratedProfile(temporaryProfile); err != nil {
		return err
	}
	if err := durable.Directory(temporaryProfile); err != nil {
		return fmt.Errorf("sync temporary default profile: %w", err)
	}
	if _, err := os.Lstat(backupRoot); err == nil {
		candidate, err := profileArtifactDigest(temporaryProfile)
		if err != nil {
			return err
		}
		backup, err := profileArtifactDigest(backupRoot)
		if err != nil {
			return err
		}
		if candidate != backup {
			return fmt.Errorf("profile migration backup differs from legacy state: %s", backupRoot)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect profile migration backup: %w", err)
	} else {
		temporaryBackup := filepath.Join(temporaryParent, "backup")
		if err := copyProfileArtifact(temporaryProfile, temporaryBackup); err != nil {
			return fmt.Errorf("prepare legacy profile backup: %w", err)
		}
		if err := durable.Directory(temporaryParent); err != nil {
			return fmt.Errorf("sync temporary profile migration directory: %w", err)
		}
		if err := os.Rename(temporaryBackup, backupRoot); err != nil {
			return fmt.Errorf("retain legacy profile state: %w", err)
		}
		if err := durable.Directory(filepath.Dir(backupRoot)); err != nil {
			return fmt.Errorf("sync legacy profile backup publication: %w", err)
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create profiles directory: %w", err)
	}
	if err := durable.Directory(temporaryParent); err != nil {
		return fmt.Errorf("sync temporary profile migration directory: %w", err)
	}
	if err := os.Rename(temporaryProfile, profile.StateDirectory); err != nil {
		return fmt.Errorf("publish migrated profile: %w", err)
	}
	if err := errors.Join(durable.Directory(root), durable.Directory(temporaryParent)); err != nil {
		return fmt.Errorf("sync migrated profile publication: %w", err)
	}
	// Load remains the compatibility seam for installation-level diagnostics.
	// Keep its configured directory checks valid without leaving any profile
	// facts there; profile-aware operations use LoadProfile and the directories
	// under profiles/<name>.
	if err := os.MkdirAll(configuration.Storage.BackupsPath, 0o700); err != nil {
		return fmt.Errorf("restore installation backup directory placeholder: %w", err)
	}
	return nil
}

func validateMigratedProfile(profileRoot string) error {
	info, err := os.Lstat(profileRoot)
	if err != nil {
		return fmt.Errorf("inspect migrated profile: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("migrated profile %q is not a directory", profileRoot)
	}
	registry := filepath.Join(profileRoot, "repositories.yaml")
	if _, err := LoadRepositories(registry); err != nil {
		return fmt.Errorf("validate migrated repository registry: %w", err)
	}
	currentPath := filepath.Join(profileRoot, "CURRENT")
	current, err := os.ReadFile(currentPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("validate migrated CURRENT: %w", err)
	}
	generation := string(bytes.TrimSpace(current))
	if generation == "" || strings.Trim(generation, "0123456789") != "" {
		return fmt.Errorf("validate migrated CURRENT: invalid generation %q", generation)
	}
	generationRoot := filepath.Join(profileRoot, "generations", generation)
	info, err = os.Lstat(generationRoot)
	if err != nil {
		return fmt.Errorf("validate migrated CURRENT generation %q: %w", generation, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("validate migrated CURRENT generation %q: not a directory", generation)
	}
	return nil
}

func inspectLegacyRuntime(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("inspect legacy runtime: %w", err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect legacy runtime %q: %w", entry.Name(), err)
		}
		if info.IsDir() || info.Mode().IsRegular() {
			continue
		}
		if entry.Name() != "daemon.sock" || info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("unexpected special legacy artifact %q", filepath.Join(root, entry.Name()))
		}
		connection, err := net.DialTimeout("unix", filepath.Join(root, entry.Name()), time.Second)
		if err == nil {
			_ = connection.Close()
			return errors.New("profile migration blocked by a running daemon; stop it and retry")
		}
		if !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check legacy daemon: %w", err)
		}
	}
	return nil
}

func copyProfileRegistry(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read repository registry %q: %w", source, err)
	}
	if _, err := writeInitialFile(destination, data, true); err != nil {
		return fmt.Errorf("copy repository registry to %q: %w", destination, err)
	}
	return nil
}

func copyProfileArtifact(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect legacy profile artifact %q: %w", source, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("legacy profile artifact %q is a symbolic link", source)
	}
	if info.IsDir() {
		if err := os.Mkdir(destination, info.Mode().Perm()); err != nil {
			return fmt.Errorf("copy legacy profile directory %q: %w", source, err)
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return fmt.Errorf("read legacy profile directory %q: %w", source, err)
		}
		for _, entry := range entries {
			if err := copyProfileArtifact(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
		}
		if err := durable.Directory(destination); err != nil {
			return fmt.Errorf("sync copied legacy profile directory %q: %w", destination, err)
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("legacy profile artifact %q is not a regular file", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("read legacy profile artifact %q: %w", source, err)
	}
	openedInfo, err := input.Stat()
	if err != nil {
		return errors.Join(fmt.Errorf("inspect opened legacy profile artifact %q: %w", source, err), input.Close())
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return errors.Join(fmt.Errorf("legacy profile artifact %q changed while it was opened", source), input.Close())
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return errors.Join(fmt.Errorf("create copied artifact %q: %w", destination, err), input.Close())
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	if err := errors.Join(copyErr, syncErr, output.Close(), input.Close()); err != nil {
		return fmt.Errorf("copy legacy profile artifact %q: %w", source, err)
	}
	return nil
}

func profileArtifactDigest(root string) ([32]byte, error) {
	digest := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected recovery artifact %q", path)
		}
		fmt.Fprintf(digest, "%q %d\n", relative, info.Mode())
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(digest, "%d\n", info.Size())
		_, err = io.Copy(digest, file)
		return errors.Join(err, file.Close())
	})
	var result [32]byte
	copy(result[:], digest.Sum(nil))
	return result, err
}

// ListProfiles returns profiles in canonical name order.
func ListProfiles(configPath string) ([]Profile, error) {
	configuration, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	// Loading an installation created before profiles existed is the upgrade
	// boundary. Copy its published state before listing so every ordinary
	// command migrates without requiring another init or a reindex.
	if err := ensureDefaultProfile(configuration, configuration.Workspace.RepositoriesFile); err != nil {
		return nil, err
	}
	return listProfiles(configuration)
}

func listProfiles(configuration Config) ([]Profile, error) {
	entries, err := os.ReadDir(profilesRoot(configuration))
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	profiles := make([]Profile, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || ValidateProfileName(entry.Name()) != nil {
			continue
		}
		profile := profileAt(configuration, entry.Name())
		if _, err := os.Stat(profile.RepositoriesPath); err != nil {
			continue
		}
		profiles = append(profiles, profile)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	if len(profiles) == 0 {
		return nil, errors.New("profiles: installation has no profiles")
	}
	for _, profile := range profiles {
		if profile.Default {
			return profiles, nil
		}
	}
	return nil, fmt.Errorf("profiles.default %q: %w", configuration.Profiles.Default, ErrProfileNotFound)
}

// LoadProfile loads one profile and rewrites only the state that belongs to
// its independently published graph. Analyzer targets, the event log and the
// content-addressed fact cache remain shared at installation scope.
func LoadProfile(configPath, name string) (Loaded, error) {
	return loadProfile(configPath, name, true)
}

// ReadProfile reads an already migrated profile without creating directories,
// acquiring write locks, or initiating a migration.
func ReadProfile(configPath, name string) (Loaded, error) {
	return loadProfile(configPath, name, false)
}

func loadProfile(configPath, name string, migrate bool) (Loaded, error) {
	loaded, err := Load(configPath)
	if err != nil {
		return Loaded{}, err
	}
	if name == "" {
		name = loaded.Config.Profiles.Default
	}
	if err := ValidateProfileName(name); err != nil {
		return Loaded{}, fmt.Errorf("profile name: %w", err)
	}
	profile := profileAt(loaded.Config, name)
	if migrate {
		if err := ensureDefaultProfile(loaded.Config, loaded.RepositoriesPath); err != nil {
			return Loaded{}, err
		}
	}
	profiles, err := listProfiles(loaded.Config)
	if err != nil {
		return Loaded{}, err
	}
	legacyRegistry := len(profiles) == 1 && name == loaded.Config.Profiles.Default
	repositoriesPath := profile.RepositoriesPath
	if legacyRegistry {
		repositoriesPath = loaded.RepositoriesPath
	}
	repositories, err := LoadRepositories(repositoriesPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Loaded{}, fmt.Errorf("profile %q: %w", name, ErrProfileNotFound)
		}
		return Loaded{}, fmt.Errorf("load profile %q: %w", name, err)
	}
	loaded.Profile = name
	loaded.Repositories = repositories
	loaded.RepositoriesPath = repositoriesPath
	loaded.TopologyPath = profile.TopologyPath
	sharedFactCache := filepath.Join(filepath.Dir(filepath.Dir(profile.StateDirectory)), "factcache")
	loaded.Config.Workspace.RepositoriesFile = repositoriesPath
	loaded.Config.Storage.DatabasePath = filepath.Join(profile.StateDirectory, "graph.lbdb")
	loaded.Config.Storage.BackupsPath = filepath.Join(profile.StateDirectory, "backups")
	loaded.Config.Indexing.FactCachePath = sharedFactCache
	loaded.Config.Go.SyntheticWorkFile = filepath.Join(profile.StateDirectory, "go.work")
	return loaded, nil
}

// CreateProfile creates an empty repository registry without indexing it.
func CreateProfile(configPath, name string) error {
	if err := ValidateProfileName(name); err != nil {
		return fmt.Errorf("profile name: %w", err)
	}
	configuration, err := LoadConfig(configPath)
	if err != nil {
		return err
	}
	profiles, err := ListProfiles(configPath)
	if err != nil {
		return err
	}
	// The legacy registry remains authoritative while one profile exists so
	// older callers stay byte-compatible. Creating the second profile is the
	// adoption boundary: capture its latest contents before profiles diverge.
	if len(profiles) == 1 {
		defaultProfile := profileAt(configuration, configuration.Profiles.Default)
		if err := copyProfileRegistry(configuration.Workspace.RepositoriesFile, defaultProfile.RepositoriesPath); err != nil {
			return fmt.Errorf("adopt default profile registry: %w", err)
		}
	}
	profile := profileAt(configuration, name)
	if _, err := os.Stat(profile.StateDirectory); err == nil {
		return fmt.Errorf("profile %q: %w", name, ErrProfileExists)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect profile %q: %w", name, err)
	}
	if err := os.MkdirAll(profile.StateDirectory, 0o700); err != nil {
		return fmt.Errorf("create profile %q: %w", name, err)
	}
	if err := SaveRepositories(profile.RepositoriesPath, RepositoriesFile{Version: CurrentSchemaVersion, Repositories: []Repository{}}); err != nil {
		_ = os.RemoveAll(profile.StateDirectory)
		return fmt.Errorf("create profile %q registry: %w", name, err)
	}
	return nil
}

// UseProfile moves the default pointer only after proving the target exists.
func UseProfile(configPath, name string) (err error) {
	if err := ValidateProfileName(name); err != nil {
		return fmt.Errorf("profile name: %w", err)
	}
	resolved, err := resolveConfigPath(configPath)
	if err != nil {
		return err
	}
	lock, err := acquireConfigLock(resolved)
	if err != nil {
		return err
	}
	defer releaseConfigLock(lock, &err)
	configuration, _, err := loadConfigFile(resolved)
	if err != nil {
		return err
	}
	if _, err := os.Stat(profileAt(configuration, name).RepositoriesPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("profile %q: %w", name, ErrProfileNotFound)
		}
		return fmt.Errorf("inspect profile %q: %w", name, err)
	}
	return writeDefaultProfileLocked(resolved, name)
}

// RemoveProfile removes one non-default profile and refuses to leave none.
func RemoveProfile(configPath, name string) error {
	if err := ValidateProfileName(name); err != nil {
		return fmt.Errorf("profile name: %w", err)
	}
	profiles, err := ListProfiles(configPath)
	if err != nil {
		return err
	}
	for _, profile := range profiles {
		if profile.Name != name {
			continue
		}
		if profile.Default {
			return fmt.Errorf("profile %q: %w; select another profile first", name, ErrDefaultProfile)
		}
		if len(profiles) == 1 {
			return errors.New("profile remove: refusing to leave the installation with zero profiles")
		}
		if err := os.RemoveAll(profile.StateDirectory); err != nil {
			return fmt.Errorf("remove profile %q: %w", name, err)
		}
		return nil
	}
	return fmt.Errorf("profile %q: %w", name, ErrProfileNotFound)
}

func writeDefaultProfileLocked(resolved, name string) error {
	data, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("read config %q: %w", resolved, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("decode config %q: %w", resolved, err)
	}
	document := &root
	if document.Kind == yaml.DocumentNode && len(document.Content) == 1 {
		document = document.Content[0]
	}
	profiles := mappingValue(document, "profiles")
	if profiles == nil {
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "profiles"}
		profiles = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		document.Content = append(document.Content, key, profiles)
	}
	value := mappingValue(profiles, "default")
	if value == nil {
		profiles.Content = append(profiles.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "default"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name})
	} else {
		value.Kind, value.Tag, value.Value = yaml.ScalarNode, "!!str", name
	}
	encoded, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("encode config %q: %w", resolved, err)
	}
	if _, err := writeInitialFile(resolved, encoded, true); err != nil {
		return fmt.Errorf("write config %q: %w", resolved, err)
	}
	return nil
}
