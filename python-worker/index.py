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


# A quoted annotation is source like any other: `def f(ctx: "Context")` uses
# Context exactly as the unquoted form does, and so does `t.cast("Context", x)`.
# Read as a string constant it is no occurrence at all -- no edge and no
# unresolved row -- and a declaration used only that way was answered as used by
# nobody, with a verdict of COMPLETE.
def is_quoted_annotation(node):
    if not (isinstance(node, ast.Constant) and isinstance(node.value, str)):
        return False
    child, parent = node, getattr(node, "parent", None)
    while isinstance(parent, (ast.Subscript, ast.Tuple, ast.List, ast.BinOp)):
        if isinstance(parent, ast.Subscript):
            head = parent.value
            name = head.id if isinstance(head, ast.Name) else getattr(head, "attr", "")
            # The strings of `Literal["a"]` are values and those of
            # `Annotated[T, "note"]` are metadata; neither names anything.
            if name in {"Literal", "Annotated"}:
                return False
        child, parent = parent, getattr(parent, "parent", None)
    if isinstance(parent, (ast.arg, ast.AnnAssign)) and parent.annotation is child:
        return True
    if isinstance(parent, (ast.FunctionDef, ast.AsyncFunctionDef)) and parent.returns is child:
        return True
    if isinstance(parent, ast.Call) and child is node and parent.args and parent.args[0] is node:
        callee = parent.func
        return (callee.id if isinstance(callee, ast.Name) else getattr(callee, "attr", "")) == "cast"
    return False


# quoted_names yields the names a quoted annotation uses, with where each one is
# in the file. Only a plain one-line ASCII literal is read: anything else -- a
# prefix, an escape, a concatenation -- would put the offsets somewhere the name
# is not, and an occurrence with the wrong evidence is worse than none.
def quoted_names(node, text, start):
    value = node.value
    if not value.isascii() or "\n" in value or text[start:start + 1] not in ("'", '"'):
        return
    if text[start + 1:start + 1 + len(value)] != value:
        return
    try:
        tree = ast.parse(value, mode="eval")
    except SyntaxError:
        return
    for inner in ast.walk(tree):
        if isinstance(inner, ast.Name):
            yield inner.id, start + 1 + inner.col_offset, False
        elif isinstance(inner, ast.Attribute):
            yield inner.attr, start + 1 + inner.end_col_offset - len(inner.attr), True


