package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// writeFileAtomic replaces a unit file in one step.
//
// A supervisor reads the file on its own schedule -- launchd on bootstrap,
// systemd on daemon-reload -- so a truncate-then-write would give it a window in
// which the unit is half a file. The temporary lands in the same directory
// because rename is only atomic within a filesystem.
func writeFileAtomic(path string, contents []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".kivgraph-supervisor-*")
	if err != nil {
		return fmt.Errorf("supervisor: create temporary in %s: %w", directory, err)
	}
	name := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(name)
	}()
	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("supervisor: write %s: %w", name, err)
	}
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("supervisor: chmod %s: %w", name, err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("supervisor: sync %s: %w", name, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("supervisor: close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("supervisor: rename onto %s: %w", path, err)
	}
	return nil
}

// run executes a supervisor command and keeps its output in the error.
//
// The output is the whole diagnostic value: `launchctl bootstrap` and
// `systemctl --user enable` both fail with an exit status and an explanation,
// and dropping the explanation would leave a caller with "exit status 5".
func run(name string, arguments ...string) error {
	command := exec.Command(name, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}
