package goloader

import (
	"context"
	"errors"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/goworkspace"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestLoadResolvesSymbolsAcrossModulesOfTheSyntheticWorkspace(t *testing.T) {
	root := testsupport.TempDir(t)
	provider := filepath.Join(root, "provider")
	consumer := filepath.Join(root, "consumer")
	writeFiles(t, provider, map[string]string{
		"go.mod": "module example.com/provider\n\ngo 1.24\n",
		"value.go": `package provider

// Value is the exported fact consumers resolve.
const Value = 41

// Compute is the exported function consumers call.
func Compute(input int) int { return input + Value }
`,
	})
	writeFiles(t, consumer, map[string]string{
		"go.mod": "module example.com/consumer\n\ngo 1.24\n",
		"main.go": `package main

import (
	"fmt"

	"example.com/provider"
)

func main() { fmt.Println(provider.Compute(1)) }
`,
	})

	workFile := buildWorkspace(t, root, provider, consumer)
	result, err := Load(context.Background(), Options{
		Directory: consumer,
		WorkFile:  workFile,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v, want none", result.Errors)
	}
	if len(result.Packages) != 1 || result.Packages[0].PkgPath != "example.com/consumer" {
		t.Fatalf("packages = %#v", result.Packages)
	}

	loaded := result.Packages[0]
	if loaded.TypesInfo == nil || loaded.Types == nil || len(loaded.Syntax) == 0 {
		t.Fatalf("package lacks the semantic mode: types=%v info=%v syntax=%d",
			loaded.Types != nil, loaded.TypesInfo != nil, len(loaded.Syntax))
	}

	// The use of provider.Compute must resolve to an object owned by the
	// provider module, which is what makes the cross-module edge exact.
	var resolved *types.Func
	for _, object := range loaded.TypesInfo.Uses {
		function, isFunction := object.(*types.Func)
		if isFunction && function.Name() == "Compute" {
			resolved = function
			break
		}
	}
	if resolved == nil {
		t.Fatalf("Compute was not resolved through TypesInfo.Uses")
	}
	if got := resolved.Pkg().Path(); got != "example.com/provider" {
		t.Fatalf("Compute belongs to %q, want example.com/provider", got)
	}
	position := result.Fset.Position(resolved.Pos())
	if filepath.Dir(position.Filename) != provider {
		t.Fatalf("Compute declared in %q, want a file of %q", position.Filename, provider)
	}

	modulePaths := make([]string, 0, len(result.Modules))
	for _, module := range result.Modules {
		modulePaths = append(modulePaths, module.Path)
	}
	if !containsAll(modulePaths, []string{"example.com/consumer", "example.com/provider"}) {
		t.Fatalf("modules = %v, want both workspace modules", modulePaths)
	}
}

func TestLoadFollowsALocalReplaceDirective(t *testing.T) {
	root := testsupport.TempDir(t)
	consumer := filepath.Join(root, "consumer")
	writeFiles(t, consumer, map[string]string{
		"go.mod": `module example.com/consumer

go 1.24

require example.com/legacy v1.0.0

replace example.com/legacy => ./legacy
`,
		"main.go": `package main

import (
	"fmt"

	"example.com/legacy"
)

func main() { fmt.Println(legacy.Answer) }
`,
	})
	writeFiles(t, filepath.Join(consumer, "legacy"), map[string]string{
		"go.mod":   "module example.com/legacy\n\ngo 1.24\n",
		"value.go": "package legacy\n\n// Answer is the replaced fact.\nconst Answer = 42\n",
	})

	result, err := Load(context.Background(), Options{
		Directory: consumer,
		Patterns:  []string{"."},
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v, want none", result.Errors)
	}

	var replaced *Module
	for index, module := range result.Modules {
		if module.Path == "example.com/legacy" {
			replaced = &result.Modules[index]
		}
	}
	if replaced == nil {
		t.Fatalf("modules = %#v, want the replaced module", result.Modules)
	}
	if replaced.ReplacedBy == "" || replaced.Directory != filepath.Join(consumer, "legacy") {
		t.Fatalf("replacement = %#v, want the local directory", replaced)
	}
}

func TestLoadReportsPartialErrorsWithoutDiscardingValidPackages(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":        "module example.com/module\n\ngo 1.24\n",
		"good/good.go":  "package good\n\n// Value is well typed.\nconst Value = 1\n",
		"broken/bad.go": "package broken\n\n// Value does not typecheck.\nvar Value int = \"text\"\n",
	})

	result, err := Load(context.Background(), Options{Directory: module})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Packages) != 2 {
		t.Fatalf("packages = %d, want both the valid and the broken one", len(result.Packages))
	}
	if len(result.Errors) == 0 {
		t.Fatalf("errors = none, want the type error reported")
	}
	found := false
	for _, failure := range result.Errors {
		if failure.Kind == TypeError && failure.PackagePath == "example.com/module/broken" {
			found = true
		}
		if failure.PackagePath == "example.com/module/good" {
			t.Fatalf("valid package reported an error: %#v", failure)
		}
	}
	if !found {
		t.Fatalf("errors = %#v, want a TYPE error for the broken package", result.Errors)
	}

	for _, loaded := range result.Packages {
		if loaded.PkgPath == "example.com/module/good" && loaded.Types.Scope().Lookup("Value") == nil {
			t.Fatalf("valid package lost its type information")
		}
	}
}

