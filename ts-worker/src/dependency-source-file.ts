import type { Program } from "typescript/unstable/async";
import type { SourceFile } from "typescript/unstable/ast";

/**
 * isDependencySourceFile reports whether a file of the program is not this
 * repository's own code: a file found through a dependency, or one of the
 * compiler's default library declarations (`lib.es5.d.ts` and the rest).
 *
 * Both questions are asked on purpose. The engine answers "from an external
 * library" for the default library files by whichever of two routes reached the
 * file first while it loads the program in parallel, so the same program gave 63
 * of its 65 default library files as local in four runs out of five and 23 in the
 * rest. Whether a file is the default library is stable, and it is what decides
 * here. Without it, a repository that contains its own copy of the engine -- the
 * worker's own is under `ts-worker/node_modules` -- published tens of thousands of
 * declarations of the language itself as that repository's symbols, and a
 * different number of them on every pass.
 */
export async function isDependencySourceFile(
  program: Program,
  sourceFile: SourceFile,
): Promise<boolean> {
  return (
    (await program.isSourceFileDefaultLibrary(sourceFile)) ||
    (await program.isSourceFileFromExternalLibrary(sourceFile))
  );
}
