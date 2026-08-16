package main

import (
	"encoding/json"
	"io"
	"os"

	"github.com/Luqueee/kivgraph/internal/version"
)

func runVersionJSON(stdout, stderr io.Writer) int {
	executablePath, err := os.Executable()
	if err != nil {
		writeCommandError(stderr, "version --json: resolve executable: %v", err)
		return 1
	}
	workingDir, err := os.Getwd()
	if err != nil {
		writeCommandError(stderr, "version --json: resolve working directory: %v", err)
		return 1
	}
	provenance, err := version.Collect(executablePath, workingDir)
	if err != nil {
		writeCommandError(stderr, "version --json: %v", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(provenance); err != nil {
		writeCommandError(stderr, "version --json: encode output: %v", err)
		return 1
	}
	return 0
}
