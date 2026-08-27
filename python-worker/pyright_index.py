#!/usr/bin/env python3
"""Emit Kivgraph facts using a Pyright-compatible LSP server.

AST supplies stable source occurrences and the language server resolves them.
No exact edge is emitted unless the server returns a declaration inside the
indexed source set. The bundled AST worker remains the explicit fallback.
"""

import argparse
import ast
import json
import os
import os.path
import pathlib
import shlex
import shutil
import subprocess
import sys
import urllib.parse


def uri(path):
    return pathlib.Path(path).resolve().as_uri()


def starts(text):
    result = [0]
    for index, char in enumerate(text):
        if char == "\n":
            result.append(index + 1)
    return result


def utf16_column(text, offset):
    line_start = text.rfind("\n", 0, offset) + 1
    return len(text[line_start:offset].encode("utf-16-le")) // 2


def offset_at(text, line, character):
    line_starts = starts(text)
    if line < 0 or line >= len(line_starts):
        return 0
    index = line_starts[line]
    remaining = character
    while index < len(text) and text[index] != "\n" and remaining > 0:
        remaining -= 2 if ord(text[index]) > 0xFFFF else 1
        index += 1
    return index


def point(node, line_starts, text):
    line = max(1, getattr(node, "lineno", 1))
    column = max(0, getattr(node, "col_offset", 0))
    start = line_starts[min(line - 1, len(line_starts) - 1)] + column
    end_line = max(line, getattr(node, "end_lineno", line))
    end_column = max(column, getattr(node, "end_col_offset", column))
    end = line_starts[min(end_line - 1, len(line_starts) - 1)] + end_column
    return {"startLine": line, "startColumn": column, "start": start,
            "endLine": end_line, "endColumn": end_column, "end": end}


# attribute_point spans the member name of `box.get`, not the whole expression:
# the occurrence being resolved is `get`, and its evidence has to say where that
# is. Python records the end of the attribute node at the end of its name, so
# the name starts that many characters earlier on that line.
def attribute_point(node, line_starts, text):
    end_line = max(1, getattr(node, "end_lineno", getattr(node, "lineno", 1)))
    end_column = max(0, getattr(node, "end_col_offset", 0))
    column = max(0, end_column - len(node.attr))
    base = line_starts[min(end_line - 1, len(line_starts) - 1)]
    return {"startLine": end_line, "startColumn": column, "start": base + column,
            "endLine": end_line, "endColumn": end_column, "end": base + end_column}


def module_name(root, path):
    relative = pathlib.Path(path).relative_to(root).with_suffix("")
    parts = list(relative.parts)
    if parts and parts[-1] == "__init__":
        parts.pop()
    return ".".join(parts) or root.name


def split_command(command):
    """Split a configured command line the way its own platform would.

    shlex is a POSIX shell lexer, and a POSIX shell treats a backslash as an
    escape. On Windows it is the path separator, so the default mode deletes
    every one of them: measured on the host, `C:\\tools\\node\\pyright-langserver`
    comes back as `C:toolsnodepyright-langserver`. Any configured analyser on a
    Windows machine was therefore unrunnable, and the loader reported it as the
    analyser being unavailable rather than as the path having been eaten.

    posix=False keeps the backslashes and also keeps the quotation marks inside
    the token, so a quoted path -- which is the normal case under
    "Program Files" -- has to be unwrapped here.
    """
    if os.name != "nt":
        return shlex.split(command)
    fields = []
    for field in shlex.split(command, posix=False):
        if len(field) > 1 and field[0] == '"' and field[-1] == '"':
            field = field[1:-1]
        fields.append(field)
    return fields


def resolve_program(program):
    """Return the file a platform would actually run for this name.

    CreateProcess searches PATH but does not append PATHEXT, so a bare
    `pyright-langserver` raises FileNotFoundError on Windows even though
    `pyright-langserver.CMD` is right there on the PATH -- which is exactly how
    npm installs it. shutil.which does consult PATHEXT, and the path it
    returns starts fine.

    A name that already resolves, or that names an existing file, is left
    alone: the caller may have configured something which is not on the PATH at
    all.
    """
    if os.path.isfile(program):
        return program
    return shutil.which(program) or program


