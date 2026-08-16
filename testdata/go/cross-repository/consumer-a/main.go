// Package main exercises direct calls, methods and callbacks.
package main

import "example.com/kivgraph-fixture/shared/api"

func main() {
	shape := api.Shape{Width: api.Answer}
	direct := api.Compute(shape.Width)
	method := shape.Area()
	callback := api.Register(api.Compute)
	_ = direct + method + callback
}
