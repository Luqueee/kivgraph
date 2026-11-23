package agenthook

import (
	"context"
	"reflect"
	"testing"
)

func classifyCommand(t *testing.T, command string) Question {
	t.Helper()
	return Classify(payloadOf(t, "Bash", bashInput{Command: command}))
}

// A wrapper is only transparent when what it wraps is a search. The ones that
// wrap anything else, or nothing, or ask a question about the wrapper itself,
// are none of the gate's business.
func TestWrappersAreNotSearchesWhenWhatTheyWrapIsNot(t *testing.T) {
	for _, command := range []string{
		"time ls", "time", "timeout 5 go test ./...", "timeout", "timeout 5", "timeout -k",
		"nice make", "nice -n 5", "nohup ./run.sh", "command -v rg", "command", "exec", "env", "env -i",
		"env FOO=1 make", `sh -c "ls -la"`, `bash -c`, `sh -c ''`, `bash -lc "go build ./..."`,
		"(ls)", "( )", `sh -c "echo rg Publish"`, "time -p", "timeout --kill-after=5",
		"sh -x", "sh script.sh", "> out.txt", "2> err.log",
	} {
		if question := classifyCommand(t, command); question.Kind != KindNone {
			t.Errorf("%q classified as %#v, want not a search", command, question)
		}
	}
}

// The escape named by the refusal is on the very command being gated, and a
// wrapper in front of it does not change that: an agent that was told to prefix
// the retry will sometimes prefix it behind `timeout`.
func TestTheEscapeWorksBehindAWrapper(t *testing.T) {
	for _, command := range []string{
		"timeout 5 KIVGRAPH_DISABLE_HOOK=1 rg Publish",
		"timeout 5 env KIVGRAPH_DISABLE_HOOK=1 rg Publish",
		"env KIVGRAPH_DISABLE_HOOK=1 time rg Publish",
		`sh -c "KIVGRAPH_DISABLE_HOOK=1 rg Publish"`,
		`KIVGRAPH_DISABLE_HOOK=1 sh -c "rg Publish"`,
	} {
		if question := classifyCommand(t, command); question.Kind != KindNone {
			t.Errorf("%q classified as %#v, want the escape to hold", command, question)
		}
	}
}

func TestAWrapperDoesNotHideASearch(t *testing.T) {
	for _, command := range []string{
		"time rg Publish internal", "time -p rg Publish internal",
		"timeout 20 rg Publish internal", "timeout -k 5 20 rg Publish internal",
		"timeout --signal=KILL 20 rg Publish internal", "timeout -s KILL 20 rg Publish internal",
		"nice rg Publish internal", "nice -n 10 rg Publish internal", "nice -10 rg Publish internal",
		"nohup rg Publish internal", "command rg Publish internal", "exec rg Publish internal",
		"env rg Publish internal", "env -i FOO=1 rg Publish internal", "env FOO=1 BAR=2 rg Publish internal",
		"timeout 5 nice -n 5 time rg Publish internal",
		`sh -c "rg Publish internal"`, `bash -c 'rg Publish internal'`, `bash -lc "cd x && rg Publish internal"`,
		`zsh -c "rg Publish internal | head"`, `timeout 9 sh -c "rg Publish internal"`,
		"(rg Publish internal)", "( cd x && rg Publish internal )",
		"rtk timeout 5 rg Publish internal",
	} {
		question := classifyCommand(t, command)
		if question.Kind != KindSymbol || question.Pattern != "Publish" || !reflect.DeepEqual(question.Paths, []string{"internal"}) {
			t.Errorf("%q classified as %#v, want a symbol question about Publish over internal", command, question)
		}
	}
}

// What a command writes to is not where it looks. `rg Publish > out.txt` named
// one file the graph does not cover, and a search over files nobody indexed is
// let through: an ambiguous name escaped the gate by being redirected.
func TestARedirectionIsNotAPathTheSearchLooks(t *testing.T) {
	for command, wantPaths := range map[string][]string{
		"rg Publish > out.txt":                {},
		"rg Publish >out.txt":                 {},
		"rg Publish>out.txt":                  {},
		"rg Publish >> notes.md":              {},
		"rg Publish internal > /tmp/out.log":  {"internal"},
		"rg Publish 2>/dev/null":              {},
		"rg Publish 2> err.log":               {},
		"rg Publish internal 2>err.log":       {"internal"},
		"rg Publish < in.txt":                 {},
		"rg Publish > out.txt internal":       {"internal"},
		`rg Publish "a > b.go"`:               {"a > b.go"},
		`rg Publish 'x<y.go'`:                 {"x<y.go"},
		`rg Publish > "out file.txt"`:         {},
		"grep -rn Publish internal > out.txt": {"internal"},
	} {
		question := classifyCommand(t, command)
		if question.Kind != KindSymbol || question.Pattern != "Publish" || !reflect.DeepEqual(question.Paths, wantPaths) {
			t.Errorf("%q: %#v, want a Publish question over %v", command, question, wantPaths)
		}
	}
}

// The whole point, through the gate: an ambiguous name is refused whether or not
// the output is redirected to a file the graph does not cover.
func TestAnAmbiguousNameIsRefusedWhateverTheOutputGoesTo(t *testing.T) {
	ambiguous := Facts{Declarations: 3, Repositories: 1, Sample: []string{"repo:a.go:1"}}
	gate := Gate{Graph: &fakeGraph{symbol: ambiguous}, Indexed: IndexedExtensions([]string{".go"})}
	for _, command := range []string{
		"rg Publish", "rg Publish > out.txt", "rg Publish internal 2> err.log",
		"timeout 20 rg Publish internal > /tmp/out.log", `sh -c "rg Publish"`,
	} {
		decision := gate.Decide(context.Background(), classifyCommand(t, command))
		if !decision.Deny {
			t.Errorf("%q was let through; an ambiguous name must be refused whatever the output goes to", command)
		}
	}
	// And a search that really does look only at files the graph does not cover
	// is still left alone, redirected or not.
	for _, command := range []string{"rg version package.json", "rg version package.json > out.txt", "rg Publish app.log 2>/dev/null"} {
		if decision := gate.Decide(context.Background(), classifyCommand(t, command)); decision.Deny {
			t.Errorf("%q was refused, but it only reads files the graph does not index", command)
		}
	}
}
