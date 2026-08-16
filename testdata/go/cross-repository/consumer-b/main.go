// Package main exercises a package alias and a replaced module.
package main

import (
	shared "example.com/kivgraph-fixture/shared/api"

	"example.com/kivgraph-fixture/legacy"
)

func main() {
	aliased := shared.Compute(shared.Answer)
	replaced := legacy.Legacy()
	_ = aliased + replaced
}
