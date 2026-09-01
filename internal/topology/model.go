// Package topology defines the identities and configuration that connect
// mutable source instances to independently published resolution profiles.
//
// It deliberately has no filesystem or Git side effects. A later indexing
// phase can observe a Worktree and produce a SourceObservation, while this
// package keeps the identity rules independent from how that observation was
// collected.
package topology

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maximumOpaqueIDBytes    = 256
	maximumProfileIDBytes   = 64
	sourceObservationPrefix = "obs-"
)

var (
	ErrInvalidID                = errors.New("invalid topology identity")
	ErrInvalidSourceObservation = errors.New("invalid source observation")
	ErrInvalidTopology          = errors.New("invalid topology")
	ErrProfileNotFound          = errors.New("profile does not exist in topology")
)

// LogicalRepositoryID identifies a source repository independently of any
// checkout path. The same ID may be represented by several Worktrees.
type LogicalRepositoryID string

// WorktreeID identifies one mutable checkout. It remains stable when the
// checkout path, branch or commit changes; those are observed state instead.
type WorktreeID string

// SourceObservationID identifies the state of one worktree that a pass
// actually observed. It is derived from the worktree ID and observed content,
// never from its path.
type SourceObservationID string

// ProfileID identifies a resolution universe. It is also the name used by the
// existing profile state layout, so it follows the same path-element rules.
type ProfileID string

// GenerationID identifies one published generation of a profile. The current
// generation store uses six decimal digits; keeping that representation here
// makes the domain type explicit without changing its persistent contract.
type GenerationID string

// LogicalRepository is the stable source identity and optional display
// metadata. Name may change without changing ID.
type LogicalRepository struct {
	ID   LogicalRepositoryID `yaml:"id"`
	Name string              `yaml:"name,omitempty"`
}

// GitLayout records where a worktree's Git metadata was found. These paths are
// discovery metadata and are never used to derive WorktreeID.
type GitLayout struct {
	GitDirectory    string `yaml:"git_directory,omitempty"`
	CommonDirectory string `yaml:"common_directory,omitempty"`
}

// Worktree is one mutable source instance represented by a logical repository.
// Path and Git are replaceable discovery metadata, not identity fields.
type Worktree struct {
	ID         WorktreeID          `yaml:"id"`
	Repository LogicalRepositoryID `yaml:"repository"`
	Path       string              `yaml:"path"`
	Git        GitLayout           `yaml:"git,omitempty"`
}

// SourceObservation is the source state captured for one analysis. Content
// digest covers the bytes the pass analysed, including dirty content.
type SourceObservation struct {
	ID            SourceObservationID `yaml:"id"`
	Worktree      WorktreeID          `yaml:"worktree"`
	Commit        string              `yaml:"commit"`
	Branch        string              `yaml:"branch,omitempty"`
	Dirty         bool                `yaml:"dirty"`
	ContentDigest string              `yaml:"content_digest"`
}

// PublishedGeneration identifies one immutable publication belonging to one
// profile. A newer source observation produces a new generation; it never
// mutates one that a reader may already be serving.
type PublishedGeneration struct {
	ID      GenerationID `yaml:"id"`
	Profile ProfileID    `yaml:"profile"`
}

// WorktreeSelection is an explicit ownership entry in a profile. Repository
// is repeated intentionally: it makes the selected provider visible in the
// configuration and lets validation reject a mismatched worktree.
type WorktreeSelection struct {
	Repository LogicalRepositoryID `yaml:"repository"`
	Worktree   WorktreeID          `yaml:"worktree"`
}

// Profile is an effective dependency-resolution universe. It can select
// worktrees from several logical repositories. A profile must not select two
// worktrees representing the same logical repository.
type Profile struct {
	ID        ProfileID           `yaml:"id"`
	Worktrees []WorktreeSelection `yaml:"worktrees,omitempty"`
}

// ProfileComposition is the effective registry selected by one profile. The
// order of Worktrees and Repositories follows the profile declaration so
// diagnostics can explain exactly which inputs formed the resolution universe.
// It contains membership only; source-backed dependency edges are produced by
// language providers after this composition is selected.
type ProfileComposition struct {
	Profile      Profile
	Repositories []LogicalRepository
	Worktrees    []Worktree
}

