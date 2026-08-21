#!/usr/bin/env python3
"""Small, deterministic Python facts worker used when SCIP-Python is absent.

The worker deliberately emits unresolved facts for dynamic constructs. It is
not a replacement for a type checker; it gives Kivgraph a hermetic baseline
and keeps the payload boundary compatible with a future SCIP-Python backend.
"""
import argparse
import ast
import json
import os
import pathlib
import sys


def offsets(text):
    result = [0]
    for index, char in enumerate(text):
        if char == "\n":
            result.append(index + 1)
    return result


def point(node, starts, text):
    line = max(1, getattr(node, "lineno", 1))
    column = max(0, getattr(node, "col_offset", 0))
    start = starts[min(line - 1, len(starts) - 1)] + column
    end_line = max(line, getattr(node, "end_lineno", line))
    end_column = max(column, getattr(node, "end_col_offset", column))
    end = starts[min(end_line - 1, len(starts) - 1)] + end_column
    return {"startLine": line, "startColumn": column, "start": start,
            "endLine": end_line, "endColumn": end_column, "end": end}


def module_name(root, path):
    relative = pathlib.Path(path).relative_to(root).with_suffix("")
    parts = list(relative.parts)
    if parts and parts[-1] == "__init__":
        parts.pop()
    return ".".join(parts) or root.name


