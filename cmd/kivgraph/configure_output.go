package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/Luqueee/kivgraph/internal/integrations"
)

// configureSurface is one user-visible part of an agent setup. The wizard
// reports all four together because a successful MCP entry alone does not make
// an agent ready to use Kivgraph as intended.
type configureSurface uint8

const (
	configureSurfaceMCP configureSurface = iota
	configureSurfaceSkill
	configureSurfaceHook
	configureSurfaceInstructions
	configureSurfaceCount
)

var configureSurfaceHeaders = [...]string{
	"MCP",
	"Skill",
	"Search guard",
	"Instructions",
}

type configureSurfaceState uint8

const (
	configureStateUnreported configureSurfaceState = iota
	configureStateReady
	configureStateInstalled
	configureStatePlanned
	configureStateNotSupported
	configureStateShared
	configureStateFailed
)

type configureCell struct {
	text  string
	state configureSurfaceState
}

type configureTargetReport struct {
	target integrations.Target
	cells  [configureSurfaceCount]configureCell
}

// configureReport retains presentation facts only. Paths and implementation
// details remain available from the focused integration commands; duplicating
// them for every row made the first-run command impossible to scan.
type configureReport struct {
	targets     []configureTargetReport
	index       map[integrations.Target]int
	dryRun      bool
	initialized bool
	transport   string
	changed     int
	ready       int
	planned     int
	unsupported int
	failures    int
}

func newConfigureReport(targets []integrations.Target, dryRun bool) *configureReport {
	report := &configureReport{
		targets: make([]configureTargetReport, len(targets)),
		index:   make(map[integrations.Target]int, len(targets)),
		dryRun:  dryRun,
	}
	for index, target := range targets {
		report.targets[index].target = target
		report.index[target] = index
		for surface := configureSurfaceMCP; surface < configureSurfaceCount; surface++ {
			report.targets[index].cells[surface] = configureCell{text: "not run", state: configureStateUnreported}
		}
	}
	return report
}

func configureTransport(options integrations.Options, configure configureOptions, failed bool) string {
	if failed {
		return "unavailable"
	}
	if configure.Stdio || options.Endpoint.URL == "" {
		return "stdio"
	}
	return "daemon"
}

func (report *configureReport) plan(target integrations.Target, surface configureSurface, plan integrations.Plan) {
	state := configureStateReady
	text := "already configured"
	switch plan.Status {
	case "managed":
	case "installed":
		state, text = configureStateInstalled, "installed"
	case "would-install":
		state, text = configureStatePlanned, "will install"
	default:
		if plan.Changed {
			state, text = configureStateInstalled, "updated"
		} else {
			text = strings.ReplaceAll(plan.Status, "-", " ")
		}
	}
	report.cell(target, surface, configureCell{text: text, state: state})
	if plan.Changed {
		if plan.DryRun {
			report.planned++
		} else {
			report.changed++
		}
		return
	}
	report.ready++
}

func (report *configureReport) instructions(
	manager integrations.Manager,
	targets []integrations.Target,
	installedFor integrations.Target,
	plan integrations.InstructionsPlan,
) error {
	_, path, err := manager.InstructionsDestinationForTarget(installedFor)
	if err != nil {
		return err
	}
	for _, target := range targets {
		_, targetPath, destinationErr := manager.InstructionsDestinationForTarget(target)
		if destinationErr != nil {
			return destinationErr
		}
		if targetPath != path {
			continue
		}
		if target == installedFor {
			report.instructionsPlan(target, plan)
			continue
		}
		report.cell(target, configureSurfaceInstructions, configureCell{text: "shared", state: configureStateShared})
	}
	return nil
}

func (report *configureReport) instructionsFailed(
	manager integrations.Manager,
	targets []integrations.Target,
	failedFor integrations.Target,
) error {
	_, path, err := manager.InstructionsDestinationForTarget(failedFor)
	if err != nil {
		return err
	}
	for _, target := range targets {
		_, targetPath, destinationErr := manager.InstructionsDestinationForTarget(target)
		if destinationErr != nil {
			return destinationErr
		}
		if targetPath == path {
			report.failed(target, configureSurfaceInstructions)
		}
	}
	return nil
}

func (report *configureReport) instructionsPlan(target integrations.Target, plan integrations.InstructionsPlan) {
	report.plan(target, configureSurfaceInstructions, integrations.Plan{
		Status:  plan.Status,
		Changed: plan.Changed,
		DryRun:  plan.DryRun,
	})
}

func (report *configureReport) notSupported(target integrations.Target, surface configureSurface) {
	report.cell(target, surface, configureCell{text: "not supported", state: configureStateNotSupported})
	report.unsupported++
}

