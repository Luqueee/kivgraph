package main

import (
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/integrations"
	tea "github.com/charmbracelet/bubbletea"
)

func TestIntegrationSelectionModelControls(t *testing.T) {
	detections := []integrations.TargetDetection{
		{Target: integrations.TargetClaudeCode, Detected: true},
		{Target: integrations.TargetCodex},
		{Target: integrations.TargetOpenCode},
	}
	model := newIntegrationSelectionModel(detections, []int{0}, integrations.ScopeUser, false)
	if got := model.selectedIndices(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("default selection = %v, want [0]", got)
	}

	update := func(message tea.Msg) tea.Cmd {
		next, command := model.Update(message)
		var ok bool
		model, ok = next.(*integrationSelectionModel)
		if !ok {
			t.Fatalf("updated model type = %T", next)
		}
		return command
	}

	if command := update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}); command != nil {
		t.Fatalf("none command = %v, want nil", command)
	}
	if model.selectedCount() != 0 {
		t.Fatalf("selection after none = %d, want 0", model.selectedCount())
	}
	if command := update(tea.KeyMsg{Type: tea.KeyEnter}); command != nil {
		t.Fatalf("empty enter command = %v, want nil", command)
	}
	if model.confirmed || model.message == "" {
		t.Fatalf("empty enter state = confirmed %v message %q", model.confirmed, model.message)
	}

	update(tea.KeyMsg{Type: tea.KeySpace})
	update(tea.KeyMsg{Type: tea.KeyDown})
	update(tea.KeyMsg{Type: tea.KeyDown})
	update(tea.KeyMsg{Type: tea.KeySpace})
	if got := model.selectedIndices(); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("selected indices = %v, want [0 2]", got)
	}
	if command := update(tea.KeyMsg{Type: tea.KeyEnter}); command == nil {
		t.Fatal("confirm command = nil, want tea quit command")
	}
	if !model.confirmed {
		t.Fatal("model is not confirmed")
	}

	view := model.View()
	for _, expected := range []string{"Coding agents for user scope", "Claude Code", "space toggle", "enter confirm"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("view %q does not contain %q", view, expected)
		}
	}
	if strings.Contains(view, "\x1b[") {
		t.Fatalf("plain TUI view contains ANSI escapes: %q", view)
	}
}
