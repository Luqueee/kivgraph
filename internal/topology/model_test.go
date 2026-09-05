package topology

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIdentityConstructorsRejectMalformedValues(t *testing.T) {
	tests := []struct {
		name string
		make func() error
	}{
		{name: "empty logical repository", make: func() error {
			_, err := NewLogicalRepositoryID("")
			return err
		}},
		{name: "oversized logical repository", make: func() error {
			_, err := NewLogicalRepositoryID(strings.Repeat("a", maximumOpaqueIDBytes+1))
			return err
		}},
		{name: "invalid UTF-8 logical repository", make: func() error {
			_, err := NewLogicalRepositoryID(string([]byte{0xff}))
			return err
		}},
		{name: "surrounding whitespace logical repository", make: func() error {
			_, err := NewLogicalRepositoryID(" service")
			return err
		}},
		{name: "absolute logical repository", make: func() error {
			_, err := NewLogicalRepositoryID("/service")
			return err
		}},
		{name: "backslash absolute logical repository", make: func() error {
			_, err := NewLogicalRepositoryID(`\service`)
			return err
		}},
		{name: "drive-qualified absolute logical repository", make: func() error {
			_, err := NewLogicalRepositoryID(`C:\service`)
			return err
		}},
		{name: "path worktree", make: func() error {
			_, err := NewWorktreeID("service/main")
			return err
		}},
		{name: "backslash path worktree", make: func() error {
			_, err := NewWorktreeID(`service\main`)
			return err
		}},
		{name: "control character worktree", make: func() error {
			_, err := NewWorktreeID("service\nmain")
			return err
		}},
		{name: "empty profile", make: func() error {
			_, err := NewProfileID("")
			return err
		}},
		{name: "oversized profile", make: func() error {
			_, err := NewProfileID(strings.Repeat("a", maximumProfileIDBytes+1))
			return err
		}},
		{name: "reserved profile", make: func() error {
			_, err := NewProfileID("*")
			return err
		}},
		{name: "invalid profile character", make: func() error {
			_, err := NewProfileID("feature/login")
			return err
		}},
		{name: "short generation", make: func() error {
			_, err := NewGenerationID("42")
			return err
		}},
		{name: "non-decimal generation", make: func() error {
			_, err := NewGenerationID("00000x")
			return err
		}},
		{name: "invalid observation worktree", make: func() error {
			_, err := NewSourceObservation(WorktreeID("/service"), "commit", "main", false, strings.Repeat("a", 64))
			return err
		}},
		{name: "empty observation commit", make: func() error {
			_, err := NewSourceObservation(WorktreeID("service"), "", "main", false, strings.Repeat("a", 64))
			return err
		}},
		{name: "whitespace observation commit", make: func() error {
			_, err := NewSourceObservation(WorktreeID("service"), "commit hash", "main", false, strings.Repeat("a", 64))
			return err
		}},
		{name: "control observation branch", make: func() error {
			_, err := NewSourceObservation(WorktreeID("service"), "commit", "main\x00", false, strings.Repeat("a", 64))
			return err
		}},
		{name: "whitespace observation branch", make: func() error {
			_, err := NewSourceObservation(WorktreeID("service"), "commit", "feature branch", false, strings.Repeat("a", 64))
			return err
		}},
		{name: "short observation digest", make: func() error {
			_, err := NewSourceObservation(WorktreeID("service"), "commit", "main", false, "short")
			return err
		}},
		{name: "non-hex observation digest", make: func() error {
			_, err := NewSourceObservation(WorktreeID("service"), "commit", "main", false, strings.Repeat("z", 64))
			return err
		}},
		{name: "invalid published profile", make: func() error {
			_, err := NewPublishedGeneration(ProfileID("/profile"), GenerationID("000001"))
			return err
		}},
		{name: "invalid published generation", make: func() error {
			_, err := NewPublishedGeneration(ProfileID("feature"), GenerationID("current"))
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.make()
			if err == nil {
				t.Fatalf("%s: constructor error = nil, want rejection", test.name)
			}
			if !errors.Is(err, ErrInvalidID) && !errors.Is(err, ErrInvalidSourceObservation) {
				t.Fatalf("%s: constructor error = %v, want a classified identity error", test.name, err)
			}
		})
	}
}

