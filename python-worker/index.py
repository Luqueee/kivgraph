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


# is_test_path is internal/testpath.Is, the one rule that says a path is a test
# file. It is written twice because this worker has to decide what to read
# before the graph exists, and a test in internal/pythonloader holds the two to
# the same answer over a corpus of paths. With a rule of its own the worker
# left `test_models.py` in the graph as production code while `exclude_tests`
# filtered it as a test.
TEST_DIRECTORIES = {"test", "tests", "__tests__", "spec", "specs", "integration_test"}


def is_test_path(relative):
    normalized = relative.lower()
    directory, _, base = normalized.rpartition("/")
    if (base.endswith(("_test.go", "_test.py", "_test.dart")) or
            (base.startswith("test_") and base.endswith(".py")) or base == "conftest.py" or
            ".test." in base or ".spec." in base):
        return True
    original = relative.rpartition("/")[2]
    stem, dot, extension = original.rpartition(".")
    if dot and extension in {"java", "cs"} and stem.endswith(("Test", "Tests")):
        return True
    if any(component in TEST_DIRECTORIES for component in directory.split("/")):
        return True
    return "/src/it/" in "/" + directory + "/"


# package_name is what a module is imported as when a directory above it is the
# import root: `src/click/core.py` is `click.core`, because `click` is a package
# and `src` is not. It is empty when that is the name the file already has, or
# when the file is in no package at all.
def package_name(root, path):
    parts = list(pathlib.Path(path).relative_to(root).with_suffix("").parts)
    directory, kept = pathlib.Path(path).parent, 1
    while directory != root and ((directory / "__init__.py").is_file() or (directory / "__init__.pyi").is_file()):
        directory, kept = directory.parent, kept + 1
    if kept == 1 or kept >= len(parts):
        return ""
    name = parts[-kept:]
    if name[-1] == "__init__":
        name.pop()
    return ".".join(name)


def exported(name):
    return not name.startswith("_")


# Directories that hold no source of this repository: a dependency tree, an
# environment, or what git keeps.
SKIPPED_DIRECTORIES = {".git", ".venv", "venv", "__pycache__", ".tox", "node_modules"}


def other_checkout(directory):
    """Another Git checkout below the root: a clone or a linked worktree. A
    submodule is not one. The same rule as workspace.IsOtherCheckout."""
    marker = os.path.join(directory, ".git")
    if os.path.isdir(marker) and not os.path.islink(marker):
        return True
    if not os.path.isfile(marker) or os.path.islink(marker):
        return False
    try:
        with open(marker, encoding="utf-8", errors="replace") as handle:
            pointer = handle.read().strip()
    except OSError:
        return False
    return pointer.startswith("gitdir:") and "/modules/" not in pointer.replace(os.sep, "/")


