import { SymbolFlags } from "typescript/unstable/async";
import type { Symbol as TypeScriptSymbol } from "typescript/unstable/async";

interface AliasChecker {
  getAliasedSymbol(symbol: TypeScriptSymbol): Promise<TypeScriptSymbol>;
  isUnknownSymbol(symbol: TypeScriptSymbol): Promise<boolean>;
}

/**
 * Resolve checker symbols to values present in a local symbol index.
 *
 * Direct symbols are returned without a native alias call. Symbols outside the
 * index are followed only when TypeScript marks them as aliases; unresolved or
 * external targets return undefined. Identity is always the checker symbol id.
 */
export async function resolveLocalSymbols<T>(
  checker: AliasChecker,
  symbols: readonly (TypeScriptSymbol | undefined)[],
  localById: ReadonlyMap<number, T>,
): Promise<(T | undefined)[]> {
  const aliasCandidates = new Map<number, TypeScriptSymbol>();
  for (const symbol of symbols) {
    if (
      symbol !== undefined &&
      !localById.has(symbol.id) &&
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
        return [symbolId, localById.get(target.id)];
      },
    ),
  );
  const resolvedAliases = new Map(aliasResults);

  return symbols.map((symbol) => {
    if (symbol === undefined) {
      return undefined;
    }
    return localById.get(symbol.id) ?? resolvedAliases.get(symbol.id);
  });
}
