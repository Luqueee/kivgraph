export const value = 1;

export interface Shape {
  readonly value: number;
}

export function compute(input: number): number {
  return input + value;
}
