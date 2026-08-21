// A relative import of a name a local barrel put behind it. `barrel.ts` does
// `export { value as republished } from "@kivgraph-fixture/shared"`, so nothing
// in this file's own text names a package, and the declaration it binds lives
// in another repository. Without the barrel being followed, the binding does
// not exist and every use below has no target at all.
import { republished } from "./barrel.js";

export const doubled = republished + republished;
