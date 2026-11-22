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
	Intent(ctx context.Context, intent, repository string) (Facts, error)
	// Declares answers whether a repository holds a declaration of a name.
	Declares(ctx context.Context, name, repository string) (bool, error)
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
	// Repository narrows an intent question to the repository holding the
	// caller's working directory. A symbol name remains workspace-wide so
	// homonyms across repositories are still visible.
	Repository string
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
		if gate.searchesOnlyUncoveredFiles(question) {
			return Allow
		}
		return gate.decideSymbol(ctx, question)
	case KindIntent:
		if gate.searchesOnlyUncoveredFiles(question) {
			return Allow
		}
		return gate.decideIntent(ctx, question)
	default:
		return Allow
	}
}

// searchesOnlyUncoveredFiles reports whether a search says where it looks and
// looks only at files the graph does not cover: a manifest, a log, a glob over
// configuration.
//
// The pattern alone cannot tell. `version` is declared by a dozen symbols and
// is also a key in every package.json; refusing the second search because of
// the first sent the caller to find_references for a JSON key.
//
// It needs one path that names a file type and is not source, and no path that
// is. A path with no extension is a directory and can hold anything, so on its
// own it proves nothing -- but beside a glob it is only where the glob looks.
// A gate that was not told what the repository indexes cannot call any path
// uncovered.
func (gate Gate) searchesOnlyUncoveredFiles(question Question) bool {
	if gate.Indexed == nil {
		return false
	}
	uncovered := false
	for _, name := range question.Paths {
		extension := strings.ToLower(path.Ext(strings.TrimSpace(name)))
		// A lone dot is no file type: `.`, `..` and `src/.` are directories,
		// and `grep -rn Name .` was let through as a search of files the
		// graph does not cover.
		if len(extension) < 2 {
			continue
		}
		if gate.Indexed(name) || namesLanguageType(extension) {
			return false
		}
		uncovered = true
	}
	return uncovered
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
		escapeFor(question),
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
		escapeFor(question),
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
	// The count is of the whole graph, and the search is in one repository.
	// When the graph holds no declaration of the name there, it is not what
	// that search was about to read -- and it may hold nothing of the
	// repository at all: a search over a backend whose packages were not
	// indexed was refused because an application in another language declares
	// `build`. It costs a second call, paid only by a search about to be
	// refused.
	if gate.Repository != "" {
		if declared, err := gate.Graph.Declares(ctx, question.Pattern, gate.Repository); err != nil || !declared {
			return Allow
		}
	}
	return denySymbol(question, facts, fmt.Sprintf(
		"has %d declarations in %d %s, and a text search cannot tell them apart",
		facts.Declarations, facts.Repositories,
		plural(facts.Repositories, "repository", "repositories")))
}

// denySymbol builds the refusal for a name the graph answers better.
func denySymbol(question Question, facts Facts, because string) Decision {
	name := question.Pattern
	lines := []string{
		fmt.Sprintf("STOP: %s %s.", quote(name), because),
		"  find_references(name=" + quote(name) + ")      who calls it",
		// find_references takes the bare name and answers with the candidates;
		// get_blast_radius has no `name`, and the call this line used to print
		// was refused by the tool.
		"  get_blast_radius(repository=…, path=…, qualified_name=…)   what breaks if you change the one you mean",
	}
	if len(facts.Sample) > 0 {
		lines = append(lines, "Declared at:")
		for _, row := range facts.Sample {
			lines = append(lines, "  "+row)
		}
	}
	return Decision{Deny: true, Reason: strings.Join(append(lines, escapeFor(question)), "\n")}
}

// decideIntent answers a pattern that gropes for a name.
func (gate Gate) decideIntent(ctx context.Context, question Question) Decision {
	if gate.Graph == nil {
		return Allow
	}
	facts, err := gate.Graph.Intent(ctx, question.Pattern, gate.Repository)
	if err != nil || !facts.Known() {
		return Allow
	}
	call := "  find_by_intent(intent=" + quote(question.Pattern)
	if gate.Repository != "" {
		call += ", repo=" + quote(gate.Repository)
	}
	call += ")   ranked candidates"
	lines := []string{
		fmt.Sprintf("STOP: %s is a regular expression groping for a name the graph already knows.",
			quote(question.Pattern)),
		call,
	}
	if len(facts.Sample) > 0 {
		lines = append(lines, "It matches:")
		for _, row := range facts.Sample {
			lines = append(lines, "  "+row)
		}
	}
	return Decision{Deny: true, Reason: strings.Join(append(lines, escapeFor(question)), "\n")}
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

// nativeEscapeLine is the same for a call that has no command to put a variable
// before. The hook process reads the variable from its own environment, so the
// only way to insist is the one that environment allows: the session has to have
// been started with it, which is the user's to do and not the agent's.
const nativeEscapeLine = "To run this one anyway there is no command to prefix: ask the user to start the session with " + DisableVariable + "=1."

// escapeFor picks the line that tells the caller how to insist.
func escapeFor(question Question) string {
	if question.Native {
		return nativeEscapeLine
	}
	return escapeLine
}

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