func TestIdentityConstructorsAcceptStableValues(t *testing.T) {
	if _, err := NewLogicalRepositoryID("github.com/acme/service"); err != nil {
		t.Fatalf("NewLogicalRepositoryID(%q): %v", "github.com/acme/service", err)
	}
	if _, err := NewWorktreeID("service-main"); err != nil {
		t.Fatalf("NewWorktreeID(%q): %v", "service-main", err)
	}
	if _, err := NewProfileID("Feature_login-09."); err != nil {
		t.Fatalf("NewProfileID(%q): %v", "Feature_login-09.", err)
	}
	if _, err := NewGenerationID("000042"); err != nil {
		t.Fatalf("NewGenerationID(%q): %v", "000042", err)
	}
	generation, err := NewPublishedGeneration(ProfileID("feature"), GenerationID("000042"))
	if err != nil {
		t.Fatalf("NewPublishedGeneration(%q, %q): %v", "feature", "000042", err)
	}
	if err := generation.Validate(); err != nil {
		t.Fatalf("PublishedGeneration.Validate(%#v): %v", generation, err)
	}
	if _, err := NewSourceObservation(WorktreeID("service"), "commit", "main", false, strings.Repeat("A", 64)); err != nil {
		t.Fatalf("NewSourceObservation(%q, %q, %q, %t, %q): %v", "service", "commit", "main", false, strings.Repeat("A", 64), err)
	}
}

func TestTopologyRejectsAmbiguousOrIncompleteDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Topology)
		want   string
	}{
		{name: "invalid repository identity", mutate: func(value *Topology) {
			value.Repositories[0].ID = ""
		}, want: "repositories[0].id"},
		{name: "duplicate repository", mutate: func(value *Topology) {
			value.Repositories = append(value.Repositories, value.Repositories[0])
		}, want: "duplicate"},
		{name: "invalid worktree identity", mutate: func(value *Topology) {
			value.Worktrees[0].ID = "bad/path"
		}, want: "worktrees[0].id"},
		{name: "duplicate worktree", mutate: func(value *Topology) {
			value.Worktrees = append(value.Worktrees, value.Worktrees[0])
		}, want: "duplicate worktree"},
		{name: "invalid worktree repository", mutate: func(value *Topology) {
			value.Worktrees[0].Repository = ""
		}, want: "worktrees[0].repository"},
		{name: "unknown repository", mutate: func(value *Topology) {
			value.Worktrees[0].Repository = "missing"
		}, want: "not declared"},
		{name: "empty worktree path", mutate: func(value *Topology) {
			value.Worktrees[0].Path = "  "
		}, want: "path"},
		{name: "invalid profile identity", mutate: func(value *Topology) {
			value.Profiles[0].ID = "*"
		}, want: "profiles[0].id"},
		{name: "duplicate profile", mutate: func(value *Topology) {
			value.Profiles = append(value.Profiles, value.Profiles[0])
		}, want: "duplicate of profiles"},
		{name: "unknown worktree selection", mutate: func(value *Topology) {
			value.Profiles[0].Worktrees[0].Worktree = "missing"
		}, want: "worktree is not declared"},
		{name: "mismatched repository selection", mutate: func(value *Topology) {
			value.Profiles[0].Worktrees[0].Repository = "backend"
		}, want: "selection says repository"},
		{name: "duplicate selection", mutate: func(value *Topology) {
			value.Profiles[0].Worktrees = append(value.Profiles[0].Worktrees, value.Profiles[0].Worktrees[0])
		}, want: "duplicate ownership"},
		{name: "conflicting worktrees", mutate: func(value *Topology) {
			value.Profiles[0].Worktrees = append(value.Profiles[0].Worktrees, WorktreeSelection{
				Repository: "backend",
				Worktree:   "backend-maintenance",
			})
		}, want: "conflicting worktrees"},
		{name: "unknown overlay target", mutate: func(value *Topology) {
			value.Profiles[0].Worktrees[1].Overlays = "missing"
		}, want: "overlay worktree is not declared"},
		{name: "self overlay", mutate: func(value *Topology) {
			value.Profiles[0].Worktrees[1].Overlays = "backend-main"
		}, want: "cannot overlay itself"},
		{name: "cross repository overlay", mutate: func(value *Topology) {
			value.Profiles[0].Worktrees[1].Overlays = "frontend"
		}, want: "same logical repository"},
		{name: "overlay target selected by same profile", mutate: func(value *Topology) {
			value.Profiles[0].Worktrees[1] = WorktreeSelection{
				Repository: "backend", Worktree: "backend-maintenance", Overlays: "backend-main",
			}
			value.Profiles[0].Worktrees = append(value.Profiles[0].Worktrees, WorktreeSelection{
				Repository: "backend", Worktree: "backend-main",
			})
		}, want: "overlay target is also selected"},
		{name: "overlay target is another overlay", mutate: func(value *Topology) {
			value.Profiles[0].Worktrees[1].Overlays = "backend-maintenance"
			value.Profiles[1].Worktrees[0].Overlays = "backend-main"
		}, want: "cannot overlay another overlay"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := topologyFixture()
			test.mutate(&value)
			err := value.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want an explicit rejection")
			}
			if !errors.Is(err, ErrInvalidTopology) {
				t.Fatalf("Validate() error = %v, want ErrInvalidTopology", err)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %q, want substring %q", err, test.want)
			}
		})
	}
}

