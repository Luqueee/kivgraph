package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/Luqueee/kivgraph/internal/audit"
	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// doctorRepositoriesOptions carries the flags of
// `kivgraph doctor repositories`.
type doctorRepositoriesOptions struct {
	ConfigPath string
	Repository string
	JSON       bool
}

// doctorRepositoriesFlagSet declares them in one place, so the parser that
// runs the command and the help and completion that describe it read the same
// definitions.
func doctorRepositoriesFlagSet(options *doctorRepositoriesOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("doctor repositories", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	flags.StringVar(&options.Repository, "repository", "", "audit only this registered repository")
	flags.BoolVar(&options.JSON, "json", false, "emit the report as JSON on stdout")
	return flags
}

// runDoctorRepositories audits every registered repository without indexing
// it, and says what to change where it would contribute less than its
// registration promises.
//
// It exits non-zero only when a finding is blocking -- a repository, or a
// package of one, that contributes nothing at all. A partial finding is a hole
// the owner may have chosen, and an audit that failed on those would be a
// gate nobody could pass.
func runDoctorRepositories(args []string, stdout, stderr io.Writer) int {
	var options doctorRepositoriesOptions
	flags := doctorRepositoriesFlagSet(&options)
	if parsed, code := parseCommandFlags("doctor repositories", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "doctor repositories: unexpected arguments: %v\n", flags.Args())
		return 2
	}

	loaded, err := config.Load(options.ConfigPath)
	if err != nil {
		fmt.Fprintf(stderr, "doctor repositories: %v\n", err)
		return 1
	}
	ctx := context.Background()
	registry, err := workspace.NewRegistry(ctx, loaded.Repositories)
	if err != nil {
		fmt.Fprintf(stderr, "doctor repositories: register repositories: %v\n", err)
		return 1
	}
	repositories := registry.List()
	if options.Repository != "" && !namesRepository(repositories, options.Repository) {
		fmt.Fprintf(stderr, "doctor repositories: no repository named %q is registered\n", options.Repository)
		return 2
	}

	auditOptions := audit.OptionsFromConfig(loaded.Config)
	auditOptions.Repository = options.Repository
	report, err := audit.Run(ctx, repositories, auditOptions)
	if err != nil {
		fmt.Fprintf(stderr, "doctor repositories: %v\n", err)
		return 1
	}

	if options.JSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(stderr, "doctor repositories: write report: %v\n", err)
			return 1
		}
	} else {
		writeAuditReport(stdout, report)
	}
	if report.Blocking() != 0 {
		return 1
	}
	return 0
}

// writeAuditReport prints the report so that the first line answers the
// question and every finding names its repository, what was observed and what
// to do about it.
func writeAuditReport(stdout io.Writer, report audit.Report) {
	writeInfo(stdout, "audit: %d repositories, %d finding(s), %d blocking",
		report.Repositories, len(report.Findings), report.Blocking())
	for _, finding := range report.Findings {
		line := fmt.Sprintf("audit.%s: %s [%s] %s",
			finding.Severity, finding.Repository, finding.Code, finding.Detail)
		if finding.Severity == audit.SeverityBlocking {
			writeWarning(stdout, "%s", line)
		} else {
			writeInfo(stdout, "%s", line)
		}
		writeInfo(stdout, "  remedy: %s", finding.Remedy.Summary)
		if finding.Remedy.Path != "" {
			writeInfo(stdout, "  remedy.path: %s", finding.Remedy.Path)
		}
		if finding.Remedy.ConfigKey != "" {
			writeInfo(stdout, "  remedy.config: %s: %s", finding.Remedy.ConfigKey, finding.Remedy.ConfigValue)
		}
		if finding.Remedy.Command != "" {
			writeInfo(stdout, "  remedy.command: %s", finding.Remedy.Command)
		}
	}
	if len(report.Findings) == 0 {
		writeInfo(stdout, "audit: every registered repository declares what a pass needs to read it")
	}
}

func namesRepository(repositories []workspace.Repository, name string) bool {
	for _, repository := range repositories {
		if repository.Name == name {
			return true
		}
	}
	return false
}
