/** Escape values inserted into XML text and attributes. */
export function escapeXml(value) {
  return String(value).replace(
    /[<>&'"]/g,
    (character) =>
      ({
        "&": "&amp;",
        "<": "&lt;",
        ">": "&gt;",
        '"': "&quot;",
        "'": "&apos;",
      })[character],
  );
}
