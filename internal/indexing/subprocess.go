package indexing

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ErrIndexProcess reports that the child index did not produce a graph.
var ErrIndexProcess = errors.New("index process failed")

// maximumChildDiagnostics bounds how much of the child's stderr is kept to
// explain a failure. A loader can print thousands of diagnostic lines, and an
// error a client cannot read is not a better error.
const maximumChildDiagnostics = 8 << 10

// childWaitDelay bounds how long a cancelled child may hold the pipes open
// after it has been killed.
const childWaitDelay = 10 * time.Second

// DetachedOptions configures one full index run in a child process.
type DetachedOptions struct {
	// Executable is the binary to run, and defaults to this one. The child
	// must be this build: it carries the same storage library, the same
	// resolver and the same schema, and a graph published by anything else is
	// not the graph this process knows how to serve.
	Executable string

	ConfigPath       string
	RepositoriesPath string
	ResolverVersion  string
	// WorkingDirectory is where the child resolves a relative repository path,
	// so it must be the directory the request was made from.
	WorkingDirectory string

	// Progress receives every step the child reports. A nil sink is not an
	// absence of progress: the child still reports, and the lines are dropped
	// here rather than costing the child a channel it cannot see.
	Progress func(ProjectProgress)

	// Log receives the child's own stderr as it is written. The child logs the
	// diagnostics a loader produced without failing the pass, and a diagnostic
	// nobody can read is a diagnostic nobody has; a server passes the stream it
	// already logs to. The tail is kept to explain a failure either way.
	Log io.Writer
}

// RunDetached indexes the registered repositories in a child process and
// returns what that pass concluded.
//
// A full pass holds the type universe of every Go module, the reply of every
// TypeScript worker and the SCIP index of every Cargo workspace at once. Its
// peak is measured in gigabytes, and a Go heap that has once grown to it keeps
// the arena: a server that indexed in its own process parks at that peak for as
// long as it runs, whether or not anybody queries it again. Running the pass in
// a child makes the peak die with the child, and the server pays only for the
// snapshot it then loads. See ADR 0042.
func RunDetached(ctx context.Context, options DetachedOptions) (FullDocument, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return FullDocument{}, err
	}
	executable := strings.TrimSpace(options.Executable)
	if executable == "" {
		resolved, err := os.Executable()
		if err != nil {
			return FullDocument{}, fmt.Errorf("%w: resolve this executable: %w", ErrIndexProcess, err)
		}
		executable = resolved
	}

	arguments := []string{"index", "--full", "--json"}
	for _, pair := range []struct{ flag, value string }{
		{flag: "--config", value: options.ConfigPath},
		{flag: "--repositories", value: options.RepositoriesPath},
		{flag: "--resolver-version", value: options.ResolverVersion},
	} {
		if strings.TrimSpace(pair.value) != "" {
			arguments = append(arguments, pair.flag, pair.value)
		}
	}

	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = options.WorkingDirectory
	command.WaitDelay = childWaitDelay
	// The child gets no stdin. A server's stdin is the MCP stream, and a child
	// that inherited it would read the client's next request and lose it.
	command.Stdin = nil
	stdout, err := command.StdoutPipe()
	if err != nil {
		return FullDocument{}, fmt.Errorf("%w: open child stdout: %w", ErrIndexProcess, err)
	}
	diagnostics := &tailBuffer{limit: maximumChildDiagnostics}
	command.Stderr = io.Writer(diagnostics)
	if options.Log != nil {
		command.Stderr = io.MultiWriter(diagnostics, options.Log)
	}

	if err := command.Start(); err != nil {
		return FullDocument{}, fmt.Errorf("%w: start %s: %w", ErrIndexProcess, executable, err)
	}

	document, decoded, decodeErr := readFullEvents(stdout, options.Progress)
	waitErr := command.Wait()

	switch {
	case waitErr != nil && decoded && document.Error != "":
		// The child said why before it exited; its own message is the useful
		// one, and the exit code adds nothing to it.
		return document, fmt.Errorf("%w: %s", ErrIndexProcess, document.Error)
	case waitErr != nil:
		return FullDocument{}, fmt.Errorf("%w: %w%s", ErrIndexProcess, waitErr, describeDiagnostics(diagnostics.String()))
	case decodeErr != nil:
		return FullDocument{}, fmt.Errorf("%w: read child report: %w", ErrIndexProcess, decodeErr)
	case !decoded:
		return FullDocument{}, fmt.Errorf("%w: the child published no report%s",
			ErrIndexProcess, describeDiagnostics(diagnostics.String()))
	case !document.Passed:
		detail := document.Error
		if detail == "" {
			detail = "the pass did not pass its gates"
		}
		return document, fmt.Errorf("%w: %s", ErrIndexProcess, detail)
	}
	return document, nil
}

// readFullEvents consumes the child's event stream, forwarding progress and
// keeping the one result it reports.
//
// A line that is not an event, or an event of an unknown kind, is skipped: the
// stream is a protocol between two builds of this program, and a reader that
// refused an unrecognised line would make adding an event kind a breaking
// change.
func readFullEvents(
	stdout io.Reader,
	progress func(ProjectProgress),
) (FullDocument, bool, error) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	document := FullDocument{}
	decoded := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var event FullEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		switch event.Event {
		case FullEventProgress:
			if progress != nil && event.Progress != nil {
				progress(*event.Progress)
			}
		case FullEventResult:
			if event.Result != nil {
				document = *event.Result
				decoded = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return document, decoded, err
	}
	return document, decoded, nil
}

func describeDiagnostics(tail string) string {
	trimmed := strings.TrimSpace(tail)
	if trimmed == "" {
		return ""
	}
	return ": " + trimmed
}

// tailBuffer keeps the last limit bytes written to it. A rebuild can print far
// more than an error should carry, and the end is the part that says why it
// stopped.
type tailBuffer struct {
	mu      sync.Mutex
	limit   int
	content []byte
}

func (buffer *tailBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	buffer.content = append(buffer.content, data...)
	if overflow := len(buffer.content) - buffer.limit; overflow > 0 {
		buffer.content = append(buffer.content[:0], buffer.content[overflow:]...)
	}
	return len(data), nil
}

func (buffer *tailBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(buffer.content)
}
