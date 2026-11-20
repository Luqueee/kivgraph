package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func runImpactCommand(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errors bytes.Buffer
	code = runImpact(args, &out, &errors)
	return code, out.String(), errors.String()
}

// The flags are checked before git or the graph is touched, so each refusal says
// which flag is wrong and what it may be, and none of them needs a repository.
func TestImpactRefusesAnInvocationItCannotAnswer(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args []string
		want string
	}{
		{"no base", []string{}, "--base is required"},
		{"an empty base", []string{"--base", ""}, "--base is required"},
		{"a stray argument", []string{"--base", "HEAD", "extra"}, "unexpected arguments"},
		{"depth zero", []string{"--base", "HEAD", "--depth", "0"}, "--depth must be between 1 and"},
		{"depth over the maximum", []string{"--base", "HEAD", "--depth", "99"}, "--depth must be between 1 and"},
		{"max nodes zero", []string{"--base", "HEAD", "--max-nodes", "0"}, "--max-nodes must be between 1 and"},
		{"max nodes over the maximum", []string{"--base", "HEAD", "--max-nodes", "99999999"}, "--max-nodes must be between 1 and"},
		{"limit zero", []string{"--base", "HEAD", "--limit", "0"}, "--limit must be at least 1"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			code, stdout, stderr := runImpactCommand(t, testCase.args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, testCase.want) {
				t.Fatalf("code = %d stdout = %q stderr = %q, want exit 2 naming %q", code, stdout, stderr, testCase.want)
			}
		})
	}
}

func TestImpactOutsideACheckoutFailsAndSaysSo(t *testing.T) {
	t.Chdir(t.TempDir())
	code, stdout, stderr := runImpactCommand(t, "--base", "HEAD")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "not inside a git checkout") {
		t.Fatalf("code = %d stdout = %q stderr = %q, want exit 1 naming the problem", code, stdout, stderr)
	}
}

// A ref git does not know is refused before the graph is opened, so a typo costs
// nothing and the message names it.
func TestImpactNamesARefThatIsNotACommit(t *testing.T) {
	directory := t.TempDir()
	for _, arguments := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "base"}} {
		command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", arguments, err, output)
		}
	}
	t.Chdir(directory)
	code, stdout, stderr := runImpactCommand(t, "--base", "origin/mian")
	if code != 1 || stdout != "" || !strings.Contains(stderr, `unknown ref "origin/mian"`) {
		t.Fatalf("code = %d stdout = %q stderr = %q, want exit 1 naming the ref", code, stdout, stderr)
	}
}

func TestImpactHelpListsItsFlagsAndTheCommandIsRegistered(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "impact", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("impact --help exited %d: %s", code, stderr.String())
	}
	for _, flag := range []string{"--base", "--depth", "--max-nodes", "--limit", "--allow-stale", "--json", "--config"} {
		if !strings.Contains(stdout.String(), flag) {
			t.Errorf("help does not list %s:\n%s", flag, stdout.String())
		}
	}
}
