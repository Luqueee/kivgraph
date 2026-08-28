#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

python3 - <<'PY'
import json
from pathlib import Path

manifest_path = Path("testdata/semantic-coverage/manifest.json")
manifest = json.loads(manifest_path.read_text())
if manifest.get("version") != 1:
    raise SystemExit("unsupported semantic coverage manifest version")
expected = {"go", "typescript", "python", "dart", "java"}
actual = set(manifest.get("languages", {}))
if actual != expected:
    raise SystemExit(f"coverage languages = {sorted(actual)}, want {sorted(expected)}")
for language, entry in manifest["languages"].items():
    for key in ("analyzer", "fixture", "test", "capabilities"):
        if not entry.get(key):
            raise SystemExit(f"{language} coverage entry has no {key}")
    fixture = Path(entry["fixture"])
    test = Path(entry["test"])
    if not fixture.exists():
        raise SystemExit(f"{language} fixture does not exist: {fixture}")
    if not test.exists():
        raise SystemExit(f"{language} test does not exist: {test}")
    if len(set(entry["capabilities"])) != len(entry["capabilities"]):
        raise SystemExit(f"{language} coverage capabilities contain duplicates")
    if len(entry["capabilities"]) < 10:
        raise SystemExit(f"{language} coverage matrix is too small")
print(f"semantic coverage manifest: {len(actual)} languages, all entries present")
PY

go test ./internal/goloader ./internal/facts ./internal/indexer ./internal/indexing ./internal/pythonloader ./internal/dartloader ./internal/scip ./internal/javaloader

pnpm --dir ts-worker check
pnpm --dir ts-worker build

python3 -m py_compile python-worker/index.py python-worker/pyright_index.py

if [[ -z "${KIVGRAPH_PYRIGHT_LANGSERVER:-}" ]]; then
  if command -v pyright-langserver >/dev/null 2>&1; then
    KIVGRAPH_PYRIGHT_LANGSERVER=$(command -v pyright-langserver)
  elif command -v pnpm >/dev/null 2>&1; then
    KIVGRAPH_PYRIGHT_LANGSERVER=$(pnpm dlx --package pyright sh -c 'command -v pyright-langserver')
  else
    echo "Pyright language server is required for exact Python coverage" >&2
    exit 1
  fi
fi
KIVGRAPH_PYRIGHT_LANGSERVER="$KIVGRAPH_PYRIGHT_LANGSERVER" \
  go test ./internal/pythonloader -run 'TestRunExactPyright' -count=1

if ! command -v dart >/dev/null 2>&1; then
  echo "Dart SDK is required for exact Dart coverage" >&2
  exit 1
fi
KIVGRAPH_DART_ROOT="${KIVGRAPH_DART_ROOT:-$repo_root/testdata/dart/advanced}" \
  go test ./internal/dartloader -run 'TestRun(Fixture|AgainstConfiguredDartProject)' -count=1

# Java's exact coverage needs both halves of the toolchain: scip-java drives the
# repository's own build, so an indexer without a build tool indexes nothing.
# The hermetic tests above already ran against a recorded index; these two are
# what prove the recording still describes the fixture.
if ! command -v scip-java >/dev/null 2>&1; then
  echo "scip-java is required for exact Java coverage" >&2
  exit 1
fi
if ! command -v mvn >/dev/null 2>&1; then
  echo "Maven is required for exact Java coverage: scip-java runs the project build" >&2
  exit 1
fi
go test ./internal/javaloader -run 'TestRun(AgainstTheFixture)|TestRecordedIndexMatchesTheToolchain' -count=1

find python-worker/__pycache__ -type f -name '*.pyc' -delete 2>/dev/null || true
rmdir python-worker/__pycache__ 2>/dev/null || true
