package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/agenthook"
	"github.com/Luqueee/kivgraph/internal/config"
)

// hookDeadline bounds everything the gate does once it has decided the call is
// worth asking about.
//
// It is short because of where this runs: in front of a tool the user is
// waiting on, on every shell command of every session. A daemon on loopback
// answers a resolved reference in single-digit milliseconds, so this is not a
// budget the honest path spends -- it is the ceiling on how long a wedged or
// half-started daemon can hold up a `grep` before the gate gives up and lets it
// run.
const hookDeadline = 700 * time.Millisecond

// hookInputCeiling bounds the payload the gate will read.
//
// A prompt can be arbitrarily long and arrives inside a subagent dispatch, so
// the reader needs an end. Truncating one costs a less specific refusal; not
// having a limit costs the memory of whatever the agent chose to send.
const hookInputCeiling = 1 << 20

// runHookRun answers one gate call.
//
// It returns 0 whatever happens. Both hosting agents read a non-zero exit as a
// refusal of its own, so a gate that failed and said so would block the call it
// had just failed to form an opinion about -- which is the one outcome a gate
// must never produce.
//
// Deciding what reaches stdout is Write's job and not this one's: an allow
// writes nothing, a refusal writes a verdict and a briefing writes context
// without one. Testing the decision here instead would put that rule in two
// places and let them disagree.
func runHookRun(stdin io.Reader, stdout io.Writer) int {
	_ = hookDecision(stdin).Write(stdout)
	return 0
}

// hookDecision is the gate's whole judgement, and every path out of it that
// did not establish a reason to refuse returns Allow.
func hookDecision(stdin io.Reader) agenthook.Decision {
	if disabled(os.Getenv(agenthook.DisableVariable)) {
		return agenthook.Allow
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, hookInputCeiling))
	if err != nil {
		return agenthook.Allow
	}
	var payload agenthook.Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return agenthook.Allow
	}

	// Classification is pure and costs nothing, so it comes before the
	// configuration read and long before the daemon. Nearly every call the
	// gate ever sees leaves here, and none of them should pay for a graph
	// they were never going to be measured against.
	question := agenthook.Classify(payload)
	if question.Kind == agenthook.KindNone {
		return agenthook.Allow
	}

	loaded, ok := loadForHook()
	if !ok {
		return agenthook.Allow
	}

	// A call to Kivgraph's own tools is never measured against the graph,
	// so it takes neither the repository lookup nor the daemon: it needs
	// only somewhere to remember the session. It also deliberately skips
	// the cwd test the searches below apply -- these tools answer across
	// every registered repository, so a session driving them from a
	// directory outside all of them is still a session worth briefing.
	if question.Kind == agenthook.KindGraphTool {
		gate := agenthook.Gate{
			Briefing:  agenthook.Briefing{Directory: briefDirectory(loaded)},
			SessionID: payload.SessionID,
		}
		return gate.Decide(context.Background(), question)
	}

	repository, inside := repositoryHolding(loaded, payload.CWD)
	if !inside {
		return agenthook.Allow
	}

	gate := agenthook.Gate{
		Indexed: agenthook.IndexedExtensions(agenthook.ExtensionsFor(repository.Languages)),
	}
	ctx, cancel := context.WithTimeout(context.Background(), hookDeadline)
	defer cancel()
	if graph, close, err := agenthook.Dial(ctx, stateDirectory(loaded)); err == nil {
		defer close()
		gate.Graph = graph
	}
	return gate.Decide(ctx, question)
}

// briefDirectory is where the gate remembers which sessions it has briefed.
//
// It sits under the state directory rather than beside the graph generations
// because nothing here is derived from a graph: the markers survive a re-index,
// a rollback and a clean, and none of those should re-brief a session that is
// still running.
func briefDirectory(loaded config.Loaded) string {
	return filepath.Join(stateDirectory(loaded), "briefs")
}

// loadForHook reads the configuration, and treats every failure as absence.
//
// A hook is not the place to report that a configuration is malformed: nobody
// asked it a question about the configuration, and the only thing it could do
// with the answer is refuse a tool call for a reason unrelated to it.
func loadForHook() (config.Loaded, bool) {
	path, err := config.DefaultConfigPath()
	if err != nil {
		return config.Loaded{}, false
	}
	loaded, err := config.Load(path)
	if err != nil {
		return config.Loaded{}, false
	}
	return loaded, true
}

// repositoryHolding answers which indexed repository a working directory sits
// in.
//
// This is the question tokensave answers by looking for its own state directory
// above the cwd. Kivgraph can do better and has to: its graph spans several
// repositories registered by absolute path, so it knows not merely that some
// index exists nearby but that *this* directory is one of the places it covers.
// A directory the graph does not cover is one where `grep` is the only tool
// that works, and the gate stands aside there.
func repositoryHolding(loaded config.Loaded, cwd string) (config.Repository, bool) {
	directory, err := filepath.Abs(strings.TrimSpace(cwd))
	if err != nil || cwd == "" {
		return config.Repository{}, false
	}
	best, found := config.Repository{}, false
	for _, repository := range loaded.Repositories.Repositories {
		root, err := filepath.Abs(repository.Path)
		if err != nil || !under(directory, root) {
			continue
		}
		// The innermost match wins, so a repository nested inside another
		// answers with its own languages rather than its parent's.
		if !found || len(root) > len(best.Path) {
			best, found = repository, true
			best.Path = root
		}
	}
	return best, found
}

// under reports whether a directory is the root or inside it.
func under(directory, root string) bool {
	if directory == root {
		return true
	}
	relative, err := filepath.Rel(root, directory)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// disabled reads the escape hatch's value.
func disabled(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "0" && !strings.EqualFold(value, "false")
}
