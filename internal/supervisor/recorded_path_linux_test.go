//go:build linux

package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// The negatives first, and the first of them is the one that would write a
// unit nobody asked for rather than a daemon that misbehaves.

// A PATH is a list of directories a user chose, so it can hold a newline the
// same way any other environment variable can -- and systemd reads a unit line
// by line. Written plainly, everything after the newline would be parsed as a
// directive of its own.
func TestARecordedPathCannotWriteADirectiveOfItsOwn(t *testing.T) {
	rendered := unit(testSpec("/state"), "/usr/bin\nExecStart=/bin/false\nRestart=always")
	for line := range strings.SplitSeq(rendered, "\n") {
		switch strings.TrimSpace(line) {
		case "ExecStart=/bin/false":
			t.Fatalf("a PATH holding a newline replaced the ExecStart:\n%s", rendered)
		case "Restart=always":
			t.Fatalf("a PATH holding a newline replaced the restart policy:\n%s", rendered)
		}
	}
	if directives := strings.Count(rendered, "\nEnvironment="); directives != 1 {
		t.Fatalf("the unit carries %d Environment directives, want exactly one:\n%s", directives, rendered)
	}
}

// The three characters systemd reads for itself, and the two it splits on.
// Each is asserted by the whole rendered line, because a wrong escape does not
// fail: it produces a daemon whose PATH is quietly not the one recorded.
func TestARecordedPathSurvivesWhatSystemdWouldReadInIt(t *testing.T) {
	for name, expectation := range map[string]struct{ path, want string }{
		"a plain list": {
			"/home/ada/.nvm/versions/node/v24.18.0/bin:/usr/bin",
			`Environment="PATH=/home/ada/.nvm/versions/node/v24.18.0/bin:/usr/bin"`,
		},
		"a space, which would start a second assignment": {
			"/opt/my apps/bin:/usr/bin",
			`Environment="PATH=/opt/my apps/bin:/usr/bin"`,
		},
		"a percent, which is a specifier": {
			"/opt/100%h/bin",
			`Environment="PATH=/opt/100%%h/bin"`,
		},
		"a quote, which would close the value": {
			`/opt/say"what/bin`,
			`Environment="PATH=/opt/say\"what/bin"`,
		},
		"a backslash, which escapes what follows it": {
			`/opt/back\slash/bin`,
			`Environment="PATH=/opt/back\\slash/bin"`,
		},
		"a newline, which would end the line": {
			"/usr/bin\n/bin",
			`Environment="PATH=/usr/bin\n/bin"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			rendered := unit(testSpec("/state"), expectation.path)
			if !strings.Contains(rendered, "\n"+expectation.want+"\n") {
				t.Fatalf("the unit does not carry %s:\n%s", expectation.want, rendered)
			}
		})
	}
}

// A machine with no PATH at all records none. An empty assignment would hand
// the daemon a worse environment than the one systemd gives it by default,
// which is the environment this whole directive exists to replace.
func TestNoPathRecordsNoDirective(t *testing.T) {
	rendered := unit(testSpec("/state"), "")
	if strings.Contains(rendered, "Environment=") {
		t.Fatalf("a unit rendered without a PATH still declares an environment:\n%s", rendered)
	}
}

// TestTheRecordedPathIsTheOnlyThingStrippingRemoves is what `status` rests on:
// it compares two renderings with their PATH taken out, so anything else the
// strip took with it would be a difference nobody could see -- an ExecStart
// naming another binary reported as installed.
func TestTheRecordedPathIsTheOnlyThingStrippingRemoves(t *testing.T) {
	spec := testSpec("/state")
	withPath, recorded := withoutRecordedPath(unit(spec, "/usr/bin:/bin"))
	withoutPath, none := withoutRecordedPath(unit(spec, ""))
	if !recorded {
		t.Fatal("a unit that records a PATH was not reported as recording one")
	}
	if none {
		t.Fatal("a unit that records no PATH was reported as recording one")
	}
	if withPath != withoutPath {
		t.Fatalf("stripping left two different units:\n%s\n---\n%s", withPath, withoutPath)
	}
	if !strings.Contains(withPath, "ExecStart=") || !strings.Contains(withPath, "Restart=on-failure") {
		t.Fatalf("stripping took the daemon with it:\n%s", withPath)
	}
}

// And an environment somebody else added is not this one. Absorbing it would
// report a hand-edited unit as installed, which is the one thing `status`
// promises never to do.
func TestAnotherEnvironmentDirectiveIsNotStripped(t *testing.T) {
	edited := unit(testSpec("/state"), "/usr/bin") + "Environment=GOFLAGS=-mod=mod\n"
	stripped, recorded := withoutRecordedPath(edited)
	if !recorded {
		t.Fatal("the recorded PATH was not found beside another environment directive")
	}
	if !strings.Contains(stripped, "Environment=GOFLAGS=-mod=mod") {
		t.Fatalf("an environment directive nobody here wrote was stripped away:\n%s", stripped)
	}
}

func TestAUserDropInMakesAUnitUnrepairable(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	spec := testSpec(t.TempDir())
	t.Setenv("PATH", "/usr/bin:/bin")
	planned, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(planned.Path), 0o755); err != nil {
		t.Fatalf("create unit directory: %v", err)
	}
	if err := os.WriteFile(planned.Path, renderedUnit(t, spec), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	dropInDirectory := planned.Path + ".d"
	if err := os.Mkdir(dropInDirectory, 0o755); err != nil {
		t.Fatalf("create drop-in directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dropInDirectory, "10-toolchain-path.conf"),
		[]byte("[Service]\nEnvironment=PATH=/custom/bin\n"), 0o644); err != nil {
		t.Fatalf("write drop-in: %v", err)
	}

	report, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() with a drop-in = %v", err)
	}
	if report.State != StateStale || report.Managed || report.Repairable {
		t.Fatalf("Status() with a drop-in = state=%q managed=%t repairable=%t; want stale, unmanaged, unrepairable",
			report.State, report.Managed, report.Repairable)
	}
	if !strings.Contains(report.Detail, "drop-ins") {
		t.Fatalf("Status() detail %q does not name the drop-in policy", report.Detail)
	}
}

func TestAUnreadableDropInPathIsAnInspectionError(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	spec := testSpec(t.TempDir())
	t.Setenv("PATH", "/usr/bin:/bin")
	planned, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(planned.Path), 0o755); err != nil {
		t.Fatalf("create unit directory: %v", err)
	}
	if err := os.WriteFile(planned.Path, renderedUnit(t, spec), 0o644); err != nil {
		t.Fatalf("write unit: %v", err)
	}
	if err := os.WriteFile(planned.Path+".d", []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write invalid drop-in path: %v", err)
	}
	if _, err := Status(spec); err == nil || !strings.Contains(err.Error(), "read drop-ins") {
		t.Fatalf("Status() with invalid drop-in path %q error = %v, want a named inspection error",
			planned.Path+".d", err)
	}
}