func TestLoadHonoursCancellation(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":   "module example.com/module\n\ngo 1.24\n",
		"value.go": "package module\n\n// Value is a fact.\nconst Value = 1\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Load(ctx, Options{Directory: module}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
}

func TestLoadRejectsAnInvalidRequest(t *testing.T) {
	if _, err := Load(context.Background(), Options{Directory: ""}); err == nil {
		t.Fatalf("Load() with no directory must fail")
	}
	root := testsupport.TempDir(t)
	if _, err := Load(context.Background(), Options{
		Directory: root,
		Patterns:  []string{"  "},
	}); !errors.Is(err, ErrNoPatterns) {
		t.Fatalf("Load() error = %v, want ErrNoPatterns", err)
	}
	if _, err := Load(context.Background(), Options{
		Directory: root,
		WorkFile:  filepath.Join(root, "missing.work"),
	}); err == nil {
		t.Fatalf("Load() with a missing work file must fail")
	}
}

// A package whose files are all excluded by a build constraint declares
// nothing in this configuration. The load must say so without claiming the
// facts of the module are untrustworthy, and the same package must appear in
// full once the tag is requested.
func TestLoadSelectsPackagesWithTheRequestedBuildTags(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":           "module example.com/module\n\ngo 1.24\n",
		"plain/value.go":   "package plain\n\n// Value is always selected.\nconst Value = 1\n",
		"tagged/tagged.go": "//go:build fixturetag\n\npackage tagged\n\n// Tagged is selected only with the tag.\nconst Tagged = 2\n",
	})
	patterns := []string{"example.com/module/plain", "example.com/module/tagged"}

	untagged, err := Load(context.Background(), Options{Directory: module, Patterns: patterns})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(untagged.BlockingErrors()) != 0 {
		t.Fatalf("blocking errors = %#v, want none", untagged.BlockingErrors())
	}
	excluded := false
	for _, failure := range untagged.Errors {
		if failure.PackagePath == "example.com/module/tagged" && failure.NoPackage() {
			excluded = true
		}
	}
	if !excluded {
		t.Fatalf("errors = %#v, want the excluded package reported", untagged.Errors)
	}

	tagged, err := Load(context.Background(), Options{
		Directory: module,
		Patterns:  patterns,
		BuildTags: []string{"fixturetag"},
	})
	if err != nil {
		t.Fatalf("Load() with build tags error = %v", err)
	}
	if len(tagged.Errors) != 0 {
		t.Fatalf("errors = %#v, want none once the tag is requested", tagged.Errors)
	}
	found := false
	for _, loaded := range tagged.Packages {
		if loaded.PkgPath != "example.com/module/tagged" {
			continue
		}
		found = true
		if loaded.Types.Scope().Lookup("Tagged") == nil {
			t.Fatalf("tagged package loaded without its declaration")
		}
	}
	if !found {
		t.Fatalf("packages = %#v, want the tagged package", tagged.Packages)
	}
}

