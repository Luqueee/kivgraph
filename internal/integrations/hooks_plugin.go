package integrations

import (
	"bytes"
	"encoding/json"
	"strings"
)

// pluginBody is the OpenCode shim with this installation's executable in it.
//
// The path is baked in rather than looked up on PATH because a plugin runs
// inside the editor's process, with whatever environment the editor was
// launched from -- a desktop launcher's PATH is not a shell's, and a shim that
// resolved `kivgraph` at call time would work from a terminal and silently
// stop working everywhere else.
func (manager Manager) pluginBody() []byte {
	return manager.pluginBodyFor(TargetOpenCode)
}

// pluginBodyFor is the client-native module with this installation's
// executable in it.
func (manager Manager) pluginBodyFor(target Target) []byte {
	template := embeddedOpenCodePlugin
	if target == TargetOhMyPi {
		template = embeddedOhMyPiExtension
	}
	// The placeholder stands where a JavaScript string goes, quotes included,
	// and the value is encoded rather than pasted. A Windows path is mostly
	// backslashes, and JavaScript does not refuse an escape it does not know:
	// it drops the backslash, so `\o` is `o` and the path still looks
	// plausible -- while `C:\bin\kivgraph.exe` becomes a path with a
	// backspace in it. A shim that names a corrupted binary fails at the
	// moment the editor calls it, with nothing to connect the failure to the
	// install that wrote it.
	//
	// JSON's string syntax is a subset of JavaScript's, so the encoder is the
	// right one and there is no second escaping to keep in step.
	encoded, err := json.Marshal(manager.executable)
	if err != nil {
		// A Go string is always encodable, so this cannot happen; quoting
		// without escaping would be worse than an obviously wrong path.
		encoded = []byte(`""`)
	}
	return []byte(strings.ReplaceAll(
		string(template), executablePlaceholder, string(encoded)))
}

// installPlugin writes a client-native code module.
func (manager Manager) installPlugin(document hookDocument, dryRun, force bool) (Plan, error) {
	body := manager.pluginBodyFor(document.target)
	data, exists, err := readDestination(document.path)
	if err != nil {
		return Plan{}, err
	}
	status := "absent"
	if exists {
		switch {
		case bytes.Equal(data, body):
			if !force {
				return manager.plan(ActionInstall, document, "managed",
					"pre-tool-use plugin already matches Kivgraph"), nil
			}
			status = "managed"
		case isKivgraphPlugin(data):
			// Ours, from another install prefix or an older release.
			// Replacing it is the whole point of running install again,
			// so it does not need forcing.
			status = statusSuperseded
		default:
			status = "incompatible"
			if !force {
				return Plan{}, incompatibleError(document.path)
			}
		}
	}
	plan := manager.plan(ActionInstall, document, status, "install the Kivgraph pre-tool-use plugin")
	plan.Changed, plan.DryRun = true, dryRun
	if dryRun {
		plan.Status = "would-install"
		return plan, nil
	}
	if err := writeDestination(document.path, body, exists, data); err != nil {
		return Plan{}, err
	}
	plan.Status = "installed"
	return plan, nil
}

// removePlugin deletes a client-native code module.
func (manager Manager) removePlugin(document hookDocument, dryRun, force bool) (Plan, error) {
	data, exists, err := readDestination(document.path)
	if err != nil {
		return Plan{}, err
	}
	if !exists {
		return manager.plan(ActionRemove, document, "absent",
			"no Kivgraph pre-tool-use plugin is installed"), nil
	}
	status := statusSuperseded
	switch {
	case bytes.Equal(data, manager.pluginBodyFor(document.target)):
		status = "managed"
	case !isKivgraphPlugin(data):
		if !force {
			return Plan{}, incompatibleError(document.path)
		}
		status = "incompatible"
	}
	plan := manager.plan(ActionRemove, document, status, "remove the Kivgraph pre-tool-use plugin")
	plan.Changed, plan.DryRun = true, dryRun
	if dryRun {
		plan.Status = "would-remove"
		return plan, nil
	}
	if err := removeDestination(document.path, data); err != nil {
		return Plan{}, err
	}
	plan.Status = "removed"
	return plan, nil
}

// statusPlugin inspects a client-native code module.
func (manager Manager) statusPlugin(document hookDocument) (Plan, error) {
	data, exists, err := readDestination(document.path)
	if err != nil {
		return Plan{}, err
	}
	status := "absent"
	if exists {
		switch {
		case bytes.Equal(data, manager.pluginBodyFor(document.target)):
			status = "managed"
		case isKivgraphPlugin(data):
			status = statusSuperseded
		default:
			status = "incompatible"
		}
	}
	return manager.plan(ActionStatus, document, status, pluginStatusDetail(status)), nil
}

// pluginMarker is what identifies a file as one Kivgraph wrote.
//
// It is the command the shim runs, which is the one line in it that cannot
// change without the file ceasing to be this shim. Matching the whole body
// instead would report every release's own plugin as a stranger's.
var pluginMarker = []byte(`["hook", "run"]`)

// isKivgraphPlugin reports whether a file is a Kivgraph module, whatever
// release wrote it and whatever path it names.
func isKivgraphPlugin(data []byte) bool {
	return bytes.Contains(data, pluginMarker)
}

// pluginStatusDetail explains a status to a reader.
func pluginStatusDetail(status string) string {
	switch status {
	case "managed":
		return "pre-tool-use plugin matches Kivgraph"
	case statusSuperseded:
		return "pre-tool-use plugin is Kivgraph's but names another binary: install replaces it"
	case "incompatible":
		return "a plugin exists at this path and Kivgraph did not write it"
	default:
		return "no Kivgraph pre-tool-use plugin is installed"
	}
}
