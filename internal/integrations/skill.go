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
// It is shorter than KnownTargets and has to be said separately, for the same
// reason HookTargets is: Claude Desktop reads no local skill directory, so
// completing its name would offer a word the command then refuses.
func SkillTargets() []Target {
	return []Target{TargetClaudeCode, TargetCodex, TargetOpenCode, TargetOhMyPi}
}
