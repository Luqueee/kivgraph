package testsupport

import (
	"os/exec"
	"strings"
	"testing"
)

// RequireRustAnalyzer skips the test unless this machine can actually run
// rust-analyzer.
//
// Presence on PATH does not answer that. rustup installs a proxy named
// rust-analyzer for every toolchain, whether or not the component behind it
// was ever added, and that proxy exits with "Unknown binary 'rust-analyzer'
// in official toolchain". A guard that only looks the name up reports the
// analyzer as installed on any machine with rustup, so the tests run and
// fail instead of skipping — which is exactly what a hosted runner without
// `rustup component add rust-analyzer` looks like.
//
// Running `--version` is the cheapest question whose answer is the one the
// caller needs.
func RequireRustAnalyzer(t testing.TB) {
	t.Helper()
	resolved, err := exec.LookPath("rust-analyzer")
	if err != nil {
		t.Skipf("rust-analyzer is not installed: %v", err)
	}
	output, err := exec.Command(resolved, "--version").CombinedOutput()
	if err != nil {
		t.Skipf("rust-analyzer at %s does not run: %v: %s",
			resolved, err, strings.TrimSpace(string(output)))
	}
}
