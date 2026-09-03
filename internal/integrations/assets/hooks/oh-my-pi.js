// Managed by Kivgraph. `kivgraph hook remove --target oh-my-pi` deletes it,
// and `kivgraph hook install` rewrites it; edits here are lost.

// Oh My Pi loads this module as an extension and calls its `tool_call` handlers
// before a tool executes. The handler forwards the same payload understood by
// `kivgraph hook run` and returns a block result only when the gate refuses the
// call. Every local failure is treated as no opinion so a broken installation
// never turns into a gate that blocks unrelated work.

import { spawn } from "node:child_process"

const EXECUTABLE = __KIVGRAPH_EXECUTABLE__

// DEADLINE_MS bounds a gate that never answers. The command's own budget is
// smaller; this is only the ceiling on how long an extension can hold a tool
// call open before it gives up and lets it run.
const DEADLINE_MS = 2000

export default function kivgraphGate(pi) {
  pi.on("tool_call", async (event, ctx) => {
    const refusal = await refusalFor({
      hook_event_name: "PreToolUse",
      cwd: ctx?.cwd ?? process.cwd(),
      tool_name: event?.toolName,
      tool_input: event?.input ?? {},
    })
    if (refusal) return { block: true, reason: refusal }
  })
}

// refusalFor returns the reason the gate refused, and null for everything else.
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
    child.stdin.on("error", () => finish(null))
    child.stdout.on("error", () => finish(null))
    child.stdout.on("data", (chunk) => {
      answer += chunk
    })
    child.on("close", () => {
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