def span(text, start, length):
    line_start = text.rfind("\n", 0, start) + 1
    line = text.count("\n", 0, start) + 1
    column = start - line_start
    return {"startLine": line, "startColumn": column, "start": start,
            "endLine": line, "endColumn": column + length, "end": start + length}


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
    parser.add_argument("--include-tests", action="store_true")
    parser.add_argument("--include-generated", action="store_true")
    parser.add_argument("--include-external", action="store_true")
    args = parser.parse_args()
    root = pathlib.Path(args.root).resolve()
    paths = []
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.suffix not in (".py", ".pyi"):
            continue
        relative_parts = path.relative_to(root).parts
        if not args.include_tests and any(part in {"test", "tests", "integration_test"} for part in relative_parts):
            continue
        if not args.include_generated and any(part.endswith(("_generated.py", ".generated.py", ".pyc")) or part in {"__pycache__", "build", "dist", ".tox"} for part in relative_parts):
            continue
        if any(part in {".git", ".venv", "venv", "__pycache__", ".tox"} for part in relative_parts):
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
            for parent in ast.walk(tree):
                for child in ast.iter_child_nodes(parent):
                    child.parent = parent
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

        def visit(node, qualified_prefix, class_id=None):
            name = None
            kind = None
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                name, kind = node.name, "method" if class_id else "function"
            elif isinstance(node, ast.ClassDef):
                name, kind = node.name, "class"
            elif isinstance(node, (ast.Assign, ast.AnnAssign)) and class_id is None and qualified_prefix == module:
                targets = node.targets if isinstance(node, ast.Assign) else [node.target]
                for target in targets:
                    if isinstance(target, ast.Name):
                        name, kind = target.id, "variable"
                        break
            next_prefix = qualified_prefix
            next_class = class_id
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
                next_prefix = qualified
                next_class = symbol_id if isinstance(node, ast.ClassDef) else None
            for child in ast.iter_child_nodes(node):
                visit(child, next_prefix, next_class)

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
    function_symbol_ids = {symbol["id"] for symbol in symbols if symbol["kind"] in {"function", "method"}}
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
                parent = getattr(node, "parent", None)
                if id(node) in base_nodes:
                    kind = "EXTENDS"
                elif isinstance(parent, ast.Call) and parent.func is node:
                    kind = "CALLS_DIRECT"
                elif isinstance(parent, ast.Return):
                    kind = "RETURNS_FUNCTION" if target in function_symbol_ids else "REFERENCES"
                elif is_type_position(node):
                    kind = "TYPE_USES"
                elif isinstance(parent, (ast.Assign, ast.AnnAssign, ast.NamedExpr)):
                    kind = "ASSIGNS_FUNCTION" if target in function_symbol_ids else "REFERENCES"
                elif isinstance(parent, ast.Call) and node in parent.args:
                    kind = "PASSES_AS_CALLBACK" if target in function_symbol_ids else "REFERENCES"
                else:
                    kind = "REFERENCES"
                references.append({"file": relative, "sourceId": source_id, "targetId": target, "kind": kind, **point(node, starts, text), "text": node.id})
            elif isinstance(node, ast.Attribute):
                # This worker reads names, not types, so it cannot say what
                # `ctx.scope` or `termui.prompt` is. It can say that it saw one
                # and did not resolve it, and it has to: with no row at all, a
                # method used through every one of its receivers was answered
                # as used by nobody, and the answer said COMPLETE.
                end = point(node, starts, text)["end"]
                occurrence = span(text, end - len(node.attr), len(node.attr))
                unresolved.append({"file": relative, "sourceId": source_symbol(relative, occurrence["start"]),
                                   "requestedSymbol": node.attr, "reason": "NAME_NOT_RESOLVED", **occurrence})
            elif is_quoted_annotation(node):
                for name, offset, member in quoted_names(node, text, point(node, starts, text)["start"]):
                    occurrence = span(text, offset, len(name))
                    source_id = source_symbol(relative, offset)
                    target = "" if member else local_bindings.get(name)
                    if not target and not member:
                        candidates = symbols_by_short.get((module, name), [])
                        if len(candidates) == 1:
                            target = candidates[0]["id"]
                    if not target:
                        unresolved.append({"file": relative, "sourceId": source_id, "requestedSymbol": name,
                                           "reason": "NAME_NOT_RESOLVED", **occurrence})
                        continue
                    references.append({"file": relative, "sourceId": source_id, "targetId": target,
                                       "kind": "TYPE_USES", **occurrence, "text": name})

    # Keep a source-text fallback for call expressions whose AST parent is a
    # selector or a wrapped expression.
    for reference in references:
        end = reference["end"]
        suffix = text_at(parsed, reference["file"], root, end)
        if suffix.lstrip().startswith("("):
            reference["kind"] = "CALLS_DIRECT"

    result = {"version": 1, "authoritative": False, "analyzer": "python-ast-fallback", "repository": root.name, "language": "python",
              "package": {"name": root.name, "rootPath": str(root)},
            "files": payload_files, "symbols": list(symbols),
              "references": references, "imports": imports, "unresolved": unresolved}
    json.dump(result, sys.stdout, separators=(",", ":"))
    sys.stdout.write("\n")


def text_at(parsed, relative, root, offset):
    path = root / relative
    text, _, _ = parsed[path]
    return text[offset:offset + 4]


def is_type_position(node):
    parent = getattr(node, "parent", None)
    if isinstance(parent, (ast.AnnAssign, ast.arg)) and getattr(parent, "annotation", None) is node:
        return True
    if isinstance(parent, (ast.FunctionDef, ast.AsyncFunctionDef)) and (
        getattr(parent, "returns", None) is node or any(getattr(arg, "annotation", None) is node for arg in parent.args.args)
    ):
        return True
    if isinstance(parent, ast.ClassDef) and node in parent.bases:
        return True
    return False


if __name__ == "__main__":
    main()
