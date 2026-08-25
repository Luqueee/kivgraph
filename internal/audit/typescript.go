package audit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// javaScriptSourceExtensions and typeScriptSourceExtensions are used to
// propose a project, never to decide what one claims: that answer belongs to
// workspace.ClaimedTypeScriptSources. What they answer is which of the two
// config files to propose, and a proposal has to name the language the
// repository actually writes.
var javaScriptSourceExtensions = []string{".mjs", ".cjs", ".js", ".jsx"}

var typeScriptSourceExtensions = []string{".ts", ".tsx", ".mts", ".cts"}

// auditTypeScript reports why a repository registered as TypeScript would
// contribute less than its registration promises.
func auditTypeScript(
	ctx context.Context,
	repository workspace.Repository,
	options Options,
) ([]Finding, error) {
	findings := make([]Finding, 0)
	registry, err := workspace.NewTypeScriptPackageRegistry(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("discover TypeScript packages: %w", err)
	}
	discovery, err := workspace.DiscoverTypeScript(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("discover TypeScript manifests: %w", err)
	}

	for _, conflict := range registry.Conflicts() {
		manifests := make([]string, 0, len(conflict.Manifests))
		for _, manifest := range conflict.Manifests {
			manifests = append(manifests, relativePath(repository.RealPath, manifest))
		}
		findings = append(findings, Finding{
			Repository: repository.Name,
			Language:   "typescript",
			Code:       CodeTypeScriptDuplicateName,
			Severity:   SeverityBlocking,
			Detail: fmt.Sprintf("the name %q is declared by %s, so no manifest can be its provider and every one of them leaves the registry",
				conflict.Name, strings.Join(manifests, " and ")),
			Remedy: Remedy{
				Summary: fmt.Sprintf("rename all but one of the packages called %q, or exclude the copies from the repository registration", conflict.Name),
			},
		})
	}

	packages := registry.List()
	withProject := 0
	for _, packageValue := range packages {
		if strings.TrimSpace(packageValue.ProjectPath) == "" {
			continue
		}
		withProject++
		claimed, err := workspace.ClaimedTypeScriptSources(packageValue.ProjectPath, repository.RealPath)
		if err != nil {
			return nil, fmt.Errorf("resolve sources of %q: %w", packageValue.ProjectPath, err)
		}
		if len(claimed) != 0 {
			continue
		}
		present := countByExtension(packageValue.RootPath, repository.Exclusions)
		findings = append(findings, Finding{
			Repository: repository.Name,
			Language:   "typescript",
			Code:       CodeTypeScriptProjectClaimsNo,
			Severity:   SeverityBlocking,
			Detail: fmt.Sprintf("the package %q declares the project %s and it claims no source file, while %s holds %s",
				packageValue.Name,
				relativePath(repository.RealPath, packageValue.ProjectPath),
				relativePath(repository.RealPath, packageValue.RootPath),
				describeExtensions(present)),
			Remedy: claimsNothingRemedy(repository, packageValue, present),
		})
	}

	switch {
	case len(packages) == 0 && len(registry.Conflicts()) == 0:
		findings = append(findings, Finding{
			Repository: repository.Name,
			Language:   "typescript",
			Code:       CodeTypeScriptNoPackage,
			Severity:   SeverityBlocking,
			Detail: fmt.Sprintf("no package.json in the tree declares a name, so there is no package to index (%d manifest(s) read)",
				len(discovery.PackageManifests)),
			Remedy: Remedy{
				Summary: "give the package.json a \"name\", or take the repository out of the registry if it holds no package",
				Path:    "package.json",
			},
		})
	case withProject == 0 && len(packages) != 0:
		findings = append(findings, noProjectFinding(repository, packages))
	}

	if withProject != 0 && !options.IncludeUnclaimedSources {
		unclaimed, err := workspace.UnclaimedTypeScriptSources(ctx, repository, discovery)
		if err != nil {
			return nil, fmt.Errorf("resolve unclaimed sources: %w", err)
		}
		if len(unclaimed) != 0 {
			findings = append(findings, Finding{
				Repository: repository.Name,
				Language:   "typescript",
				Code:       CodeTypeScriptUnclaimedSources,
				Severity:   SeverityPartial,
				Detail: fmt.Sprintf("%d source file(s) no project claims, starting at %s; nothing type-checks them and nothing reports them absent",
					len(unclaimed), relativePath(repository.RealPath, unclaimed[0])),
				Remedy: Remedy{
					Summary:     "widen the \"include\" of the project that should own them, or index them through the inferred project",
					ConfigKey:   "typescript.include_unclaimed_sources",
					ConfigValue: "true",
				},
			})
		}
	}
	return findings, nil
}

