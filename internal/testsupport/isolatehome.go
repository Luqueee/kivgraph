package testsupport

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// IsolateHome runs a package's tests under a throwaway home directory and
// returns their exit code. It is for a TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testsupport.IsolateHome(m)) }
//
// SetHome redirects one test, and a test has to know it needs it. The ones that
// did not were initialising a configuration at a temporary path, which looks
// isolated and is not: the synthetic Go workspace of a configuration outside the
// default location lives in the user's state directory by design, so each of
// them left a directory under the real `~/.local/state/kivgraph`. Enough of
// those and the next real run finds legacy state beside an old recovery point
// and refuses to start.
//
// The toolchain caches stay where they are. They are found through the home
// directory too, and a package that type-checks Go or runs cargo against an
// empty cache downloads the world or fails without a network.
func IsolateHome(m *testing.M) int {
	home, err := os.MkdirTemp("", "kivgraph-test-home-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "isolate home: %v\n", err)
		return 1
	}
	defer os.RemoveAll(home)

	for _, name := range []string{"GOMODCACHE", "GOCACHE", "GOPATH"} {
		if os.Getenv(name) != "" {
			continue
		}
		if out, err := exec.Command("go", "env", name).Output(); err == nil {
			if value := strings.TrimSpace(string(out)); value != "" {
				os.Setenv(name, value)
			}
		}
	}
	if real, err := os.UserHomeDir(); err == nil {
		for name, directory := range map[string]string{"RUSTUP_HOME": ".rustup", "CARGO_HOME": ".cargo"} {
			if os.Getenv(name) == "" {
				os.Setenv(name, filepath.Join(real, directory))
			}
		}
	}

	// The same variables SetHome sets, for the same reasons.
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	os.Setenv("CODEX_HOME", "")
	os.Setenv("PI_CODING_AGENT_DIR", "")
	if runtime.GOOS == "windows" {
		os.Setenv("LocalAppData", home)
		os.Setenv("APPDATA", home)
	}
	return m.Run()
}
