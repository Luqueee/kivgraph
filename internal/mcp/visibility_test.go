package mcp

import (
	"os"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/integrations"
)

// Exercise the two delivery paths, not just their source strings: an installed
// skill (including user-scope links) and the instructions a client receives.
// The cold indexing server matters: Desktop has no skill and can call
// index_project before any graph exists. This does not test model compliance.
func TestInstalledSkillsAndHandshakeShareToolVisibility(t *testing.T) {
	for _, state := range []struct {
		name   string
		server func() *sdkmcp.Server
	}{
		{"no graph", NewServer},
		{"indexing without graph", func() *sdkmcp.Server {
			return NewServerWithIndexer(&fakeProjectIndexer{})
		}},
		{"published graph", func() *sdkmcp.Server { return publishedServer(t) }},
	} {
		t.Run(state.name, func(t *testing.T) {
			session := connectToServer(t, state.server())
			instructions := session.InitializeResult().Instructions
			if len(instructions) > 2048 {
				t.Fatalf("handshake instructions exceed the client budget: %d bytes", len(instructions))
			}
			for _, scope := range []integrations.Scope{integrations.ScopeUser, integrations.ScopeProject} {
				for _, target := range integrations.SkillTargets() {
					t.Run(string(scope)+"/"+string(target), func(t *testing.T) {
						manager, err := integrations.New(integrations.Options{
							HomeDir: t.TempDir(), ProjectDir: t.TempDir(), GOOS: "darwin",
						})
						if err != nil {
							t.Fatal(err)
						}
						plan, err := manager.InstallSkill(target, scope, false, false)
						if err != nil {
							t.Fatal(err)
						}
						skill, err := os.ReadFile(plan.Path)
						if err != nil {
							t.Fatal(err)
						}
						_, section, found := strings.Cut(string(skill), "## Visible tool use\n\n")
						if !found {
							t.Fatal("installed skill has no visible tool use contract")
						}
						paragraph, _, _ := strings.Cut(section, "\n\n")
						policy := strings.Join(strings.Fields(paragraph), " ")
						first, _, _ := strings.Cut(instructions, "\n\n")
						if policy == "" || first != policy || strings.Count(instructions, policy) != 1 {
							t.Fatalf("handshake must lead with the installed skill's policy exactly once:\nhandshake: %q\nskill: %q", first, policy)
						}
					})
				}
			}
		})
	}
}

// TestVisibleToolUseNamesTheIntentQuery guards the one query value the desktop
// call chrome does not render. The model must state it in the conversation
// before it calls the retrieval, rather than leaving the user with only the
// generic "Find by intent" label.
func TestVisibleToolUseNamesTheIntentQuery(t *testing.T) {
	for _, state := range []struct {
		name   string
		server *sdkmcp.Server
	}{
		{"no graph", NewServer()},
		{"published graph", publishedServer(t)},
	} {
		instructions := connectToServer(t, state.server).InitializeResult().Instructions
		for _, want := range []string{
			"Kivgraph · <tool> — <target>: <purpose>",
			"For find_by_intent, quote its exact \"intent\" value as the target, never a summary.",
		} {
			if !strings.Contains(instructions, want) {
				t.Fatalf("%s instructions = %q, want visible find_by_intent preamble contract %q", state.name, instructions, want)
			}
		}
	}
}

// A graph for another checkout is not a weaker form of evidence for this one.
// The client must repair the graph through its consent-gated mutation and
// attest the new publication before asking semantic questions.
func TestFreshnessPolicyRepairsTheTargetCheckoutBeforeUsingGraphEvidence(t *testing.T) {
	server := NewServerWithIndexer(&fakeProjectIndexer{})
	instructions := connectToServer(t, server).InitializeResult().Instructions
	for _, want := range []string{
		"Freshness is a gate",
		"index_project",
		"reconnect if graph_status was absent",
		"call graph_status again",
		"Only the default profile carries content freshness",
		"If index_project is exposed, use it through its approval flow",
		"reconnect this server before calling graph_status",
	} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("instructions = %q, want %q", instructions, want)
		}
	}

	manager, err := integrations.New(integrations.Options{
		HomeDir: t.TempDir(), ProjectDir: t.TempDir(), GOOS: "darwin",
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := manager.InstallSkill(integrations.TargetCodex, integrations.ScopeUser, false, false)
	if err != nil {
		t.Fatal(err)
	}
	skill, err := os.ReadFile(plan.Path)
	if err != nil {
		t.Fatal(err)
	}
	policy := strings.Join(strings.Fields(string(skill)), " ")
	for _, want := range []string{
		"does not attest the target checkout",
		"index_project",
		"reconnect if graph_status was absent",
		"then call graph_status again",
		"Only the default profile carries content freshness",
	} {
		if !strings.Contains(policy, want) {
			t.Fatalf("installed skill does not require fresh graph evidence: want %q", want)
		}
	}
}
