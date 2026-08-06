import { compute } from "@ladygraph-fixture/twin";
import { value as sharedValue } from "@ladygraph-fixture/shared";
import { unmapped } from "@ladygraph-fixture/unmapped";
import { missing } from "@ladygraph-fixture/unmapped";
import { duplicated } from "@ladygraph-fixture/duplicated";
import { drifted } from "@ladygraph-fixture/drifting";
import { plain } from "@ladygraph-fixture/nomap";

/** Local homonym: shares its name with an export of the shared provider. */
export const value = "local homonym";

export const total =
  compute(sharedValue) + unmapped + duplicated + drifted + plain + Number(missing);
