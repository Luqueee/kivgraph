package csharploader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// The registry's `exclusions` did not reach this loader at all: the indexer
// builds the whole project, and every document it wrote became part of the
// graph whatever the repository said.
func TestExclusionsLeaveTheFileOutOfThePayload(t *testing.T) {
	data, err := os.ReadFile(coverageIndex)
	if err != nil {
		t.Fatal(err)
	}
	index, err := scipwire.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(coverageFixture)
	if err != nil {
		t.Fatal(err)
	}
	convertWith := func(exclusions ...string) (map[string]int, map[string]int, int, error) {
		payload, err := Convert(index, Options{
			Repository: workspace.Repository{Name: "coverage", Path: root, RealPath: root, Exclusions: exclusions},
		}, root, filepath.Join(root, "coverage.csproj"))
		files, symbols := map[string]int{}, map[string]int{}
		for _, file := range payload.Files {
			files[file.Path]++
		}
		for _, symbol := range payload.Symbols {
			symbols[symbol.File]++
		}
		dangling := 0
		known := map[string]bool{}
		for _, symbol := range payload.Symbols {
			known[symbol.ID] = true
		}
		for _, reference := range payload.References {
			if !known[reference.TargetID] && reference.Target == nil {
				dangling++
			}
		}
		return files, symbols, dangling, err
	}

	files, symbols, _, err := convertWith()
	if err != nil {
		t.Fatal(err)
	}
	var shapes string
	for file := range files {
		if strings.HasPrefix(filepath.Base(file), "Shapes.") {
			shapes = file
		}
	}
	if shapes == "" || symbols[shapes] == 0 || len(files) < 2 {
		t.Fatalf("the fixture has no Shapes file with declarations among %v", files)
	}

	for name, pattern := range map[string]string{
		"its path":        shapes,
		"a glob":          "**/Shapes.*",
		"an absolute one": filepath.Join(root, filepath.FromSlash(shapes)),
	} {
		t.Run(name, func(t *testing.T) {
			left, declared, dangling, err := convertWith(pattern)
			if err != nil {
				t.Fatal(err)
			}
			if left[shapes] != 0 || declared[shapes] != 0 {
				t.Errorf("%s is still in the payload: file=%d symbols=%d", shapes, left[shapes], declared[shapes])
			}
			if len(left) != len(files)-1 {
				t.Errorf("%d files left of %d: more than the excluded one went", len(left), len(files))
			}
			// The files that stay use what was declared there. Those uses must
			// not point at a declaration the payload no longer has.
			if dangling != 0 {
				t.Errorf("%d references point at a declaration that is not in the payload", dangling)
			}
		})
	}
	if _, _, _, err := convertWith("[unterminated"); err == nil {
		t.Error("a pattern the registry refuses was read as matching nothing")
	}
}
