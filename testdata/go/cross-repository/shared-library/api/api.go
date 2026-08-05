// Package api is the exported surface consumers resolve against.
package api

// Answer is an exported constant.
const Answer = 42

// Shape carries an exported field and an exported method.
type Shape struct {
	// Width is read by consumers.
	Width int
}

// Area is a method consumers call.
func (shape Shape) Area() int { return shape.Width }

// Compute is a function consumers call directly.
func Compute(input int) int { return input + Answer }

// Register invokes the callback it receives.
func Register(handler func(int) int) int { return handler(Answer) }
