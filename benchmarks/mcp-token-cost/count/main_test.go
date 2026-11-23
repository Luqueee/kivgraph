package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The counter is run as the command it is: a recapture pipes JSON through it,
// and the contract is the JSON that comes back.
func TestCountPricesEveryTextByItsKey(t *testing.T) {
	if os.Getenv("KIVGRAPH_COUNT_CHILD") == "1" {
		main()
		return
	}
	run := func(stdin string) (string, error) {
		command := exec.Command(os.Args[0], "-test.run=TestCountPricesEveryTextByItsKey")
		command.Env = append(os.Environ(), "KIVGRAPH_COUNT_CHILD=1")
		command.Stdin = strings.NewReader(stdin)
		var stdout bytes.Buffer
		command.Stdout = &stdout
		err := command.Run()
		return stdout.String(), err
	}

	// Not JSON, or JSON that is not a map of strings, is refused and counts nothing.
	for _, input := range []string{"", "not json", `["a"]`, `{"a": 1}`} {
		if out, err := run(input); err == nil || strings.Contains(out, `"a"`) {
			t.Errorf("input %q: err = %v, stdout = %q, want a refusal", input, err, out)
		}
	}

	out, err := run(`{"empty":"","word":"hello","two":"hello world"}`)
	if err != nil {
		t.Fatalf("count failed: %v", err)
	}
	var counts map[string]int
	// The test binary appends its own `PASS` after the child's output.
	if err := json.NewDecoder(strings.NewReader(out)).Decode(&counts); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if counts["empty"] != 0 || counts["word"] != 1 || counts["two"] != 2 {
		t.Fatalf("counts = %v, want empty 0, \"hello\" 1 and \"hello world\" 2 in cl100k_base", counts)
	}
}
