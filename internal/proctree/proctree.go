// Package proctree runs external tools that start processes of their own, and
// makes sure none of them outlives the reason it was started.
//
// An indexer is not one process. `scip-dotnet` runs `dotnet restore`, a Roslyn
// build host and MSBuild worker nodes; `scip-java` runs Maven or Gradle and
// their JVMs. exec.CommandContext knows the first of them only: a timeout
// killed the launcher and left the rest, and a kivgraph that was itself killed
// left all of it.
package proctree

import (
	"bytes"
	"os/exec"
	"time"
)

// waitDelay bounds how long Wait lingers on output pipes after the command was
// cancelled. A process that escaped the group and kept a pipe open would
// otherwise hold the caller for as long as it lived.
const waitDelay = 5 * time.Second

// CombinedOutput is exec.Cmd.CombinedOutput for a tool that starts a tree:
// cancelling the context kills the tree, this process dying kills the tree, and
// whatever the tool left running when it returned is killed too -- build
// daemons and worker nodes that would idle for minutes holding memory for an
// index that is already written.
//
// The command must come from exec.CommandContext.
func CombinedOutput(cmd *exec.Cmd) ([]byte, error) {
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	Contain(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	release := Guard(cmd)
	err := cmd.Wait()
	release()
	_ = killTree(cmd)
	return output.Bytes(), err
}