func TestTopologySupportsMultipleRepositoriesAndIsolatedVariants(t *testing.T) {
	value := topologyFixture()
	if err := value.Validate(); err != nil {
		t.Fatalf("original topology validation: %v", err)
	}
	moved := value
	moved.Worktrees = append([]Worktree(nil), value.Worktrees...)
	moved.Worktrees[0].Path = "/moved/frontend"
	if err := moved.Validate(); err != nil {
		t.Fatalf("moved frontend topology validation: %v", err)
	}
}

func TestSourceObservationIdentityUsesObservedStateNotPath(t *testing.T) {
	digest := strings.Repeat("a", 64)
	first, err := NewSourceObservation("frontend", "abc123", "main", false, digest)
	if err != nil {
		t.Fatalf("NewSourceObservation(%q, %q, %q, %t, %q): %v", "frontend", "abc123", "main", false, digest, err)
	}
	second, err := NewSourceObservation("frontend", " abc123 ", "main", false, strings.ToUpper(digest))
	if err != nil {
		t.Fatalf("NewSourceObservation(%q, %q, %q, %t, %q) normalized equivalent observation: %v", "frontend", " abc123 ", "main", false, strings.ToUpper(digest), err)
	}
	if first.ID != second.ID {
		t.Fatalf("equivalent observation IDs differ: %q != %q", first.ID, second.ID)
	}
	if second.Worktree != "frontend" || second.Commit != "abc123" ||
		second.Branch != "main" || second.Dirty || second.ContentDigest != digest {
		t.Fatalf("normalized observation = %#v, want canonical observed state", second)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("first source observation %#v Validate(): %v", first, err)
	}

	dirty, err := NewSourceObservation("frontend", "abc123", "main", true, digest)
	if err != nil {
		t.Fatalf("NewSourceObservation(%q, %q, %q, %t, %q) dirty observation: %v", "frontend", "abc123", "main", true, digest, err)
	}
	if dirty.ID == first.ID {
		t.Fatal("dirty and clean observations must have different identities")
	}
	changed, err := NewSourceObservation("frontend", "abc123", "main", false, strings.Repeat("b", 64))
	if err != nil {
		t.Fatalf("NewSourceObservation(%q, %q, %q, %t, %q) changed-content observation: %v", "frontend", "abc123", "main", false, strings.Repeat("b", 64), err)
	}
	if changed.ID == first.ID {
		t.Fatal("different content must have different observation identities")
	}

	tampered := first
	tampered.ID = "obs-tampered"
	if err := tampered.Validate(); !errors.Is(err, ErrInvalidSourceObservation) {
		t.Fatalf("tampered Validate() error = %v, want ErrInvalidSourceObservation", err)
	}
	if err := (SourceObservation{Worktree: "frontend"}).Validate(); !errors.Is(err, ErrInvalidSourceObservation) {
		t.Fatalf("empty Validate() error = %v, want ErrInvalidSourceObservation", err)
	}
}

func TestPublishedGenerationRemainsBoundToProfile(t *testing.T) {
	generation, err := NewPublishedGeneration("feature-login", "000042")
	if err != nil {
		t.Fatal(err)
	}
	if generation.Profile != "feature-login" || generation.ID != "000042" {
		t.Fatalf("generation = %#v, want profile and generation identities", generation)
	}
	generation.Profile = "*"
	if err := generation.Validate(); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("invalid generation profile error = %v, want ErrInvalidID", err)
	}

	generation.Profile = "feature-login"
	generation.ID = "current"
	if err := generation.Validate(); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("invalid generation ID error = %v, want ErrInvalidID", err)
	}
}

func TestTopologyYAMLRoundTrip(t *testing.T) {
	want := topologyFixture()
	data, err := yaml.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got Topology
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("YAML round trip changed topology:\n got: %#v\nwant: %#v", got, want)
	}
}

func topologyFixture() Topology {
	return Topology{
		Repositories: []LogicalRepository{
			{ID: "frontend", Name: "Frontend"},
			{ID: "backend", Name: "Backend"},
		},
		Worktrees: []Worktree{
			{ID: "frontend", Repository: "frontend", Path: "/workspace/frontend", Git: GitLayout{GitDirectory: "/workspace/frontend/.git"}},
			{ID: "backend-main", Repository: "backend", Path: "/workspace/backend-main", Git: GitLayout{CommonDirectory: "/workspace/backend/.git"}},
			{ID: "backend-maintenance", Repository: "backend", Path: "/workspace/backend-maintenance"},
		},
		Profiles: []Profile{
			{ID: "feature-login", Worktrees: []WorktreeSelection{
				{Repository: "frontend", Worktree: "frontend"},
				{Repository: "backend", Worktree: "backend-main"},
			}},
			{ID: "maintenance", Worktrees: []WorktreeSelection{
				{Repository: "backend", Worktree: "backend-maintenance"},
			}},
		},
	}
}
