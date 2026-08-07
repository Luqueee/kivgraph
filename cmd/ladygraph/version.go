package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/Luqueee/ladygraph/internal/version"
)

func runVersionJSON(stdout, stderr io.Writer) int {
	executablePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "version --json: resolve executable: %v\n", err)
		return 1
	}
	workingDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "version --json: resolve working directory: %v\n", err)
		return 1
	}
	provenance, err := version.Collect(executablePath, workingDir)
	if err != nil {
		fmt.Fprintf(stderr, "version --json: %v\n", err)
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(provenance); err != nil {
		fmt.Fprintf(stderr, "version --json: encode output: %v\n", err)
		return 1
	}
	return 0
}
