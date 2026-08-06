// Package main exercises a package alias and a replaced module.
package main

import (
	shared "example.com/ladygraph-fixture/shared/api"

	"example.com/ladygraph-fixture/legacy"
)

func main() {
	aliased := shared.Compute(shared.Answer)
	replaced := legacy.Legacy()
	_ = aliased + replaced
}
