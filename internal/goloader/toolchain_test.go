package goloader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The pin is asked for only when the installed `go` is newer than this build:
// an older or equal one already reads a standard library this build parses,
// and forcing a toolchain it does not have would break a pass that works.
func TestToolchainPin(t *testing.T) {
	for _, testCase := range []struct {
		installed, built, want string
	}{
		{"go1.27.4", "go1.26.11", "go1.26.11"},
		{"go1.26.12", "go1.26.11", "go1.26.11"},
		{"go1.27rc1", "go1.26.11", "go1.26.11"},
		{"go1.26.11", "go1.26.11", ""},
		{"go1.25.0", "go1.26.11", ""},
		// Nothing to compare: no `go`, or a build of the development tree.
		{"", "go1.26.11", ""},
		{"go1.27.4", "devel +abc123", ""},
		{"not a version", "go1.26.11", ""},
	} {
		if got := toolchainPin(testCase.installed, testCase.built); got != testCase.want {
			t.Errorf("toolchainPin(%q, %q) = %q, want %q", testCase.installed, testCase.built, got, testCase.want)
		}
	}
}

// A GOTOOLCHAIN the user set is not replaced, and nothing sets one twice.
func TestLoadEnvironmentLeavesTheUsersToolchainAlone(t *testing.T) {
	t.Setenv("GOTOOLCHAIN", "go1.20.0")
	environment, err := loadEnvironment(Options{})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, entry := range environment {
		if strings.HasPrefix(entry, "GOTOOLCHAIN=") {
			seen++
			if entry != "GOTOOLCHAIN=go1.20.0" {
				t.Errorf("environment carries %q, want the user's", entry)
			}
		}
	}
	if seen != 1 {
		t.Errorf("GOTOOLCHAIN appears %d times, want the inherited one alone", seen)
	}
}

// `go env -w GOTOOLCHAIN=...` is a choice as much as the variable is, and the
// pin overrode it: the user's file is read, and a value there is left alone.
func TestToolchainPinLeavesASavedChoiceAlone(t *testing.T) {
	directory := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for name, test := range map[string]struct {
		path string
		want bool
	}{
		"saved":              {write("saved", "GOFLAGS=-mod=mod\nGOTOOLCHAIN=go1.99.0\n"), true},
		"saved with spaces":  {write("spaced", "  GOTOOLCHAIN=local  \r\n"), true},
		"other keys only":    {write("other", "GOFLAGS=-mod=mod\nNOTGOTOOLCHAIN=x\n"), false},
		"set to nothing":     {write("empty", "GOTOOLCHAIN=\n"), false},
		"no file":            {filepath.Join(directory, "missing"), false},
		"no file configured": {"", false},
	} {
		if got := savedToolchain(test.path); got != test.want {
			t.Errorf("%s: savedToolchain = %t, want %t", name, got, test.want)
		}
	}

	t.Setenv("GOENV", "off")
	if got := userGoEnvironmentFile(); got != "" {
		t.Errorf("GOENV=off: file = %q, want none", got)
	}
	t.Setenv("GOENV", write("chosen", "GOTOOLCHAIN=go1.99.0\n"))
	if got := userGoEnvironmentFile(); got != filepath.Join(directory, "chosen") {
		t.Errorf("GOENV set: file = %q", got)
	}
	// Not covered: that ToolchainPin asks this before it pins. Under `go test`
	// the `go` on PATH is the toolchain that built the test, so nothing is
	// ever newer than the build and there is no pin to withhold.
}
