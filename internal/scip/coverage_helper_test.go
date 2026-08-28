package scip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const coverageRoot = "../../testdata/java/coverage"

// convertCoverage is the richer fixture: inheritance, an enum, a record,
// generics, overloads and a lambda. The basic fixture is deliberately small
// and cannot exercise a hierarchy.
func convertCoverage(t *testing.T) facts.SemanticPayload {
	t.Helper()
	data, err := os.ReadFile("../../testdata/java/index/coverage.scip")
	if err != nil {
		t.Fatalf("read coverage index: %v", err)
	}
	index, err := scipwire.Decode(data)
	if err != nil {
		t.Fatalf("decode coverage index: %v", err)
	}
	payload, err := Convert(index, Options{
		Language:      facts.LanguageJava,
		Repository:    "coverage",
		Package:       "coverage",
		PackageRoot:   coverageRoot,
		Authoritative: true,
		ReadFile: func(relative string) ([]byte, error) {
			return os.ReadFile(filepath.Join(coverageRoot, filepath.FromSlash(relative)))
		},
		IncludeFile: func(relative string) bool {
			return strings.HasSuffix(relative, ".java")
		},
	})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return payload
}

func coverageRepository() workspace.Repository {
	absolute, err := filepath.Abs(coverageRoot)
	if err != nil {
		absolute = coverageRoot
	}
	return workspace.Repository{
		Name: "coverage", Path: absolute, RealPath: absolute, Languages: []string{"java"},
	}
}
