package integrations

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// hookState is one agent's hook file, opened.
type hookState struct {
	root   map[string]json.RawMessage
	events map[string]json.RawMessage
	// entries is the PreToolUse array, in the order the file holds it.
	entries []json.RawMessage
	// ours is the index of Kivgraph's entry, or -1.
	ours   int
	data   []byte
	exists bool
	status string
}

// hookEntryValue is one registration in a PreToolUse array.
type hookEntryValue struct {
	Matcher string             `json:"matcher"`
	Hooks   []hookHandlerValue `json:"hooks"`
}

// hookHandlerValue is one command an entry runs.
type hookHandlerValue struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// expectedHookEntry is the registration Kivgraph owns.
func (manager Manager) expectedHookEntry(document hookDocument) hookEntryValue {
	return hookEntryValue{
		Matcher: document.matcher,
		Hooks: []hookHandlerValue{{
			Type: "command", Command: manager.hookCommand(), Timeout: hookTimeoutSeconds,
		}},
	}
}

// readHooks opens a hook file and finds Kivgraph's entry in it.
//
// A hooks file differs from an MCP file in the way that decides this whole
// implementation: `mcpServers` is a map and our key is either ours or someone
// else's, but `PreToolUse` is an array that holds every gate the user installed.
// Ours is found by the command it runs, never by position, and everything else
// in the array is none of our business -- which is why nothing here can report
// "incompatible" and why an install never needs to overwrite.
func (manager Manager) readHooks(document hookDocument) (hookState, error) {
	data, exists, err := readDestination(document.path)
	if err != nil {
		return hookState{}, err
	}
	state := hookState{root: map[string]json.RawMessage{}, events: map[string]json.RawMessage{},
		ours: -1, data: data, exists: exists, status: "absent"}
	if exists && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &state.root); err != nil || state.root == nil {
			return hookState{}, fmt.Errorf("parse %s: top-level JSON value must be an object", document.path)
		}
	}
	if raw, ok := state.root["hooks"]; ok {
		if err := json.Unmarshal(raw, &state.events); err != nil || state.events == nil {
			return hookState{}, fmt.Errorf("parse %s: hooks must be an object", document.path)
		}
	}
	if raw, ok := state.events[preToolUse]; ok {
		if err := json.Unmarshal(raw, &state.entries); err != nil {
			return hookState{}, fmt.Errorf("parse %s: hooks.%s must be an array", document.path, preToolUse)
		}
	}
	expected := manager.expectedHookEntry(document)
	for index, raw := range state.entries {
		var entry hookEntryValue
		if err := json.Unmarshal(raw, &entry); err != nil || !manager.ownsHookEntry(entry) {
			continue
		}
		state.ours = index
		state.status = statusSuperseded
		if hookEntriesEqual(entry, expected) {
			state.status = "managed"
		}
		break
	}
	return state, nil
}

// ownsHookEntry reports whether an entry is one Kivgraph wrote.
//
// The test is the command, not the matcher: a user who edited the matcher still
// owns a Kivgraph gate, and one whose binary has since moved owns a stale one.
// Both are ours to replace, and matching the whole entry instead would leave a
// second copy behind on every upgrade that changed a default.
//
// Recognising it by the base name alone was wrong and a sandbox install caught
// it: the binary is only called `kivgraph` when it was installed under that
// exact name, and a build called `kivgraph-hook` registered a gate that status
// then reported as absent and remove refused to touch. So the current
// installation's own command is recognised whatever it is called, and any other
// path is ours only if it still looks like this project's binary.
func (manager Manager) ownsHookEntry(entry hookEntryValue) bool {
	expected := manager.hookCommand()
	for _, handler := range entry.Hooks {
		command := strings.TrimSpace(handler.Command)
		if command == expected {
			return true
		}
		if !strings.HasSuffix(command, hookOperation) {
			continue
		}
		program := strings.Trim(strings.TrimSpace(strings.TrimSuffix(command, hookOperation)), `"`)
		if strings.Contains(strings.ToLower(filepath.Base(program)), "kivgraph") {
			return true
		}
	}
	return false
}

