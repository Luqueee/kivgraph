import type { Program } from "typescript/unstable/async";
import type { SourceFile } from "typescript/unstable/ast";
import { describe, expect, it } from "vitest";

import { isDependencySourceFile } from "./dependency-source-file.js";

const file = {} as SourceFile;

function programAnswering(
  isDefaultLibrary: () => boolean,
  isExternal: () => boolean,
): Program {
  return {
    isSourceFileDefaultLibrary: async () => isDefaultLibrary(),
    isSourceFileFromExternalLibrary: async () => isExternal(),
  } as unknown as Program;
}

describe("dependency source files", () => {
  it.each([
    {
      name: "own code",
      isDefaultLibrary: false,
      isExternal: false,
      want: false,
    },
    {
      name: "a dependency",
      isDefaultLibrary: false,
      isExternal: true,
      want: true,
    },
    {
      name: "the default library",
      isDefaultLibrary: true,
      isExternal: false,
      want: true,
    },
    { name: "both", isDefaultLibrary: true, isExternal: true, want: true },
  ])("classifies $name", async ({ isDefaultLibrary, isExternal, want }) => {
    const program = programAnswering(
      () => isDefaultLibrary,
      () => isExternal,
    );
    expect(await isDependencySourceFile(program, file)).toBe(want);
  });

  // The engine answers "external" for a default library file by whichever route
  // reached it first, so the answer must not depend on that one.
  it("does not depend on a default library file being called external", async () => {
    let calls = 0;
    const program = programAnswering(
      () => true,
      () => {
        calls += 1;
        return calls % 2 === 0;
      },
    );
    const answers = [
      await isDependencySourceFile(program, file),
      await isDependencySourceFile(program, file),
      await isDependencySourceFile(program, file),
      await isDependencySourceFile(program, file),
    ];
    expect(answers).toEqual([true, true, true, true]);
  });
});