def exported(name):
    return not name.startswith("_")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    args = parser.parse_args()
    root = pathlib.Path(args.root).resolve()
    paths = []
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.suffix not in (".py", ".pyi"):
            continue
        if any(part in {".git", ".venv", "venv", "__pycache__", "build", "dist", ".tox"} for part in path.parts):
            continue
        paths.append(path)

    parsed = {}
    payload_files = []
    symbols = []
    symbol_by_module_name = {}
    tree_by_path = {}
    for path in paths:
        relative = path.relative_to(root).as_posix()
        text = path.read_text(encoding="utf-8", errors="replace")
        payload_files.append({"path": relative})
        try:
            tree = ast.parse(text, filename=str(path))
        except SyntaxError as error:
            parsed[path] = (text, None, str(error))
            continue
        parsed[path] = (text, tree, "")
        tree_by_path[path] = tree
        module = module_name(root, path)
        module_id = f"{relative}\x00{module}\x00module"
        module_symbol = {"id": module_id, "file": relative, "name": module.split(".")[-1],
                         "qualifiedName": module, "kind": "module", "exported": True,
                         "signature": "module " + module}
        module_symbol.update(point(tree, offsets(text), text))
        symbols.append(module_symbol)
        symbol_by_module_name[(module, "__module__")] = module_id

    for path, (text, tree, error) in parsed.items():
        if tree is None:
            continue
        relative = path.relative_to(root).as_posix()
        module = module_name(root, path)
        starts = offsets(text)

        def visit(node, qualified_prefix, parent_id):
            name = None
            kind = None
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                name, kind = node.name, "method" if parent_id else "function"
            elif isinstance(node, ast.ClassDef):
                name, kind = node.name, "class"
            elif isinstance(node, (ast.Assign, ast.AnnAssign)) and parent_id is None:
                targets = node.targets if isinstance(node, ast.Assign) else [node.target]
                for target in targets:
                    if isinstance(target, ast.Name):
                        name, kind = target.id, "variable"
                        break
            next_prefix = qualified_prefix
            next_parent = parent_id
            if name:
                qualified = qualified_prefix + "." + name
                symbol_id = f"{relative}\x00{qualified}\x00{kind}"
                symbol = {"id": symbol_id, "file": relative, "name": name,
                          "qualifiedName": qualified, "kind": kind,
                          "exported": exported(name),
                          "signature": f"{kind} {qualified}"}
                symbol.update(point(node, starts, text))
                symbols.append(symbol)
                symbol_by_module_name[(module, qualified[len(module) + 1:])] = symbol_id
                next_prefix = qualified if isinstance(node, ast.ClassDef) else qualified_prefix
                next_parent = symbol_id if isinstance(node, ast.ClassDef) else parent_id
            for child in ast.iter_child_nodes(node):
                visit(child, next_prefix, next_parent)

        visit(tree, module, None)

    # A dynamic module can assign the same name more than once. The canonical
    # graph has one DEFINES edge per durable symbol, so retain the first
    # declaration and let later assignments remain ordinary references or
    # unresolved dynamic behavior.
    unique_symbols = {}
    for symbol in symbols:
        unique_symbols.setdefault(symbol["id"], symbol)
    symbols = list(unique_symbols.values())
    symbols_by_id = {symbol["id"]: symbol for symbol in symbols}
    symbols_by_short = {}
    for symbol in symbols:
        module = symbol["qualifiedName"].split(".")
        symbols_by_short.setdefault((".".join(module[:-1]), symbol["name"]), []).append(symbol)

    def source_symbol(path, offset):
        candidates = [symbol for symbol in symbols
                      if symbol["file"] == path and symbol.get("start", 0) <= offset <= symbol.get("end", 0)]
        if not candidates:
            return symbol_by_module_name.get((module_name(root, root / path), "__module__"), "")
        candidates.sort(key=lambda symbol: (symbol.get("end", 0) - symbol.get("start", 0), symbol["qualifiedName"]))
        return candidates[0]["id"]

    references = []
    imports = []
    unresolved = []
    for path, (text, tree, parse_error) in parsed.items():
        relative = path.relative_to(root).as_posix()
        if tree is None:
            unresolved.append({"file": relative, "requestedSymbol": "", "reason": "PARSE_ERROR", "detail": parse_error, "start": 0, "startLine": 1})
            continue
        module = module_name(root, path)
        starts = offsets(text)
        local_bindings = {}
        base_nodes = set()
        for candidate in ast.walk(tree):
            if isinstance(candidate, ast.ClassDef):
                base_nodes.update(id(base) for base in candidate.bases)
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                for alias in node.names:
                    local = alias.asname or alias.name.split(".")[0]
                    target = symbol_by_module_name.get((alias.name, "__module__"))
                    imports.append({"file": relative, "requestedPackage": alias.name, "requestedSymbol": local,
                                    "targetId": target or "", **point(node, starts, text)})
                    if target:
                        local_bindings[local] = target
            elif isinstance(node, ast.ImportFrom):
                base = node.module or ""
                if node.level:
                    prefix = module.split(".")[:-node.level]
                    base = ".".join(prefix + ([base] if base else []))
                for alias in node.names:
                    local = alias.asname or alias.name
                    target = symbol_by_module_name.get((base, alias.name))
                    imports.append({"file": relative, "requestedPackage": base, "requestedSymbol": alias.name,
                                    "targetId": target or "", **point(node, starts, text)})
                    if target:
                        local_bindings[local] = target
            elif isinstance(node, ast.Name) and isinstance(node.ctx, ast.Load):
                source_id = source_symbol(relative, getattr(node, "col_offset", 0) + starts[getattr(node, "lineno", 1) - 1])
                target = local_bindings.get(node.id)
                if not target:
                    candidates = symbols_by_short.get((module, node.id), [])
                    if len(candidates) == 1:
                        target = candidates[0]["id"]
                if not target:
                    unresolved.append({"file": relative, "sourceId": source_id, "requestedSymbol": node.id,
                                       "reason": "NAME_NOT_RESOLVED", **point(node, starts, text)})
                    continue
                parent_call = any(isinstance(parent, ast.Call) and node in ast.iter_child_nodes(parent) for parent in [])
                if id(node) in base_nodes:
                    kind = "EXTENDS"
                else:
                    kind = "CALLS_DIRECT" if isinstance(getattr(node, "parent", None), ast.Call) else "REFERENCES"
                references.append({"file": relative, "sourceId": source_id, "targetId": target, "kind": kind, **point(node, starts, text), "text": node.id})

    # ast does not retain parent pointers; reclassify identifier spans that are
    # direct call function expressions using source text after collection.
    for reference in references:
        end = reference["end"]
        suffix = text_at(parsed, reference["file"], root, end)
        if suffix.lstrip().startswith("("):
            reference["kind"] = "CALLS_DIRECT"

    result = {"version": 1, "repository": root.name, "language": "python",
              "package": {"name": root.name, "rootPath": str(root)},
              "files": payload_files, "symbols": list(symbols),
              "references": references, "imports": imports, "unresolved": unresolved}
    json.dump(result, sys.stdout, separators=(",", ":"))
    sys.stdout.write("\n")


def text_at(parsed, relative, root, offset):
    path = root / relative
    text, _, _ = parsed[path]
    return text[offset:offset + 4]


if __name__ == "__main__":
    main()
