package mcp

// serverInstructions is the one string this server can send that survives every
// client's schema deferral.
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
const serverInstructions = `Kivgraph answers "what breaks if I change this" from a published code graph over Go, TypeScript, Rust, Python and Dart. Go, TypeScript and Rust edges are type-checked; Dart edges are resolved by Dart Analysis Server; Python uses exact semantic facts when a configured analyzer provides them and CANDIDATE facts in its bundled AST fallback. Before grepping or reading files to find callers, references or impact, call find_references or get_blast_radius; to read the code they name, call get_source.

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
const staleServerInstructions = `Kivgraph has no published graph to answer from, so it exposes no query tools. Run "kivgraph index --full" to build one, then restart this server. Until then, use the host's own search and file tools.`
