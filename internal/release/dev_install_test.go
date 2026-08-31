package release_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDevInstallRefusesAMissingInstallation(t *testing.T) {
	base := t.TempDir()
	bundle := fakeDevBundle(t, base, "new")
	installRoot := filepath.Join(base, "missing")

	result := runDevInstaller(t, base, installRoot, bundle, false)
	if result.err == nil {
		t.Fatalf("dev installer created installation %q from bundle %q instead of replacing one",
			installRoot, bundle)
	}
	if !strings.Contains(result.output, "existing Kivgraph installation") {
		t.Fatalf("missing installation error gave no remedy (installRoot=%q, bundle=%q):\n%s",
			installRoot, bundle, result.output)
	}
}

func TestDevInstallRefusesAnInstallationHeldByAnotherProcess(t *testing.T) {
	base := t.TempDir()
	installRoot := fakeDevInstallation(t, base, "old", "absent")
	bundle := fakeDevBundle(t, base, "new")
	if err := os.Mkdir(filepath.Join(installRoot, ".kivgraph-dev-install.lock"), 0o700); err != nil {
		t.Fatal(err)
	}

	result := runDevInstaller(t, base, installRoot, bundle, false)
	if result.err == nil {
		t.Fatalf("dev installer entered an installation held by another process (installRoot=%q, bundle=%q)",
			installRoot, bundle)
	}
	if !strings.Contains(result.output, "another development installation") {
		t.Fatalf("lock contention error gave no remedy:\n%s", result.output)
	}
	assertDevMarker(t, installRoot, "old")
}

func TestDevInstallRefusesAStaleSupervisorBeforeReplacing(t *testing.T) {
	base := t.TempDir()
	installRoot := fakeDevInstallation(t, base, "old", "stale")
	bundle := fakeDevBundle(t, base, "new")

	result := runDevInstaller(t, base, installRoot, bundle, false)
	if result.err == nil {
		t.Fatalf("dev installer replaced a bundle whose supervisor was stale (installRoot=%q, bundle=%q)",
			installRoot, bundle)
	}
	if !strings.Contains(result.output, "supervisor is stale") {
		t.Fatalf("stale supervisor error gave no remedy:\n%s", result.output)
	}
	assertDevMarker(t, installRoot, "old")
}

