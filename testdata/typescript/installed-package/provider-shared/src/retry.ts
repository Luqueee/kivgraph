export function expBackoffJitter(attempt: number, baseMs: number): number {
  return baseMs * 2 ** attempt;
}

export async function withRetry<T>(run: () => Promise<T>, attempts = 3): Promise<T> {
  let last: unknown;
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    try {
      return await run();
    } catch (error: unknown) {
      last = error;
    }
  }
  throw last;
}
