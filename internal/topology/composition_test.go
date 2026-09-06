package topology

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func compositionFixture() Topology {
	return Topology{
		Repositories: []LogicalRepository{
			{ID: LogicalRepositoryID("frontend"), Name: "Frontend"},
			{ID: LogicalRepositoryID("backend"), Name: "Backend"},
		},
		Worktrees: []Worktree{
			{ID: WorktreeID("frontend-main"), Repository: LogicalRepositoryID("frontend"), Path: "/work/frontend"},
			{ID: WorktreeID("backend-main"), Repository: LogicalRepositoryID("backend"), Path: "/work/backend"},
			{ID: WorktreeID("backend-maintenance"), Repository: LogicalRepositoryID("backend"), Path: "/work/backend-maintenance"},
		},
		Profiles: []Profile{
			{
				ID: ProfileID("feature-login"),
				Worktrees: []WorktreeSelection{
					{Repository: LogicalRepositoryID("frontend"), Worktree: WorktreeID("frontend-main")},
					{Repository: LogicalRepositoryID("backend"), Worktree: WorktreeID("backend-main")},
				},
			},
			{
				ID: ProfileID("maintenance"),
				Worktrees: []WorktreeSelection{
					{Repository: LogicalRepositoryID("backend"), Worktree: WorktreeID("backend-maintenance")},
				},
			},
		},
	}
}

func TestComposeRejectsUnknownProfile(t *testing.T) {
	_, err := compositionFixture().Compose(ProfileID("missing"))
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("Compose(missing) error = %v, want ErrProfileNotFound", err)
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Compose() error = %q, want the profile name", err)
	}
}

func TestComposeRejectsInvalidProfileID(t *testing.T) {
	_, err := compositionFixture().Compose(ProfileID("feature/login"))
	if !errors.Is(err, ErrInvalidID) {
		t.Fatalf("Compose() error = %v, want ErrInvalidID", err)
	}
}

func TestComposeRejectsInvalidTopologyBeforeSelecting(t *testing.T) {
	topology := compositionFixture()
	topology.Profiles[0].Worktrees = append(topology.Profiles[0].Worktrees,
		WorktreeSelection{Repository: LogicalRepositoryID("backend"), Worktree: WorktreeID("backend-maintenance")})
	_, err := topology.Compose(ProfileID("feature-login"))
	if !errors.Is(err, ErrInvalidTopology) {
		t.Fatalf("Compose() error = %v, want ErrInvalidTopology", err)
	}
	if !strings.Contains(err.Error(), "conflicting worktrees") {
		t.Fatalf("Compose() error = %q, want the conflicting-worktrees reason", err)
	}
}

func TestComposePreservesInitializedEmptyWorktrees(t *testing.T) {
	topology := Topology{
		Profiles: []Profile{{
			ID:        ProfileID("empty"),
			Worktrees: []WorktreeSelection{},
		}},
	}
	composition, err := topology.Compose(ProfileID("empty"))
	if err != nil {
		t.Fatalf("Compose(empty) error = %v", err)
	}
	if len(composition.Profile.Worktrees) != 0 {
		t.Fatalf("Compose(empty) profile worktrees = %#v, want an empty slice", composition.Profile.Worktrees)
	}
}

func TestComposeSelectsMultipleRepositoriesInDeclarationOrder(t *testing.T) {
	topology := compositionFixture()
	got, err := topology.Compose(ProfileID("feature-login"))
	if err != nil {
		t.Fatalf("Compose(feature-login) error = %v", err)
	}
	want := ProfileComposition{
		Profile: topology.Profiles[0],
		Repositories: []LogicalRepository{
			topology.Repositories[0],
			topology.Repositories[1],
		},
		Worktrees: []Worktree{
			topology.Worktrees[0],
			topology.Worktrees[1],
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("composition = %#v, want %#v", got, want)
	}
}

func TestComposePreservesWorktreeOverlayWithoutChangingRepositoryIdentity(t *testing.T) {
	topology := compositionFixture()
	topology.Profiles[0].Worktrees[1] = WorktreeSelection{
		Repository: "backend", Worktree: "backend-maintenance", Overlays: "backend-main",
	}
	topology.Profiles[1].Worktrees[0] = WorktreeSelection{
		Repository: "backend", Worktree: "backend-main",
	}

	composition, err := topology.Compose("feature-login")
	if err != nil {
		t.Fatalf("Compose(feature-login) error = %v", err)
	}
	if got := composition.Repositories[1].ID; got != "backend" {
		t.Fatalf("composed logical repository = %q, want backend", got)
	}
	if got := composition.Worktrees[1].ID; got != "backend-maintenance" {
		t.Fatalf("composed selected worktree = %q, want backend-maintenance", got)
	}
	if got := composition.Profile.Worktrees[1].Overlays; got != "backend-main" {
		t.Fatalf("composed overlay target = %q, want backend-main", got)
	}
	if want := []Worktree{topology.Worktrees[1]}; !reflect.DeepEqual(composition.OverlayWorktrees, want) {
		t.Fatalf("composed overlay worktrees = %#v, want %#v", composition.OverlayWorktrees, want)
	}
}

func TestComposeKeepsProfilesIsolated(t *testing.T) {
	topology := compositionFixture()
	feature, err := topology.Compose(ProfileID("feature-login"))
	if err != nil {
		t.Fatalf("Compose(feature-login) error = %v", err)
	}
	maintenance, err := topology.Compose(ProfileID("maintenance"))
	if err != nil {
		t.Fatalf("Compose(maintenance) error = %v", err)
	}
	if len(feature.Worktrees) != 2 || len(maintenance.Worktrees) != 1 {
		t.Fatalf("composed worktrees = %v and %v, want 2 and 1", feature.Worktrees, maintenance.Worktrees)
	}
	if feature.Worktrees[1].ID != WorktreeID("backend-main") {
		t.Fatalf("feature backend = %q, want backend-main", feature.Worktrees[1].ID)
	}
	if maintenance.Worktrees[0].ID != WorktreeID("backend-maintenance") {
		t.Fatalf("maintenance backend = %q, want backend-maintenance", maintenance.Worktrees[0].ID)
	}
}
