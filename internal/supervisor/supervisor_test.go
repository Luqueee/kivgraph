package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func testSpec(directory string) Spec {
	return Spec{Executable: "/opt/kivgraph/bin/kivgraph", StateDirectory: directory}
}

// TestALabelSeparatesTwoStateDirectories is the identity claim. A daemon serves
// one state directory, so two configurations have to be able to hold two
// supervised daemons at once; one label for both would let an install silently
// replace the other operator's daemon.
func TestALabelSeparatesTwoStateDirectories(t *testing.T) {
	first, err := testSpec(t.TempDir()).Label()
	if err != nil {
		t.Fatalf("Label() error = %v", err)
	}
	second, err := testSpec(t.TempDir()).Label()
	if err != nil {
		t.Fatalf("Label() error = %v", err)
	}
	if first == second {
		t.Fatalf("two state directories produced one label %q, so one install would replace the other", first)
	}
	if !strings.HasPrefix(first, labelPrefix+".") {
		t.Fatalf("label %q does not carry the operator-facing prefix %q", first, labelPrefix)
	}
}

// TestALabelIsStableAcrossSpellings covers the other half: the same daemon must
// keep its label, or an update would install a second unit beside the first
// instead of replacing it.
func TestALabelIsStableAcrossSpellings(t *testing.T) {
	directory := t.TempDir()
	plain, err := testSpec(directory).Label()
	if err != nil {
		t.Fatalf("Label() error = %v", err)
	}
	decorated, err := testSpec(filepath.Join(directory, "child", "..")).Label()
	if err != nil {
		t.Fatalf("Label() error = %v", err)
	}
	if plain != decorated {
		t.Fatalf("two spellings of one directory produced %q and %q, so an update would install a second unit", plain, decorated)
	}
}

// TestAnIncompleteSpecIsRefused keeps an install from writing a unit whose
// ExecStart names nothing. Both fields land in the file as absolute paths, so
// neither can be filled in later by the supervisor.
func TestAnIncompleteSpecIsRefused(t *testing.T) {
	for name, spec := range map[string]Spec{
		"no executable":      {StateDirectory: t.TempDir()},
		"no state directory": {Executable: "/opt/kivgraph/bin/kivgraph"},
		"blank executable":   {Executable: "   ", StateDirectory: t.TempDir()},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Install(spec); !errors.Is(err, ErrIncompleteSpec) {
				t.Fatalf("Install() error = %v, want ErrIncompleteSpec", err)
			}
			if _, err := Status(spec); !errors.Is(err, ErrIncompleteSpec) {
				t.Fatalf("Status() error = %v, want ErrIncompleteSpec", err)
			}
			if _, err := Remove(spec); !errors.Is(err, ErrIncompleteSpec) {
				t.Fatalf("Remove() error = %v, want ErrIncompleteSpec", err)
			}
		})
	}
}

// TestArgumentsCarryOnlyWhatWasAsked keeps a default install from recording an
// empty --config or --addr, which the daemon would reject at start: a supervised
// daemon that cannot start is the failure this package exists to prevent.
func TestArgumentsCarryOnlyWhatWasAsked(t *testing.T) {
	bare := testSpec("/state").arguments()
	want := []string{"/opt/kivgraph/bin/kivgraph", "daemon"}
	if strings.Join(bare, " ") != strings.Join(want, " ") {
		t.Fatalf("arguments() = %v, want %v", bare, want)
	}
	full := Spec{
		Executable:     "/opt/kivgraph/bin/kivgraph",
		StateDirectory: "/state",
		ConfigPath:     "/etc/kivgraph.yaml",
		Address:        "127.0.0.1:9000",
	}.arguments()
	wantFull := "/opt/kivgraph/bin/kivgraph daemon --config /etc/kivgraph.yaml --addr 127.0.0.1:9000"
	if strings.Join(full, " ") != wantFull {
		t.Fatalf("arguments() = %q, want %q", strings.Join(full, " "), wantFull)
	}
}

// TestARemoteBindTravelsWithItsPermission is the pair the daemon refuses to
// separate: it rejects a non-loopback bind without --allow-remote, so a unit
// recording the address and not the permission would start a daemon that exits
// immediately -- and the supervisor would restart it, forever.
func TestARemoteBindTravelsWithItsPermission(t *testing.T) {
	recorded := strings.Join(Spec{
		Executable:     "/opt/kivgraph/bin/kivgraph",
		StateDirectory: "/state",
		Address:        "192.0.2.1:9000",
		AllowRemote:    true,
	}.arguments(), " ")
	want := "/opt/kivgraph/bin/kivgraph daemon --addr 192.0.2.1:9000 --allow-remote"
	if recorded != want {
		t.Fatalf("arguments() = %q, want %q", recorded, want)
	}
}

