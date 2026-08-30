package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
)

func TestProfileCommandsCreateListAndUse(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("initialize %q: %v", configPath, err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "profile", "create", "--config", configPath, "frontend"}, &stdout, &stderr); code != 0 {
		t.Fatalf("profile create code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"kivgraph", "profile", "use", "--config", configPath, "frontend"}, &stdout, &stderr); code != 0 {
		t.Fatalf("profile use code = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"kivgraph", "profile", "list", "--config", configPath}, &stdout, &stderr); code != 0 {
		t.Fatalf("profile list code = %d, stderr = %q", code, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "  default\n") || !strings.Contains(got, "* frontend\n") {
		t.Fatalf("profile list output = %q", got)
	}
}

func TestProfileRemoveNeedsConfirmation(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatalf("initialize %q: %v", configPath, err)
	}
	if err := config.CreateProfile(configPath, "temporary"); err != nil {
		t.Fatalf("create profile %q in %q: %v", "temporary", configPath, err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "profile", "remove", "--config", configPath, "temporary"}, &stdout, &stderr); code == 0 {
		t.Fatal("profile remove without --yes succeeded")
	}
	profiles, err := config.ListProfiles(configPath)
	if err != nil || len(profiles) != 2 {
		t.Fatalf("profiles after refused removal = %#v, %v", profiles, err)
	}
}
