package pythonloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestRunConfiguredSemanticProviderIsAuthoritative(t *testing.T) {
	root := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(t.TempDir(), "python-provider")
	payload := `{"version":1,"repository":"semantic","language":"python","package":{"name":"semantic","rootPath":"PROJECT"},"files":[{"path":"main.py"}],"symbols":[{"id":"main","file":"main.py","name":"main","qualifiedName":"main","kind":"function","exported":true,"signature":"def main()","startLine":1,"start":0,"endLine":1,"end":10},{"id":"helper","file":"main.py","name":"helper","qualifiedName":"helper","kind":"function","exported":true,"signature":"def helper()","startLine":2,"start":11,"endLine":2,"end":23}],"references":[{"file":"main.py","sourceId":"main","targetId":"helper","kind":"CALLS_DIRECT","startLine":1,"start":5,"endLine":1,"end":11}],"imports":[],"unresolved":[]}`
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s'", payload)
	if err := os.WriteFile(provider, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), provider, "python3", workspace.Repository{Name: "semantic", Path: root, RealPath: root}, root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Authoritative {
		t.Fatal("configured semantic provider was marked non-authoritative")
	}
	set, err := facts.NormalizeSemantic(context.Background(), workspace.Repository{Name: "semantic", Path: root, RealPath: root}, result)
	if err != nil {
		t.Fatal(err)
	}
	for _, edge := range set.Edges {
		if edge.Kind == facts.CallsDirect && edge.Confidence != facts.ExactTypechecked {
			t.Fatalf("configured provider call confidence = %q, want exact", edge.Confidence)
		}
	}
}

func TestRunWithBundledFallbackWorker(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	payload, err := Run(context.Background(), "missing-scippython", "python3", workspace.Repository{Name: "kivgraph", Path: root, RealPath: root}, root)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Language != facts.LanguagePython || len(payload.Files) == 0 || len(payload.Symbols) == 0 {
		t.Fatalf("payload = language %q, files=%d, symbols=%d", payload.Language, len(payload.Files), len(payload.Symbols))
	}
	set, err := facts.NormalizeSemantic(context.Background(), workspace.Repository{Name: "kivgraph", Path: root, RealPath: root}, payload)
	if err != nil {
		t.Fatalf("NormalizeSemantic() error = %v", err)
	}
	if len(set.Files) == 0 || len(set.Symbols) == 0 {
		t.Fatalf("normalized Python facts = files=%d symbols=%d", len(set.Files), len(set.Symbols))
	}
	for _, edge := range set.Edges {
		if edge.Kind == facts.CallsDirect || edge.Kind == facts.References {
			if edge.Confidence != facts.Candidate {
				t.Fatalf("fallback Python edge confidence = %q, want CANDIDATE", edge.Confidence)
			}
		}
	}
}

func TestRunFixtureResolvesPythonDeclarationsAndCalls(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "testdata", "python", "basic"))
	if err != nil {
		t.Fatal(err)
	}
	repository := workspace.Repository{Name: "python-basic", Path: root, RealPath: root}
	payload, err := Run(context.Background(), "missing-scippython", "python3", repository, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Files) != 3 {
		t.Fatalf("files = %d, want 3", len(payload.Files))
	}
	var hasVehicle, hasBuild, hasImport, hasCall, hasExtends bool
	for _, symbol := range payload.Symbols {
		hasVehicle = hasVehicle || symbol.Name == "Vehicle"
		hasBuild = hasBuild || symbol.Name == "build_vehicle"
	}
	for _, importFact := range payload.Imports {
		hasImport = hasImport || importFact.RequestedPackage == "pkg.models"
	}
	for _, reference := range payload.References {
		hasCall = hasCall || reference.Kind == "CALLS_DIRECT"
		hasExtends = hasExtends || reference.Kind == "EXTENDS"
	}
	if !hasVehicle || !hasBuild || !hasImport || !hasCall || !hasExtends {
		t.Fatalf("fixture facts: vehicle=%v build=%v import=%v call=%v extends=%v", hasVehicle, hasBuild, hasImport, hasCall, hasExtends)
	}
	if _, err := facts.NormalizeSemantic(context.Background(), repository, payload); err != nil {
		t.Fatalf("NormalizeSemantic() error = %v", err)
	}
}
