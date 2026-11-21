package main

import (
	"bytes"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/integrations"
	"github.com/Luqueee/kivgraph/internal/supervisor"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// The case found in practice: a harness runs `serve` under a throwaway home,
// and the daemon it installed was started by the supervisor under the real one.
func TestForeignHomeNamesTheAccountsHomeUnderAnotherOne(t *testing.T) {
	current, err := user.Current()
	if err != nil || current.HomeDir == "" {
		t.Skipf("the account cannot be looked up here: %v", err)
	}
	testsupport.SetHome(t, testsupport.TempDir(t))
	account, foreign := foreignHome()
	if !foreign || account != current.HomeDir {
		t.Fatalf("foreignHome() = %q, %t, want %q, true", account, foreign, current.HomeDir)
	}
}

// And the three that are the account's own home: itself, a link to it, and a
// spelling of it that is not the one the account database holds.
func TestForeignHomeAcceptsEverySpellingOfTheAccountsHome(t *testing.T) {
	current, err := user.Current()
	if err != nil || current.HomeDir == "" {
		t.Skipf("the account cannot be looked up here: %v", err)
	}
	if _, err := os.Stat(current.HomeDir); err != nil {
		t.Skipf("the account's home is not a directory here: %v", err)
	}
	spellings := map[string]string{
		"itself":         current.HomeDir,
		"trailing slash": current.HomeDir + string(filepath.Separator),
		"through a dot":  filepath.Join(current.HomeDir, ".") + string(filepath.Separator) + ".",
	}
	link := filepath.Join(testsupport.TempDir(t), "home")
	if err := os.Symlink(current.HomeDir, link); err == nil {
		spellings["a link"] = link
	}
	for name, home := range spellings {
		t.Run(name, func(t *testing.T) {
			testsupport.SetHome(t, home)
			if account, foreign := foreignHome(); foreign {
				t.Fatalf("foreignHome() under %q = %q, true, want it accepted", home, account)
			}
		})
	}
}

func TestSameDirectoryFallsBackToThePathWhenNeitherExists(t *testing.T) {
	missing := filepath.Join(testsupport.TempDir(t), "absent")
	if !sameDirectory(missing, missing+string(filepath.Separator)) {
		t.Error("the same missing path, spelled twice, compared different")
	}
	if sameDirectory(missing, missing+"-other") {
		t.Error("two different missing paths compared equal")
	}
}

// The explicit path -- `mcp install`, `configure` -- installed the same
// unit `serve` no longer does. Under a home that is not the account's it writes
// the stdio entry and says why; an explicit --daemon is refused, because it
// cannot be downgraded.
func TestTheExplicitPathInstallsNoDaemonUnderAnotherHome(t *testing.T) {
	current, err := user.Current()
	if err != nil || current.HomeDir == "" {
		t.Skipf("the account cannot be looked up here: %v", err)
	}
	loaded := initialisedHome(t)
	spec, _, err := supervisorSpec("", supervisorOptions{})
	if err != nil {
		t.Fatalf("supervisorSpec: %v", err)
	}
	report, err := supervisor.Status(spec)
	if err != nil {
		t.Fatalf("supervisor.Status: %v", err)
	}
	absent := func(supervisor.Spec) (supervisor.Report, error) {
		return supervisor.Report{State: supervisor.StateAbsent}, nil
	}

	var stdout bytes.Buffer
	_, err = integrationManagerOptionsWithStatus(
		integrationOptions{Scope: integrations.ScopeUser, Daemon: true}, true, strings.NewReader(""), &stdout, absent)
	if err == nil || !strings.Contains(err.Error(), "--daemon: ") || !strings.Contains(err.Error(), current.HomeDir) ||
		!strings.Contains(err.Error(), "--stdio") {
		t.Errorf("--daemon error = %v, want it refused, naming the account's home and the way out", err)
	}

	// With consent given, which a test cannot give through the prompt: that
	// one only believes a terminal.
	_, err = ensureDaemon(loaded.ConfigPath, stateDirectory(loaded), integrationOptions{}, &stdout)
	var fallback errStdioFallback
	if !errors.As(err, &fallback) || !strings.Contains(fallback.reason, current.HomeDir) {
		t.Errorf("ensureDaemon() error = %v, want a declared stdio fallback", err)
	}

	if report.Path != "" {
		if _, err := os.Stat(report.Path); !os.IsNotExist(err) {
			t.Errorf("a unit was written at %q (stat error %v)", report.Path, err)
		}
	}
}
