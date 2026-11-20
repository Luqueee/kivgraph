package main

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

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
