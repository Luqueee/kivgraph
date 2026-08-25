package audit

import (
	"context"
	"fmt"
	"strings"

	"github.com/Luqueee/kivgraph/internal/goloader"
	"github.com/Luqueee/kivgraph/internal/goworkspace"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// buildConstraintMessage is the go command's own wording for a package none of
// whose files the build configuration selects. It is matched rather than
// re-derived because it is the go command that decides it.
const buildConstraintMessage = "build constraints exclude all Go files"

// auditGo reports what the go command refuses to resolve in a repository
// registered as Go, listing packages without building a type universe.
func auditGo(
	ctx context.Context,
	repository workspace.Repository,
	options Options,
) ([]Finding, error) {
	discovery, err := workspace.DiscoverGo(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("discover Go modules: %w", err)
	}
	if len(discovery.Modules) == 0 {
		return []Finding{{
			Repository: repository.Name,
			Language:   "go",
			Code:       CodeGoNoModule,
			Severity:   SeverityBlocking,
			Detail:     "no go.mod in the tree, so there is no module to load",
			Remedy: Remedy{
				Summary: "run `go mod init` in the module root, or take the repository out of the registry if it holds no Go module",
				Command: "go mod init <module path>",
			},
		}}, nil
	}

	findings := make([]Finding, 0)
	for _, module := range discovery.Modules {
		patterns := goworkspace.PackagePatternsForModule(discovery.Packages, module)
		if len(patterns) == 0 {
			continue
		}
		inspected, err := goloader.Inspect(ctx, goloader.Options{
			Directory:    module.RootPath,
			Patterns:     patterns,
			BuildTags:    append([]string(nil), options.GoBuildTags...),
			AllowNetwork: options.GoAllowNetwork,
		})
		if err != nil {
			findings = append(findings, Finding{
				Repository: repository.Name,
				Language:   "go",
				Code:       CodeGoPackageError,
				Severity:   SeverityBlocking,
				Detail:     fmt.Sprintf("the go command could not list %s: %v", relativePath(repository.RealPath, module.RootPath), err),
				Remedy: Remedy{
					Summary: "make the module list cleanly, then re-run the audit",
					Command: "go list ./...",
				},
			})
			continue
		}
		filesByImportPath := make(map[string][]string, len(discovery.Packages))
		for _, packageValue := range discovery.Packages {
			filesByImportPath[packageValue.ImportPath] = packageValue.Files
		}
		for _, packageValue := range inspected.Packages {
			for _, message := range packageValue.Errors {
				findings = append(findings, goPackageFinding(repository,
					packageValue.PackagePath, message, filesByImportPath[packageValue.PackagePath]))
			}
		}
	}
	return findings, nil
}

// goPackageFinding classifies one message the go command produced. A package
// excluded by build constraints is the common one and has a remedy; anything
// else is reported as observed rather than guessed at.
func goPackageFinding(repository workspace.Repository, packagePath, message string, files []string) Finding {
	if strings.Contains(message, buildConstraintMessage) {
		remedy := Remedy{
			Summary:   "grant the build tag the package is guarded by, or accept the exclusion when the package is development-only",
			ConfigKey: "go.build_tags",
		}
		if tags := buildTagsOf(files); len(tags) != 0 {
			remedy.Summary = fmt.Sprintf("grant the %s tag, or accept the exclusion when the package is development-only",
				strings.Join(tags, " and "))
			remedy.ConfigValue = "[" + strings.Join(tags, ", ") + "]"
		}
		return Finding{
			Repository: repository.Name,
			Language:   "go",
			Code:       CodeGoBuildConstraints,
			Severity:   SeverityPartial,
			Detail: fmt.Sprintf("%s declares no file the build configuration selects, so nothing it holds is in the graph: %s",
				packagePath, message),
			Remedy: remedy,
		}
	}
	return Finding{
		Repository: repository.Name,
		Language:   "go",
		Code:       CodeGoPackageError,
		Severity:   SeverityPartial,
		Detail:     fmt.Sprintf("%s: %s", packagePath, message),
		Remedy: Remedy{
			Summary: "fix what the go command reports; a package it cannot resolve carries no edge",
			Command: "go list ./...",
		},
	}
}
