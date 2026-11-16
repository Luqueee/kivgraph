package indexer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/executable"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// The facts entry point of the worker takes the arguments of the `facts`
// subcommand directly, and the launcher that every other layout goes through is
// what removes the word. A worker configured as `node <build>/facts-cli.js` has
// no launcher, so the indexer has to remove it, as it already does when it runs
// the entry point of a checkout. Without that the entry point reads `facts` as the
// name of the repository and prints its usage.
var factsInvocation = []string{
	"facts", "repo", "/work/repo", "/tmp/out.json", "--project", "/work/repo/tsconfig.json",
}

func workerProgram(t *testing.T) string {
	t.Helper()
	program := filepath.Join(testsupport.TempDir(t), executable.Name("node"))
	writeFullFixture(t, program, "#!/bin/sh\necho node\n")
	if err := os.Chmod(program, 0o700); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	return program
}

func TestFactsCommandKeepsTheFactsWordWhereALauncherWillRemoveIt(t *testing.T) {
	program := workerProgram(t)
	for name, configured := range map[string]string{
		"a launcher":                           program,
		"a script that is not the facts entry": program + " /work/ts-worker/dist/worker.js",
		"a script with a longer name":          program + " /work/ts-worker/dist/my-facts-cli.js",
		"a map of the entry":                   program + " /work/ts-worker/dist/facts-cli.js.map",
		"a directory called facts-cli.js":      program + " /work/facts-cli.js/index.js",
		"the entry in a flag":                  program + " --require=/work/facts-cli.js",
	} {
		command, arguments, err := factsCommand(FullOptions{TypeScriptWorker: configured}, factsInvocation)
		if err != nil {
			t.Fatalf("%s: factsCommand() error = %v", name, err)
		}
		want := append(strings.Fields(configured)[1:], factsInvocation...)
		if command != program || !slices.Equal(arguments, want) {
			t.Errorf("%s: arguments = %v, want the invocation kept whole, facts included: %v", name, arguments, want)
		}
	}
}

func TestFactsCommandRunsTheFactsEntryDirectly(t *testing.T) {
	program := workerProgram(t)
	entry := "/work/ts-worker/dist/facts-cli.js"
	for name, directory := range map[string]string{
		"a build output directory": "/work/ts-worker/dist",
		"any other directory":      "/work/scripts",
	} {
		entry := filepath.Join(directory, "facts-cli.js")
		command, arguments, err := factsCommand(FullOptions{TypeScriptWorker: program + " " + entry}, factsInvocation)
		if err != nil {
			t.Fatalf("%s: factsCommand() error = %v", name, err)
		}
		want := append([]string{entry}, factsInvocation[1:]...)
		if command != program || !slices.Equal(arguments, want) {
			t.Errorf("%s: %s %v, want %s %v", name, command, arguments, program, want)
		}
	}

	// Extra arguments the user configured stay in front, where they were.
	_, arguments, err := factsCommand(FullOptions{TypeScriptWorker: program + " --max-old-space-size=4096 " + entry}, factsInvocation)
	if err != nil {
		t.Fatal(err)
	}
	if want := append([]string{"--max-old-space-size=4096", entry}, factsInvocation[1:]...); !slices.Equal(arguments, want) {
		t.Errorf("arguments = %v, want %v", arguments, want)
	}
}

// Only the subcommand word is removed: an invocation that does not start with it
// is not the one this rule was written for, and nothing else is taken out.
func TestFactsCommandRemovesNothingButTheFactsWord(t *testing.T) {
	program := workerProgram(t)
	entry := "/work/ts-worker/dist/facts-cli.js"
	options := FullOptions{TypeScriptWorker: program + " " + entry}
	for name, invocation := range map[string][]string{
		"nothing, as the fingerprint asks": nil,
		"an empty list":                    {},
		"another subcommand":               {"hello", "repo"},
		"the word in another place":        {"repo", "facts"},
		"the word twice":                   {"facts", "facts", "repo"},
	} {
		_, arguments, err := factsCommand(options, invocation)
		if err != nil {
			t.Fatalf("%s: factsCommand() error = %v", name, err)
		}
		var rest []string
		if len(invocation) > 0 && invocation[0] == "facts" {
			rest = invocation[1:]
		} else {
			rest = invocation
		}
		if want := append([]string{entry}, rest...); !slices.Equal(arguments, want) {
			t.Errorf("%s: arguments = %v, want %v", name, arguments, want)
		}
	}
}

// The identity of the worker is asked with no invocation at all, and it has to see
// the same entry and the same build whether or not an invocation follows.
func TestWorkerIdentityOfAConfiguredFactsEntryIsTheBuild(t *testing.T) {
	fixture := newWorkerFixture(t)
	command, arguments, err := factsCommand(fixture.options, nil)
	if err != nil {
		t.Fatalf("factsCommand() error = %v", err)
	}
	if command == "" || !slices.Equal(arguments, []string{fixture.entry}) {
		t.Fatalf("factsCommand(nil) = %s %v, want the program and the entry alone", command, arguments)
	}
	before := fixture.fingerprint()
	fixture.write("symbol-extractor.js", "export const one = 2\n")
	if fixture.fingerprint() == before {
		t.Fatal("a configured facts entry stopped following its build")
	}
}
