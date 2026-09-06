/** Compiler-proven relations confined to one live generation. Names only
 * eliminate impossible pairs; the native checker decides assignability. */
import path from "node:path";
import { ModifierFlags } from "typescript/unstable/ast";
import type { Node } from "typescript/unstable/ast";
import {
  isClassDeclaration,
  isTypeReferenceNode,
  isNewExpression,
} from "typescript/unstable/ast/is";
import {
  DiagnosticCategory,
  SymbolFlags,
  TypeFlags,
} from "typescript/unstable/async";
import type { Symbol as TSSymbol, Type } from "typescript/unstable/async";
import { modifierFlags } from "./declaration-classifier.js";
import type { ExtendsEdge } from "./extends-resolver.js";
import type {
  ImportedSymbol,
  ImportedSymbolIdentity,
} from "./imported-symbol-resolver.js";
import { LanguageService, LanguageServiceError } from "./language-service.js";
import type { ProjectView } from "./language-service.js";
import { extractLocalSymbols } from "./symbol-extractor.js";
import type { LocalSymbol, LocalSymbolExtraction } from "./symbol-extractor.js";
import { symbolDeclarationKey } from "./symbol-resolution.js";

export interface ImplementationEdge extends ExtendsEdge {
  readonly detection: "declared" | "structural";
  readonly relation: "IMPLEMENTS" | "OVERRIDES";
}
export interface ImplementationResolution {
  readonly generation: number;
  readonly edges: readonly ImplementationEdge[];
  readonly limitations: readonly string[];
}
interface Target {
  symbol: TSSymbol;
  local?: LocalSymbol;
  imported?: ImportedSymbol;
  types: Map<number, Type>;
}

