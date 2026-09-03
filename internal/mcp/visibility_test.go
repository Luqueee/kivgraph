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
