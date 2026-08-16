package integrations

import (
	_ "embed"
)

// embeddedSkill is copied to a client-native skill path by `kivgraph skill
// install`. The same source is included in distribution bundles.
//
//go:embed assets/kivgraph/SKILL.md
var embeddedSkill []byte
