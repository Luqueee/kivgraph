import { SymbolFlags } from "typescript/unstable/async";
import type { Symbol as TypeScriptSymbol } from "typescript/unstable/async";

interface AliasChecker {
  getAliasedSymbol(symbol: TypeScriptSymbol): Promise<TypeScriptSymbol>;
  isUnknownSymbol(symbol: TypeScriptSymbol): Promise<boolean>;
}

/**
 * Build a snapshot-scoped key for a native declaration handle.
 *
 * TypeScript can return an instantiated symbol for a generic member use. Its
 * id differs from the declaration symbol id, but both symbols retain the same
 * declaration handle.
 */
export function symbolDeclarationKey(declaration: {
  readonly path: string;
  readonly index: number;
}): string {
  return `${declaration.path}:${declaration.index}`;
}

/**
 * Resolve checker symbols to values present in a local symbol index.
 *
 * Direct symbols are returned without a native alias call. Symbols outside the
 * index are followed only when TypeScript marks them as aliases; unresolved or
 * external targets return undefined. Declaration handles provide a fallback
 * for instantiated generic members whose checker id is not their declaration
 * symbol id.
 */
export async function resolveLocalSymbols<T>(
  checker: AliasChecker,
  symbols: readonly (TypeScriptSymbol | undefined)[],
  localById: ReadonlyMap<number, T>,
  localByDeclaration?: ReadonlyMap<string, T>,
): Promise<(T | undefined)[]> {
  const findLocal = (symbol: TypeScriptSymbol): T | undefined => {
    const direct = localById.get(symbol.id);
    if (direct !== undefined) {
      return direct;
    }
    if (localByDeclaration !== undefined) {
      for (const declaration of symbol.declarations) {
        const local = localByDeclaration.get(symbolDeclarationKey(declaration));
        if (local !== undefined) {
          return local;
        }
      }
    }
    return undefined;
  };

  const aliasCandidates = new Map<number, TypeScriptSymbol>();
  for (const symbol of symbols) {
    if (
      symbol !== undefined &&
      findLocal(symbol) === undefined &&
      (symbol.flags & SymbolFlags.Alias) !== 0
    ) {
      aliasCandidates.set(symbol.id, symbol);
    }
  }

  const aliasResults = await Promise.all(
    [...aliasCandidates.entries()].map(
      async ([symbolId, symbol]): Promise<readonly [number, T | undefined]> => {
        const target = await checker.getAliasedSymbol(symbol);
        if (await checker.isUnknownSymbol(target)) {
          return [symbolId, undefined];
        }
        return [symbolId, findLocal(target)];
      },
    ),
  );
  const resolvedAliases = new Map(aliasResults);

  return symbols.map((symbol) => {
    if (symbol === undefined) {
      return undefined;
    }
    return findLocal(symbol) ?? resolvedAliases.get(symbol.id);
  });
}
