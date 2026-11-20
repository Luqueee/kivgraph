//go:build unix

package pythonloader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// A producer that fails says why on standard error, and that text is the whole
// diagnosis the operator gets: it has to survive however the process is run.
func TestRunReportsWhatAFailingProducerWrote(t *testing.T) {
	root := t.TempDir()
	producer := filepath.Join(t.TempDir(), "producer")
	script := "#!/bin/sh\necho 'half a payload'\necho 'no interpreter for this project' >&2\nexit 3\n"
	if err := os.WriteFile(producer, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), producer, "python3", workspace.Repository{Name: "broken", Path: root, RealPath: root}, root)
	if err == nil {
		t.Fatal("Run() accepted a producer that exited 3")
	}
	for _, want := range []string{"exit status 3", "no interpreter for this project"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Run() error = %q, want it to contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "half a payload") {
		t.Errorf("Run() error = %q, want standard output kept out of the diagnosis", err)
	}
}
