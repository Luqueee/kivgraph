package rustloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// MetadataOptions asks for one resolution of a Cargo workspace under the same
// rules the analyzer runs it: the target directory is redirected out of the
// repository and cargo stays offline unless the caller allows the network.
type MetadataOptions struct {
	// Workspace is the absolute path of the directory holding the manifest.
	Workspace string
	// TargetDirectory keeps build state out of the repository. Empty leaves
	// cargo's default, which writes inside it.
	TargetDirectory string
	AllowNetwork    bool
}

// MetadataResult is what cargo answered.
type MetadataResult struct {
	// Resolved reports whether cargo resolved the dependency graph. When it
	// is false, the analyzer that runs the same command falls back to
	// `--no-deps` and indexes the crate with no dependency at all -- so the
	// workspace loads, and every edge leaving it is unresolved.
	Resolved bool
	// Detail is cargo's own first error line when Resolved is false.
	Detail string
}

// ErrCargoUnavailable reports that no cargo is on the PATH, so nothing can be
// asked about the workspace.
var ErrCargoUnavailable = errors.New("cargo is not on the PATH")

// Metadata resolves a Cargo workspace with `cargo metadata`, which is the
// command rust-analyzer runs before it can load one.
//
// It answers the failure that costs the most and shows the least: cargo
// resolving nothing because the local registry cache does not hold a version
// the lockfile pins, while the analyzer succeeds anyway with no dependencies.
// The pass reports that as one warning among hundreds; asked directly, it is a
// yes or a no.
func Metadata(ctx context.Context, options MetadataOptions) (MetadataResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	workspace := strings.TrimSpace(options.Workspace)
	if workspace == "" {
		return MetadataResult{}, errors.New("the workspace path must not be empty")
	}
	binary, err := exec.LookPath("cargo")
	if err != nil {
		return MetadataResult{}, ErrCargoUnavailable
	}

	arguments := []string{"metadata", "--format-version", "1", "--locked"}
	environment := os.Environ()
	if target := strings.TrimSpace(options.TargetDirectory); target != "" {
		environment = append(environment, "CARGO_TARGET_DIR="+target)
	}
	if !options.AllowNetwork {
		arguments = append(arguments, "--offline")
		environment = append(environment, "CARGO_NET_OFFLINE=true")
	}

	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir = workspace
	command.Env = environment
	var stderr bytes.Buffer
	command.Stderr = &stderr
	command.Stdout = nil
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return MetadataResult{}, ctxErr
		}
		return MetadataResult{Resolved: false, Detail: firstCargoError(stderr.String())}, nil
	}
	return MetadataResult{Resolved: true}, nil
}

// firstCargoError reads the line that says what cargo refused. Cargo prints
// the reason first and then several lines of advice, and the advice is not the
// finding.
func firstCargoError(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "error:") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "error:"))
		}
	}
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "cargo failed without writing a reason"
	}
	return fmt.Sprintf("cargo failed: %s", strings.SplitN(trimmed, "\n", 2)[0])
}