func (report *configureReport) failed(target integrations.Target, surface configureSurface) {
	report.cell(target, surface, configureCell{text: "failed", state: configureStateFailed})
	report.failures++
}

func (report *configureReport) cell(target integrations.Target, surface configureSurface, cell configureCell) {
	index, ok := report.index[target]
	if !ok {
		return
	}
	report.targets[index].cells[surface] = cell
}

func writeConfigureReport(stdout io.Writer, report *configureReport) {
	paint := styleFor(stdout)
	title := "Kivgraph configured"
	if report.failures != 0 {
		title = "Kivgraph configuration incomplete"
	}
	if report.dryRun {
		title = "Kivgraph configuration plan"
	}
	fmt.Fprintf(stdout, "%s%s%s\n", paint.bold, title, paint.reset)
	fmt.Fprintln(stdout, "  Scope: user")
	fmt.Fprintf(stdout, "  MCP transport: %s\n", report.transport)
	if report.initialized {
		fmt.Fprintln(stdout, "  Setup: initialized")
	}
	if report.dryRun {
		fmt.Fprintln(stdout, "  Mode: dry-run; no files changed")
	}

	widths := configureReportWidths(report)
	fmt.Fprintln(stdout)
	writeConfigureReportRow(stdout, paint, widths, []string{
		"Agent", configureSurfaceHeaders[configureSurfaceMCP], configureSurfaceHeaders[configureSurfaceSkill],
		configureSurfaceHeaders[configureSurfaceHook], configureSurfaceHeaders[configureSurfaceInstructions],
	}, nil)
	for _, target := range report.targets {
		values := []string{integrationTargetLabel(target.target)}
		states := make([]configureSurfaceState, 0, configureSurfaceCount)
		for surface := configureSurfaceMCP; surface < configureSurfaceCount; surface++ {
			values = append(values, target.cells[surface].text)
			states = append(states, target.cells[surface].state)
		}
		writeConfigureReportRow(stdout, paint, widths, values, states)
	}
	fmt.Fprintf(stdout, "\n  Changes: %s\n", report.changesText())
}

func configureReportWidths(report *configureReport) []int {
	widths := []int{len("Agent")}
	for _, header := range configureSurfaceHeaders {
		widths = append(widths, len(header))
	}
	for _, target := range report.targets {
		widths[0] = max(widths[0], len(integrationTargetLabel(target.target)))
		for surface := configureSurfaceMCP; surface < configureSurfaceCount; surface++ {
			widths[surface+1] = max(widths[surface+1], len(target.cells[surface].text))
		}
	}
	return widths
}

func writeConfigureReportRow(
	stdout io.Writer,
	paint style,
	widths []int,
	values []string,
	states []configureSurfaceState,
) {
	fmt.Fprint(stdout, "  ")
	for index, value := range values {
		prefix, suffix := "", ""
		if index == 0 {
			prefix, suffix = paint.accent, paint.reset
		} else if states != nil {
			prefix, suffix = configureCellStyle(states[index-1], paint)
		}
		fmt.Fprint(stdout, prefix, value, suffix, strings.Repeat(" ", widths[index]-len(value)))
		if index != len(values)-1 {
			fmt.Fprint(stdout, "  ")
		}
	}
	fmt.Fprintln(stdout)
}

func configureCellStyle(state configureSurfaceState, paint style) (string, string) {
	switch state {
	case configureStateInstalled:
		return paint.success, paint.reset
	case configureStatePlanned:
		return paint.accent, paint.reset
	case configureStateFailed:
		return paint.error, paint.reset
	case configureStateNotSupported, configureStateShared, configureStateReady, configureStateUnreported:
		return paint.dim, paint.reset
	default:
		return "", ""
	}
}

func (report *configureReport) changesText() string {
	parts := make([]string, 0, 4)
	if report.dryRun {
		if report.planned == 0 {
			parts = append(parts, "none planned")
		} else {
			parts = append(parts, fmt.Sprintf("%d planned", report.planned))
		}
	} else if report.changed == 0 {
		parts = append(parts, "none applied")
	} else {
		parts = append(parts, fmt.Sprintf("%d applied", report.changed))
	}
	if report.ready != 0 {
		parts = append(parts, fmt.Sprintf("%d already configured", report.ready))
	}
	if report.unsupported != 0 {
		parts = append(parts, fmt.Sprintf("%d not supported", report.unsupported))
	}
	if report.failures != 0 {
		parts = append(parts, fmt.Sprintf("%d failed; see errors above", report.failures))
	}
	return strings.Join(parts, ", ") + "."
}
