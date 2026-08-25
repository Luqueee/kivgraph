/**
 * The MCP clients `kivgraph mcp install` can write an entry for, declared once
 * because two surfaces name them: the compatibility line under the hero and the
 * integrations band further down. The list is the one in
 * `internal/integrations/integrations.go`, which is the only place that decides
 * what the selector offers -- a sixth client on this page would be a promise the
 * binary cannot keep.
 *
 * `page` is the deepest documentation that exists for the target. Three of them
 * have a page of their own; Claude Desktop and OpenCode are covered by the
 * clients table, which is where their configuration paths live.
 */
export interface Client {
  /** The `--target` value, exactly as the CLI accepts it. */
  target: string;
  /** The product's own name, for a reader rather than for the flag. */
  label: string;
  /** Where its configuration is documented. */
  page: string;
  /** Set where the target is narrower than the other four. */
  note?: string;
}

export const CLIENTS: readonly Client[] = [
  { target: "claude-code", label: "Claude Code", page: "/mcp/claude-code/" },
  { target: "codex", label: "Codex", page: "/mcp/codex/" },
  { target: "opencode", label: "OpenCode", page: "/mcp/clients/" },
  {
    target: "claude-desktop",
    label: "Claude Desktop",
    page: "/mcp/clients/",
    note: "user scope only, and the one target with no local skill install",
  },
  { target: "oh-my-pi", label: "Oh My Pi", page: "/mcp/oh-my-pi/" },
];
