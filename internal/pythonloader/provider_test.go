package pythonloader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/executable"
)

// The configured Python provider is a program the loader runs and reads a
// document from. These fixtures wrote a `#!/bin/sh` script, which is the third
// fixture on this branch to do that and the third to be unrunnable on Windows:
// the loader could not execute it, fell back to the bundled AST analyser, and
// the test read that as the loader refusing to honour a configured provider.
//
// It was not the environment either -- pyright was installed on the host and
// the failure did not move.
//
// So the provider is this test binary, re-executed. The document it should
// print travels in a variable, because os/exec inherits the environment, and
// TestMain acts on it before the testing package has parsed a flag.
const providerDocumentVariable = "KIVGRAPH_TEST_PYTHON_PROVIDER_DOCUMENT"

func TestMain(m *testing.M) {
	if document := os.Getenv(providerDocumentVariable); document != "" {
		fmt.Println(document)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// writeProvider returns the path of a program that prints document, in the
// shape the running platform can execute.
func writeProvider(t *testing.T, name, document string) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve the test binary: %v", err)
	}
	source, err := os.Open(self)
	if err != nil {
		t.Fatalf("open the test binary: %v", err)
	}
	defer source.Close()
	provider := filepath.Join(t.TempDir(), executable.Name(name))
	target, err := os.OpenFile(provider, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		t.Fatalf("copy the test binary: %v", copyErr)
	}
	if closeErr != nil {
		t.Fatalf("close provider: %v", closeErr)
	}
	t.Setenv(providerDocumentVariable, document)
	return provider
}
