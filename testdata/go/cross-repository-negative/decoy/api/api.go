// Package api mirrors the shape of every other decoy package.
package api

// Answer is a constant.
const Answer = 1

// Shape declares a method with a name shared by every decoy.
type Shape struct {
	// Width is a field.
	Width int
}

// Area is the method a consumer may reach by mistake.
func (shape Shape) Area() int { return shape.Width + Answer }

// Compute is the homonym function.
func Compute(input int) int { return input + Answer }

// Register invokes the callback it receives.
func Register(handler func(int) int) int { return handler(Answer) }
