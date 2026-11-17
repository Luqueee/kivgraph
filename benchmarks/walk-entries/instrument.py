#!/usr/bin/env python3
"""Instrument a throwaway worktree so that every walk of a pass is counted.

    instrument.py <worktree>

The worktree is changed in place and must not be the repository you work in.
It replaces `filepath.WalkDir` and `os.ReadDir` in every non-test Go file under
`internal/` and `cmd/` with the counting versions in walkcount.go.txt, which name
each walk by the function that called it; makes the `edit-frequency` harness reset
the counters before a pass and print them after it; and adds an environment switch,
KIVGRAPH_NO_CACHETAG, that turns the cache-directory rule off so that one binary
measures both sides of it.

Nothing here is meant to be committed to the code it instruments.
"""
import os
import pathlib
import re
import subprocess
import sys

IMPORT = '\t"github.com/Luqueee/kivgraph/internal/walkcount"\n'


def main(worktree: pathlib.Path) -> None:
    here = pathlib.Path(__file__).parent
    package = worktree / "internal" / "walkcount"
    package.mkdir(exist_ok=True)
    (package / "walkcount.go").write_text((here / "walkcount.go.txt").read_text())

    changed = []
    for root in ("internal", "cmd"):
        for path in (worktree / root).rglob("*.go"):
            if path.name.endswith("_test.go") or "walkcount" in path.parts:
                continue
            text = path.read_text()
            new = text.replace("filepath.WalkDir(", "walkcount.WalkDir(").replace("os.ReadDir(", "walkcount.ReadDir(")
            if new == text:
                continue
            if "import (\n" not in new:
                sys.exit(f"{path}: no import block to extend")
            new = new.replace("import (\n", "import (\n" + IMPORT, 1)
            path.write_text(new)
            changed.append(path)

    cachedir = worktree / "internal" / "workspace" / "cachedir.go"
    text = cachedir.read_text()
    marker = "func IsCacheDirectory(directory string) bool {\n"
    assert marker in text
    text = text.replace(marker, marker + '\tif os.Getenv("KIVGRAPH_NO_CACHETAG") != "" {\n\t\treturn false\n\t}\n')
    cachedir.write_text(text)

    harness = worktree / "benchmarks" / "edit-frequency" / "main.go"
    text = harness.read_text()
    marker = "func measurePass(ctx context.Context, base indexing.FullOptions, step int, label, edited string) (passCost, error) {\n"
    assert marker in text
    text = text.replace(marker, marker + "\twalkcount.Reset()\n\tdefer walkcount.Dump(os.Stderr, fmt.Sprintf(\"%s %d\", label, step))\n")
    text = text.replace("import (\n", "import (\n" + IMPORT, 1)
    harness.write_text(text)

    library = subprocess.run(["scripts/fetch-ladybug.sh"], cwd=worktree, capture_output=True, text=True, check=True).stdout.strip()
    env = dict(os.environ, CGO_ENABLED="1", CGO_CFLAGS=f"-I{library}", CGO_LDFLAGS=f"-L{library}")
    # Removing the last use of `os` or `filepath` from a file leaves an unused
    # import; the compiler names each one, so delete exactly those.
    for _ in range(20):
        build = subprocess.run(["go", "build", "-tags", "ladybug", "./..."], cwd=worktree, env=env, capture_output=True, text=True)
        unused = re.findall(r"^(\S+\.go):(\d+):\d+: \"([^\"]+)\" imported and not used", build.stderr, re.M)
        if not unused:
            if build.returncode != 0:
                sys.exit(build.stderr)
            break
        by_file = {}
        for name, line, _ in unused:
            by_file.setdefault(worktree / name, set()).add(int(line))
        for path, lines in by_file.items():
            kept = [row for number, row in enumerate(path.read_text().splitlines(keepends=True), 1) if number not in lines]
            path.write_text("".join(kept))
    else:
        sys.exit("unused imports did not settle")
    print(f"instrumented {len(changed)} files; builds with -tags ladybug")


if __name__ == "__main__":
    main(pathlib.Path(sys.argv[1]).resolve())