func TestLoadRejectsABuildTagTheGoCommandCannotCarry(t *testing.T) {
	root := testsupport.TempDir(t)
	module := filepath.Join(root, "module")
	writeFiles(t, module, map[string]string{
		"go.mod":   "module example.com/module\n\ngo 1.24\n",
		"value.go": "package module\n\n// Value is a fact.\nconst Value = 1\n",
	})
	for name, tags := range map[string][]string{
		"empty": {" "},
		"comma": {"one,two"},
		"space": {"one two"},
	} {
		if _, err := Load(context.Background(), Options{Directory: module, BuildTags: tags}); err == nil {
			t.Fatalf("Load() with the %s tag must fail", name)
		}
	}
}

// The advisory go/packages attaches when the go command is newer than the
// toolchain of this binary qualifies another diagnostic; on its own it says
// nothing about the code and must never abort an index.
func TestToolchainSkewAdvisoryDoesNotBlock(t *testing.T) {
	advisory := PackageError{
		Kind:     UnknownError,
		Position: "-",
		Message: "This application uses version go1.24 of the source-processing packages " +
			"but runs version go1.25 of 'go list'. It may fail to process source files.",
	}
	if advisory.Blocking() {
		t.Fatalf("advisory must not block")
	}
	unknown := PackageError{Kind: UnknownError, Position: "-", Message: "the driver failed"}
	if !unknown.Blocking() {
		t.Fatalf("an unclassified diagnostic must block")
	}
	missing := PackageError{Kind: ListError, Message: "no required module provides package example.com/missing"}
	if !missing.Blocking() {
		t.Fatalf("a resolution failure must block")
	}
}

// Indexing is hermetic by default: a module the local cache does not hold is
// reported, not fetched. A multi-repository workspace resolves one shared
// build list, so the caller must be able to lift that.
func TestLoadEnvironmentIsHermeticUnlessNetworkIsAllowed(t *testing.T) {
	hermetic, err := loadEnvironment(Options{})
	if err != nil {
		t.Fatalf("loadEnvironment() error = %v", err)
	}
	if !containsAll(hermetic, []string{"GOPROXY=off", "GOFLAGS=-mod=readonly"}) {
		t.Fatalf("hermetic environment lost its proxy and mod guards")
	}

	networked, err := loadEnvironment(Options{AllowNetwork: true})
	if err != nil {
		t.Fatalf("loadEnvironment(AllowNetwork) error = %v", err)
	}
	for _, entry := range networked {
		if entry == "GOPROXY=off" {
			t.Fatalf("AllowNetwork must not disable the module proxy")
		}
	}
}

func buildWorkspace(t *testing.T, root string, directories ...string) string {
	t.Helper()
	repositories := make([]workspace.Repository, 0, len(directories))
	for _, directory := range directories {
		repositories = append(repositories, workspace.Repository{
			Name:     filepath.Base(directory),
			Path:     directory,
			RealPath: directory,
		})
	}
	plan, err := goworkspace.BuildPlan(context.Background(), repositories, goworkspace.Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	target := filepath.Join(root, "state", "go.work")
	if _, err := goworkspace.Write(context.Background(), target, plan, repositories); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	return target
}

func writeFiles(t *testing.T, directory string, files map[string]string) {
	t.Helper()
	for name, contents := range files {
		target := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatalf("create %q: %v", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %q: %v", target, err)
		}
	}
}

func containsAll(values, wanted []string) bool {
	for _, want := range wanted {
		found := false
		for _, value := range values {
			if value == want {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
