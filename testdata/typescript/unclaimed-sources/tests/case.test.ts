import { getRequiredField } from "../src/case.js";
import { record } from "./helpers/fixture.js";

export function readsTheRequiredField(): string {
  return getRequiredField(record, "field");
}
