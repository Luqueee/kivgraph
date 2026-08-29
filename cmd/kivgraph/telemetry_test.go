package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/version"
)

// The daemon and `serve` share `runConfiguredServe`, and its call site used to
// pass a literal: the daemon reported itself as `stdio`. That is not cosmetic.
// The marker is created once per version, so the wrong row is the only row that
// version will ever produce, in the one field that exists to tell the two
// arrangements apart.
//
// The assertion is on what the emitter is handed rather than on the mapping
// behind it, so a call site that stops using the value fails here too.
func TestFirstRunOptionsCarryTheArrangementThatWillServe(t *testing.T) {
	state := t.TempDir()
	loaded := config.Loaded{Config: config.Config{}}
	loaded.Config.Storage.DatabasePath = filepath.Join(state, "kivgraph.db")

	for command, want := range map[string]string{
		"daemon": "daemon",
		"serve":  "stdio",
	} {
		options := firstRunOptions(loaded, command)
		if options.Transport != want {
			t.Errorf("firstRunOptions(%q).Transport = %q, want %q",
				command, options.Transport, want)
		}
		if options.StateDirectory != state {
			t.Errorf("firstRunOptions(%q).StateDirectory = %q, want %q",
				command, options.StateDirectory, state)
		}
		if options.Version != version.Value {
			t.Errorf("firstRunOptions(%q).Version = %q, want the compiled %q",
				command, options.Version, version.Value)
		}
	}
}

// `serve` relaying is the one case where the command and the arrangement
// disagree, and the whole reason the transport is a field rather than a
// constant per command.
func TestARelayingServeReportsTheDaemonThatWillAnswer(t *testing.T) {
	loaded := config.Loaded{Config: config.Config{}}
	loaded.Config.Storage.DatabasePath = filepath.Join(t.TempDir(), "kivgraph.db")

	if inProcess := firstRunOptions(loaded, "serve").Transport; inProcess != "stdio" {
		t.Fatalf("a serve that answers in process reports %q, want stdio", inProcess)
	}
	if relaying := relayedFirstRunOptions(loaded).Transport; relaying != "daemon" {
		t.Fatalf("a serve that relays reports %q, want daemon: the command is the"+
			" same and the arrangement that answers is not", relaying)
	}
}

// One byte on stdout corrupts an MCP session, and the notice is the only thing
// this package prints. A refactor that sends it to stdout would be invisible
// until a client failed to parse a frame.
func TestTheFirstRunNoticeNeverGoesToStdout(t *testing.T) {
	loaded := config.Loaded{Config: config.Config{}}
	loaded.Config.Storage.DatabasePath = filepath.Join(t.TempDir(), "kivgraph.db")

	if notice := firstRunOptions(loaded, "serve").Notice; notice != os.Stderr {
		t.Fatalf("the first-run notice writes to %v, want os.Stderr", notice)
	}
}