// TestStatusReportsTheThreeStates is the whole reason Status exists: an operator
// asking whether the daemon has an owner gets one of three answers, and a unit
// somebody edited by hand is reported rather than overruled.
func TestStatusReportsTheThreeStates(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("this platform has no supervisor: %s", runtime.GOOS)
	}
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	spec := testSpec(t.TempDir())

	absent, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if absent.State != StateAbsent {
		t.Fatalf("Status() on a clean home = %q, want %q", absent.State, StateAbsent)
	}
	if absent.Path == "" {
		t.Fatal("Status() named no path, so an operator cannot see where the unit would land")
	}

	// Writing exactly what an install would write is what "installed" means:
	// the comparison is against the rendered unit, not against a marker file.
	if err := os.MkdirAll(filepath.Dir(absent.Path), 0o755); err != nil {
		t.Fatalf("create unit directory: %v", err)
	}
	if err := os.WriteFile(absent.Path, renderedUnit(t, spec), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	installed, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if installed.State != StateInstalled {
		t.Fatalf("Status() over a rendered unit = %q, want %q", installed.State, StateInstalled)
	}

	if err := os.WriteFile(absent.Path, []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	stale, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if stale.State != StateStale {
		t.Fatalf("Status() over an edited unit = %q, want %q", stale.State, StateStale)
	}
}

// TestRemovingAnAbsentUnitIsNotAFailure keeps `daemon remove` usable as the
// cleanup half of an install that never happened: it reports the state the
// caller asked for.
func TestRemovingAnAbsentUnitIsNotAFailure(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("this platform has no supervisor: %s", runtime.GOOS)
	}
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	report, err := Remove(testSpec(t.TempDir()))
	if err != nil {
		t.Fatalf("Remove() on a clean home error = %v", err)
	}
	if report.State != StateAbsent {
		t.Fatalf("Remove() = %q, want %q", report.State, StateAbsent)
	}
}

// TestTheUnitPromisesARestart is the contract the whole change rests on. Without
// it the supervisor is a launcher, and registering every client against one
// daemon would trade memory for a failure nothing recovers from.
func TestTheUnitPromisesARestart(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("this platform has no supervisor: %s", runtime.GOOS)
	}
	rendered := string(renderedUnit(t, testSpec("/state")))
	promise := map[string]string{
		"darwin": "KeepAlive",
		"linux":  "Restart=on-failure",
	}[runtime.GOOS]
	if !strings.Contains(rendered, promise) {
		t.Fatalf("the rendered unit does not carry %q, so nothing restarts the daemon:\n%s", promise, rendered)
	}
	if !strings.Contains(rendered, "/opt/kivgraph/bin/kivgraph") {
		t.Fatalf("the rendered unit does not name the executable:\n%s", rendered)
	}
}

// TestAPathNeedingQuotingSurvives covers the encoding, which is not cosmetic: an
// ampersand breaks a plist launchd refuses to parse, and a space splits one
// ExecStart word into two arguments. Both produce a daemon that never starts.
func TestAPathNeedingQuotingSurvives(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("this platform has no supervisor: %s", runtime.GOOS)
	}
	spec := Spec{
		Executable:     "/opt/my apps/kiv & graph/kivgraph",
		StateDirectory: "/state dir",
	}
	rendered := string(renderedUnit(t, spec))
	switch runtime.GOOS {
	case "darwin":
		if strings.Contains(rendered, "kiv & graph") {
			t.Fatalf("a raw ampersand reached the plist, which launchd cannot parse:\n%s", rendered)
		}
		if !strings.Contains(rendered, "kiv &amp; graph") {
			t.Fatalf("the plist does not carry the escaped executable:\n%s", rendered)
		}
	case "linux":
		if !strings.Contains(rendered, `ExecStart="/opt/my apps/kiv & graph/kivgraph" daemon`) {
			t.Fatalf("the unit does not quote a path holding a space:\n%s", rendered)
		}
	}
}

// TestRestartingAnUnownedUnitChangesNothing fixes the contract `kivgraph
// update` rests on. Restart is a request and not a question, so it could have
// returned ErrUnsupportedPlatform the way Install and Remove do -- but a caller
// facing a daemon nobody supervises has something else to do about it and has
// to be told which case it is, not handed an error that reads as a failure to
// restart something that was there.
//
// The states below are also the ones where nothing may be executed: a stale
// unit is an operator's hand edit, and restarting through it would start
// whatever they wrote instead of what this spec describes.
func TestRestartingAnUnownedUnitChangesNothing(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("this platform has no supervisor: %s", runtime.GOOS)
	}
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	spec := testSpec(t.TempDir())

	absent, err := Restart(spec)
	if err != nil {
		t.Fatalf("Restart() on a clean home error = %v", err)
	}
	if absent.State != StateAbsent {
		t.Fatalf("Restart() on a clean home = %q, want %q", absent.State, StateAbsent)
	}

	if err := os.MkdirAll(filepath.Dir(absent.Path), 0o755); err != nil {
		t.Fatalf("create unit directory: %v", err)
	}
	if err := os.WriteFile(absent.Path, []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	stale, err := Restart(spec)
	if err != nil {
		t.Fatalf("Restart() over an edited unit error = %v", err)
	}
	if stale.State != StateStale {
		t.Fatalf("Restart() over an edited unit = %q, want %q", stale.State, StateStale)
	}
}

