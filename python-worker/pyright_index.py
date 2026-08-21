#!/usr/bin/env python3
"""Emit Kivgraph facts using a Pyright-compatible LSP server.

AST supplies stable source occurrences and the language server resolves them.
No exact edge is emitted unless the server returns a declaration inside the
indexed source set. The bundled AST worker remains the explicit fallback.
"""

import argparse
import ast
import json
import pathlib
import shlex
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


def module_name(root, path):
    relative = pathlib.Path(path).relative_to(root).with_suffix("")
    parts = list(relative.parts)
    if parts and parts[-1] == "__init__":
        parts.pop()
    return ".".join(parts) or root.name


class LSP:
    def __init__(self, command, root):
        fields = shlex.split(command) or ["pyright-langserver"]
        if "--stdio" not in fields:
            fields.append("--stdio")
        self.process = subprocess.Popen(fields, stdin=subprocess.PIPE,
                                         stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.next_id = 1
        self.request("initialize", {"processId": None, "rootUri": uri(root),
                                     "capabilities": {}, "workspaceFolders":
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
    parsed = urllib.parse.urlparse(value or "")
    return urllib.parse.unquote(parsed.path)


def location_offset(location, contents):
    selection = location.get("range", location.get("targetSelectionRange", {}))
    start = selection.get("start", {})
    path = path_from_uri(location.get("uri", ""))
    return path, offset_at(contents.get(path, ""), start.get("line", 0), start.get("character", 0))


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

    contents = {str(path): text for path, (text, _) in parsed.items()}
    lsp = LSP(args.analyzer, root)
    try:
        for path, text in contents.items():
            lsp.notify("textDocument/didOpen", {"textDocument":
                {"uri": uri(path), "languageId": "python", "version": 1, "text": text}})

        symbols = []
        by_location = {}
        for path in files:
            text = contents[str(path)]
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

            def visit(entries, prefix=""):
                for row in entries:
                    name = row.get("name", "").strip()
                    if not name:
                        continue
                    qualified = f"{prefix}.{name}" if prefix else f"{module_name(root, path)}.{name}"
                    symbol_range = row.get("range", row.get("location", {}).get("range", {}))
                    selection = row.get("selectionRange", symbol_range)
                    start = offset_at(text, selection.get("start", {}).get("line", 0), selection.get("start", {}).get("character", 0))
                    span = symbol_range or selection
                    end = offset_at(text, span.get("end", {}).get("line", 0), span.get("end", {}).get("character", 0))
                    kind = symbol_kind(row.get("kind", 0))
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
                    visit(row.get("children", []), qualified)
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
                return (target_id if target_id in symbol_by_id else ""), target_path

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
                if not isinstance(node, ast.Name) or not isinstance(getattr(node, "ctx", None), ast.Load):
                    continue
                target_id, target_path = definition_target(line_starts[node.lineno - 1] + node.col_offset)
                if not target_id:
                    unresolved.append({"file": relative, "sourceId": module_id,
                                       "requestedSymbol": node.id,
                                       "reason": "TARGET_NOT_INDEXED" if target_path else "NAME_NOT_RESOLVED",
                                       "detail": target_path,
                                       **point(node, line_starts, text)})
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
                references.append({"file": relative, "sourceId": module_id,
                                   "targetId": target_id, "kind": kind,
                                   **point(node, line_starts, text), "text": node.id})

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