// Topology is the declarative model used to validate profile composition. It
// is intentionally separate from the published graph: changing a path or
// source state changes what a future pass observes, not an already published
// GenerationID.
type Topology struct {
	Repositories []LogicalRepository `yaml:"repositories,omitempty"`
	Worktrees    []Worktree          `yaml:"worktrees,omitempty"`
	Profiles     []Profile           `yaml:"profiles,omitempty"`
}

// NewLogicalRepositoryID validates a logical repository identity. It accepts
// repository coordinates such as github.com/acme/service, but rejects an
// absolute path so a path cannot accidentally become the identity.
func NewLogicalRepositoryID(value string) (LogicalRepositoryID, error) {
	if err := validateOpaqueID(value, "logical repository", true); err != nil {
		return "", err
	}
	return LogicalRepositoryID(value), nil
}

// NewWorktreeID validates a path-independent worktree identity.
func NewWorktreeID(value string) (WorktreeID, error) {
	if err := validateOpaqueID(value, "worktree", false); err != nil {
		return "", err
	}
	return WorktreeID(value), nil
}

// NewProfileID validates a profile name that can safely be used as one state
// directory element. The all-profiles selector is intentionally reserved.
func NewProfileID(value string) (ProfileID, error) {
	if value == "" {
		return "", fmt.Errorf("profile: %w: must not be empty", ErrInvalidID)
	}
	if len(value) > maximumProfileIDBytes {
		return "", fmt.Errorf("profile %q: %w: must be at most %d bytes, got %d", value, ErrInvalidID, maximumProfileIDBytes, len(value))
	}
	if value == "." || value == ".." || value == "*" {
		return "", fmt.Errorf("profile %q: %w: reserved name", value, ErrInvalidID)
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '-' || character == '_' || character == '.' {
			continue
		}
		return "", fmt.Errorf("profile %q: %w: must contain only ASCII letters, digits, '.', '-' or '_', got %q", value, ErrInvalidID, value)
	}
	return ProfileID(value), nil
}

// NewGenerationID validates the six-digit generation identity used by the
// persistent generation store.
func NewGenerationID(value string) (GenerationID, error) {
	if len(value) != 6 {
		return "", fmt.Errorf("generation %q: %w: must contain six decimal digits", value, ErrInvalidID)
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return "", fmt.Errorf("generation %q: %w: must contain six decimal digits", value, ErrInvalidID)
		}
	}
	return GenerationID(value), nil
}

// NewSourceObservation creates an observation whose ID is deterministic for
// the observed source state. A repeated observation of the same bytes gets the
// same ID, even if the worktree path changes.
func NewSourceObservation(worktree WorktreeID, commit, branch string, dirty bool, contentDigest string) (SourceObservation, error) {
	if err := validateWorktreeID(worktree); err != nil {
		return SourceObservation{}, fmt.Errorf("%w: %w", ErrInvalidSourceObservation, err)
	}
	commit = strings.TrimSpace(commit)
	if commit == "" || strings.IndexFunc(commit, unicode.IsSpace) >= 0 {
		return SourceObservation{}, fmt.Errorf("commit: %w: must be a non-empty token", ErrInvalidSourceObservation)
	}
	if strings.IndexFunc(branch, func(character rune) bool {
		return unicode.IsControl(character) || unicode.IsSpace(character)
	}) >= 0 {
		return SourceObservation{}, fmt.Errorf("branch: %w: must not contain whitespace or control characters", ErrInvalidSourceObservation)
	}
	contentDigest = strings.ToLower(strings.TrimSpace(contentDigest))
	if len(contentDigest) != sha256.Size*2 {
		return SourceObservation{}, fmt.Errorf("content digest: %w: want a SHA-256 hex digest", ErrInvalidSourceObservation)
	}
	if _, err := hex.DecodeString(contentDigest); err != nil {
		return SourceObservation{}, fmt.Errorf("content digest: %w: want a SHA-256 hex digest: %w", ErrInvalidSourceObservation, err)
	}
	id := sourceObservationID(worktree, commit, branch, dirty, contentDigest)
	return SourceObservation{
		ID:            id,
		Worktree:      worktree,
		Commit:        commit,
		Branch:        branch,
		Dirty:         dirty,
		ContentDigest: contentDigest,
	}, nil
}

// Validate checks that an observation ID agrees with the state it names.
func (observation SourceObservation) Validate() error {
	expected, err := NewSourceObservation(observation.Worktree, observation.Commit, observation.Branch, observation.Dirty, observation.ContentDigest)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSourceObservation, err)
	}
	if observation.ID != expected.ID {
		return fmt.Errorf("source observation ID %q: %w: does not match observed state", observation.ID, ErrInvalidSourceObservation)
	}
	return nil
}

