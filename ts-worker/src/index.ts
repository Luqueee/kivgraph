import { pathToFileURL } from "node:url";
import { createInterface } from "node:readline";

export { extractLocalSymbols } from "./symbol-extractor.js";
export type {
  LocalExport,
  LocalSymbol,
  LocalSymbolExtraction,
  LocalSymbolKind,
  SymbolExtractionOptions,
} from "./symbol-extractor.js";

export function handleCommand(command: string): string {
  if (command.trim() === "hello") {
    return "hello";
  }

  throw new Error(`unknown command: ${command.trim()}`);
}

export async function run(
  stdin: NodeJS.ReadableStream,
  stdout: NodeJS.WritableStream,
): Promise<number> {
  const input = createInterface({ input: stdin });

  try {
    for await (const line of input) {
      if (line.trim() === "") {
        continue;
      }

      try {
        stdout.write(`${handleCommand(line)}\n`);
      } catch (error: unknown) {
        const message =
          error instanceof Error ? error.message : "unknown error";
        stdout.write(`error: ${message}\n`);
        return 1;
      }
    }

    return 0;
  } finally {
    input.close();
  }
}

if (
  process.argv[1] &&
  pathToFileURL(process.argv[1]).href === import.meta.url
) {
  process.exitCode = await run(process.stdin, process.stdout);
}
