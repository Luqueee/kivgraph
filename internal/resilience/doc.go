// Package resilience holds the cross-component failure tests of phase 12.
//
// The per-component packages already prove their own recovery: tsworker
// restarts a dead worker, rebuild refuses to publish a broken graph,
// hotsnapshot rejects an invalid envelope. What none of them can state alone
// is the property the plan actually requires -- that a failure in one
// component never takes the published graph away from readers.
//
// This package has no production code on purpose. It exists so those seams
// are covered by tests that import both sides.
package resilience