// hookEntriesEqual compares two registrations by value.
func hookEntriesEqual(left, right hookEntryValue) bool {
	if left.Matcher != right.Matcher || len(left.Hooks) != len(right.Hooks) {
		return false
	}
	for index := range left.Hooks {
		if left.Hooks[index] != right.Hooks[index] {
			return false
		}
	}
	return true
}

// installHookEntry writes Kivgraph's registration into a hook file.
//
// An array holds every gate the user installed, so there is no foreign value
// under our key to overwrite: we replace our own entry or append a new one,
// and every other entry in the file survives untouched. Force only refreshes
// a matching Kivgraph entry.
func (manager Manager) installHookEntry(document hookDocument, dryRun, force bool) (Plan, error) {
	state, err := manager.readHooks(document)
	if err != nil {
		return Plan{}, err
	}
	if state.status == "managed" && !force {
		return manager.plan(ActionInstall, document, state.status,
			"pre-tool-use gate already matches Kivgraph"), nil
	}
	encoded, err := json.Marshal(manager.expectedHookEntry(document))
	if err != nil {
		return Plan{}, fmt.Errorf("encode hook entry: %w", err)
	}
	if state.ours >= 0 {
		state.entries[state.ours] = encoded
	} else {
		state.entries = append(state.entries, encoded)
	}
	data, err := state.encode(document)
	if err != nil {
		return Plan{}, err
	}
	plan := manager.plan(ActionInstall, document, state.status, "register the Kivgraph pre-tool-use gate")
	plan.Changed, plan.DryRun = true, dryRun
	if dryRun {
		plan.Status = "would-install"
		return plan, nil
	}
	if err := writeDestination(document.path, data, state.exists, state.data); err != nil {
		return Plan{}, err
	}
	plan.Status = "installed"
	return plan, nil
}

// removeHookEntry deletes Kivgraph's registration and nothing else.
func (manager Manager) removeHookEntry(document hookDocument, dryRun bool) (Plan, error) {
	state, err := manager.readHooks(document)
	if err != nil {
		return Plan{}, err
	}
	if state.ours < 0 {
		return manager.plan(ActionRemove, document, "absent",
			"no Kivgraph pre-tool-use gate is registered"), nil
	}
	state.entries = append(state.entries[:state.ours], state.entries[state.ours+1:]...)
	data, err := state.encode(document)
	if err != nil {
		return Plan{}, err
	}
	plan := manager.plan(ActionRemove, document, state.status, "remove the Kivgraph pre-tool-use gate")
	plan.Changed, plan.DryRun = true, dryRun
	if dryRun {
		plan.Status = "would-remove"
		return plan, nil
	}
	if err := writeDestination(document.path, data, state.exists, state.data); err != nil {
		return Plan{}, err
	}
	plan.Status = "removed"
	return plan, nil
}

// encode folds the edited array back into the document it came from.
//
// An emptied array is deleted rather than written as `[]`, and an emptied
// `hooks` object with it: a remove should leave a file that looks like one
// nobody ever installed into, not one carrying the shape of what was taken out.
func (state hookState) encode(document hookDocument) ([]byte, error) {
	if len(state.entries) == 0 {
		delete(state.events, preToolUse)
	} else {
		encoded, err := json.Marshal(state.entries)
		if err != nil {
			return nil, fmt.Errorf("encode hooks.%s: %w", preToolUse, err)
		}
		state.events[preToolUse] = encoded
	}
	if len(state.events) == 0 {
		delete(state.root, "hooks")
	} else {
		encoded, err := json.Marshal(state.events)
		if err != nil {
			return nil, fmt.Errorf("encode hooks: %w", err)
		}
		state.root["hooks"] = encoded
	}
	data, err := json.MarshalIndent(state.root, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", document.path, err)
	}
	return append(data, '\n'), nil
}

// hookStatusDetail explains a status to a reader.
func hookStatusDetail(status string) string {
	switch status {
	case "managed":
		return "pre-tool-use gate matches Kivgraph"
	case statusSuperseded:
		return "pre-tool-use gate is Kivgraph's but names another binary: install replaces it"
	default:
		return "no Kivgraph pre-tool-use gate is registered"
	}
}
