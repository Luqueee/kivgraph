import { compute, value, type Shape } from "@ladygraph-fixture/facade";

export function total(shape: Shape): number {
  return compute(value + shape.value);
}
