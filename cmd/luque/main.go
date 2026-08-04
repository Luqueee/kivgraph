package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	mcpserver "github.com/Luqueee/luque/internal/mcp"
	"github.com/Luqueee/luque/internal/storage/ladybug"
	"github.com/Luqueee/luque/internal/synthetic"
	"github.com/Luqueee/luque/internal/version"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "serve" {
		if err := mcpserver.Run(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
			os.Exit(1)
		}
		return
	}

	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

type storageDiagnoser func(context.Context, string) (ladybug.StorageDiagnosis, error)

func run(args []string, stdout, stderr io.Writer) int {
	return runWithStorageDiagnoser(args, stdout, stderr, ladybug.DiagnoseStorage)
}

func runWithStorageDiagnoser(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser) int {
	if len(args) == 2 && args[1] == "version" {
		fmt.Fprintln(stdout, version.Value)
		return 0
	}
	if len(args) >= 3 && args[1] == "doctor" && args[2] == "storage" {
		return runDoctorStorage(args[3:], stdout, stderr, diagnose)
	}
	if len(args) >= 3 && args[1] == "benchmark" && args[2] == "generate-graph" {
		return runGenerateGraph(args[3:], stdout, stderr)
	}

	fmt.Fprintf(stderr, "usage: %s version|serve|doctor storage --database PATH|benchmark generate-graph [flags]\n", args[0])
	return 2
}

func runDoctorStorage(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser) int {
	flags := flag.NewFlagSet("doctor storage", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var databasePath string
	flags.StringVar(&databasePath, "database", "", "existing LadybugDB database path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "doctor storage: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if databasePath == "" {
		fmt.Fprintln(stderr, "doctor storage: --database is required")
		return 2
	}

	diagnosis, err := diagnose(context.Background(), databasePath)
	if err != nil {
		fmt.Fprintf(stderr, "doctor storage: %v\n", err)
		return 1
	}
	state := "FAIL"
	if diagnosis.Healthy {
		state = "PASS"
	}
	fmt.Fprintf(stdout, "storage doctor: %s\n", state)
	fmt.Fprintf(stdout, "database: %s\n", diagnosis.Path)
	for _, check := range diagnosis.Checks {
		fmt.Fprintf(stdout, "[%s] %s: %s\n", check.Status, check.Name, check.Detail)
	}
	if diagnosis.Healthy {
		return 0
	}
	return 1
}

func runGenerateGraph(args []string, stdout, stderr io.Writer) int {
	config := synthetic.DefaultConfig()
	flags := flag.NewFlagSet("generate-graph", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.IntVar(&config.Repositories, "repositories", config.Repositories, "number of repositories")
	flags.IntVar(&config.Files, "files", config.Files, "number of files")
	flags.IntVar(&config.Symbols, "symbols", config.Symbols, "number of symbols")
	flags.IntVar(&config.Edges, "edges", config.Edges, "number of total edges")
	flags.Int64Var(&config.Seed, "seed", config.Seed, "deterministic corpus seed")
	flags.StringVar(&config.OutputDir, "output", config.OutputDir, "output directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "generate-graph: unexpected arguments: %v\n", flags.Args())
		return 2
	}

	manifest, err := synthetic.Generate(context.Background(), config)
	if err != nil {
		fmt.Fprintf(stderr, "generate-graph: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "generated %d repositories, %d files, %d symbols, %d edges at %s (seed %d)\n",
		manifest.Repositories,
		manifest.Files,
		manifest.Symbols,
		manifest.Edges,
		config.OutputDir,
		manifest.Seed,
	)
	return 0
}