class LSP:
    def __init__(self, command, root):
        fields = split_command(command) or ["pyright-langserver"]
        if "--stdio" not in fields:
            fields.append("--stdio")
        fields[0] = resolve_program(fields[0])
        self.process = subprocess.Popen(fields, stdin=subprocess.PIPE,
                                         stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.next_id = 1
        # Hierarchical document symbols, declared: without this capability
        # Pyright answers with the flat SymbolInformation[] shape, which carries
        # no children, and visit() below derives every qualified name from the
        # module prefix. Two methods of two classes in one file then collapse
        # onto one qualified name -- `Vehicle.drive` and `Car.drive` both become
        # `pkg.models.drive` -- and the canonical set is refused for defining a
        # symbol twice.
        self.request("initialize", {"processId": None, "rootUri": uri(root),
                                     "capabilities": {"textDocument": {"documentSymbol": {
                                         "hierarchicalDocumentSymbolSupport": True}}},
                                     "workspaceFolders":
                                     [{"uri": uri(root), "name": root.name}]})
        self.notify("initialized", {})

    def notify(self, method, params):
        self._write({"jsonrpc": "2.0", "method": method, "params": params})

    def request(self, method, params):
        request_id = self.next_id
        self.next_id += 1
        self._write({"jsonrpc": "2.0", "id": request_id, "method": method, "params": params})
        while True:
            message = self._read()
            if message.get("id") != request_id:
                continue
            if "error" in message:
                raise RuntimeError(f"LSP {method}: {message['error']}")
            return message.get("result")

    def _write(self, message):
        body = json.dumps(message, separators=(",", ":")).encode()
        self.process.stdin.write(f"Content-Length: {len(body)}\r\n\r\n".encode() + body)
        self.process.stdin.flush()

    def _read(self):
        headers = {}
        while True:
            line = self.process.stdout.readline()
            if not line:
                raise RuntimeError("Python language server closed its output")
            line = line.decode("ascii").strip()
            if not line:
                break
            key, value = line.split(":", 1)
            headers[key.lower()] = value.strip()
        body = self.process.stdout.read(int(headers["content-length"]))
        return json.loads(body)

    def close(self):
        try:
            self.notify("exit", None)
        finally:
            self.process.kill()


def position(text, offset):
    return {"line": text.count("\n", 0, offset),
            "character": utf16_column(text, offset)}


def path_from_uri(value):
    """Turn a file URI back into a path this platform can open.

    The URI path is not the file path on Windows. `file:///c%3A/x/main.py`
    parses to `/c%3A/x/main.py`: a leading slash before the drive, the colon
    percent-encoded, and the separators the wrong way round. Nothing in
    `contents` matches that, so every declaration the language server returned
    looked like one outside the indexed source set and the worker emitted no
    exact edge at all -- measured on the host as 23 symbols and 0 references,
    with pyright answering correctly the whole time and 63 occurrences
    recorded as TARGET_NOT_INDEXED against a detail of
    `\\c:\\...\\contracts.pyi`.

    urllib.request.url2pathname is the documented inverse and is not enough on
    its own: it looks for the drive before unquoting, so `%3A` hides it and the
    leading slash survives. Unquoting first is what makes the drive visible,
    and it is safe here because a file URI encodes every literal percent.
    """
    parsed = urllib.parse.urlparse(value or "")
    path = urllib.parse.unquote(parsed.path or "")
    if os.name == "nt":
        path = path.replace("/", os.sep)
        # `\c:\x` -- a rooted path whose first component is a drive. The slash
        # is the URI's, not the file system's.
        if len(path) > 2 and path[0] == os.sep and path[2] == ":":
            path = path[1:]
    return os.path.normpath(path) if path else path


def compare_path(path):
    """The one spelling this worker compares paths by.

    Windows file names are case-insensitive and the language server does not
    use the same case we do: it answers `c:\\kivgraph\\...` where the walk
    produced `C:\\kivgraph\\...`. Comparing those as strings is comparing
    two spellings of one file and concluding they are two files.

    On POSIX normcase is the identity, so this is abspath and nothing else.
    """
    return os.path.normcase(os.path.abspath(path))


def location_offset(location, contents):
    selection = location.get("range", location.get("targetSelectionRange", {}))
    start = selection.get("start", {})
    path = path_from_uri(location.get("uri", ""))
    text = contents.get(compare_path(path), "") if path else ""
    return path, offset_at(text, start.get("line", 0), start.get("character", 0))


def is_type_position(node):
    parent = getattr(node, "parent", None)
    if isinstance(parent, (ast.AnnAssign, ast.arg)) and getattr(parent, "annotation", None) is node:
        return True
    if isinstance(parent, (ast.FunctionDef, ast.AsyncFunctionDef)) and (
            getattr(parent, "returns", None) is node or
            any(getattr(argument, "annotation", None) is node
                for argument in [*parent.args.posonlyargs, *parent.args.args,
                                 *parent.args.kwonlyargs] + ([parent.args.vararg] if parent.args.vararg else []) +
                                ([parent.args.kwarg] if parent.args.kwarg else []))):
        return True
    if isinstance(parent, ast.ClassDef) and node in parent.bases:
        return True
    return False


def symbol_kind(value):
    # LSP SymbolKind values are fixed by the protocol. Keeping this mapping
    # here makes symbol IDs stable across Pyright and BasedPyright.
    return {2: "module", 3: "namespace", 5: "class", 6: "method",
            7: "property", 8: "field", 9: "constructor", 10: "enum",
            11: "interface", 12: "function", 13: "variable",
            14: "constant", 22: "enum_member", 23: "struct",
            26: "type_parameter"}.get(value, "symbol")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--analyzer", default="pyright-langserver")
    parser.add_argument("--include-tests", action="store_true")
    parser.add_argument("--include-generated", action="store_true")
    parser.add_argument("--include-external", action="store_true")
    args = parser.parse_args()
    root = pathlib.Path(args.root).resolve()
    files = []
    parsed = {}
    for path in sorted(root.rglob("*")):
        if not path.is_file() or path.suffix not in (".py", ".pyi"):
            continue
        relative_parts = path.relative_to(root).parts
        if any(part in {".git", ".venv", "venv", "__pycache__", ".tox"} for part in relative_parts):
            continue
        if not args.include_tests and any(part in {"tests", "test", "integration_test"} for part in relative_parts):
            continue
        if not args.include_generated and any(part in {"build", "dist"} or part.endswith(("_generated.py", ".generated.py")) for part in relative_parts):
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        try:
            tree = ast.parse(text, filename=str(path))
            for parent in ast.walk(tree):
                for child in ast.iter_child_nodes(parent):
                    child.parent = parent
        except SyntaxError:
            tree = None
        files.append(path)
        parsed[path] = (text, tree)

    # Keyed by the comparison spelling, because the only reader that looks a
    # path up here is holding one the language server chose rather than one
    # this walk produced.
    contents = {compare_path(path): text for path, (text, _) in parsed.items()}
    lsp = LSP(args.analyzer, root)
    try:
        for path, (text, _) in parsed.items():
            lsp.notify("textDocument/didOpen", {"textDocument":
                {"uri": uri(path), "languageId": "python", "version": 1, "text": text}})

        symbols = []
        by_location = {}
        for path in files:
            text = contents[compare_path(path)]
            rows = lsp.request("textDocument/documentSymbol", {"textDocument": {"uri": uri(path)}}) or []
            relative = path.relative_to(root).as_posix()

            module = module_name(root, path)
            module_id = f"{relative}\x00{module}\x00module"
            module_symbol = {"id": module_id, "file": relative,
                             "name": module.split(".")[-1],
                             "qualifiedName": module, "kind": "module",
                             "exported": True, "signature": f"module {module}",
                             "startLine": 1, "startColumn": 0, "start": 0,
                             "endLine": text.count("\n") + 1,
                             "endColumn": utf16_column(text, len(text)), "end": len(text)}
            symbols.append(module_symbol)
            by_location[(str(path), 0)] = module_id

            # inside_callable says the entries being visited are the body of a
            # function or a method. Their variables are locals and parameters:
            # nothing outside can name them, which is the same rule the Go path
            # applies to a declaration that no path from the package scope
            # reaches. Publishing them made a function hold edges to its own
            # locals, and every one of those was an EXACT edge the source does
            # not contain.
            def visit(entries, prefix="", inside_callable=False):
                for row in entries:
                    name = row.get("name", "").strip()
                    if not name:
                        continue
                    kind = symbol_kind(row.get("kind", 0))
                    if inside_callable and kind in {"variable", "constant", "type_parameter"}:
                        continue
                    qualified = f"{prefix}.{name}" if prefix else f"{module_name(root, path)}.{name}"
                    symbol_range = row.get("range", row.get("location", {}).get("range", {}))
                    selection = row.get("selectionRange", symbol_range)
                    start = offset_at(text, selection.get("start", {}).get("line", 0), selection.get("start", {}).get("character", 0))
                    span = symbol_range or selection
                    end = offset_at(text, span.get("end", {}).get("line", 0), span.get("end", {}).get("character", 0))
                    symbol_id = f"{relative}\x00{qualified}\x00{kind}"
                    symbol = {"id": symbol_id, "file": relative, "name": name,
                              "qualifiedName": qualified, "kind": kind,
                              "exported": not name.startswith("_"),
                              "signature": f"{kind} {qualified}"}
                    symbol.update({"startLine": text.count("\n", 0, start) + 1,
                                  "startColumn": utf16_column(text, start), "start": start,
                                  "endLine": text.count("\n", 0, end) + 1,
                                  "endColumn": utf16_column(text, end), "end": end})
                    symbols.append(symbol)
                    by_location[(str(path), start)] = symbol_id
                    visit(row.get("children", []), qualified,
                          inside_callable or kind in {"function", "method", "constructor", "property"})
            visit(rows)

        symbol_by_id = {row["id"]: row for row in symbols}
        function_symbol_ids = {row["id"] for row in symbols
                               if row["kind"] in {"function", "method", "constructor"}}

        def target_at(path, offset):
            try:
                relative = pathlib.Path(path).resolve().relative_to(root).as_posix()
            except ValueError:
                return ""
            candidates = [row for row in symbols
                          if row["file"] == relative
                          and row["start"] <= offset <= row["end"]]
            if not candidates:
                return ""
            candidates.sort(key=lambda row: (row["end"] - row["start"], row["qualifiedName"]))
            return candidates[0]["id"]
        references, imports, unresolved = [], [], []
        for path in files:
            text, tree = parsed[path]
            relative = path.relative_to(root).as_posix()
            if tree is None:
                unresolved.append({"file": relative, "reason": "PARSE_ERROR", "start": 0, "startLine": 1})
                continue
            module_id = next((row["id"] for row in symbols if row["file"] == relative and row["kind"] == "module"), "")
            line_starts = starts(text)

            def definition_target(offset):
                result = lsp.request("textDocument/definition", {"textDocument": {"uri": uri(path)},
                    "position": position(text, offset)})
                locations = result if isinstance(result, list) else ([result] if result else [])
                if not locations:
                    return "", ""
                target_path, target_offset = location_offset(locations[0], contents)
                target_id = target_at(target_path, target_offset)
                if target_id not in symbol_by_id:
                    return "", target_path
                # A definition that lands inside a file but on no declaration
                # resolves to the module, because that is the only symbol whose
                # range covers the whole file. That is not a resolved target: it
                # is a target we could not identify -- an `@overload`ed `def`
                # among others -- and publishing the module instead would be an
                # EXACT edge earned by being the only candidate left.
                if symbol_by_id[target_id]["kind"] == "module" and target_offset > 0:
                    return "", target_path
                return target_id, target_path

            def import_name_offset(alias):
                name = alias.asname or alias.name
                alias_line = max(1, getattr(alias, "lineno", node.lineno))
                alias_column = max(0, getattr(alias, "col_offset", node.col_offset))
                search_start = line_starts[min(alias_line - 1, len(line_starts) - 1)] + alias_column
                line_end = text.find("\n", search_start)
                if line_end < 0:
                    line_end = len(text)
                offset = text.find(name, search_start, line_end)
                if offset < 0:
                    offset = text.find(name, line_starts[node.lineno - 1] + node.col_offset)
                return offset if offset >= 0 else search_start

            for node in ast.walk(tree):
                if isinstance(node, (ast.Import, ast.ImportFrom)):
                    package = node.module or ""
                    for alias in node.names:
                        target_id, _ = definition_target(import_name_offset(alias))
                        imports.append({"file": relative, "sourceId": module_id,
                                        "requestedPackage": package or alias.name,
                                        "requestedSymbol": alias.name,
                                        "targetId": target_id,
                                        **point(node, line_starts, text)})
                # An attribute is an occurrence like any other. Skipping them
                # made `box.get()` invisible: no edge, and no unresolved row
                # either, so find_references answered COMPLETE over a call the
                # source makes. Pyright resolves a member at its name, so the
                # occurrence is asked exactly like a bare name and refused the
                # same way when it does not resolve.
                if isinstance(node, ast.Name) and isinstance(getattr(node, "ctx", None), ast.Load):
                    occurrence = point(node, line_starts, text)
                    requested = node.id
                elif isinstance(node, ast.Attribute) and isinstance(getattr(node, "ctx", None), ast.Load):
                    occurrence = attribute_point(node, line_starts, text)
                    requested = node.attr
                else:
                    continue
                target_id, target_path = definition_target(occurrence["start"])
                if not target_id:
                    # The enclosing declaration owns its failures too: a row
                    # attributed to the module says the file could not resolve
                    # something, which is a coarser fact than the one observed.
                    unresolved.append({"file": relative,
                                       "sourceId": target_at(str(path), occurrence["start"]) or module_id,
                                       "requestedSymbol": requested,
                                       "reason": "TARGET_NOT_INDEXED" if target_path else "NAME_NOT_RESOLVED",
                                       "detail": target_path,
                                       **occurrence})
                    continue
                parent = getattr(node, "parent", None)
                kind = "REFERENCES"
                if isinstance(parent, ast.Call) and parent.func is node:
                    kind = "CALLS_DIRECT"
                elif isinstance(parent, ast.ClassDef) and node in parent.bases:
                    kind = "EXTENDS"
                elif isinstance(parent, ast.Return):
                    kind = "RETURNS_FUNCTION" if target_id in function_symbol_ids else "REFERENCES"
                elif is_type_position(node):
                    kind = "TYPE_USES"
                elif isinstance(parent, (ast.Assign, ast.AnnAssign, ast.NamedExpr)):
                    kind = "ASSIGNS_FUNCTION" if target_id in function_symbol_ids else "REFERENCES"
                elif isinstance(parent, ast.Call) and node in parent.args:
                    kind = "PASSES_AS_CALLBACK" if target_id in function_symbol_ids else "REFERENCES"
                # The enclosing declaration, not the module: a reference belongs
                # to whoever makes it. Attributing every one to the module made
                # find_references answer at file granularity, and it fabricated
                # relations that the source does not contain -- `class
                # ElectricVehicle(Vehicle)` was published as the module itself
                # extending Vehicle, as an EXACT edge.
                source_id = target_at(str(path), occurrence["start"]) or module_id
                # A declaration does not reference itself: the name in `def
                # drive(self)` resolves to `drive`, and publishing that is a
                # self loop the source does not contain. The Dart path already
                # refuses the same shape.
                if source_id == target_id:
                    continue
                references.append({"file": relative, "sourceId": source_id,
                                   "targetId": target_id, "kind": kind,
                                   **occurrence, "text": requested})

        result = {"version": 1, "authoritative": True, "analyzer": args.analyzer, "repository": root.name,
                  "language": "python", "package": {"name": root.name, "rootPath": str(root)},
                  "files": [{"path": path.relative_to(root).as_posix()} for path in files],
                  "symbols": symbols, "references": references, "imports": imports,
                  "unresolved": unresolved}
        json.dump(result, sys.stdout, separators=(",", ":"))
        sys.stdout.write("\n")
    finally:
        lsp.close()


if __name__ == "__main__":
    main()
