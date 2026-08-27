package indexing

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// The child RunDetached spawns used to be a shell script, which kept every
// test of it on Unix: there is no interpreter for a `#!` line on Windows and
// no /bin/sh to name in one. Writing each body twice, once as a batch file,
// would have made the fixture the thing under review -- and the fixture is not
// what RunDetached is.
//
// So the child is this test binary, re-executed. `os/exec` inherits the
// environment, so the parent hands the child its whole script in one variable
// and TestMain acts on it before the testing package has parsed a flag or run
// a case. What the child can do is now a closed set -- record its arguments,
// write lines to each stream, report what stdin gave it, exit with a code --
// which is narrower than a shell and is every capability these tests used.
//
// The gain is not only Windows: a scripted child cannot drift into shell
// quoting that means something different under a different /bin/sh, and the
// arguments it records are the ones the exec layer actually passed rather than
// whatever a word-splitting pass made of them.
const fakeIndexChildVariable = "KIVGRAPH_TEST_FAKE_INDEX_CHILD"

// stdinPlaceholder is replaced, in any stdout line carrying it, by the first
// line the child read from stdin -- or by "none" when stdin gave it nothing,
// which is the case RunDetached exists to guarantee.
const stdinPlaceholder = "{{stdin}}"

type fakeChildScript struct {
	ArgumentsFile string   `json:"arguments_file"`
	Stdout        []string `json:"stdout"`
	Stderr        []string `json:"stderr"`
	ExitCode      int      `json:"exit_code"`
}

func TestMain(m *testing.M) {
	if encoded := os.Getenv(fakeIndexChildVariable); encoded != "" {
		os.Exit(runFakeIndexChild(encoded))
	}
	os.Exit(m.Run())
}

// runFakeIndexChild answers like `kivgraph index --full --json` was told to.
// Its exit code 2 is reserved for the fixture failing, so that a test cannot
// read a broken child as the failure it was scripted to produce.
func runFakeIndexChild(encoded string) int {
	var script fakeChildScript
	if err := json.Unmarshal([]byte(encoded), &script); err != nil {
		fmt.Fprintf(os.Stderr, "fake index child: decode script: %v\n", err)
		return 2
	}
	if script.ArgumentsFile != "" {
		recorded := strings.Join(os.Args[1:], "\n") + "\n"
		if err := os.WriteFile(script.ArgumentsFile, []byte(recorded), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "fake index child: record arguments: %v\n", err)
			return 2
		}
	}
	for _, line := range script.Stderr {
		fmt.Fprintln(os.Stderr, line)
	}
	// stdin is read only when a line asks for it. Reading it unconditionally
	// would block a child whose parent left the pipe open, which is a hang in
	// the fixture wearing the costume of a hang in the code.
	stdin := ""
	for _, line := range script.Stdout {
		if strings.Contains(line, stdinPlaceholder) {
			if stdin == "" {
				stdin = firstStdinLine()
			}
			line = strings.ReplaceAll(line, stdinPlaceholder, stdin)
		}
		fmt.Fprintln(os.Stdout, line)
	}
	return script.ExitCode
}

func firstStdinLine() string {
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text()
	}
	return "none"
}

// fakeIndexChild points RunDetached at this test binary and gives it a script.
// The returned arguments file holds one argument per line once the child has
// run, which is the half of the contract the event stream cannot show.
func fakeIndexChild(t *testing.T, script fakeChildScript) (executable, argumentsFile string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve the test binary to re-execute as the child: %v", err)
	}
	argumentsFile = filepath.Join(testsupport.TempDir(t), "arguments")
	script.ArgumentsFile = argumentsFile
	encoded, err := json.Marshal(script)
	if err != nil {
		t.Fatalf("encode the child script: %v", err)
	}
	// t.Setenv restores the variable when the test ends, so a later test that
	// spawns anything does not inherit a child that answers as this one.
	t.Setenv(fakeIndexChildVariable, string(encoded))
	return executable, argumentsFile
}
