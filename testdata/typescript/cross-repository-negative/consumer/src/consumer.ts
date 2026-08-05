import { compute } from "@luque-fixture/twin";
import { value as sharedValue } from "@luque-fixture/shared";
import { unmapped } from "@luque-fixture/unmapped";
import { missing } from "@luque-fixture/unmapped";
import { duplicated } from "@luque-fixture/duplicated";
import { drifted } from "@luque-fixture/drifting";

/** Local homonym: shares its name with an export of the shared provider. */
export const value = "local homonym";

export const total =
  compute(sharedValue) + unmapped + duplicated + drifted + Number(missing);
