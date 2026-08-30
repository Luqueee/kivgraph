//go:build darwin

package supervisor

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
)

func renderAgent(t *testing.T, path string) string {
	t.Helper()
	rendered, err := plist(testSpec("/state"), "com.kivgraph.daemon.0badcafe", path)
	if err != nil {
		t.Fatalf("plist() error = %v", err)
	}
	return string(rendered)
}

// The negatives first, and the first of them is the one that produces a file
// launchd refuses to load rather than a daemon that misbehaves.

// A PATH is a list of directories a user chose, so it can hold the characters
// XML reserves. Written raw, an ampersand ends the document early and an angle
// bracket opens an element nobody wrote.
func TestARecordedPathStaysWellFormed(t *testing.T) {
	for name, path := range map[string]string{
		"an ampersand":   "/opt/r&d/bin:/usr/bin",
		"angle brackets": "/opt/<tmp>/bin:/usr/bin",
		"a quote":        `/opt/say"what/bin:/usr/bin`,
	} {
		t.Run(name, func(t *testing.T) {
			rendered := renderAgent(t, path)
			decoder := xml.NewDecoder(strings.NewReader(rendered))
			for {
				_, err := decoder.Token()
				if errors.Is(err, io.EOF) {
					return
				}
				if err != nil {
					t.Fatalf("the rendered agent does not parse: %v\n%s", err, rendered)
				}
			}
		})
	}
}

// A machine with no PATH at all records none. An empty one would hand the
// daemon a worse environment than launchd's own default, which is the
// environment this block exists to replace.
func TestNoPathRecordsNoEnvironmentBlock(t *testing.T) {
	if rendered := renderAgent(t, ""); strings.Contains(rendered, environmentKey) {
		t.Fatalf("an agent rendered without a PATH still declares an environment:\n%s", rendered)
	}
}

// TestStrippingTakesTheEnvironmentAndNothingElse is what `status` rests on: it
// compares two renderings with their PATH taken out, so anything else the
// strip took with it would be a difference nobody could see. KeepAlive is the
// one to watch -- it is the other two-space dict in the file, and taking it
// would report an agent that no longer restarts the daemon as installed.
func TestStrippingTakesTheEnvironmentAndNothingElse(t *testing.T) {
	withPath, recorded := withoutRecordedPath(renderAgent(t, "/opt/homebrew/bin:/usr/bin"))
	withoutPath, none := withoutRecordedPath(renderAgent(t, ""))
	if !recorded {
		t.Fatal("an agent that records a PATH was not reported as recording one")
	}
	if none {
		t.Fatal("an agent that records no PATH was reported as recording one")
	}
	if withPath != withoutPath {
		t.Fatalf("stripping left two different agents:\n%s\n---\n%s", withPath, withoutPath)
	}
	for _, needle := range []string{"KeepAlive", "SuccessfulExit", "ProgramArguments", "RunAtLoad"} {
		if !strings.Contains(withPath, needle) {
			t.Fatalf("stripping took %s with it:\n%s", needle, withPath)
		}
	}
}
