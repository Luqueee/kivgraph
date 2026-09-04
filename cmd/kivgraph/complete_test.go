package main

import (
	"bytes"
	"strings"
	"testing"
)

func candidates(t *testing.T, words ...string) []string {
	t.Helper()
	return completionCandidates(words)
}

func userFacingCandidates(t *testing.T, words ...string) []string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := runComplete(words, &stdout, &stderr); code != 0 {
		t.Fatalf("runComplete(%v) = %d, stderr=%q", words, code, stderr.String())
	}
	if stdout.Len() == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
}

func has(candidates []string, want string) bool {
	for _, candidate := range candidates {
		if candidate == want {
			return true
		}
	}
	return false
}

func TestCompleteNamesCommands(t *testing.T) {
	got := candidates(t, "")
	for _, want := range []string{"init", "logs", "tool-stats", "doctor", "graph", "benchmark", "mcp", "help"} {
		if !has(got, want) {
			t.Fatalf("the bare line did not offer %q: %v", want, got)
		}
	}
	// A hidden command is not something a person invokes.
	if has(got, "__complete") {
		t.Fatalf("the bare line offered the completion entry point: %v", got)
	}
}

func TestCompleteNarrowsToWhatWasTyped(t *testing.T) {
	got := candidates(t, "tool-s")
	if len(got) != 1 || got[0] != "tool-stats" {
		t.Fatalf("\"tool-s\" completed to %v, want just tool-stats", got)
	}
}

// A two-word command must not be swallowed by its one-word prefix.
func TestCompleteOffersTheSecondWordOfATwoWordCommand(t *testing.T) {
	got := candidates(t, "doctor", "")
	for _, want := range []string{"storage", "graph"} {
		if !has(got, want) {
			t.Fatalf("`doctor ` did not offer %q: %v", want, got)
		}
	}
	// `doctor` is a command in its own right, so its own flags belong here too.
	if !has(got, "--config") {
		t.Fatalf("`doctor ` did not offer its own flags: %v", got)
	}
}

// This is the drift the whole change exists to prevent: the help line for logs
// names four flags, and completion must still know all eight.
func TestCompleteOffersEveryFlagNotOnlyTheAdvertisedOnes(t *testing.T) {
	got := candidates(t, "logs", "")
	for _, want := range []string{
		"--config", "--kind", "--tool", "--since", "--limit", "--follow", "--failures", "--json", "--help",
	} {
		if !has(got, want) {
			t.Fatalf("`logs ` did not offer %q: %v", want, got)
		}
	}
}

func TestCompleteOffersAClosedVocabulary(t *testing.T) {
	got := candidates(t, "logs", "--kind", "")
	want := []string{"index", "serve", "tool"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("`logs --kind ` = %v, want %v", got, want)
	}
	if narrowed := candidates(t, "logs", "--kind", "s"); len(narrowed) != 1 || narrowed[0] != "serve" {
		t.Fatalf("`logs --kind s` = %v, want just serve", narrowed)
	}
}

// A boolean flag is complete on its own. Treating it as waiting for a value
// would make the next word impossible to type.
func TestCompleteDoesNotGiveABooleanFlagAValue(t *testing.T) {
	got := candidates(t, "logs", "--json", "")
	if !has(got, "--kind") {
		t.Fatalf("`logs --json ` stopped offering flags: %v", got)
	}
	// And a flag already on the line is not offered twice.
	if has(got, "--json") {
		t.Fatalf("`logs --json ` offered --json again: %v", got)
	}
}

func TestCompleteDefersPathsToTheShell(t *testing.T) {
	got := candidates(t, "logs", "--config", "")
	if len(got) != 1 || got[0] != completeFileMarker {
		t.Fatalf("`logs --config ` = %v, want the file marker", got)
	}
	if got := candidates(t, "doctor", "storage", "--database", ""); len(got) != 1 || got[0] != completeFileMarker {
		t.Fatalf("`doctor storage --database ` = %v, want the file marker", got)
	}
}

// A shell hands over --flag=value as a single word.
func TestCompleteHandlesTheEqualsForm(t *testing.T) {
	got := candidates(t, "logs", "--kind=s")
	if len(got) != 1 || got[0] != "--kind=serve" {
		t.Fatalf("`logs --kind=s` = %v, want --kind=serve", got)
	}
	if got := candidates(t, "logs", "--config="); len(got) != 1 || got[0] != completeFileMarker {
		t.Fatalf("`logs --config=` = %v, want the file marker", got)
	}
}

