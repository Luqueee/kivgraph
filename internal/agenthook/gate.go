package agenthook

import (
	"context"
	"fmt"
	"path"
	"strings"
)

// Facts are what the graph knows about a name, at the granularity the gate
// needs to choose between itself and `grep`.
//
// It is deliberately three numbers and a few rows rather than a result set. The
// gate is not answering the caller's question -- the tool it names will do that
// -- it is only deciding who answers it better, and that decision turns on how
// ambiguous and how widespread the name is.
type Facts struct {
	// Declarations is how many things carry this name.
	Declarations int
	// Repositories is how many repositories they live in.
	Repositories int
	// References is how many places use them.
	References int
	// Sample are a few rows, already formatted, for the refusal to quote.
	// A refusal that names real rows is a redirect; one that recites a rule
	// is an obstacle.
	Sample []string
}

// Known reports whether the graph has anything at all under this name.
func (facts Facts) Known() bool { return facts.Declarations > 0 }

// Graph is the part of the daemon the gate asks.
//
// It is an interface because the gate's decisions are worth testing against
// facts a test states outright, and because every implementation of it has to
// be allowed to fail: a daemon that is not running is the common case, not an
// error, and the gate's answer to it is to stand aside.
type Graph interface {
	// Symbol answers what is declared under a name. A name the graph does
	// not know is the zero Facts and a nil error.
	Symbol(ctx context.Context, name string) (Facts, error)
	// Intent answers what a loose description most likely names.
	Intent(ctx context.Context, intent string) (Facts, error)
}

// ambiguousAt is the number of declarations at which `grep` stops being able to
// answer.
//
// Two is not a tuning choice, it is the definition: with one declaration a text
// search finds the thing and every use of it, and with two it cannot tell which
// uses belong to which -- and neither can the person reading its output.
//
// It is also the only threshold the measurements support. Over the six
// questions of `benchmarks/mcp-token-cost` (commit `1a7d0266`, `cl100k_base`)
// the answer factor splits cleanly by how many things share the name and not at
// all by how many references the symbol has -- every question there has between
// zero and four:
//
//	rare_name     MergeAll 0,85x   CanonicalColumns 0,91x   DiscoverGo 0,83x
//	shared_name   BuildPlan 1,92x
//	common_name   NewServer 1,68x  Publish 6,42x
//
// The three below `1,0` are ours to lose, and all three are unambiguous names.
// A reference-count floor was written here first and removed: the corpus has
// nothing above four references, so any number would have been invented. See
// ADR 0077 for what would have to be measured to add one back.
const ambiguousAt = 2

// Gate decides whether a search should be refused in favour of the graph.
type Gate struct {
	// Graph answers the two questions the gate asks. A nil Graph -- no
	// daemon, or one that could not be reached -- allows everything that
	// needs an answer from it.
	Graph Graph
	// Indexed reports whether a path or glob names a file the graph covers.
	// A nil Indexed treats nothing as indexed.
	Indexed func(name string) bool
	// Briefing remembers which sessions have already been briefed. Its zero
	// value never briefs, which is what a gate with nowhere to write should
	// do.
	Briefing Briefing
	// SessionID is the conversation this call belongs to, and is empty for
	// a host that does not send one.
	SessionID string
}

// Decide answers a classified call.
//
// Every branch that cannot establish a positive reason to refuse returns Allow.
// That is not caution for its own sake: the gate's whole claim is that the tool
// it names answers better, and it has no standing to refuse a call it knows
// nothing about.
func (gate Gate) Decide(ctx context.Context, question Question) Decision {
	switch question.Kind {
	case KindGraphTool:
		return gate.briefSession()
	case KindResearchAgent:
		return gate.denyResearchAgent(question)
	case KindFiles:
		return gate.decideFiles(question)
	case KindSymbol:
		return gate.decideSymbol(ctx, question)
	case KindIntent:
		return gate.decideIntent(ctx, question)
	default:
		return Allow
	}
}

// briefSession attaches the briefing to the first Kivgraph tool call of a
// session, and says nothing to the rest.
//
// This is the one branch that never refuses. A call to the graph is already the
// outcome every other branch is arguing for, so refusing it -- even to insist
// the skill be read first -- would put friction on precisely the behaviour the
// gate exists to encourage, and would leave the tools dead for the session if
// the briefing were ever missed.
func (gate Gate) briefSession() Decision {
	if !gate.Briefing.First(gate.SessionID) {
		return Allow
	}
	return Decision{Context: briefText}
}

// denyResearchAgent refuses a subagent sent to read the codebase.
//
// This is the one branch that asks the graph nothing, and the reason is
// arithmetic rather than confidence: a subagent reads files until it is
// satisfied, so its floor is already higher than the ceiling of any single tool
// call here. There is no fact about the codebase that would make dispatching
// one the cheaper way to find out where something is.
func (gate Gate) denyResearchAgent(question Question) Decision {
	intent := question.Pattern
	if intent == "" {
		intent = "what you are looking for"
	}
	return Decision{Deny: true, Reason: strings.Join([]string{
		"STOP: a subagent reads files until it is satisfied; one graph call answers this.",
		"  find_by_intent(intent=" + quote(intent) + ")   which symbols that names",
		"  find_references(name=\"…\")                    who calls one of them",
		"Dispatch the subagent only after the graph could not answer.",
		escapeLine,
	}, "\n")}
}

