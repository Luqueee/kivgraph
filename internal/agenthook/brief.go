package agenthook

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// briefText is what a session is told before its first Kivgraph tool call.
//
// It deliberately repeats nothing from the server's `instructions` field. That
// paragraph already reaches every client at handshake and says what the graph
// resolves and where it loses; saying it twice would spend the one injection
// this gate gets on words the model has already read.
//
// What it carries instead is the part that field cannot. `instructions` is the
// routing card, truncated at 2 KB and read before any question exists, and
// `internal/mcp/instructions.go` explains why a call budget does not belong in
// it: a system prompt rewritten on every re-index invalidates the client's
// prompt cache, which is how tokensave's own budget came to cost more than the
// calls it discouraged. An advisory attached to one tool call has neither
// problem -- it is not in the system prompt, and it is read at the moment the
// budget is about to be spent rather than long before.
//
// The numbers are static measurements, not facts read from the graph. Nothing
// here changes when a repository is indexed.
const briefText = `Kivgraph, before the first call of this session.

Cost, measured over MCP on a 53-repository index in cl100k_base tokens:
graph_status 5541, list_repositories 4787, find_references 1618,
find_symbol 1539, get_source 788, find_by_intent with view="files" 300,
get_symbol 245.

The first two scale with the corpus and say nothing about your symbol. Opening
a session with both spends 10328 tokens before the first question is asked,
which is 34 times what that question costs. Reach for them when you need what
they hold, which is not usually.

- Do not resolve a name before asking about it. find_references takes a bare
  name, and an ambiguous one answers with its candidates instead of guessing,
  so narrowing is copying one back.
- Ask at the granularity you need. view="files" answers which files without the
  line of each reference, for roughly a third of the tokens.
- Every row carries a repository, a repository-relative path, a qualified name
  and a line range, and every tool takes that triple. Build the next call from
  the answer you already have.`

// briefRetention is how long a session is remembered as briefed.
//
// It bounds the directory rather than the correctness of anything: a marker
// that outlives its session is a few bytes, and one dropped too early costs a
// second briefing in a session long enough to have forgotten the first. A day
// is comfortably longer than any session and short enough that the directory
// cannot grow without end.
const briefRetention = 24 * time.Hour

// Briefing remembers which sessions have already been briefed.
//
// The zero value never briefs, which is the right answer when there is nowhere
// to write: a gate that cannot remember would otherwise attach the same
// paragraph to every call of the session, and an unsolicited paragraph on every
// tool result is worse than no briefing at all.
type Briefing struct {
	// Directory holds one marker file per session. Empty disables the
	// briefing entirely.
	Directory string
}

// First reports whether this call is the first of its session, and records that
// it no longer is.
//
// The race is settled by the filesystem rather than by a lock: two concurrent
// first calls both try to create the marker with O_EXCL and exactly one
// succeeds, so a session cannot be briefed twice even when its agent fires two
// tool calls at once.
//
// Every failure answers false. This runs in front of a tool call that is going
// to proceed either way, so there is no failure here worth telling anyone
// about -- the cost of a missed briefing is that the session reads the skill
// the way it did before this existed.
// The id is tested for blankness on a trimmed copy and then used as it
// arrived. It is opaque and belongs to the host, so normalising it would merge
// two sessions a host considers distinct -- and the second of them would lose
// the briefing that is this function's whole purpose.
func (briefing Briefing) First(sessionID string) bool {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(briefing.Directory) == "" {
		return false
	}
	if err := os.MkdirAll(briefing.Directory, 0o700); err != nil {
		return false
	}
	briefing.prune()
	file, err := os.OpenFile(briefing.marker(sessionID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}

// marker is where one session's marker lives.
//
// The name is a digest and not the session id: the id arrives from the host and
// this is a path, so a value holding a separator would otherwise decide where
// the gate writes.
func (briefing Briefing) marker(sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	return filepath.Join(briefing.Directory, hex.EncodeToString(digest[:])+".brief")
}

// prune drops markers older than briefRetention.
//
// It runs only on the path that is about to create one, which is once per
// session rather than once per call, and it ignores every error it meets: a
// marker that cannot be read or removed is not a reason to withhold a briefing.
func (briefing Briefing) prune() {
	entries, err := os.ReadDir(briefing.Directory)
	if err != nil {
		return
	}
	deadline := time.Now().Add(-briefRetention)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".brief") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(deadline) {
			continue
		}
		_ = os.Remove(filepath.Join(briefing.Directory, entry.Name()))
	}
}
