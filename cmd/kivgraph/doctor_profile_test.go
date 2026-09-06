package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestDoctorReadsServedProfileAndPreservesLegacyState(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	configPath := filepath.Join(home, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	legacy, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(legacy.Config.Storage.DatabasePath)
	profile, err := config.LoadProfile(configPath, "")
	if err != nil {
		t.Fatal(err)
	}
	// This abandoned legacy marker must never override the current profile.
	marker := filepath.Join(root, "CURRENT")
	if err := os.WriteFile(marker, []byte("invalid-legacy-generation\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := runDoctor([]string{"--config", configPath}, &out, &diagnostic); code != 0 {
		t.Fatalf("doctor=%d\n%s\n%s", code, out.String(), diagnostic.String())
	}
	profileRoot := filepath.Dir(profile.Config.Storage.DatabasePath)
	if !strings.Contains(out.String(), profileRoot) {
		t.Fatalf("doctor did not report profile root %q:\n%s", profileRoot, out.String())
	}
	if !strings.Contains(out.String(), "graph.store: PASS (no published generation)") {
		t.Fatalf("doctor did not report an unpublished graph.store:\n%s", out.String())
	}
	if body, err := os.ReadFile(marker); err != nil || string(body) != "invalid-legacy-generation\n" {
		t.Fatalf("legacy state changed: %q %v", body, err)
	}
}

func TestDoctorReportsIncompleteProfilesAndContinues(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	configPath := filepath.Join(home, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{ConfigPath: configPath}); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	profiles := filepath.Join(filepath.Dir(loaded.Config.Storage.DatabasePath), "profiles")
	if err := os.RemoveAll(profiles); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(profiles, 0o700); err != nil {
		t.Fatal(err)
	}

	var out, diagnostic bytes.Buffer
	if code := runDoctor([]string{"--config", configPath}, &out, &diagnostic); code != 1 {
		t.Fatalf("doctor=%d, want 1\n%s\n%s", code, out.String(), diagnostic.String())
	}
	if !strings.Contains(out.String(), "config: PASS") || !strings.Contains(out.String(), "profiles: FAIL") {
		t.Fatalf("doctor did not preserve config checks and report profile failure:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "state.database_parent:") {
		t.Fatalf("doctor stopped before installation checks:\n%s", out.String())
	}
}
