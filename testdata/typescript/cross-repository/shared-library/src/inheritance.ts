import type { Shape } from "./value.js";

/**
 * A name, mixed into `NamedShape`. Declared in its own file to prove local
 * `EXTENDS` resolution works across files within one repository, not only
 * within one: `NamedShape` below reaches this declaration exactly the same
 * way it reaches `Shape`, imported from "./value.js".
 */
export interface Named {
  readonly name: string;
}

/**
 * Local inheritance, entirely within this repository: two bases in one
 * `extends` clause, each its own edge, one declared in this file and one in
 * "./value.js".
 */
export interface NamedShape extends Shape, Named {
  readonly label: string;
}

/**
 * Cross-repository inheritance target: consumer-a's `LabeledWidget`
 * `extends` this class through `@kivgraph-fixture/shared`.
 */
export class Widget {
  constructor(readonly id: string) {}
}
