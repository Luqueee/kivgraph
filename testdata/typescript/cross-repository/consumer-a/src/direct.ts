import { compute, value, type Shape } from "@luque-fixture/shared";

export function total(shape: Shape): number {
  return compute(value + shape.value);
}
