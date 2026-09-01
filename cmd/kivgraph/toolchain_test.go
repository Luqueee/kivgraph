package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/toolchain"
)

func TestToolchainCompletionNamesTheFamilyAndItsTool(t *testing.T) {
	for _, want := range []string{"status", "install", "remove"} {
		if !has(completionCandidates([]string{"toolchain", ""}), want) {
			t.Fatalf("`toolchain ` did not offer %q", want)
		}
	}
	got := completionCandidates([]string{"toolchain", "install", ""})
	if !has(got, toolchain.Pyright) {
		t.Fatalf("`toolchain install ` did not offer Pyright: %v", got)
	}
}

func TestToolchainRemoveRequiresExplicitConfirmation(t *testing.T) {
	testsupport.SetHome(t, t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "toolchain", "remove", "pyright"}, &stdout, &stderr); code != 2 {
		t.Fatalf("remove without --yes = %d, want 2 (stderr=%q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "--yes is required") {
		t.Fatalf("remove error = %q, want the confirmation reason", stderr.String())
	}
}

func TestToolchainInstallActivatesAndRemoveRestoresPythonFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the npm fixture is a POSIX shell script; Windows install behavior needs a .cmd fixture")
	}
	testsupport.SetHome(t, t.TempDir())
	configPath := filepath.Join(t.TempDir(), ".kivgraph", "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	npmDirectory := t.TempDir()
	writeFakeNPM(t, npmDirectory)
	t.Setenv("PATH", npmDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "toolchain", "install", "pyright", "--config", configPath, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("install = %d, stderr=%q", code, stderr.String())
	}
	var installed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &installed); err != nil {
		t.Fatalf("install JSON %q: %v", stdout.String(), err)
	}
	if installed["analyzer_mode"] != "exact" || installed["version"] != toolchain.DefaultPyrightVersion {
		t.Fatalf("install result = %#v", installed)
	}
	configuration, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() after install error = %v", err)
	}
	stateDirectory := filepath.Dir(configuration.Storage.DatabasePath)
	if configuration.Python.AnalyzerMode != "exact" || !toolchain.IsManagedPyrightCommand(configuration.Python.AnalyzerCommand, stateDirectory) {
		t.Fatalf("Python configuration after install = %#v", configuration.Python)
	}
	pyrightRoot := toolchain.PyrightRoot(stateDirectory)
	if _, err := os.Stat(pyrightRoot); err != nil {
		t.Fatalf("Pyright state root %q after install: %v", pyrightRoot, err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"kivgraph", "toolchain", "status", "--config", configPath, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("status = %d, stderr=%q", code, stderr.String())
	}
	var status map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatalf("status JSON %q: %v", stdout.String(), err)
	}
	tools, ok := status["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("status result = %#v", status)
	}
	tool, ok := tools[0].(map[string]any)
	if !ok || tool["state"] != "installed" {
		t.Fatalf("status tool = %#v", tools[0])
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"kivgraph", "toolchain", "remove", "pyright", "--config", configPath, "--yes", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("remove = %d, stderr=%q", code, stderr.String())
	}
	configuration, err = config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() after remove error = %v", err)
	}
	if configuration.Python.AnalyzerMode != "fallback" || configuration.Python.AnalyzerCommand != config.DefaultPythonAnalyzerCommand {
		t.Fatalf("Python configuration after remove = %#v", configuration.Python)
	}
	if _, err := os.Stat(pyrightRoot); !os.IsNotExist(err) {
		t.Fatalf("Pyright state %q still exists: %v", pyrightRoot, err)
	}
}

func writeFakeNPM(t *testing.T, directory string) {
	t.Helper()
	path := filepath.Join(directory, "npm")
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
if [ "$ignore_scripts" -ne 1 ] || [ "$requested" != "pyright@` + toolchain.DefaultPyrightVersion + `" ]; then
  echo "unexpected npm arguments" >&2
  exit 1
fi
mkdir -p "$prefix/node_modules/pyright" "$prefix/node_modules/.bin"
printf '{"version":"` + toolchain.DefaultPyrightVersion + `"}\n' > "$prefix/node_modules/pyright/package.json"
printf '#!/bin/sh\nexit 0\n' > "$prefix/node_modules/.bin/pyright-langserver"
chmod 700 "$prefix/node_modules/.bin/pyright-langserver"
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
}
