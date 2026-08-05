/**
 * Exact position mapping from a declaration artifact to its source.
 *
 * LUQUE-0703 bridges files; this module decodes the `mappings` field of the
 * declaration map so a symbol declared in a `.d.ts` can be reported at the
 * line and column it occupies in the real source file.
 *
 * A position with no covering segment stays unmapped. Nothing is approximated:
 * the closest preceding segment on the *same generated line* is the only
 * answer the source-map format actually supports.
 */

import { loadDeclarationSourceMap } from "./declaration-source-resolver.js";

/** A position in a real source file. */
export interface SourcePosition {
  readonly fileName: string;
  /** 1-based, matching the rest of the worker contract. */
  readonly line: number;
  /** 0-based UTF-16 offset inside the line. */
  readonly character: number;
}

/** One decoded segment of a declaration map. */
export interface DeclarationMapSegment {
  readonly generatedCharacter: number;
  readonly sourceIndex: number;
  readonly sourceLine: number;
  readonly sourceCharacter: number;
}

/** Decoded declaration map ready for repeated position queries. */
export class DeclarationPositionMapper {
  readonly #sources: readonly (string | undefined)[];
  readonly #lines: readonly (readonly DeclarationMapSegment[])[];

  private constructor(
    sources: readonly (string | undefined)[],
    lines: readonly (readonly DeclarationMapSegment[])[],
  ) {
    this.#sources = sources;
    this.#lines = lines;
  }

  /** Load and decode the map of one declaration file, if it has one. */
  static async create(
    declarationFile: string,
  ): Promise<DeclarationPositionMapper | undefined> {
    const sourceMap = await loadDeclarationSourceMap(declarationFile);
    return sourceMap === undefined
      ? undefined
      : new DeclarationPositionMapper(
          sourceMap.sources,
          decodeMappings(sourceMap.mappings),
        );
  }

  /**
   * Map a generated position to its source position.
   *
   * `line` is 1-based and `character` 0-based, as produced by the checker via
   * `getLineAndCharacterOfPosition`.
   */
  lookup(line: number, character: number): SourcePosition | undefined {
    const segments = this.#lines[line - 1];
    if (segments === undefined || segments.length === 0) {
      return undefined;
    }
    let match: DeclarationMapSegment | undefined;
    for (const segment of segments) {
      if (segment.generatedCharacter > character) {
        break;
      }
      match = segment;
    }
    if (match === undefined) {
      return undefined;
    }
    const fileName = this.#sources[match.sourceIndex];
    if (fileName === undefined) {
      return undefined;
    }
    return {
      fileName,
      line: match.sourceLine + 1,
      character: match.sourceCharacter,
    };
  }
}

/**
 * Decode the `mappings` field into per-line segments.
 *
 * Segments carry deltas: the generated column resets on every line while the
 * source index, line and column accumulate across the whole document.
 */
export function decodeMappings(mappings: string): DeclarationMapSegment[][] {
  const lines: DeclarationMapSegment[][] = [];
  let sourceIndex = 0;
  let sourceLine = 0;
  let sourceCharacter = 0;

  for (const rawLine of mappings.split(";")) {
    const segments: DeclarationMapSegment[] = [];
    let generatedCharacter = 0;
    for (const rawSegment of rawLine.split(",")) {
      if (rawSegment === "") {
        continue;
      }
      const fields = decodeVlq(rawSegment);
      if (fields === undefined || fields.length === 0) {
        return lines;
      }
      generatedCharacter += fields[0] ?? 0;
      if (fields.length < 4) {
        // A one-field segment marks generated code with no source origin.
        continue;
      }
      sourceIndex += fields[1] ?? 0;
      sourceLine += fields[2] ?? 0;
      sourceCharacter += fields[3] ?? 0;
      segments.push({
        generatedCharacter,
        sourceIndex,
        sourceLine,
        sourceCharacter,
      });
    }
    segments.sort(
      (left, right) => left.generatedCharacter - right.generatedCharacter,
    );
    lines.push(segments);
  }
  return lines;
}

const BASE64 =
  "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
const VLQ_CONTINUATION = 0b100000;
const VLQ_VALUE_MASK = 0b011111;

function decodeVlq(segment: string): number[] | undefined {
  const values: number[] = [];
  let shift = 0;
  let accumulator = 0;
  for (const character of segment) {
    const digit = BASE64.indexOf(character);
    if (digit === -1) {
      return undefined;
    }
    accumulator += (digit & VLQ_VALUE_MASK) << shift;
    if ((digit & VLQ_CONTINUATION) !== 0) {
      shift += 5;
      continue;
    }
    const negative = (accumulator & 1) === 1;
    const value = accumulator >> 1;
    values.push(negative ? -value : value);
    shift = 0;
    accumulator = 0;
  }
  return shift === 0 ? values : undefined;
}
