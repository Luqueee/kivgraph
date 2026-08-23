// Package blinded loads, and holds a package the index cannot read. Every
// answer about this repository is therefore a lower bound: the excluded package
// declares everything in the source and nothing in the graph.
package blinded

// Visible is declared where the index can read it.
func Visible() string { return "visible" }
