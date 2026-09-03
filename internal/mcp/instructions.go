package mcp

// toolVisibilityInstructions is shared by both availability states: even a
// cold server can expose index_project. Keep it aligned with the installed
// skill; visibility is client/model behavior, not a server-injected chat event.
const toolVisibilityInstructions = `Before every Kivgraph MCP tool call, send a brief user-visible chat preamble in the conversation's language: Kivgraph · <tool> — <target>: <purpose>. Name the exact tool, the symbol/file/repository/scope being queried, and the question the call will answer. For parallel calls, one preamble may list a separate line for each call; announce each repeated call too. State intent, not success; do not dump arguments or secrets. This notice is not approval for index_project.`

// serverInstructions carries routing and visibility guidance across schema
// deferral in clients that consume MCP connection instructions.
//
// Oh My Pi mounts each MCP tool as a device whose documentation is read on
// demand; Claude Code defers tool schemas behind its tool search and injects
// this field at session start. In both, what a model sees of Kivgraph before it
// calls anything is a list of tool names and this paragraph. Claude Code
// truncates it at 2 KB, so the decisive sentence goes first.
//
// Nothing here is derived from the graph. A snapshot id or a symbol count would
// rewrite bytes of the client's system prompt on every re-index and invalidate
// its prompt cache, which is how tokensave's own call budget ended up costing
// more than the calls it discouraged (their issue #260). Volatile facts live in
// `graph_status`, which is a call.
//
// The last paragraph is the part most servers omit: where this one loses. A tool
// that claims to win everywhere gets called where it does not and spends the
// call twice.
const serverInstructions = toolVisibilityInstructions + `

Kivgraph answers "what breaks if I change this" from a published code graph over Go, TypeScript, Rust, Python, Dart, Java and C#. Go, TypeScript and Rust edges are type-checked; Dart edges are resolved by Dart Analysis Server; Java and C# edges come from SCIP indexes scip-java and scip-dotnet emit through javac and Roslyn; Python uses exact semantic facts when a configured analyzer provides them and CANDIDATE facts in its bundled AST fallback. Before grepping or reading files to find callers, references or impact, call find_references or get_blast_radius; to read the code they name, call get_source.

Its edges are resolved by language analyzers or explicitly marked as CANDIDATE/UNRESOLVED; they are never created by matching names. Read confidence and completeness before treating an empty or partial answer as proof of absence. Grep cannot provide that distinction.

Rows are addressable: every one carries a repository, a repository-relative path, a qualified name and a line range, and every tool accepts that triple instead of a stable key.

Where it loses: a rare name in a single small repository is cheaper to grep, and one small file is cheaper to read than to outline. It wins on common names, on transitive impact, on cross-repository consumers and on proving an absence.`

// staleServerInstructions replaces the routing card when there is no published
// generation to answer from.
//
// A client spawns this server itself, so exiting looks like a crash and says
// nothing. Completing the handshake with no tools and this text is the only way
// to be told what to do; a server that publishes tools it cannot answer with
// teaches the agent that the tools do not work.
//
// It says no query tool can answer *from a graph* rather than that none is
// exposed, because one shape does expose them: `serve --introspection` lists
// the catalogue for an inspector that has nothing else to read. The sentence
// stays true of graph_status too, which answers that the graph is empty rather
// than answering from one.
//
// What it must not do is explain that shape. This is the routing card every
// client reads at handshake, truncated at 2 KB and the only text that survives
// schema deferral; introspection is a mode no client will ever be in, and
// spending the budget on it is how a paragraph that routes becomes a paragraph
// that is skipped. What a client needs here is the one command that repairs
// this.
const staleServerInstructions = toolVisibilityInstructions + `

Kivgraph has no published graph, so no query tool can answer from one. Run "kivgraph index --full" to build one, then restart this server. Until then, use the host's own search and file tools.`
