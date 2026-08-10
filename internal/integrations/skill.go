package integrations

import (
	_ "embed"
)

// embeddedSkill is copied to a client-native skill path by `ladygraph skill
// install`. The same source is included in distribution bundles.
//
//go:embed assets/ladygraph/SKILL.md
var embeddedSkill []byte