// An incomplete spec is refused before anything is read, the same way Install,
// Remove and Status refuse it: the executable and the state directory are
// absolute paths in the installed file and neither can be inferred later.
func TestRestartingAnIncompleteSpecIsRefused(t *testing.T) {
	if _, err := Restart(Spec{StateDirectory: t.TempDir()}); !errors.Is(err, ErrIncompleteSpec) {
		t.Fatalf("Restart() without an executable error = %v, want %v", err, ErrIncompleteSpec)
	}
	if _, err := Restart(Spec{Executable: "/opt/kivgraph/bin/kivgraph"}); !errors.Is(err, ErrIncompleteSpec) {
		t.Fatalf("Restart() without a state directory error = %v, want %v", err, ErrIncompleteSpec)
	}
}

// TestTheUnitRecordsThePathThatInstalledIt is the fix for a daemon that cannot
// find node. Neither supervisor reads a shell profile, so a unit that declares
// no PATH runs the daemon on the platform's own -- which holds nothing anybody
// installed for themselves, and makes `kivgraph-ts-worker` die on `exec node`
// while the same command typed in a terminal works.
func TestTheUnitRecordsThePathThatInstalledIt(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("this platform has no supervisor: %s", runtime.GOOS)
	}
	t.Setenv("PATH", "/home/ada/.nvm/versions/node/v24.18.0/bin:/usr/bin:/bin")
	rendered := string(renderedUnit(t, testSpec("/state")))
	if !strings.Contains(rendered, "/home/ada/.nvm/versions/node/v24.18.0/bin:/usr/bin:/bin") {
		t.Fatalf("the rendered unit records no PATH, so the daemon inherits the supervisor's:\n%s", rendered)
	}
}

// TestAnotherShellsPathDoesNotMakeAnInstallStale is the other half, and the
// one that decides whether the fix is usable. The recorded PATH belongs to the
// terminal that ran `daemon install`; every later shell has its own. Comparing
// it would report a working daemon stale from any of them and send its
// operator to reinstall for nothing -- which is how `stale` stops meaning
// anything.
func TestAnotherShellsPathDoesNotMakeAnInstallStale(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("this platform has no supervisor: %s", runtime.GOOS)
	}
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	spec := testSpec(t.TempDir())

	t.Setenv("PATH", "/home/ada/.nvm/versions/node/v24.18.0/bin:/usr/bin")
	installed, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(installed.Path), 0o755); err != nil {
		t.Fatalf("create unit directory: %v", err)
	}
	if err := os.WriteFile(installed.Path, renderedUnit(t, spec), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}

	t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin:/bin")
	report, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if report.State != StateInstalled {
		t.Fatalf("Status() from a shell with another PATH = %q (%s), want %q",
			report.State, report.Detail, StateInstalled)
	}
}

// TestAUnitCarryingNoPathIsStale covers the upgrade, which is the case every
// existing installation is in: the unit on disk was written before any PATH was
// recorded, and the daemon under it is precisely the one that cannot resolve
// node. Reporting it installed would leave the defect in place on every machine
// that already has a daemon.
func TestAUnitCarryingNoPathIsStale(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("this platform has no supervisor: %s", runtime.GOOS)
	}
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	spec := testSpec(t.TempDir())

	// An empty PATH renders exactly what the previous release wrote: recording
	// an empty one would be worse than recording none, so the unit carries no
	// environment at all.
	t.Setenv("PATH", "")
	planned, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	previousRelease := renderedUnit(t, spec)
	if strings.Contains(string(previousRelease), "PATH") {
		t.Fatalf("a unit rendered without a PATH still mentions one:\n%s", previousRelease)
	}
	if err := os.MkdirAll(filepath.Dir(planned.Path), 0o755); err != nil {
		t.Fatalf("create unit directory: %v", err)
	}
	if err := os.WriteFile(planned.Path, previousRelease, 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}

	t.Setenv("PATH", "/opt/homebrew/bin:/usr/bin:/bin")
	report, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if report.State != StateStale {
		t.Fatalf("Status() over a unit with no recorded PATH = %q, want %q", report.State, StateStale)
	}
	if !strings.Contains(report.Detail, "PATH") {
		t.Fatalf("Status() detail %q does not say what is missing", report.Detail)
	}
}