def source_files(root):
    """The Python files below root, in path order, without entering a skipped
    directory: listing everything and filtering read every file of
    node_modules, and indexed the Python some packages ship there."""
    found = []
    for directory, names, files in os.walk(root):
        names[:] = [name for name in names
                    if name not in SKIPPED_DIRECTORIES and not other_checkout(os.path.join(directory, name))]
        # A named pipe called `x.py` is listed too, and reading one blocks.
        found.extend(pathlib.Path(directory, name) for name in files
                     if name.endswith((".py", ".pyi")) and os.path.isfile(os.path.join(directory, name)))
    return sorted(found)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--include-tests", action="store_true")
    parser.add_argument("--include-generated", action="store_true")
    parser.add_argument("--include-external", action="store_true")
    args = parser.parse_args()
    root = pathlib.Path(args.root).resolve()
    paths = []
    for path in source_files(root):
        relative_parts = path.relative_to(root).parts
        if not args.include_tests and is_test_path("/".join(relative_parts)):
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

    # A module is also reachable by its package name, which is the one an import
    # outside the package writes. A name two modules would claim, or one that is
    # already some module's own, is left out: this worker does not choose.
    aliases = {}
    for path in tree_by_path:
        alias = package_name(root, path)
        if alias:
            aliases[module_name(root, path)] = alias
    claimed = {}
    for alias in aliases.values():
        claimed[alias] = claimed.get(alias, 0) + 1
    aliases = {module: alias for module, alias in aliases.items()
               if claimed[alias] == 1 and (alias, "__module__") not in symbol_by_module_name}
    module_of_id = {symbol_id: module for (module, name), symbol_id in symbol_by_module_name.items()
                    if name == "__module__"}
    for (module, name), symbol_id in list(symbol_by_module_name.items()):
        if module in aliases:
            symbol_by_module_name.setdefault((aliases[module], name), symbol_id)

    def import_base(module, path, node):
        base = node.module or ""
        if not node.level:
            return base
        # One dot is the package the module is in. For `__init__` that is the
        # module itself, which is its own package.
        up = node.level - 1 if path.stem == "__init__" else node.level
        prefix = module.split(".")
        prefix = prefix[:len(prefix) - up] if up else prefix
        return ".".join(prefix + ([base] if base else []))

    # What a module makes available without declaring it: `from .core import
    # Command` in `__init__` is why `click.Command` exists. Followed until
    # nothing new resolves, because a package re-exports what its subpackages
    # re-export.
    exports = {}

    # A name one module binds to two declarations: an import under a platform
    # test and a `def` in the other branch, or two imports under `try`. Which
    # one a use means is decided when the program runs, and this worker does
    # not choose. It attributed every use to one of them, and the other was
    # answered as used by nobody, COMPLETE.
    contested = set()

    def lookup(module, name):
        if (module, name) in contested:
            return None
        return (symbol_by_module_name.get((module, name)) or exports.get((module, name)) or
                symbol_by_module_name.get((module + "." + name, "__module__")))

    imported = []
    for path, tree in tree_by_path.items():
        module = module_name(root, path)
        for node in ast.walk(tree):
            if isinstance(node, ast.ImportFrom):
                base = import_base(module, path, node)
                imported.extend((module, base, alias.name, alias.asname or alias.name) for alias in node.names)

    def follow_imports():
        for _ in range(16):
            progressed = False
            for module, base, name, local in imported:
                if (module, local) in symbol_by_module_name or (module, local) in exports or (module, local) in contested:
                    continue
                target = lookup(base, name)
                if target:
                    exports[(module, local)] = target
                    if module in aliases:
                        exports.setdefault((aliases[module], local), target)
                    progressed = True
            if not progressed:
                break

    follow_imports()
    bound = {}
    for module, base, name, local in imported:
        targets = bound.setdefault((module, local), {symbol_by_module_name.get((module, local))})
        targets.add(lookup(base, name))
    for (module, local), targets in bound.items():
        if len(targets - {None}) > 1:
            contested.add((module, local))
            if module in aliases:
                contested.add((aliases[module], local))
    if contested:
        # What was re-exported through a contested name is not known either.
        exports.clear()
        follow_imports()

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

    def named(module, name, bindings):
        if (module, name) in contested:
            return None
        candidates = symbols_by_short.get((module, name), [])
        return bindings.get(name) or (candidates[0]["id"] if len(candidates) == 1 else None)

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
                base = import_base(module, path, node)
                for alias in node.names:
                    local = alias.asname or alias.name
                    target = lookup(base, alias.name)
                    imports.append({"file": relative, "requestedPackage": base, "requestedSymbol": alias.name,
                                    "targetId": target or "", **point(node, starts, text)})
                    if target:
                        local_bindings[local] = target
            elif isinstance(node, ast.Name) and isinstance(node.ctx, ast.Load):
                source_id = source_symbol(relative, getattr(node, "col_offset", 0) + starts[getattr(node, "lineno", 1) - 1])
                target = named(module, node.id, local_bindings)
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
                # The one member it can name: `module.member`, where the module
                # is one this file imported. That is reading an import, which
                # is what the worker does, and not inferring a type.
                owner = local_bindings.get(node.value.id) if isinstance(node.value, ast.Name) else None
                target = lookup(module_of_id[owner], node.attr) if owner in module_of_id else None
                if target:
                    parent = getattr(node, "parent", None)
                    kind = "CALLS_DIRECT" if isinstance(parent, ast.Call) and parent.func is node else (
                        "TYPE_USES" if is_type_position(node) else "REFERENCES")
                    references.append({"file": relative, "sourceId": source_symbol(relative, occurrence["start"]),
                                       "targetId": target, "kind": kind, **occurrence, "text": node.attr})
                    continue
                unresolved.append({"file": relative, "sourceId": source_symbol(relative, occurrence["start"]),
                                   "requestedSymbol": node.attr, "reason": "NAME_NOT_RESOLVED", **occurrence})
            elif is_quoted_annotation(node):
                for name, offset, member in quoted_names(node, text, point(node, starts, text)["start"]):
                    occurrence = span(text, offset, len(name))
                    source_id = source_symbol(relative, offset)
                    target = None if member else named(module, name, local_bindings)
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