func TestDevInstallRefusesAnIncompatibleNativeLibrary(t *testing.T) {
	base := t.TempDir()
	installRoot := fakeDevInstallation(t, base, "old", "absent")
	bundle := fakeDevBundle(t, base, "new")
	for _, name := range []string{"liblbug.dylib", "liblbug.so"} {
		if err := os.WriteFile(filepath.Join(bundle, "lib", name), []byte("different library\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeDevChecksums(t, bundle)

	result := runDevInstaller(t, base, installRoot, bundle, false)
	if result.err == nil {
		t.Fatal("dev installer combined a binary with an incompatible native library")
	}
	if !strings.Contains(result.output, "different LadybugDB library") {
		t.Fatalf("native library mismatch gave no remedy:\n%s", result.output)
	}
	assertDevMarker(t, installRoot, "old")
}

func TestDevInstallRollsBackWhenDaemonRestartFails(t *testing.T) {
	base := t.TempDir()
	installRoot := fakeDevInstallation(t, base, "old", "installed")
	bundle := fakeDevBundle(t, base, "new")

	result := runDevInstaller(t, base, installRoot, bundle, true)
	if result.err == nil {
		t.Fatalf("dev installer reported success after restart failure (installRoot=%q, bundle=%q)",
			installRoot, bundle)
	}
	log, err := os.ReadFile(filepath.Join(base, "supervisor.log"))
	if err != nil {
		t.Fatalf("restart failure did not attempt the supervisor: %v", err)
	}
	want := strings.Join([]string{
		expectedSupervisorInvocation("new"),
		expectedSupervisorInvocation("old"),
	}, "\n")
	if got := strings.TrimSpace(string(log)); got != want {
		t.Fatalf("restart and rollback invocations = %q, want %q", got, want)
	}
	assertDevMarker(t, installRoot, "old")
	assertDevInstallerCanReenter(t, base, installRoot, bundle)
}

func TestDevInstallReplacesBundleAndRestartsInstalledDaemon(t *testing.T) {
	base := t.TempDir()
	installRoot := fakeDevInstallation(t, base, "old", "installed")
	bundle := fakeDevBundle(t, base, "new")

	result := runDevInstaller(t, base, installRoot, bundle, false)
	if result.err != nil {
		t.Fatalf("dev installer failed: %v\n%s", result.err, result.output)
	}
	assertDevMarker(t, installRoot, "new")
	log, err := os.ReadFile(filepath.Join(base, "supervisor.log"))
	if err != nil {
		t.Fatalf("read supervisor log: %v", err)
	}
	want := expectedSupervisorInvocation("new")
	if got := strings.TrimSpace(string(log)); got != want {
		t.Fatalf("supervisor invocation = %q, want %q", got, want)
	}
	if !strings.Contains(result.output, "daemon restarted") {
		t.Fatalf("successful restart was not reported:\n%s", result.output)
	}
	assertDevInstallerCanReenter(t, base, installRoot, bundle)
}

func TestDevInstallCanExplicitlySkipSupervisorDiscovery(t *testing.T) {
	base := t.TempDir()
	installRoot := fakeDevInstallation(t, base, "old", "broken")
	bundle := fakeDevBundle(t, base, "new")

	result := runDevInstaller(t, base, installRoot, bundle, false, "--no-restart")
	if result.err != nil {
		t.Fatalf("dev installer --no-restart failed: %v\n%s", result.err, result.output)
	}
	assertDevMarker(t, installRoot, "new")
	log, err := os.ReadFile(filepath.Join(base, "supervisor.log"))
	if err == nil {
		t.Fatalf("--no-restart invoked a supervisor with %q", strings.TrimSpace(string(log)))
	}
	if !os.IsNotExist(err) {
		t.Fatalf("read supervisor log: %v", err)
	}
}

func runDevInstaller(t *testing.T, base, installRoot, bundle string, failRestart bool, extra ...string) commandResult {
	t.Helper()
	if !((runtime.GOOS == "linux" && runtime.GOARCH == "amd64") ||
		(runtime.GOOS == "darwin" && runtime.GOARCH == "arm64")) {
		t.Skipf("development installer is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	fakeBin := filepath.Join(base, "fake-bin")
	if err := os.MkdirAll(fakeBin, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, supervisor := range []string{"systemctl", "launchctl"} {
		body := fmt.Sprintf("#!/bin/sh\n[ -d %q ] || exit 97\nmarker=$(%q marker) || exit 98\nprintf '%s %%s marker=%%s\\n' \"$*\" \"$marker\" >> %q\n",
			filepath.Join(installRoot, ".kivgraph-dev-install.lock"),
			filepath.Join(installRoot, "bin", "kivgraph"), supervisor,
			filepath.Join(base, "supervisor.log"))
		if failRestart {
			body += "exit 1\n"
		}
		if err := os.WriteFile(filepath.Join(fakeBin, supervisor), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(fakeBin, "id"), []byte("#!/bin/sh\nprintf '4242\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	arguments := append([]string{filepath.Join("..", "..", "scripts", "install-dev.sh"), "--bundle", bundle}, extra...)
	command := exec.Command("bash", arguments...)
	command.Env = append(os.Environ(),
		"HOME="+filepath.Join(base, "home"),
		"KIVGRAPH_INSTALL_ROOT="+installRoot,
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	output, err := command.CombinedOutput()
	return commandResult{output: string(output), err: err}
}

func expectedSupervisorInvocation(marker string) string {
	if runtime.GOOS == "darwin" {
		return "launchctl kickstart -k gui/4242/com.kivgraph.daemon.test marker=" + marker
	}
	return "systemctl --user restart com.kivgraph.daemon.test.service marker=" + marker
}

func fakeDevInstallation(t *testing.T, base, marker, supervisorState string) string {
	t.Helper()
	installRoot := filepath.Join(base, "installed")
	for _, directory := range []string{"bin", "lib"} {
		if err := os.MkdirAll(filepath.Join(installRoot, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeDevBinary(t, filepath.Join(installRoot, "bin", "kivgraph"), marker, supervisorState)
	writeFakeNativeLibraries(t, installRoot)
	if err := os.WriteFile(filepath.Join(installRoot, "manifest.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return installRoot
}

func fakeDevBundle(t *testing.T, base, marker string) string {
	t.Helper()
	bundle := filepath.Join(base, "bundle-"+marker)
	for _, directory := range []string{"bin", "lib"} {
		if err := os.MkdirAll(filepath.Join(bundle, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeDevBinary(t, filepath.Join(bundle, "bin", "kivgraph"), marker, "absent")
	writeFakeNativeLibraries(t, bundle)
	if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeDevChecksums(t, bundle)
	return bundle
}

func writeDevChecksums(t *testing.T, bundle string) {
	t.Helper()
	var lines strings.Builder
	for _, relative := range []string{"bin/kivgraph", "lib/liblbug.dylib", "lib/liblbug.so", "manifest.json"} {
		body, err := os.ReadFile(filepath.Join(bundle, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&lines, "%x  %s\n", sha256.Sum256(body), relative)
	}
	if err := os.WriteFile(filepath.Join(bundle, "SHA256SUMS"), []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFakeNativeLibraries(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"liblbug.dylib", "liblbug.so"} {
		if err := os.WriteFile(filepath.Join(root, "lib", name), []byte("same pinned library\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeDevBinary(t *testing.T, path, marker, supervisorState string) {
	t.Helper()
	body := fmt.Sprintf(`#!/bin/sh
case "${1:-}" in
  version) printf '{"version":"dev"}\n' ;;
  marker) printf '%%s\n' %q ;;
  daemon)
    if [ "${2:-}" = status ]; then
      [ %q != broken ] || exit 1
      printf 'daemon.supervisor: state=%s label=com.kivgraph.daemon.test\n'
    fi
    ;;
esac
`, marker, supervisorState, supervisorState)
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
}

func assertDevMarker(t *testing.T, installRoot, want string) {
	t.Helper()
	command := exec.Command(filepath.Join(installRoot, "bin", "kivgraph"), "marker")
	got, err := command.Output()
	if err != nil {
		t.Fatalf("read installed marker: %v", err)
	}
	if strings.TrimSpace(string(got)) != want {
		t.Fatalf("installed marker = %q, want %q", strings.TrimSpace(string(got)), want)
	}
}

func assertDevInstallerCanReenter(t *testing.T, base, installRoot, bundle string) {
	t.Helper()
	result := runDevInstaller(t, base, installRoot, bundle, false, "--no-restart")
	if result.err != nil {
		t.Fatalf("development installer could not re-enter after completion: %v\n%s",
			result.err, result.output)
	}
}
