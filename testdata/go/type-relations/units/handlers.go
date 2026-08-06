package units

import geometry "example.com/luque-fixture/type-relations"

// measurer is a function stored in a package level variable rather than
// called: the checker sees geometry.Measure as a value here, which is what
// ASSIGNS_FUNCTION names. Calling it instead would be CALLS_DIRECT, and the
// distinction between the two is exactly what this file exists to prove.
var measurer = geometry.Measure

// Measurer returns a function as a value instead of calling it, which is
// what RETURNS_FUNCTION names. The returned callee is resolved by the
// checker, never guessed from the identifier.
func Measurer() func(geometry.Shape) float64 {
	return geometry.Measure
}

// Measure applies the stored function so the variable is not dead code.
func Measure(shape geometry.Shape) float64 {
	return measurer(shape)
}
