// Package geometry exercises the structural relations the Go type checker
// can decide entirely on its own: interface satisfaction (by value and by
// pointer), field and interface embedding, and a directly declared method
// that shadows one promoted from an embedded field.
package geometry

import "fmt"

// Shape is satisfied by any type that can report its area.
type Shape interface {
	Area() float64
}

// Solid embeds Shape: every Solid is also a Shape, and additionally reports
// a volume. It has no implementer in this fixture; it exists only to prove
// interface-embeds-interface.
type Solid interface {
	Shape
	Volume() float64
}

// Anything is the empty interface under a name of its own: every type here
// would trivially satisfy it, so it must never appear as an IMPLEMENTS
// target.
type Anything interface{}

// Base is embedded by value in Circle and by pointer in Square. Its ID
// method is promoted into both, unless the outer type declares its own.
type Base struct {
	Label string
}

// ID identifies the base by its label.
func (base Base) ID() string { return base.Label }

// Circle implements Shape by value: Area has a value receiver, so both
// Circle and *Circle satisfy Shape.
type Circle struct {
	Base
	Radius float64
}

// Area gives Circle a value-receiver implementation of Shape.
func (circle Circle) Area() float64 { return 3.14159 * circle.Radius * circle.Radius }

// ID overrides the one promoted from the embedded Base: Circle's own
// identity always wins over the label alone.
func (circle Circle) ID() string { return "circle:" + circle.Base.Label }

// String satisfies fmt.Stringer, a dependency interface with no repository
// of its own in this workspace: the relation is real, but its target has no
// key this pass can derive.
func (circle Circle) String() string { return fmt.Sprintf("Circle(%.2f)", circle.Radius) }

// Square implements Shape only by pointer: Area is declared on *Square, so
// the Square value type does not satisfy Shape, only *Square does.
type Square struct {
	*Base
	Side float64
}

// Area gives *Square a pointer-receiver implementation of Shape.
func (square *Square) Area() float64 { return square.Side * square.Side }

// Triangle deliberately does not implement Shape: it declares no Area
// method at all, by value or by pointer.
type Triangle struct {
	Width  float64
	Height float64
}

// Perimeter is unrelated to Shape; Triangle never satisfies the interface.
func (triangle Triangle) Perimeter() float64 { return 2 * (triangle.Width + triangle.Height) }

// Measure is declared to be handed around as a value, not only called: the
// units package assigns it to a variable and returns it from a function,
// which is what tells ASSIGNS_FUNCTION and RETURNS_FUNCTION apart from a
// plain call.
func Measure(shape Shape) float64 { return shape.Area() }

// Blob declares a method spelled exactly like Shape's and satisfies nothing:
// Area returns an int, so the method set does not match and types.Implements
// says no. It exists to prove the method pairing follows the checker rather
// than the spelling -- a name match would report Blob.Area as the
// implementation a call through Shape reaches.
type Blob struct {
	Cells int
}

// Area is the homonym trap: same name as Shape's method, different signature.
func (blob Blob) Area() int { return blob.Cells }
