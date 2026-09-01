package toolchain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestValidateRejectsUnknownAndFloatingToolVersions(t *testing.T) {
	for name, value := range map[string]string{
		"unknown tool":  "mypy",
		"empty version": "",
		"range":         "^1.1.413",
		"leading v":     "v1.1.413",
		"prerelease":    "1.1.413-beta.1",
	} {
		t.Run(name, func(t *testing.T) {
			if name == "unknown tool" {
				if err := ValidateName(value); err == nil {
					t.Fatalf("ValidateName(%q) error = nil, want an error", value)
				}
				return
			}
			if err := ValidateVersion(value); err == nil {
				t.Fatalf("ValidateVersion(%q) error = nil, want an error", value)
			}
		})
	}
	if err := ValidateVersion(DefaultPyrightVersion); err != nil {
		t.Fatalf("ValidateVersion(%q) error = %v", DefaultPyrightVersion, err)
	}
}

func TestStatusDistinguishesMissingAndTamperedInstallations(t *testing.T) {
	state := t.TempDir()
	statuses, err := Status(state)
	if err != nil {
		t.Fatalf("Status(state=%q, analyzer=%q) error = %v", state, Pyright, err)
	}
	if len(statuses) != 1 || statuses[0].State != "missing" {
		t.Fatalf("Status(%q) = %#v, want one missing Pyright status", state, statuses)
	}

	npm := fakeNPM(t, DefaultPyrightVersion)
	installed, err := Install(context.Background(), state, Pyright, DefaultPyrightVersion, npm)
	if err != nil {
		t.Fatalf("Install(state=%q, analyzer=%q, version=%q) error = %v", state, Pyright, DefaultPyrightVersion, err)
	}
	if installed.State != "installed" || installed.Version != DefaultPyrightVersion {
		t.Fatalf("Install(%q) status = %#v", DefaultPyrightVersion, installed)
	}
	if info, err := os.Stat(installed.Executable); err != nil || info.IsDir() {
		t.Fatalf("installed executable %q: err=%v info=%v", installed.Executable, err, info)
	}
	statuses, err = Status(state)
	if err != nil {
		t.Fatalf("Status(state=%q) after install error = %v", state, err)
	}
	if statuses[0].State != "installed" || statuses[0].Executable == "" {
		t.Fatalf("Status(%q) after Install(%q) = %#v, want installed status with executable", state, DefaultPyrightVersion, statuses[0])
	}

	packageJSON := filepath.Join(PyrightRoot(state), DefaultPyrightVersion, "node_modules", Pyright, "package.json")
	if err := os.WriteFile(packageJSON, []byte(`{"version":"tampered"}`), 0o600); err != nil {
		t.Fatalf("write tampered package %q: %v", packageJSON, err)
	}
	statuses, err = Status(state)
	if err != nil {
		t.Fatalf("Status(state=%q) after tampering error = %v", state, err)
	}
	if statuses[0].State != "broken" || !strings.Contains(statuses[0].Detail, "digest") {
		t.Fatalf("Status(%q, version=%q, tampered package version=%q) = %#v, want broken digest status", state, DefaultPyrightVersion, "tampered", statuses[0])
	}

	if err := Remove(state, Pyright); err != nil {
		t.Fatalf("Remove(state=%q, analyzer=%q) error = %v", state, Pyright, err)
	}
	statuses, err = Status(state)
	if err != nil {
		t.Fatalf("Status(state=%q) after remove error = %v", state, err)
	}
	if statuses[0].State != "missing" {
		t.Fatalf("Status(%q) after Remove(%q) = %#v, want missing", state, Pyright, statuses[0])
	}
}

func TestIsManagedPyrightCommandMatchesOnlyTheAnalyzerArgument(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state with spaces's")
	analyzer := filepath.Join(PyrightRoot(state), DefaultPyrightVersion, "node_modules", ".bin", "pyright-langserver")
	managed := PyrightAnalyzerCommand(analyzer)
	if !IsManagedPyrightCommand(managed, state) {
		t.Fatalf("IsManagedPyrightCommand(command=%q, state=%q) = false, want true", managed, state)
	}
	unrelated := `kivgraph-python-pyright --path "` + analyzer + `"`
	if IsManagedPyrightCommand(unrelated, state) {
		t.Fatalf("IsManagedPyrightCommand(command=%q, state=%q) marked an unrelated command as managed", unrelated, state)
	}
}

func TestSplitCommandLinePreservesWindowsUNCPaths(t *testing.T) {
	analyzer := `\\server\share\pyright-langserver`
	for name, command := range map[string]string{
		"quoted":   `kivgraph-python-pyright --analyzer "` + analyzer + `"`,
		"unquoted": `kivgraph-python-pyright --analyzer ` + analyzer,
	} {
		t.Run(name, func(t *testing.T) {
			args, err := splitCommandLine(command, true)
			if err != nil {
				t.Fatalf("splitCommandLine(%q) error = %v", command, err)
			}
			want := []string{"kivgraph-python-pyright", "--analyzer", analyzer}
			if !slices.Equal(args, want) {
				t.Fatalf("splitCommandLine(%q) = %#v, want %#v", command, args, want)
			}
		})
	}
}

func TestInstallUsesTheRequestedVersionWhenAnotherVersionIsNewer(t *testing.T) {
	state := t.TempDir()
	for _, version := range []string{"1.1.413", "1.1.400"} {
		if _, err := Install(context.Background(), state, Pyright, version, fakeNPM(t, version)); err != nil {
			t.Fatalf("Install(%s) error = %v", version, err)
		}
	}
	status, err := Install(context.Background(), state, Pyright, "1.1.400", fakeNPM(t, "1.1.400"))
	if err != nil {
		t.Fatalf("Install(%q) error = %v", "1.1.400", err)
	}
	if status.Version != "1.1.400" {
		t.Fatalf("Install(%q) status = %#v", "1.1.400", status)
	}
	if info, err := os.Stat(status.Executable); err != nil || info.IsDir() {
		t.Fatalf("reinstalled executable %q: err=%v info=%v", status.Executable, err, info)
	}
}

func fakeNPM(t *testing.T, version string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the npm fixture is a POSIX shell script; Windows install behavior needs a .cmd fixture")
	}
	path := filepath.Join(t.TempDir(), "npm")
	script := `#!/bin/sh
set -eu
prefix=
requested=
ignore_scripts=0
while [ "$#" -gt 0 ]; do
	case "$1" in
	--prefix) prefix=$2; shift 2 ;;
	--ignore-scripts) ignore_scripts=1; shift ;;
	pyright@*) requested=$1; shift ;;
	*) shift ;;
	esac
done
if [ "$ignore_scripts" -ne 1 ]; then
  echo "npm was called without --ignore-scripts" >&2
  exit 1
fi
if [ "$requested" != "pyright@` + version + `" ]; then
  echo "unexpected package request: $requested" >&2
  exit 1
fi
mkdir -p "$prefix/node_modules/pyright" "$prefix/node_modules/.bin"
printf '{"version":"` + version + `"}\n' > "$prefix/node_modules/pyright/package.json"
printf '#!/bin/sh\nexit 0\n' > "$prefix/node_modules/.bin/pyright-langserver"
chmod 700 "$prefix/node_modules/.bin/pyright-langserver"
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake npm %q: %v", path, err)
	}
	return path
}
