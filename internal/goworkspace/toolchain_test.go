package goworkspace

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// TestWrittenWorkspaceIsAcceptedByTheGoToolchain proves the emitted file is a
// workspace `go` really loads, and that a package of one repository resolves a
// package of another through it.
func TestWrittenWorkspaceIsAcceptedByTheGoToolchain(t *testing.T) {
	toolchain, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go toolchain unavailable: %v", err)
	}

	root := testsupport.TempDir(t)
	provider := filepath.Join(root, "provider")
	consumer := filepath.Join(root, "consumer")
	writeFiles(t, provider, map[string]string{
		"go.mod":   "module example.com/provider\n\ngo 1.24\n",
		"value.go": "package provider\n\n// Value is the provided fact.\nconst Value = 41\n",
	})
	writeFiles(t, consumer, map[string]string{
		"go.mod":  "module example.com/consumer\n\ngo 1.24\n",
		"main.go": "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/provider\"\n)\n\nfunc main() {\n\tfmt.Println(provider.Value + 1)\n}\n",
	})

	repositories := []workspace.Repository{
		{Name: "provider", Path: provider, RealPath: provider},
		{Name: "consumer", Path: consumer, RealPath: consumer},
	}
	plan, err := BuildPlan(context.Background(), repositories, Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	target := filepath.Join(root, "state", "go.work")
	if _, err := Write(context.Background(), target, plan, repositories); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	output := runGo(t, toolchain, consumer, target, "list", "-m", "all")
	for _, modulePath := range []string{"example.com/provider", "example.com/consumer"} {
		if !strings.Contains(output, modulePath) {
			t.Fatalf("go list -m all does not report %q:\n%s", modulePath, output)
		}
	}

	if output := runGo(t, toolchain, consumer, target, "run", "."); strings.TrimSpace(output) != "42" {
		t.Fatalf("cross-module program printed %q, want 42", strings.TrimSpace(output))
	}
}

func runGo(t *testing.T, toolchain, directory, workFile string, arguments ...string) string {
	t.Helper()
	command := exec.Command(toolchain, arguments...)
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GOWORK="+workFile,
		"GOPROXY=off",
	)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(arguments, " "), err, stderr.String())
	}
	return stdout.String()
}

func writeFiles(t *testing.T, directory string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create %q: %v", directory, err)
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
}
