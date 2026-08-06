// Package units depends on geometry, the package declared at the root of
// this module, exercising an intra-repository, intra-module package
// dependency: PACKAGE_DEPENDS_ON without MODULE_DEPENDS_ON, since both
// packages share the same Go module. The cross-repository fixture has no
// same-module, two-package case to prove this on, so it lives here instead.
package units

import geometry "example.com/luque-fixture/type-relations"

// Identify names a Base by delegating to its own ID method, giving this
// package a real, checker-resolved use of geometry.
func Identify(base geometry.Base) string {
	return base.ID()
}
