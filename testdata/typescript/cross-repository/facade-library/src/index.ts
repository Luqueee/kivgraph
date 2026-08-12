// The facade declares nothing of its own: it exists so consumers import one
// package instead of every library behind it. Every name below is declared in
// @ladygraph-fixture/shared, and the declaration map of this package points
// at that repository's source, not at this one's.
export { compute, value, Widget, type Shape } from "@ladygraph-fixture/shared";