// The mcp and skill families share their flags, and only the writers accept
// --dry-run and --force. Completion reads that from the same constructor the
// parser uses, so it cannot offer a flag the operation would reject.
func TestCompleteSeparatesWritingOperationsFromReadingOnes(t *testing.T) {
	install := candidates(t, "mcp", "install", "")
	if !has(install, "--dry-run") || !has(install, "--force") {
		t.Fatalf("`mcp install ` lost its writing flags: %v", install)
	}
	status := candidates(t, "mcp", "status", "")
	if has(status, "--dry-run") || has(status, "--force") {
		t.Fatalf("`mcp status ` offered a writing flag: %v", status)
	}
	if !has(status, "--target") || !has(status, "--scope") {
		t.Fatalf("`mcp status ` lost its own flags: %v", status)
	}
}

func TestCompleteOffersOperationsAndTargets(t *testing.T) {
	instructions := userFacingCandidates(t, "instructions", "")
	if !has(instructions, "install") {
		t.Fatalf("completion for %v did not offer install: %v", []string{"instructions", ""}, instructions)
	}
	instructionInput := []string{"instructions", "install", "--agent", ""}
	instructionAgents := userFacingCandidates(t, instructionInput...)
	for _, want := range []string{"claude", "claude-code", "codex", "omp", "oh-my-pi", "opencode"} {
		if !has(instructionAgents, want) {
			t.Fatalf("completion for %v = %v, want %q", instructionInput, instructionAgents, want)
		}
	}
	operations := candidates(t, "skill", "")
	for _, want := range []string{"install", "status", "remove"} {
		if !has(operations, want) {
			t.Fatalf("`skill ` did not offer %q: %v", want, operations)
		}
	}
	targets := candidates(t, "skill", "install", "--target", "")
	if !has(targets, "claude-code") || !has(targets, "oh-my-pi") {
		t.Fatalf("`skill install --target ` = %v, want the known clients", targets)
	}
	scopes := candidates(t, "skill", "install", "--scope", "")
	if strings.Join(scopes, ",") != "project,user" {
		t.Fatalf("`--scope ` = %v, want project,user", scopes)
	}
}

// An unknown command has no candidates, and must not fall back to offering
// every flag of some other command.
func TestCompleteAnswersNothingForAnUnknownCommand(t *testing.T) {
	if got := candidates(t, "nonsense", ""); len(got) != 0 {
		t.Fatalf("an unknown command completed to %v", got)
	}
}

// A shell that receives a non-zero status shows the user a bell, and "no
// candidates" is a normal answer.
func TestCompleteAlwaysSucceeds(t *testing.T) {
	for _, words := range [][]string{nil, {""}, {"nonsense", ""}, {"logs", "--kind", "nope"}} {
		var stdout, stderr bytes.Buffer
		if code := runComplete(words, &stdout, &stderr); code != 0 {
			t.Fatalf("runComplete(%v) = %d, want 0", words, code)
		}
	}
}

func TestCompleteWritesOneCandidatePerLine(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runComplete([]string{"logs", "--kind", ""}, &stdout, &stderr); code != 0 {
		t.Fatalf("runComplete() = %d, stderr=%q", code, stderr.String())
	}
	if got := stdout.String(); got != "index\nserve\ntool\n" {
		t.Fatalf("runComplete() wrote %q", got)
	}
}

func TestCompletionScriptEmitsOnePerShell(t *testing.T) {
	for _, shell := range updateShellNames() {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"kivgraph", "completion", shell}, &stdout, &stderr); code != 0 {
			t.Fatalf("completion %s = %d, stderr=%q", shell, code, stderr.String())
		}
		script := stdout.String()
		if !strings.Contains(script, "__complete") {
			t.Fatalf("the %s script does not call the engine:\n%s", shell, script)
		}
		// The stub must carry no command name and no flag, or it is a
		// second place the surface is spelled and it will drift.
		for _, forbidden := range []string{"tool-stats", "--follow", "claude-code"} {
			if strings.Contains(script, forbidden) {
				t.Fatalf("the %s script hardcodes %q", shell, forbidden)
			}
		}
	}
}

func TestCompletionScriptRejectsWhatItCannotEmit(t *testing.T) {
	for _, args := range [][]string{
		{"kivgraph", "completion"},
		{"kivgraph", "completion", "tcsh"},
		{"kivgraph", "completion", "bash", "zsh"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 {
			t.Fatalf("%v = %d, want 2 (stderr=%q)", args, code, stderr.String())
		}
	}
}

// A command this build cannot run is not a candidate: the help says why, and
// completion would only offer a dead end.
func TestCompleteOmitsAnUnavailableCommand(t *testing.T) {
	got := candidates(t, "")
	if has(got, "ui") == (webBundleAbsence() != "") {
		t.Fatalf("ui offered = %t, but its absence is %q", has(got, "ui"), webBundleAbsence())
	}
}
