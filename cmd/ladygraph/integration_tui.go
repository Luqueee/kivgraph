package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Luqueee/ladygraph/internal/integrations"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var errIntegrationSelectionCancelled = errors.New("selection cancelled")

type integrationSelectionModel struct {
	detections []integrations.TargetDetection
	selected   []bool
	cursor     int
	scope      integrations.Scope
	styles     integrationTUIStyles
	message    string
	confirmed  bool
	cancelled  bool
}

func newIntegrationSelectionModel(
	detections []integrations.TargetDetection,
	defaults []int,
	scope integrations.Scope,
	color bool,
) *integrationSelectionModel {
	selected := make([]bool, len(detections))
	cursor := 0
	cursorSet := false
	for _, index := range defaults {
		if index < 0 || index >= len(selected) {
			continue
		}
		selected[index] = true
		if !cursorSet {
			cursor = index
			cursorSet = true
		}
	}
	return &integrationSelectionModel{
		detections: detections,
		selected:   selected,
		cursor:     cursor,
		scope:      scope,
		styles:     newIntegrationTUIStyles(color),
	}
}

func (model *integrationSelectionModel) Init() tea.Cmd {
	return nil
}

func (model *integrationSelectionModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return model, nil
	}
	model.message = ""
	switch key.Type {
	case tea.KeyUp:
		model.moveCursor(-1)
	case tea.KeyDown:
		model.moveCursor(1)
	case tea.KeyHome:
		model.cursor = 0
	case tea.KeyEnd:
		if len(model.detections) > 0 {
			model.cursor = len(model.detections) - 1
		}
	case tea.KeySpace:
		model.toggleCurrent()
	case tea.KeyEnter:
		if model.selectedCount() == 0 {
			model.message = "select at least one coding agent"
			return model, nil
		}
		model.confirmed = true
		return model, tea.Quit
	case tea.KeyEsc, tea.KeyCtrlC:
		model.cancelled = true
		return model, tea.Quit
	case tea.KeyRunes:
		if len(key.Runes) != 1 {
			return model, nil
		}
		switch key.Runes[0] {
		case 'j':
			model.moveCursor(1)
		case 'k':
			model.moveCursor(-1)
		case ' ':
			model.toggleCurrent()
		case 'a', 'A':
			model.setAll(true)
		case 'n', 'N':
			model.setAll(false)
		case 'q', 'Q':
			model.cancelled = true
			return model, tea.Quit
		}
	}
	return model, nil
}

func (model *integrationSelectionModel) View() string {
	selected := model.selectedCount()
	total := len(model.detections)
	lines := []string{
		model.styles.render(model.styles.title, fmt.Sprintf("Coding agents for %s scope", model.scope)),
		model.styles.render(model.styles.subtitle, fmt.Sprintf("%d/%d selected", selected, total)),
		"",
	}
	for index, detection := range model.detections {
		cursor := "  "
		if index == model.cursor {
			cursor = model.styles.render(model.styles.cursor, "❯ ")
		}
		checkbox := "○"
		if model.selected[index] {
			checkbox = "●"
		}
		checkbox = model.styles.render(model.styles.checked, checkbox)
		label := integrationTargetLabel(detection.Target)
		if detection.Detected {
			label += model.styles.render(model.styles.detected, "  detected")
		}
		line := cursor + checkbox + " " + label
		if index == model.cursor {
			line = model.styles.render(model.styles.cursor, line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, "")
	if model.message != "" {
		lines = append(lines, model.styles.render(model.styles.warning, model.message))
		lines = append(lines, "")
	}
	lines = append(lines,
		model.styles.render(model.styles.help, "↑/↓ or j/k move • space toggle • a all • n none"),
		model.styles.render(model.styles.help, "enter confirm • q or esc cancel"),
	)
	return joinLines(lines)
}

func (model *integrationSelectionModel) moveCursor(delta int) {
	if len(model.detections) == 0 {
		return
	}
	model.cursor += delta
	if model.cursor < 0 {
		model.cursor = 0
	}
	if model.cursor >= len(model.detections) {
		model.cursor = len(model.detections) - 1
	}
}

func (model *integrationSelectionModel) toggleCurrent() {
	if len(model.detections) == 0 {
		return
	}
	model.selected[model.cursor] = !model.selected[model.cursor]
}

func (model *integrationSelectionModel) setAll(value bool) {
	for index := range model.selected {
		model.selected[index] = value
	}
}

func (model *integrationSelectionModel) selectedCount() int {
	count := 0
	for _, selected := range model.selected {
		if selected {
			count++
		}
	}
	return count
}

func (model *integrationSelectionModel) selectedIndices() []int {
	indices := make([]int, 0, model.selectedCount())
	for index, selected := range model.selected {
		if selected {
			indices = append(indices, index)
		}
	}
	return indices
}

func runIntegrationSelection(
	input io.Reader,
	output io.Writer,
	detections []integrations.TargetDetection,
	defaults []int,
	scope integrations.Scope,
) ([]int, error) {
	if input == nil {
		return nil, fmt.Errorf("interactive selection requires standard input")
	}
	interactive := integrationTUIIsInteractive(output)
	if !interactive {
		if file, ok := input.(*os.File); ok && !isTerminal(file) {
			return nil, fmt.Errorf("interactive selection requires a terminal; pass --target")
		}
	}
	model := newIntegrationSelectionModel(detections, defaults, scope, styleFor(output).reset != "")
	options := []tea.ProgramOption{
		tea.WithInput(input),
		tea.WithOutput(output),
	}
	if interactive {
		options = append(options, tea.WithAltScreen())
	} else {
		options = append(options, tea.WithoutRenderer())
	}
	finalModel, err := tea.NewProgram(model, options...).Run()
	if errors.Is(err, tea.ErrInterrupted) || errors.Is(err, tea.ErrProgramKilled) {
		return nil, errIntegrationSelectionCancelled
	}
	if err != nil {
		return nil, fmt.Errorf("run agent selector: %w", err)
	}
	selection, ok := finalModel.(*integrationSelectionModel)
	if !ok {
		return nil, fmt.Errorf("run agent selector: unexpected model %T", finalModel)
	}
	if selection.cancelled || !selection.confirmed {
		return nil, errIntegrationSelectionCancelled
	}
	return selection.selectedIndices(), nil
}

func integrationTUIIsInteractive(output io.Writer) bool {
	file, ok := output.(*os.File)
	return ok && isTerminal(file) && os.Getenv("TERM") != "dumb"
}

type integrationTUIStyles struct {
	color    bool
	title    lipgloss.Style
	subtitle lipgloss.Style
	cursor   lipgloss.Style
	checked  lipgloss.Style
	detected lipgloss.Style
	help     lipgloss.Style
	warning  lipgloss.Style
}

func newIntegrationTUIStyles(color bool) integrationTUIStyles {
	styles := integrationTUIStyles{color: color}
	if !color {
		return styles
	}
	styles.title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D7FF"))
	styles.subtitle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
	styles.cursor = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D7FF"))
	styles.checked = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575"))
	styles.detected = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("#777777"))
	styles.help = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("#777777"))
	styles.warning = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFCC66"))
	return styles
}

func (styles integrationTUIStyles) render(style lipgloss.Style, value string) string {
	if !styles.color {
		return value
	}
	return style.Render(value)
}

func joinLines(lines []string) string {
	return strings.Join(lines, "\n")
}