// decideFiles answers a question about which files exist.
func (gate Gate) decideFiles(question Question) Decision {
	if gate.Indexed == nil || !gate.Indexed(question.Pattern) {
		return Allow
	}
	return Decision{Deny: true, Reason: strings.Join([]string{
		fmt.Sprintf("STOP: %q lists indexed source files one path at a time.", question.Pattern),
		"  find_by_intent(intent=\"…\")        which files answer the question you have",
		"  get_file_outline(path=\"…\")        what one of them declares, without reading it",
		escapeLine,
	}, "\n")}
}

// decideSymbol answers a search for a name.
//
// The refusal needs a positive fact, and exactly one qualifies: the name is
// ambiguous, so a text search cannot separate what it finds. A unique name is
// left alone however many places use it, and that is not a concession --
// `CLAUDE.md` documents it, `benchmarks/graph-tools-comparison/trivial.md`
// measures us at `1,9x` *worse* than `grep` on that shape, and
// `benchmarks/mcp-token-cost` puts all three of its unambiguous names below
// `1,0x`. See ambiguousAt.
func (gate Gate) decideSymbol(ctx context.Context, question Question) Decision {
	facts, ok := gate.ask(ctx, question.Pattern, Graph.Symbol)
	if !ok || !facts.Known() {
		return Allow
	}
	if facts.Declarations < ambiguousAt {
		return Allow
	}
	return denySymbol(question.Pattern, facts, fmt.Sprintf(
		"has %d declarations in %d %s, and a text search cannot tell them apart",
		facts.Declarations, facts.Repositories,
		plural(facts.Repositories, "repository", "repositories")))
}

// denySymbol builds the refusal for a name the graph answers better.
func denySymbol(name string, facts Facts, because string) Decision {
	lines := []string{
		fmt.Sprintf("STOP: %s %s.", quote(name), because),
		"  find_references(name=" + quote(name) + ")      who calls it",
		"  get_blast_radius(name=" + quote(name) + ")     what breaks if you change it",
	}
	if len(facts.Sample) > 0 {
		lines = append(lines, "Declared at:")
		for _, row := range facts.Sample {
			lines = append(lines, "  "+row)
		}
	}
	return Decision{Deny: true, Reason: strings.Join(append(lines, escapeLine), "\n")}
}

// decideIntent answers a pattern that gropes for a name.
func (gate Gate) decideIntent(ctx context.Context, question Question) Decision {
	facts, ok := gate.ask(ctx, question.Pattern, Graph.Intent)
	if !ok || !facts.Known() {
		return Allow
	}
	lines := []string{
		fmt.Sprintf("STOP: %s is a regular expression groping for a name the graph already knows.",
			quote(question.Pattern)),
		"  find_by_intent(intent=" + quote(question.Pattern) + ")   ranked candidates",
	}
	if len(facts.Sample) > 0 {
		lines = append(lines, "It matches:")
		for _, row := range facts.Sample {
			lines = append(lines, "  "+row)
		}
	}
	return Decision{Deny: true, Reason: strings.Join(append(lines, escapeLine), "\n")}
}

// ask puts one question to the graph, and reports whether it answered.
//
// A missing graph and a failing graph are the same event here: the gate learned
// nothing and has no grounds to refuse. Distinguishing them would only let a
// caller act on the difference, and there is no action to take -- the daemon is
// started by the user, never by a hook.
func (gate Gate) ask(ctx context.Context, subject string,
	question func(Graph, context.Context, string) (Facts, error)) (Facts, bool) {
	if gate.Graph == nil {
		return Facts{}, false
	}
	facts, err := question(gate.Graph, ctx, subject)
	if err != nil {
		return Facts{}, false
	}
	return facts, true
}

// escapeLine tells the caller how to insist.
const escapeLine = "To run this one anyway: " + DisableVariable + "=1 before the command."

// IndexedExtensions builds an Indexed function from the languages a
// configuration names.
//
// It answers on the extension because that is all a glob carries: `**/*.go` has
// no path to resolve and no file to stat. A glob that names no extension --
// `**/*` -- is not a question about code and is left alone.
func IndexedExtensions(extensions []string) func(string) bool {
	known := make(map[string]bool, len(extensions))
	for _, extension := range extensions {
		known[strings.ToLower(extension)] = true
	}
	return func(name string) bool {
		extension := strings.ToLower(path.Ext(strings.TrimSpace(name)))
		return extension != "" && known[extension]
	}
}

func quote(text string) string { return `"` + text + `"` }

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
}