// NewPublishedGeneration creates a generation identity tied to its profile.
func NewPublishedGeneration(profile ProfileID, generation GenerationID) (PublishedGeneration, error) {
	if err := validateProfileID(profile); err != nil {
		return PublishedGeneration{}, err
	}
	if err := validateGenerationID(generation); err != nil {
		return PublishedGeneration{}, err
	}
	return PublishedGeneration{ID: generation, Profile: profile}, nil
}

// Validate checks that a published generation still has valid identities.
func (generation PublishedGeneration) Validate() error {
	_, err := NewPublishedGeneration(generation.Profile, generation.ID)
	return err
}

func sourceObservationID(worktree WorktreeID, commit, branch string, dirty bool, contentDigest string) SourceObservationID {
	hash := sha256.New()
	writeObservationField := func(value string) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}
	writeObservationField(string(worktree))
	writeObservationField(commit)
	writeObservationField(branch)
	if dirty {
		writeObservationField("dirty")
	} else {
		writeObservationField("clean")
	}
	writeObservationField(contentDigest)
	return SourceObservationID(sourceObservationPrefix + hex.EncodeToString(hash.Sum(nil)))
}

func validateOpaqueID(value, kind string, allowSlash bool) error {
	if value == "" {
		return fmt.Errorf("%s: %w: must not be empty", kind, ErrInvalidID)
	}
	if len(value) > maximumOpaqueIDBytes {
		return fmt.Errorf("%s %q: %w: must be at most %d bytes, got %d", kind, value, ErrInvalidID, maximumOpaqueIDBytes, len(value))
	}
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s %q: %w: must be valid UTF-8 without surrounding whitespace", kind, value, ErrInvalidID)
	}
	isDriveQualifiedAbsolutePath := len(value) >= 3 &&
		((value[0] >= 'a' && value[0] <= 'z') ||
			(value[0] >= 'A' && value[0] <= 'Z')) &&
		value[1] == ':' && (value[2] == '/' || value[2] == '\\')
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, `\`) || isDriveQualifiedAbsolutePath {
		return fmt.Errorf("%s %q: %w: must not be an absolute path", kind, value, ErrInvalidID)
	}
	if !allowSlash && strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%s %q: %w: must not contain path separators", kind, value, ErrInvalidID)
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return fmt.Errorf("%s %q: %w: must not contain whitespace or control characters", kind, value, ErrInvalidID)
		}
	}
	return nil
}

func validateLogicalRepositoryID(id LogicalRepositoryID) error {
	_, err := NewLogicalRepositoryID(string(id))
	return err
}

func validateWorktreeID(id WorktreeID) error {
	_, err := NewWorktreeID(string(id))
	return err
}

func validateProfileID(id ProfileID) error {
	_, err := NewProfileID(string(id))
	return err
}

func validateGenerationID(id GenerationID) error {
	_, err := NewGenerationID(string(id))
	return err
}

// Compose selects one profile's effective repository and worktree set. It
// validates the complete topology first, so a malformed or conflicting
// declaration cannot be narrowed into an apparently valid composition.
func (topology Topology) Compose(profileID ProfileID) (ProfileComposition, error) {
	if err := validateProfileID(profileID); err != nil {
		return ProfileComposition{}, fmt.Errorf("profile id: %w", err)
	}
	if err := topology.Validate(); err != nil {
		return ProfileComposition{}, err
	}

	repositories := make(map[LogicalRepositoryID]LogicalRepository, len(topology.Repositories))
	for _, repository := range topology.Repositories {
		repositories[repository.ID] = repository
	}
	worktrees := make(map[WorktreeID]Worktree, len(topology.Worktrees))
	for _, worktree := range topology.Worktrees {
		worktrees[worktree.ID] = worktree
	}
	for _, profile := range topology.Profiles {
		if profile.ID != profileID {
			continue
		}
		composition := ProfileComposition{
			Profile:      cloneProfile(profile),
			Repositories: make([]LogicalRepository, 0, len(profile.Worktrees)),
			Worktrees:    make([]Worktree, 0, len(profile.Worktrees)),
		}
		for _, selection := range profile.Worktrees {
			worktree := worktrees[selection.Worktree]
			composition.Worktrees = append(composition.Worktrees, worktree)
			composition.Repositories = append(composition.Repositories, repositories[selection.Repository])
		}
		return composition, nil
	}
	return ProfileComposition{}, fmt.Errorf("profile %q: %w", profileID, ErrProfileNotFound)
}

func cloneProfile(profile Profile) Profile {
	profile.Worktrees = append([]WorktreeSelection(nil), profile.Worktrees...)
	return profile
}

// Validate rejects ambiguous or incomplete topology rather than choosing a
// provider implicitly. The same worktree may be referenced by several
// profiles; ownership is unique within each effective profile.
func (topology Topology) Validate() (err error) {
	defer func() {
		if err != nil && !errors.Is(err, ErrInvalidTopology) {
			err = fmt.Errorf("%w: %w", ErrInvalidTopology, err)
		}
	}()

	repositories := make(map[LogicalRepositoryID]int, len(topology.Repositories))
	for index, repository := range topology.Repositories {
		if err := validateLogicalRepositoryID(repository.ID); err != nil {
			return fmt.Errorf("repositories[%d].id: %w", index, err)
		}
		if previous, exists := repositories[repository.ID]; exists {
			return fmt.Errorf("repositories[%d].id: %w: duplicate of repositories[%d]", index, ErrInvalidTopology, previous)
		}
		repositories[repository.ID] = index
	}

	worktrees := make(map[WorktreeID]Worktree, len(topology.Worktrees))
	for index, worktree := range topology.Worktrees {
		if err := validateWorktreeID(worktree.ID); err != nil {
			return fmt.Errorf("worktrees[%d].id: %w", index, err)
		}
		if _, exists := worktrees[worktree.ID]; exists {
			return fmt.Errorf("worktrees[%d].id: %w: duplicate worktree %q", index, ErrInvalidTopology, worktree.ID)
		}
		if err := validateLogicalRepositoryID(worktree.Repository); err != nil {
			return fmt.Errorf("worktrees[%d].repository: %w", index, err)
		}
		if _, exists := repositories[worktree.Repository]; !exists {
			return fmt.Errorf("worktrees[%d].repository %q: %w: logical repository is not declared", index, worktree.Repository, ErrInvalidTopology)
		}
		if strings.TrimSpace(worktree.Path) == "" {
			return fmt.Errorf("worktrees[%d].path: %w: must not be empty", index, ErrInvalidTopology)
		}
		worktrees[worktree.ID] = worktree
	}

	profiles := make(map[ProfileID]int, len(topology.Profiles))
	for index, profile := range topology.Profiles {
		if err := validateProfileID(profile.ID); err != nil {
			return fmt.Errorf("profiles[%d].id: %w", index, err)
		}
		if previous, exists := profiles[profile.ID]; exists {
			return fmt.Errorf("profiles[%d].id: %w: duplicate of profiles[%d]", index, ErrInvalidTopology, previous)
		}
		profiles[profile.ID] = index
		if err := validateProfile(profile, worktrees); err != nil {
			return fmt.Errorf("profiles[%d] %q: %w", index, profile.ID, err)
		}
	}
	return nil
}

func validateProfile(profile Profile, worktrees map[WorktreeID]Worktree) error {
	ownedRepositories := make(map[LogicalRepositoryID]int, len(profile.Worktrees))
	for index, selection := range profile.Worktrees {
		worktree, exists := worktrees[selection.Worktree]
		if !exists {
			return fmt.Errorf("worktrees[%d].worktree %q: %w: worktree is not declared", index, selection.Worktree, ErrInvalidTopology)
		}
		if worktree.Repository != selection.Repository {
			return fmt.Errorf("worktrees[%d]: %w: selection says repository %q but worktree %q belongs to %q", index, ErrInvalidTopology, selection.Repository, selection.Worktree, worktree.Repository)
		}
		if previous, exists := ownedRepositories[selection.Repository]; exists {
			if profile.Worktrees[previous] == selection {
				return fmt.Errorf("worktrees[%d].repository %q: %w: duplicate ownership of worktree %q", index, selection.Repository, ErrInvalidTopology, selection.Worktree)
			}
			return fmt.Errorf("worktrees[%d].repository %q: %w: conflicting worktrees; use separate profiles", index, selection.Repository, ErrInvalidTopology)
		}
		ownedRepositories[selection.Repository] = index
	}
	return nil
}
