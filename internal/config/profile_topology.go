package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Luqueee/kivgraph/internal/topology"
	"gopkg.in/yaml.v3"
)

// LoadProfileTopology loads the optional topology document for one profile.
// The absent document preserves the legacy repository-only profile behaviour.
func LoadProfileTopology(configPath, name string) (topology.Topology, bool, error) {
	loaded, err := LoadProfile(configPath, name)
	if err != nil {
		return topology.Topology{}, false, err
	}
	return loadProfileTopologyFile(loaded.TopologyPath, topology.ProfileID(loaded.Profile))
}

// SaveProfileTopology validates and atomically writes one profile topology.
// Paths may be relative to the topology file and are expanded when loaded.
func SaveProfileTopology(configPath, name string, value topology.Topology) error {
	loaded, err := LoadProfile(configPath, name)
	if err != nil {
		return err
	}
	profile := topology.ProfileID(loaded.Profile)
	if err := validateProfileTopology(value, profile); err != nil {
		return err
	}
	// A write is an explicit upgrade point. Reading keeps legacy documents
	// valid; persisting one through this API records the current vocabulary.
	value.Version = topology.CurrentSchemaVersion
	data, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode profile topology: %w", err)
	}
	if _, err := writeInitialFile(loaded.TopologyPath, data, true); err != nil {
		return fmt.Errorf("write profile topology %q: %w", loaded.TopologyPath, err)
	}
	return nil
}

func loadProfileTopologyFile(path string, profile topology.ProfileID) (topology.Topology, bool, error) {
	var value topology.Topology
	versionPresent, err := decodeYAML(path, &value)
	if errors.Is(err, os.ErrNotExist) {
		return topology.Topology{}, false, nil
	}
	if err != nil {
		return topology.Topology{}, false, fmt.Errorf("load profile topology %q: %w", path, err)
	}
	if !versionPresent {
		return topology.Topology{}, false, errors.New("topology.version: field is required")
	}
	if !topology.SupportedSchemaVersion(value.Version) {
		return topology.Topology{}, false, fmt.Errorf("topology.version: unsupported schema version %d, want %d or %d", value.Version, topology.LegacySchemaVersion, topology.CurrentSchemaVersion)
	}
	for index := range value.Worktrees {
		expanded, err := expandPath(value.Worktrees[index].Path, filepath.Dir(path))
		if err != nil {
			return topology.Topology{}, false, fmt.Errorf("topology.worktrees[%d].path: %w", index, err)
		}
		value.Worktrees[index].Path = expanded
	}
	if err := validateProfileTopology(value, profile); err != nil {
		return topology.Topology{}, false, err
	}
	return value, true, nil
}

func validateProfileTopology(value topology.Topology, profile topology.ProfileID) error {
	if !topology.SupportedSchemaVersion(value.Version) {
		return fmt.Errorf("topology.version: unsupported schema version %d, want %d or %d", value.Version, topology.LegacySchemaVersion, topology.CurrentSchemaVersion)
	}
	if value.Version == topology.LegacySchemaVersion {
		for profileIndex, configuredProfile := range value.Profiles {
			for selectionIndex, selection := range configuredProfile.Worktrees {
				if selection.Overlays != "" {
					return fmt.Errorf("topology.profiles[%d].worktrees[%d].overlays: topology version %d does not support worktree overlays", profileIndex, selectionIndex, topology.LegacySchemaVersion)
				}
			}
		}
	}
	if err := value.Validate(); err != nil {
		return fmt.Errorf("validate profile topology: %w", err)
	}
	if _, err := value.Compose(profile); err != nil {
		return fmt.Errorf("validate profile topology selection: %w", err)
	}
	return nil
}
