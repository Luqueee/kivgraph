package agenthook

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBriefingWithholdsWhatItCannotRemember is the negative half, and the one
// that decides whether this feature is safe to ship.
//
// Every case here is a briefing that cannot record having happened. Answering
// true in any of them attaches the same paragraph to every Kivgraph call for
// the rest of the session, which is worse than never briefing at all: the
// guidance stops being guidance and becomes noise on each tool result.
func TestBriefingWithholdsWhatItCannotRemember(t *testing.T) {
	occupied := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(occupied, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	for _, testCase := range []struct {
		name      string
		briefing  Briefing
		sessionID string
		because   string
	}{
		{
			"no directory to write in", Briefing{}, "session",
			"the zero value has nowhere to record a briefing",
		},
		{
			"a directory of only spaces", Briefing{Directory: "   "}, "session",
			"a blank path is absence, not a location",
		},
		{
			"the host sent no session id", Briefing{Directory: t.TempDir()}, "",
			"without an id every call looks like the first one of a new session",
		},
		{
			"a session id of only spaces", Briefing{Directory: t.TempDir()}, "  \t ",
			"a blank id is the same absence as a missing one",
		},
		{
			"the directory is a file", Briefing{Directory: filepath.Join(occupied, "briefs")}, "session",
			"a path that cannot be created is not a place to remember anything",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.briefing.First(testCase.sessionID) {
				t.Fatalf("briefed anyway: %s", testCase.because)
			}
		})
	}
}

// TestBriefingHappensOncePerSession is the whole contract in one test: the
// first call of a session is briefed and every later one is not.
func TestBriefingHappensOncePerSession(t *testing.T) {
	briefing := Briefing{Directory: filepath.Join(t.TempDir(), "briefs")}

	if !briefing.First("session-a") {
		t.Fatal("the first call of a session was not briefed")
	}
	for attempt := 2; attempt <= 4; attempt++ {
		if briefing.First("session-a") {
			t.Fatalf("call %d of the same session was briefed again", attempt)
		}
	}
	if !briefing.First("session-b") {
		t.Fatal("a second session was not briefed; the marker is not per session")
	}
}

// TestBriefingKeepsASessionIdOutOfThePath is the corrupt shape no caller can
// build today.
//
// The id arrives from the host and is used to name a file. An id holding
// separators or a parent reference would otherwise decide where the gate
// writes, so the digest is load-bearing rather than tidy. Nothing may appear
// outside the briefing directory, and two ids that differ must still be two
// sessions.
func TestBriefingKeepsASessionIdOutOfThePath(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "briefs")
	briefing := Briefing{Directory: directory}

	hostile := []string{
		"../../escaped",
		"/etc/passwd",
		strings.Repeat("../", 12) + "root",
		"a/b/c",
	}
	for _, sessionID := range hostile {
		if !briefing.First(sessionID) {
			t.Fatalf("session %q was not briefed", sessionID)
		}
		if briefing.First(sessionID) {
			t.Fatalf("session %q was briefed twice", sessionID)
		}
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read briefs: %v", err)
	}
	if len(entries) != len(hostile) {
		t.Fatalf("got %d markers, want %d: two ids collided", len(entries), len(hostile))
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".brief") {
			t.Fatalf("marker %q is not a plain marker file", entry.Name())
		}
	}
	// Nothing was created beside the briefing directory, which is what an
	// id that escaped the digest would have produced.
	above, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	if len(above) != 1 || above[0].Name() != "briefs" {
		t.Fatalf("a session id wrote outside its directory: %v", above)
	}
}

// TestBriefingForgetsOnlyWhatIsStale checks both sides of the boundary, because
// a prune that is too eager re-briefs a live session and one that never fires
// grows a directory without end.
func TestBriefingForgetsOnlyWhatIsStale(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "briefs")
	briefing := Briefing{Directory: directory}

	for _, sessionID := range []string{"stale", "fresh"} {
		if !briefing.First(sessionID) {
			t.Fatalf("session %q was not briefed", sessionID)
		}
	}
	// The clock is not injected -- AGENTS.md forbids a parameter that only
	// a test would use -- so the markers are aged on disk instead, which is
	// what the passage of time would have done to them anyway.
	aged := time.Now().Add(-briefRetention - time.Hour)
	if err := os.Chtimes(briefing.marker("stale"), aged, aged); err != nil {
		t.Fatalf("age the stale marker: %v", err)
	}
	// A file the prune does not own, aged past the deadline: it must
	// survive, or the gate is deleting whatever else shares the directory.
	foreign := filepath.Join(directory, "keep.txt")
	if err := os.WriteFile(foreign, []byte("not a marker"), 0o600); err != nil {
		t.Fatalf("write foreign file: %v", err)
	}
	if err := os.Chtimes(foreign, aged, aged); err != nil {
		t.Fatalf("age the foreign file: %v", err)
	}
	nested := filepath.Join(directory, "nested.brief")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatalf("make nested directory: %v", err)
	}

	// Briefing a third session is what runs the prune.
	if !briefing.First("trigger") {
		t.Fatal("the triggering session was not briefed")
	}

	if _, err := os.Stat(briefing.marker("stale")); !os.IsNotExist(err) {
		t.Fatalf("the stale marker survived: %v", err)
	}
	if _, err := os.Stat(briefing.marker("fresh")); err != nil {
		t.Fatalf("a live session lost its marker and will be briefed twice: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("the prune removed a file it does not own: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("the prune removed a directory: %v", err)
	}
	// The stale session is now unknown, so it briefs again rather than
	// staying silent forever.
	if !briefing.First("stale") {
		t.Fatal("a forgotten session was not briefed again")
	}
}

// TestBriefingBriefsExactlyOnceUnderConcurrency defends the reason the marker
// is created with O_EXCL rather than checked and then written.
//
// An agent can fire several tool calls at once, and each one runs its own hook
// process. A check-then-write would let two of them both see no marker and both
// brief, which is the duplicate the whole feature exists to avoid.
func TestBriefingBriefsExactlyOnceUnderConcurrency(t *testing.T) {
	briefing := Briefing{Directory: filepath.Join(t.TempDir(), "briefs")}

	const racers = 16
	var group sync.WaitGroup
	results := make([]bool, racers)
	start := make(chan struct{})
	for index := range racers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results[index] = briefing.First("contended")
		}()
	}
	close(start)
	group.Wait()

	briefed := 0
	for _, result := range results {
		if result {
			briefed++
		}
	}
	if briefed != 1 {
		t.Fatalf("%d of %d racers briefed; exactly one must", briefed, racers)
	}
}

// TestPruneSurvivesADirectoryItCannotRead is the third empty: not "nothing is
// stale" but "it could not be looked at".
//
// The directory is created before the prune runs, so reaching this needs it to
// vanish in between -- which a `kivgraph clean`, a tmpreaper or a user with a
// shell can all do. Answering anything but "leave it alone" would turn a
// briefing into an error on a call that is going ahead regardless.
func TestPruneSurvivesADirectoryItCannotRead(t *testing.T) {
	briefing := Briefing{Directory: filepath.Join(t.TempDir(), "gone")}
	briefing.prune()

	// And the session is still briefed afterwards: prune is housekeeping,
	// never a precondition.
	if !briefing.First("session-a") {
		t.Fatal("a session went unbriefed because the prune found nothing to read")
	}
}
