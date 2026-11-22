package goloader

import (
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