// noProjectFinding proposes the config file the repository's own sources ask
// for: a jsconfig for a package that ships JavaScript, a tsconfig otherwise.
// Proposing a tsconfig to a repository of .mjs files is what asks a
// JavaScript project to declare itself TypeScript.
func noProjectFinding(repository workspace.Repository, packages []workspace.TypeScriptPackage) Finding {
	names := make([]string, 0, len(packages))
	for _, packageValue := range packages {
		names = append(names, packageValue.Name)
	}
	sort.Strings(names)
	root := packages[0].RootPath
	if len(packages) > 1 {
		root = repository.RealPath
	}
	present := countByExtension(root, repository.Exclusions)
	javaScript := total(present, javaScriptSourceExtensions)
	typeScript := total(present, typeScriptSourceExtensions)

	remedy := Remedy{
		Summary: fmt.Sprintf("declare a project for %s", strings.Join(names, ", ")),
		Path:    filepath.ToSlash(filepath.Join(relativePath(repository.RealPath, root), "tsconfig.json")),
		Content: "{\n  \"compilerOptions\": {\n    \"module\": \"NodeNext\",\n    \"moduleResolution\": \"NodeNext\",\n    \"noEmit\": true\n  }\n}\n",
	}
	if javaScript > typeScript {
		remedy.Summary = fmt.Sprintf("declare a JavaScript project for %s with a jsconfig, which implies allowJs", strings.Join(names, ", "))
		remedy.Path = filepath.ToSlash(filepath.Join(relativePath(repository.RealPath, root), "jsconfig.json"))
	}
	return Finding{
		Repository: repository.Name,
		Language:   "typescript",
		Code:       CodeTypeScriptNoProject,
		Severity:   SeverityBlocking,
		Detail: fmt.Sprintf("%d named package(s) and no project: a package.json alone declares no program, so the repository contributes nothing while %s holds %s",
			len(packages), relativePath(repository.RealPath, root), describeExtensions(present)),
		Remedy: remedy,
	}
}

// claimsNothingRemedy answers the case of a project whose file selection
// reaches nothing: either the sources are JavaScript and the project does not
// read them, or its "include" points somewhere empty.
func claimsNothingRemedy(
	repository workspace.Repository,
	packageValue workspace.TypeScriptPackage,
	present map[string]int,
) Remedy {
	project := relativePath(repository.RealPath, packageValue.ProjectPath)
	if total(present, javaScriptSourceExtensions) > 0 && total(present, typeScriptSourceExtensions) == 0 {
		return Remedy{
			Summary: fmt.Sprintf("add \"allowJs\": true to %s, or rename it to jsconfig.json, which implies it", project),
			Path:    project,
		}
	}
	return Remedy{
		Summary: fmt.Sprintf("point the \"files\"/\"include\" of %s at the directory that holds the sources", project),
		Path:    project,
	}
}

// countByExtension counts the source files under root by extension, without
// descending into installed dependencies or build output. It answers "what
// does this tree actually hold", which is what makes a proposal specific.
func countByExtension(root string, exclusions []string) map[string]int {
	counts := make(map[string]int)
	skip := map[string]struct{}{
		"node_modules": {}, ".git": {}, "dist": {}, "build": {}, "out": {}, "coverage": {},
	}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if _, excluded := skip[entry.Name()]; excluded {
				return filepath.SkipDir
			}
			return nil
		}
		extension := filepath.Ext(path)
		for _, known := range append(append([]string(nil), javaScriptSourceExtensions...), typeScriptSourceExtensions...) {
			if extension == known {
				counts[known]++
				return nil
			}
		}
		return nil
	})
	return counts
}

func total(counts map[string]int, extensions []string) int {
	sum := 0
	for _, extension := range extensions {
		sum += counts[extension]
	}
	return sum
}

// describeExtensions reads the counts back as prose, most frequent first, so
// the detail says what the tree is made of instead of how many files it has.
func describeExtensions(counts map[string]int) string {
	if len(counts) == 0 {
		return "no source file of any language Kivgraph reads"
	}
	extensions := make([]string, 0, len(counts))
	for extension := range counts {
		extensions = append(extensions, extension)
	}
	sort.Slice(extensions, func(left, right int) bool {
		if counts[extensions[left]] != counts[extensions[right]] {
			return counts[extensions[left]] > counts[extensions[right]]
		}
		return extensions[left] < extensions[right]
	})
	parts := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		parts = append(parts, fmt.Sprintf("%d %s", counts[extension], extension))
	}
	return strings.Join(parts, ", ")
}
