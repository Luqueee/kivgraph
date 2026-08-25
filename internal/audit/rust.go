package audit

import (
	"context"
	"errors"
	"fmt"

	"github.com/Luqueee/kivgraph/internal/rustloader"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// auditRust reports whether cargo can resolve each workspace of a repository
// registered as Rust. A workspace cargo cannot resolve still loads -- the
// analyzer falls back to `--no-deps` -- and every edge leaving it is then
// unresolved, which is the failure this check exists for.
func auditRust(
	ctx context.Context,
	repository workspace.Repository,
	options Options,
) ([]Finding, error) {
	discovery, err := workspace.DiscoverCargo(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("discover Cargo workspaces: %w", err)
	}
	if len(discovery.Workspaces) == 0 {
		return []Finding{{
			Repository: repository.Name,
			Language:   "rust",
			Code:       CodeRustNoManifest,
			Severity:   SeverityBlocking,
			Detail:     "no Cargo.toml in the tree, so there is no workspace to load",
			Remedy: Remedy{
				Summary: "add the manifest of the crate, or take the repository out of the registry if it holds no Rust",
				Path:    "Cargo.toml",
			},
		}}, nil
	}

	findings := make([]Finding, 0)
	for _, cargoWorkspace := range discovery.Workspaces {
		result, err := rustloader.Metadata(ctx, rustloader.MetadataOptions{
			Workspace:       cargoWorkspace.RootPath,
			TargetDirectory: options.RustTargetDirectory,
			AllowNetwork:    options.RustAllowNetwork,
		})
		if errors.Is(err, rustloader.ErrCargoUnavailable) {
			// Which toolchain is installed is what `doctor` answers. An
			// audit that repeated it here would report the same absence
			// once per repository.
			return findings, nil
		}
		if err != nil {
			return nil, err
		}
		if result.Resolved {
			continue
		}
		findings = append(findings, Finding{
			Repository: repository.Name,
			Language:   "rust",
			Code:       CodeRustMetadataFailed,
			Severity:   SeverityPartial,
			Detail: fmt.Sprintf("cargo resolved no dependency for %s, so the analyzer loads the crate with none and every edge leaving it is unresolved: %s",
				relativePath(repository.RealPath, cargoWorkspace.ManifestPath), result.Detail),
			Remedy: Remedy{
				Summary: "populate the local registry cache for the versions the lockfile pins, or let the pass reach a registry",
				Command: "cargo fetch --locked",
			},
		})
	}
	return findings, nil
}
