/** Read a field the caller declared mandatory. */
export function getRequiredField(
  record: Record<string, string>,
  field: string,
): string {
  const value = record[field];
  if (value === undefined) {
    throw new Error(`missing field ${field}`);
  }
  return value;
}