export async function resolveImplementations(
  service: LanguageService,
  view: ProjectView,
  extraction: LocalSymbolExtraction,
  imports: readonly ImportedSymbol[],
  options: { exhaustive?: boolean } = {},
): Promise<ImplementationResolution> {
  service.assertFresh(view);
  if (
    extraction.generation !== view.generation ||
    extraction.configFileName !== view.configFileName
  ) {
    throw new LanguageServiceError(
      "STALE_GENERATION",
      "implementation symbols must belong to this project generation",
    );
  }
  const checker = view.checker;
  const byID = new Map<number, LocalSymbol>();
  const byDeclaration = new Map<string, LocalSymbol>();
  for (const local of extraction.symbols) {
    byID.set(local.symbolId, local);
    for (const declaration of local.symbol.declarations)
      byDeclaration.set(symbolDeclarationKey(declaration), local);
  }
  function localOf(symbol: TSSymbol): LocalSymbol | undefined {
    return (
      byID.get(symbol.id) ??
      symbol.declarations
        .map((declaration) =>
          byDeclaration.get(symbolDeclarationKey(declaration)),
        )
        .find((value) => value !== undefined)
    );
  }
  const sources = new Map<
    number,
    { local: LocalSymbol; types: Map<number, Type>; declared: Set<number> }
  >();
  const targets = new Map<number, Target>();
  const badDeclarations = new Set<number>();
  const limitations = new Set<string>();
  const fileNames = [
    ...new Set(extraction.symbols.map((symbol) => symbol.fileName)),
  ].sort();
  const symbolsByFile = new Map<string, LocalSymbol[]>();
  for (const symbol of extraction.symbols) {
    const group = symbolsByFile.get(symbol.fileName) ?? [];
    group.push(symbol);
    symbolsByFile.set(symbol.fileName, group);
  }
  // Serial native RPC work bounds concurrency. Caches never escape this view.
  for (const fileName of fileNames) {
    const errors = (await view.program.getSemanticDiagnostics(fileName)).filter(
      (diagnostic) => diagnostic.category === DiagnosticCategory.Error,
    );
    for (const local of symbolsByFile.get(fileName) ?? []) {
      if (
        errors.some(
          (diagnostic) =>
            diagnostic.pos < local.end && diagnostic.end > local.start,
        )
      )
        badDeclarations.add(local.symbolId);
    }
  }
  if (
    extraction.symbols.some(
      (symbol) =>
        ["class", "interface", "type"].includes(symbol.kind) &&
        badDeclarations.has(symbol.symbolId),
    )
  ) {
    limitations.add(
      "Type declarations with compiler errors are excluded from implementation proofs.",
    );
  }
  const valid = (type: Type | undefined): type is Type =>
    type !== undefined &&
    !type.isErrorType() &&
    (type.flags & (TypeFlags.Any | TypeFlags.Unknown | TypeFlags.Never)) === 0;
  for (const local of extraction.symbols) {
    if (
      !["class", "interface", "type"].includes(local.kind) ||
      badDeclarations.has(local.symbolId)
    )
      continue;
    const type = await checker.getDeclaredTypeOfSymbol(local.symbol);
    if (!valid(type)) continue;
    let abstract = false;
    for (const handle of local.symbol.declarations) {
      const declaration = await handle.resolve();
      if (declaration !== undefined && isClassDeclaration(declaration))
        abstract ||=
          (modifierFlags(declaration) & ModifierFlags.Abstract) !== 0;
    }
    if (local.kind === "class" && !abstract) {
      sources.set(local.symbolId, {
        local,
        types: new Map([[type.id, type]]),
        declared: new Set(),
      });
    } else if (type.isObjectType() || type.isIntersectionType()) {
      targets.set(local.symbolId, {
        symbol: local.symbol,
        local,
        types: new Map([[type.id, type]]),
      });
    }
  }
  for (const entry of imports) {
    if (entry.target.identity === undefined) continue;
    const original = await checker.getSymbolAtPosition(
      entry.consumer.fileName,
      entry.consumer.start,
    );
    if (original === undefined) continue;
    const symbol =
      (original.flags & SymbolFlags.Alias) !== 0
        ? await checker.getAliasedSymbol(original)
        : original;
    if (await checker.isUnknownSymbol(symbol)) continue;
    if (targets.has(symbol.id) || localOf(symbol) !== undefined) continue;
    const type = await checker.getDeclaredTypeOfSymbol(symbol);
    if (!valid(type) || !(type.isObjectType() || type.isIntersectionType()))
      continue;
    if (!["interface", "type"].includes(entry.target.identity.kind)) continue;
    targets.set(symbol.id, {
      symbol,
      imported: entry,
      types: new Map([[type.id, type]]),
    });
  }
  for (const fileName of fileNames) {
    const file = await view.program.getSourceFile(fileName);
    if (file === undefined) continue;
    const nodes: Node[] = [];
    const visit = (node: Node): void => {
      nodes.push(node);
      node.forEachChild(visit);
    };
    file.forEachChild(visit);
    for (const node of nodes) {
      if (isClassDeclaration(node) && node.name !== undefined) {
        const symbol = await checker.getSymbolAtLocation(node.name);
        const source =
          symbol === undefined ? undefined : sources.get(symbol.id);
        if (source === undefined) continue;
        for (const clause of node.heritageClauses ?? []) {
          for (const base of clause.types) {
            const type = await checker.getTypeAtLocation(base);
            let targetSymbol = await checker.getSymbolAtLocation(
              base.expression,
            );
            if (
              targetSymbol !== undefined &&
              (targetSymbol.flags & SymbolFlags.Alias) !== 0
            )
              targetSymbol = await checker.getAliasedSymbol(targetSymbol);
            const target =
              targetSymbol === undefined
                ? undefined
                : targets.get(targetSymbol.id);
            if (target !== undefined && valid(type)) {
              target.types.set(type.id, type);
              source.declared.add(target.symbol.id);
            }
          }
        }
      }
      if (!isTypeReferenceNode(node) && !isNewExpression(node)) continue;
      const type = isTypeReferenceNode(node)
        ? await checker.getTypeFromTypeNode(node)
        : await checker.getTypeAtLocation(node);
      if (!valid(type)) continue;
      const symbol = await type.getSymbol();
      if (symbol === undefined) continue;
      sources.get(symbol.id)?.types.set(type.id, type);
      targets.get(symbol.id)?.types.set(type.id, type);
    }
  }
  const providerMembers = new Map<
    number,
    Map<string, ImportedSymbolIdentity>
  >();
  const providerTargets = new Map<string, Target[]>();
  for (const target of targets.values()) {
    const imported = target.imported;
    if (
      imported === undefined ||
      imported.provider.projectPath === undefined ||
      imported.target.identity?.repository !== imported.provider.repository
    )
      continue;
    const group = providerTargets.get(imported.provider.projectPath) ?? [];
    group.push(target);
    providerTargets.set(imported.provider.projectPath, group);
  }
  for (const [project, group] of providerTargets) {
    const providerService = LanguageService.create({
      cwd: path.dirname(project),
    });
    try {
      await providerService.openProject(project);
      const providerView = providerService.project(project);
      const locals = await extractLocalSymbols(providerService, providerView);
      const declarations = new Map<string, LocalSymbol>();
      for (const local of locals.symbols)
        for (const handle of local.symbol.declarations)
          declarations.set(symbolDeclarationKey(handle), local);
      for (const target of group) {
        const entry = target.imported;
        const identity = entry?.target.identity;
        if (entry === undefined || identity === undefined) continue;
        const parent = locals.symbols.find(
          (local) =>
            local.fileName ===
              path.resolve(entry.provider.rootPath, identity.file) &&
            local.qualifiedName === identity.qualifiedName &&
            local.signature === identity.signature &&
            local.kind === identity.kind,
        );
        if (parent === undefined) {
          limitations.add(
            "Provider declaration identity could not be revalidated.",
          );
          targets.delete(target.symbol.id);
          continue;
        }
        const diagnostics = await providerView.program.getSemanticDiagnostics(
          parent.fileName,
        );
        if (
          diagnostics.some(
            (diagnostic) =>
              diagnostic.category === DiagnosticCategory.Error &&
              diagnostic.pos < parent.end &&
              diagnostic.end > parent.start,
          )
        ) {
          limitations.add(
            "Provider declarations with compiler errors are excluded.",
          );
          targets.delete(target.symbol.id);
          continue;
        }
        const type = await providerView.checker.getDeclaredTypeOfSymbol(
          parent.symbol,
        );
        if (!valid(type)) {
          limitations.add(
            "Provider declared types that fail validation contribute no member provenance.",
          );
          continue;
        }
        const members = new Map<string, ImportedSymbolIdentity>();
        for (const member of await providerView.checker.getPropertiesOfType(
          type,
        )) {
          const local = member.declarations
            .map((handle) => declarations.get(symbolDeclarationKey(handle)))
            .find((value) => value !== undefined);
          if (local === undefined || local.kind !== "method") continue;
          const file = path
            .relative(entry.provider.rootPath, local.fileName)
            .split(path.sep)
            .join("/");
          if (file.startsWith("../") || path.isAbsolute(file)) continue;
          members.set(member.name, {
            repository: identity.repository,
            package: identity.package,
            qualifiedName: local.qualifiedName,
            kind: local.kind,
            signature: local.signature,
            file,
            startLine: local.startLine,
            source: "PROVIDER_EXPORT",
          });
        }
        providerMembers.set(target.symbol.id, members);
      }
    } catch {
      limitations.add(
        "A provider project could not be analyzed, so its members contribute no provenance.",
      );
      for (const target of group) targets.delete(target.symbol.id);
    } finally {
      await providerService.close();
    }
  }
  const properties = new Map<number, readonly TSSymbol[]>();
  async function props(type: Type): Promise<readonly TSSymbol[]> {
    const cached = properties.get(type.id);
    if (cached !== undefined) return cached;
    const value = await checker.getPropertiesOfType(type);
    properties.set(type.id, value);
    return value;
  }
  const assignability = new Map<string, boolean>();
  async function assignable(source: Type, target: Type): Promise<boolean> {
    const key = `${source.id}:${target.id}`;
    const cached = assignability.get(key);
    if (cached !== undefined) return cached;
    const result = await checker.isTypeAssignableTo(source, target);
    assignability.set(key, result);
    return result;
  }
  const edges = new Map<string, ImplementationEdge>();
  function emit(
    source: LocalSymbol,
    target: Target,
    detection: ImplementationEdge["detection"],
    method = false,
  ): void {
    const edge: ImplementationEdge = {
      base: {
        fileName: source.fileName,
        sourceQualifiedName: source.qualifiedName,
        text: source.signature,
        start: source.start,
        end: source.end,
        startLine: source.startLine,
        endLine: source.endLine,
      },
      targetQualifiedName: target.local?.qualifiedName,
      targetFile: target.local?.fileName,
      identity: target.imported?.target.identity,
      packageName: target.imported?.packageName,
      exportedName: target.imported?.exportedName,
      unresolvedReason: undefined,
      unresolvedDetail: undefined,
      detection,
      relation: method ? "OVERRIDES" : "IMPLEMENTS",
    };
    const identity = target.imported?.target.identity;
    const targetLocation =
      target.local?.fileName ??
      (identity === undefined
        ? undefined
        : `${identity.repository}\u0000${identity.file}`);
    const key = `${source.fileName}:${source.qualifiedName}:${targetLocation}:${target.local?.qualifiedName ?? identity?.qualifiedName}`;
    if (edges.get(key)?.detection !== "declared") edges.set(key, edge);
  }

  const targetIDsByRequiredName = new Map<string, Set<number>>();
  const targetsWithoutRequiredMembers = new Set<number>();
  for (const target of targets.values()) {
    let hasRequiredMembers = false;
    for (const targetType of target.types.values()) {
      for (const property of await props(targetType)) {
        if ((property.flags & SymbolFlags.Optional) !== 0) continue;
        hasRequiredMembers = true;
        const matching =
          targetIDsByRequiredName.get(property.name) ?? new Set();
        matching.add(target.symbol.id);
        targetIDsByRequiredName.set(property.name, matching);
      }
    }
    if (!hasRequiredMembers)
      targetsWithoutRequiredMembers.add(target.symbol.id);
  }
  for (const source of sources.values()) {
    const candidateIDs = new Set([
      ...source.declared,
      ...targetsWithoutRequiredMembers,
    ]);
    for (const sourceType of source.types.values()) {
      for (const property of await props(sourceType)) {
        for (const targetID of targetIDsByRequiredName.get(property.name) ??
          []) {
          candidateIDs.add(targetID);
        }
      }
    }
    const candidates = options.exhaustive
      ? targets.values()
      : [...candidateIDs]
          .sort((left, right) => left - right)
          .map((targetID) => targets.get(targetID))
          .filter((target): target is Target => target !== undefined);
    for (const target of candidates) {
      const detection = source.declared.has(target.symbol.id)
        ? "declared"
        : "structural";
      let proof: { sourceType: Type; targetType: Type } | undefined;
      for (const sourceType of source.types.values()) {
        if (
          (await props(sourceType)).some((member) => {
            const local = localOf(member);
            return local !== undefined && badDeclarations.has(local.symbolId);
          })
        )
          continue;
        const sourceNames = new Set(
          (await props(sourceType)).map((property) => property.name),
        );
        for (const targetType of target.types.values()) {
          if (
            (await props(targetType)).some((member) => {
              const local = localOf(member);
              return local !== undefined && badDeclarations.has(local.symbolId);
            })
          )
            continue;
          if (
            !options.exhaustive &&
            (await props(targetType)).some(
              (property) =>
                (property.flags & SymbolFlags.Optional) === 0 &&
                !sourceNames.has(property.name),
            )
          )
            continue;
          if (await assignable(sourceType, targetType)) {
            proof = { sourceType, targetType };
            break;
          }
        }
        if (proof !== undefined) break;
      }
      if (proof === undefined) continue;
      emit(source.local, target, detection);
      for (const targetMember of await props(proof.targetType)) {
        if ((targetMember.flags & SymbolFlags.Method) === 0) continue;
        const targetLocal = localOf(targetMember);
        const sourceMember = await checker.getPropertyOfType(
          proof.sourceType,
          targetMember.name,
        );
        const sourceLocal =
          sourceMember === undefined ? undefined : localOf(sourceMember);
        const memberIdentity = providerMembers
          .get(target.symbol.id)
          ?.get(targetMember.name);
        if (
          targetLocal === undefined &&
          sourceLocal !== undefined &&
          memberIdentity !== undefined &&
          target.imported !== undefined
        ) {
          emit(
            sourceLocal,
            {
              symbol: targetMember,
              imported: {
                ...target.imported,
                target: { ...target.imported.target, identity: memberIdentity },
              },
              types: new Map(),
            },
            detection,
          );
          continue;
        }
        if (targetLocal === undefined || sourceLocal === undefined) {
          limitations.add(
            "Method declarations outside the analyzed source identity set are excluded.",
          );
          continue;
        }
        if (
          badDeclarations.has(sourceLocal.symbolId) ||
          sourceLocal.symbolId === targetLocal.symbolId
        )
          continue;
        emit(
          sourceLocal,
          { symbol: targetMember, local: targetLocal, types: new Map() },
          detection,
          target.local?.kind === "class",
        );
      }
    }
  }
  service.assertFresh(view);
  return {
    generation: view.generation,
    edges: [...edges.entries()]
      .sort(([left], [right]) => left.localeCompare(right))
      .map(([, edge]) => edge),
    limitations: [...limitations].sort(),
  };
}
