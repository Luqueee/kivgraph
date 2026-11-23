"""Recapture the native arm of one frozen corpus, from inside an Oh My Pi `eval` cell.

The native arm is the host's own tool output, verbatim, so only the host can
produce it: this script drives `tool.grep` and `tool.read` and does nothing a
person would not do by hand. Run it in a session started with
KIVGRAPH_DISABLE_HOOK=1, or the search gate refuses the grep it needs.

    %load benchmarks/mcp-token-cost/recapture.py
    await recapture("cobra", server="/path/to/kivgraph")

It expects the corpus prepared first (see README.md):

    go run ./benchmarks/mcp-token-cost/prepare --corpus benchmarks/mcp-token-cost/corpora/cobra --server /path/to/kivgraph

and ends with the harness having written results.json and report.md.
"""

import json
import os
import re

STATE = "/tmp/kivgraph-corpora"
BENCHMARK = "benchmarks/mcp-token-cost"
UNMET = re.compile(r"no captured host read for \d+ range\(s\).*?:\n(.*)", re.S)


def _text(result):
    return result["text"] if isinstance(result, dict) else str(result)


def _measure(corpus, server, check=False):
    """Run the harness against the prepared corpus; returns (ok, output)."""
    home = f"{STATE}/{corpus}/home"
    flag = " --check" if check else ""
    command = (
        f"HOME={home} KIVGRAPH_TELEMETRY=0 go run ./{BENCHMARK} --server {server} "
        f"--dir {BENCHMARK}/corpora/{corpus} --repository {corpus}{flag} 2>&1"
    )
    try:
        return True, sh(command)
    except Exception as failure:  # ShellError carries the harness output
        return False, getattr(failure, "output", str(failure))


async def recapture(corpus, server="kivgraph", rounds=5):
    directory = f"{BENCHMARK}/corpora/{corpus}"
    checkout = f"{STATE}/{corpus}/src"
    if not os.path.isdir(checkout):
        raise RuntimeError(f"{checkout} does not exist: run the prepare command first")
    questions = json.load(open(f"{directory}/questions.json"))["questions"]

    # 1. The searches. Every one is retaken, because a capture is tied to the tree.
    os.makedirs(f"{directory}/native", exist_ok=True)
    for question in questions:
        native = question["native"]
        result = await tool.grep(pattern=native["pattern"], path=checkout)
        with open(f"{directory}/{native['capture']}", "w") as out:
            out.write(_text(result))
        log(f"grep {question['symbol']}")

    # 2. The reads. The harness names the ranges it needs; the first round asks
    # for all of them, because the capture file starts empty.
    reads_path = f"{directory}/native/reads.json"
    with open(reads_path, "w") as out:
        out.write("{}\n")
    for round_number in range(rounds):
        ok, output = _measure(corpus, server)
        if ok:
            print(output)
            return
        found = UNMET.search(output)
        if not found:
            raise RuntimeError(output)
        ranges = [line for line in found.group(1).splitlines() if re.match(r"^\S+:\d+-\d+$", line)]
        texts = {}
        for key in ranges:
            path, span = key.rsplit(":", 1)
            texts[key] = _text(await tool.read(path=f"{checkout}/{path}:{span}"))
        with open("/tmp/recapture-texts.json", "w") as out:
            json.dump(texts, out)
        counted = json.loads(sh(f"go run ./{BENCHMARK}/count < /tmp/recapture-texts.json"))
        captured = json.load(open(reads_path))
        for key in ranges:
            captured[key] = {"file": key.rsplit(":", 1)[0], "tokens": counted[key]}
        with open(reads_path, "w") as out:
            json.dump(captured, out, indent=2, sort_keys=True)
            out.write("\n")
        log(f"round {round_number + 1}: captured {len(ranges)} range(s)")
    raise RuntimeError(f"still asking for ranges after {rounds} rounds")
