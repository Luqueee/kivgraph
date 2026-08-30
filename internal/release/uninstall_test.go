package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallRemovesOnlyTheManagedBundleAndLaunchers(t *testing.T) {
	installRoot, binDir, home := fakeInstallation(t)
	config := filepath.Join(home, ".config", "kivgraph", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("keep me\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := runUninstaller(t, home, installRoot, binDir, "--yes")
	if result.err != nil {
		t.Fatalf("uninstaller failed: %v\n%s", result.err, result.output)
	}
	for _, path := range []string{installRoot, filepath.Join(binDir, "kivgraph"), filepath.Join(binDir, "kivgraph-ts-worker")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("managed path %q still exists or could not be inspected: %v", path, err)
		}
	}
	if data, err := os.ReadFile(config); err != nil || string(data) != "keep me\n" {
		t.Errorf("uninstaller changed preserved configuration: data=%q err=%v", data, err)
	}
	if !strings.Contains(result.output, "configuration and graph state were preserved") {
		t.Errorf("uninstaller did not report preserved state:\n%s", result.output)
	}
}

func TestUninstallRefusesAnUnrelatedLauncher(t *testing.T) {
	installRoot, binDir, home := fakeInstallation(t)
	launcher := filepath.Join(binDir, "kivgraph")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\necho user-owned\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	result := runUninstaller(t, home, installRoot, binDir, "--yes")
	if result.err == nil {
		t.Fatal("uninstaller removed an unrelated launcher")
	}
	if !strings.Contains(result.output, "unrelated launcher") {
		t.Errorf("uninstaller gave no ownership error:\n%s", result.output)
	}
	if _, err := os.Stat(installRoot); err != nil {
		t.Errorf("uninstaller changed the bundle after refusing the launcher: %v", err)
	}
	if _, err := os.Stat(launcher); err != nil {
		t.Errorf("uninstaller changed the unrelated launcher: %v", err)
	}
}

func TestUninstallRefusesASymbolicLinkInstallationRoot(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	installRoot := filepath.Join(home, "link-to-install")
	if err := os.Symlink(target, installRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	binDir := filepath.Join(home, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}

	result := runUninstaller(t, home, installRoot, binDir, "--yes")
	if result.err == nil {
		t.Fatal("uninstaller followed a symbolic-link installation root")
	}
	if !strings.Contains(result.output, "symbolic-link installation root") {
		t.Errorf("uninstaller gave no symlink refusal:\n%s", result.output)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("uninstaller changed the symlink target: %v", err)
	}
}

func TestUninstallRequiresConfirmationWhenNonInteractive(t *testing.T) {
	installRoot, binDir, home := fakeInstallation(t)
	result := runUninstaller(t, home, installRoot, binDir)
	if result.err == nil {
		t.Fatal("uninstaller removed a bundle without confirmation")
	}
	if !strings.Contains(result.output, "confirmation required") {
		t.Errorf("uninstaller gave no non-interactive confirmation error:\n%s", result.output)
	}
	if _, err := os.Stat(installRoot); err != nil {
		t.Errorf("uninstaller changed the bundle without confirmation: %v", err)
	}
}

type commandResult struct {
	output string
	err    error
}

func runUninstaller(t *testing.T, home, installRoot, binDir string, arguments ...string) commandResult {
	t.Helper()
	script := filepath.Join("..", "..", "scripts", "uninstall.sh")
	command := exec.Command("bash", append([]string{script}, arguments...)...)
	command.Env = append(os.Environ(),
		"HOME="+home,
		"KIVGRAPH_INSTALL_ROOT="+installRoot,
		"KIVGRAPH_BIN_DIR="+binDir,
	)
	output, err := command.CombinedOutput()
	return commandResult{output: string(output), err: err}
}

func fakeInstallation(t *testing.T) (installRoot, binDir, home string) {
	t.Helper()
	base := t.TempDir()
	home = filepath.Join(base, "home")
	installRoot = filepath.Join(home, ".local", "opt", "kivgraph")
	binDir = filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(filepath.Join(installRoot, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "manifest.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"kivgraph", "kivgraph-ts-worker"} {
		path := filepath.Join(installRoot, "bin", target)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		launcher := filepath.Join(binDir, target)
		body := "#!/usr/bin/env bash\nexec " + path + " \"$@\"\n"
		if err := os.WriteFile(launcher, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return installRoot, binDir, home
}
