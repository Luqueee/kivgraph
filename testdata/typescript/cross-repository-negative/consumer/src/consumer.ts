import { compute } from "@kivgraph-fixture/twin";
import { value as sharedValue } from "@kivgraph-fixture/shared";
import { unmapped } from "@kivgraph-fixture/unmapped";
import { missing } from "@kivgraph-fixture/unmapped";
import { duplicated } from "@kivgraph-fixture/duplicated";
import { drifted } from "@kivgraph-fixture/drifting";
import { plain } from "@kivgraph-fixture/nomap";

/** Local homonym: shares its name with an export of the shared provider. */
export const value = "local homonym";

export const total =
  compute(sharedValue) + unmapped + duplicated + drifted + plain + Number(missing);
