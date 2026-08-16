package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// statsModel is the live view. It owns no state a refresh cannot rebuild: every
// tick replaces the whole observation, so a process that exits between two ticks
// simply stops having a row.
type statsModel struct {
	list     processLister
	interval time.Duration
	styles   statsStyles
	report   statsReport
	err      error
	quitting bool
}

type statsTickMsg struct{}

func newStatsModel(list processLister, interval time.Duration, color bool) *statsModel {
	model := &statsModel{list: list, interval: interval, styles: newStatsStyles(color)}
	model.refresh()
	return model
}

func (model *statsModel) refresh() {
	report, err := collectStats(model.list, os.Getpid())
	model.report, model.err = report, err
}

func (model *statsModel) Init() tea.Cmd { return model.tick() }

func (model *statsModel) tick() tea.Cmd {
	return tea.Tick(model.interval, func(time.Time) tea.Msg { return statsTickMsg{} })
}

func (model *statsModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.KeyMsg:
		switch message.String() {
		case "q", "esc", "ctrl+c":
			model.quitting = true
			return model, tea.Quit
		case "r":
			model.refresh()
			return model, nil
		}
	case statsTickMsg:
		model.refresh()
		return model, model.tick()
	}
	return model, nil
}

func (model *statsModel) View() string {
	if model.quitting {
		return ""
	}
	styles := model.styles
	lines := []string{styles.render(styles.title, "kivgraph stats")}
	if model.err != nil {
		lines = append(lines,
			styles.render(styles.warning, "the processes of this user could not be listed: "+model.err.Error()))
		return joinLines(append(lines, "", styles.render(styles.help, "q quit  ·  r refresh"))) + "\n"
	}
	if len(model.report.Rows) == 0 {
		lines = append(lines, styles.render(styles.dim, "no kivgraph process is running"))
		return joinLines(append(lines, "", styles.render(styles.help, "q quit  ·  r refresh"))) + "\n"
	}

	proportional := model.report.Proportional
	if proportional {
		lines = append(lines, styles.render(styles.dim, fmt.Sprintf("%-7s %-7s %9s %9s %9s %9s %7s %8s  %s",
			"PID", "CMD", "COST", "PRIVATE", "SHARED", "PEAK", "CPU", "UP", "")))
	} else {
		lines = append(lines, styles.render(styles.dim, fmt.Sprintf("%-7s %-7s %9s %9s %7s %8s  %s",
			"PID", "CMD", "RESIDENT", "PEAK", "CPU", "UP", "")))
	}

	heaviest := int64(0)
	for _, row := range model.report.Rows {
		if row.cost() > heaviest {
			heaviest = row.cost()
		}
	}
	for _, row := range model.report.Rows {
		command := styles.render(styles.commandStyle(row.Command), fmt.Sprintf("%-7s", row.Command))
		bar := styles.render(styles.commandStyle(row.Command), statsBar(row.cost(), heaviest))
		if proportional {
			lines = append(lines, fmt.Sprintf("%-7d %s %9s %9s %9s %9s %7s %8s  %s",
				row.PID, command,
				formatBytes(row.Proportional), formatBytes(row.PrivateDirty),
				formatBytes(row.SharedClean), formatBytes(row.Peak),
				formatDuration(row.Sample.CPU), formatDuration(row.Sample.Uptime), bar))
			continue
		}
		lines = append(lines, fmt.Sprintf("%-7d %s %9s %9s %7s %8s  %s",
			row.PID, command, formatBytes(row.Resident), formatBytes(row.Peak),
			formatDuration(row.Sample.CPU), formatDuration(row.Sample.Uptime), bar))
	}

	lines = append(lines, "")
	lines = append(lines, styles.render(styles.total, statsTotalLine(model.report)))
	if caveat := statsCaveat(model.report); caveat != "" {
		lines = append(lines, styles.render(styles.warning, caveat))
	}
	for _, row := range model.report.Rows {
		if row.Detail == "" {
			continue
		}
		lines = append(lines, styles.render(styles.dim, fmt.Sprintf("%-7d %s", row.PID, row.Detail)))
	}
	lines = append(lines, "")
	lines = append(lines, styles.render(styles.help,
		fmt.Sprintf("q quit  ·  r refresh  ·  every %s", formatDuration(model.interval))))
	return joinLines(lines) + "\n"
}

// statsBar draws a share of the heaviest row. It is deliberately not a share of
// the machine's memory: what a reader compares here is one process against
// another, and scaling to total memory would flatten every row into nothing.
func statsBar(value, heaviest int64) string {
	const width = 18
	if heaviest <= 0 || value <= 0 {
		return strings.Repeat("·", width)
	}
	filled := int(int64(width) * value / heaviest)
	if filled < 1 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("·", width-filled)
}

type statsStyles struct {
	color   bool
	title   lipgloss.Style
	dim     lipgloss.Style
	help    lipgloss.Style
	total   lipgloss.Style
	warning lipgloss.Style
	serve   lipgloss.Style
	ui      lipgloss.Style
	index   lipgloss.Style
	other   lipgloss.Style
}

func newStatsStyles(color bool) statsStyles {
	styles := statsStyles{color: color}
	if !color {
		return styles
	}
	styles.title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D7FF"))
	styles.dim = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("#777777"))
	styles.help = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("#777777"))
	styles.total = lipgloss.NewStyle().Bold(true)
	styles.warning = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFCC66"))
	styles.serve = lipgloss.NewStyle().Foreground(lipgloss.Color("#00D7FF"))
	styles.ui = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
	styles.index = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFCC66"))
	styles.other = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	return styles
}

// commandStyle colours a row by what the process is doing, because that is what
// a reader is looking for: an index pass explains a peak that a server would not.
func (styles statsStyles) commandStyle(command string) lipgloss.Style {
	switch command {
	case "serve":
		return styles.serve
	case "ui":
		return styles.ui
	case "index":
		return styles.index
	default:
		return styles.other
	}
}

func (styles statsStyles) render(style lipgloss.Style, value string) string {
	if !styles.color {
		return value
	}
	return style.Render(value)
}

func runStatsTUI(output io.Writer, list processLister, interval time.Duration) error {
	color := os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	program := tea.NewProgram(
		newStatsModel(list, interval, color),
		tea.WithOutput(output),
		tea.WithInput(os.Stdin),
	)
	_, err := program.Run()
	return err
}
