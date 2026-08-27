// Managed by Kivgraph. `kivgraph hook remove --target opencode` deletes it, and
// `kivgraph hook install` rewrites it; edits here are lost.
//
// Claude Code and Codex run `kivgraph hook run` themselves and read its verdict
// off stdout. OpenCode cannot: `tool.execute.before` returns `Promise<void>`
// and the only way to stop a tool is to throw. So this file is the translation
// and nothing else -- it forwards the same payload to the same command and
// turns a refusal into an Error.

import { spawn } from "node:child_process"

const EXECUTABLE = "__KIVGRAPH_EXECUTABLE__"

// DEADLINE_MS bounds a gate that never answers.
//
// The command's own budget is far smaller -- single-digit milliseconds against
// a running daemon -- so this is not a latency figure, it is the ceiling on how
// long a wedged process can hold up a tool call before the plugin gives up and
// lets it run.
const DEADLINE_MS = 2000

export const KivgraphGate = async ({ directory }) => ({
  "tool.execute.before": async (input, output) => {
    const refusal = await refusalFor({
      hook_event_name: "PreToolUse",
      cwd: directory,
      tool_name: input?.tool,
      tool_input: output?.args ?? {},
    })
    if (refusal) throw new Error(refusal)
  },
})

// refusalFor returns the reason the gate refused, and null for everything else.
//
// Every failure here is a null on purpose. A missing binary, a timeout, an
// unreadable answer and an ordinary allow are the same event to a caller: the
// gate formed no opinion. A plugin that threw on its own bugs would break every
// tool call in the session, which is a far worse failure than the tokens it was
// trying to save.
function refusalFor(payload) {
  return new Promise((resolve) => {
    let child
    try {
      child = spawn(EXECUTABLE, ["hook", "run"], { stdio: ["pipe", "pipe", "ignore"] })
    } catch {
      resolve(null)
      return
    }

    let answer = ""
    let settled = false
    const finish = (value) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      try {
        child.kill()
      } catch {}
      resolve(value)
    }
    const timer = setTimeout(() => finish(null), DEADLINE_MS)

    child.on("error", () => finish(null))
    child.stdout.on("data", (chunk) => {
      answer += chunk
    })
    child.on("close", () => {
      // An allow writes nothing at all, so an empty answer fails to parse
      // and lands in the catch, which is exactly where it belongs.
      try {
        const decision = JSON.parse(answer)?.hookSpecificOutput
        finish(
          decision?.permissionDecision === "deny" ? decision.permissionDecisionReason : null,
        )
      } catch {
        finish(null)
      }
    })

    try {
      child.stdin.end(JSON.stringify(payload))
    } catch {
      finish(null)
    }
  })
}
