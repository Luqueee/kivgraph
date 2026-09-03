package webapi

import (
	"errors"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/invalidation"
	"github.com/Luqueee/kivgraph/internal/sourceobservation"
	"github.com/Luqueee/kivgraph/internal/topology"
)

var (
	errTopologyInvalidArgument   = errors.New("invalid topology argument")
	errTopologyUnavailable       = errors.New("topology is unavailable")
	errTopologyGenerationChange  = errors.New("topology generation changed")
	errTopologyAmbiguous         = errors.New("topology is ambiguous")
	errTopologyRelationshipsFull = errors.New("topology relationship limit reached")
)

const maxTopologyRelationships = 10_000

// TopologyOptions supplies the installation metadata needed by the topology
// endpoint. The ordinary viewer endpoints do not need it and keep their
// existing constructor and response contracts.
type TopologyOptions struct {
	ConfigPath       string
	Profile          string
	InvalidationRoot string
}

type topologyNodeView struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type topologyResponse struct {
	APIVersion       string                     `json:"api_version"`
	TopologyVersion  int                        `json:"topology_version"`
	Status           string                     `json:"status"`
	GenerationID     string                     `json:"generation_id,omitempty"`
	SelectedProfiles []string                   `json:"selected_profiles"`
	Profiles         []topologyProfileView      `json:"profiles"`
	Repositories     []topologyRepositoryView   `json:"repositories"`
	Worktrees        []topologyWorktreeView     `json:"worktrees"`
	Sources          []topologySourceView       `json:"sources"`
	SharedInputs     []topologySharedInputView  `json:"shared_inputs"`
	Relationships    []topologyRelationshipView `json:"relationships"`
	Completeness     topologyCompletenessView   `json:"completeness"`
}

type topologyProfileView struct {
	ID           string   `json:"id"`
	GenerationID string   `json:"generation_id"`
	Status       string   `json:"status"`
	Reason       string   `json:"reason,omitempty"`
	Worktrees    []string `json:"worktrees"`
}

type topologyRepositoryView struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type topologyWorktreeView struct {
	ID         string           `json:"id"`
	Repository string           `json:"repository"`
	Path       string           `json:"path"`
	Git        *topologyGitView `json:"git,omitempty"`
}

type topologyGitView struct {
	GitDirectory    string `json:"git_directory,omitempty"`
	CommonDirectory string `json:"common_directory,omitempty"`
}

type topologySourceView struct {
	Profile    string                   `json:"profile"`
	Repository string                   `json:"repository"`
	Worktree   string                   `json:"worktree"`
	Status     string                   `json:"status"`
	Reason     string                   `json:"reason,omitempty"`
	Indexed    *topologyObservationView `json:"indexed,omitempty"`
	Current    *topologyObservationView `json:"current,omitempty"`
}

type topologyObservationView struct {
	ID            string `json:"id"`
	Worktree      string `json:"worktree"`
	Commit        string `json:"commit"`
	Branch        string `json:"branch,omitempty"`
	Dirty         bool   `json:"dirty"`
	ContentDigest string `json:"content_digest"`
}

type topologySharedInputView struct {
	Type   string   `json:"type"`
	ID     string   `json:"id"`
	Owners []string `json:"owners"`
}

type topologyRelationshipView struct {
	Profile    string            `json:"profile,omitempty"`
	Type       string            `json:"type"`
	Source     topologyNodeView  `json:"source"`
	Target     *topologyNodeView `json:"target,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Status     string            `json:"status"`
	Confidence string            `json:"confidence"`
	Provenance string            `json:"provenance"`
	Evidence   string            `json:"evidence,omitempty"`
	Reason     string            `json:"reason,omitempty"`
}

type topologyCompletenessView struct {
	Complete  bool   `json:"complete"`
	Truncated bool   `json:"truncated"`
	Reason    string `json:"reason,omitempty"`
}

type topologyQuery struct {
	Profiles       []string
	GenerationID   string
	GenerationPins map[string]string
}

type topologyStoreSelection struct {
	Name  string
	Store *hotsnapshot.SnapshotStore
}

type topologyProfileData struct {
	Name         string
	GenerationID string
	Generation   uint64
	Snapshot     *hotsnapshot.GraphSnapshot
	Composition  topology.ProfileComposition
	Manifest     sourceobservation.Manifest
	ManifestOK   bool
	State        *invalidation.ProfileState
}

type topologyRelationshipCacheEntry struct {
	GenerationID    string
	Relationships   []topologyRelationshipView
	Truncated       bool
	TruncatedReason string
}

type topologyAssembler struct {
	repositories         map[topology.LogicalRepositoryID]topology.LogicalRepository
	declaredRepositories map[topology.LogicalRepositoryID]struct{}
	worktrees            map[topology.WorktreeID]topology.Worktree
	profiles             []topologyProfileView
	sources              []topologySourceView
	shared               map[topology.WorktreeID]map[string]struct{}
	relationships        []topologyRelationshipView
	relationshipKeys     map[string]struct{}
	truncated            bool
	truncatedReason      string
}
