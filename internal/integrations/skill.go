package integrations

import (
	_ "embed"
)

// embeddedSkill is copied to a client-native skill path by `kivgraph skill
// install`. The same source is included in distribution bundles.
//
//go:embed assets/kivgraph/SKILL.md
var embeddedSkill []byte

// SkillTargets are the clients an Agent Skill can be installed into.
//
// It is shorter than KnownTargets and has to be said separately because
// Claude Desktop reads no local skill directory, while hook targets include
// its user-scoped gate.
func SkillTargets() []Target {
	return []Target{TargetClaudeCode, TargetCodex, TargetOpenCode, TargetOhMyPi}
}
