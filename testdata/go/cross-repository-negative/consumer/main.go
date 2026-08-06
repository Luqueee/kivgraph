// Package main only resolves the packages it really imports.
package main

import (
	"example.com/ladygraph-fixture/decoy/api"
	twin "example.com/ladygraph-fixture/twin/api"
)

// Compute is a local homonym of the provider functions.
func Compute(input int) int { return input * 2 }

// Shape is a local homonym of the provider types, with its own Area.
type Shape struct{ Width int }

// Area is the local method that must never be linked to a provider.
func (shape Shape) Area() int { return shape.Width }

func main() {
	local := Shape{Width: 1}
	remote := api.Shape{Width: 2}

	_ = local.Area()
	_ = remote.Area()
	_ = Compute(1)
	_ = api.Compute(2)
	_ = api.Register(Compute)
	_ = twin.Compute(3)
}
