import { legacyRetry } from "@kivgraph-fixture/drifted";
import { vendoredHelper, withRetry } from "@kivgraph-fixture/installed";

export async function run(): Promise<number> {
  return withRetry(async () => legacyRetry() + vendoredHelper());
}
