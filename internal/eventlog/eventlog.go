// Package eventlog is the durable record of what Kivgraph did: when it
// indexed and for how long, which tool a client called and whether it
// answered, and when a server came up or went down.
//
// It exists because none of that survived the process. The per-tool latency
// in internal/metrics is atomics in a map, minted fresh by every serve and
// discarded when it exits; the stage durations of a rebuild are printed to
// stderr and dropped. A reader that runs as its own process -- which is what
// `kivgraph logs` and `kivgraph tool-stats` are -- could observe neither.
//
// The store is one append-only JSON-lines file. That choice is what makes it
// safe for the several processes that write it at once: a record is a single
// write of well under a pipe buffer to a file opened O_APPEND, which POSIX
// keeps whole and unmixed. Nothing here holds a lock across processes, and
// nothing here reads before writing.
package eventlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind is the subsystem an event came from. It is what a reader filters on and
// what decides the badge a rendered line carries.
type Kind string

const (
	// KindServe is the lifecycle of a long-running process.
	KindServe Kind = "serve"
	// KindTool is one completed MCP tool call.
	KindTool Kind = "tool"
	// KindIndex is an indexing pass or one of its stages.
	KindIndex Kind = "index"
)

// Kinds is the whole vocabulary, in the order a reader meets it. A caller that
// validates or completes a --kind reads it here rather than restating the three
// constants, which is how the CLI and this package stay in step.
func Kinds() []string {
	return []string{string(KindServe), string(KindTool), string(KindIndex)}
}

// Level is the severity of an event, kept as a string so the file stays
// readable without this package.
type Level string

const (
	// LevelInfo reports progress.
	LevelInfo Level = "info"
	// LevelWarn reports a degraded result that still answered.
	LevelWarn Level = "warn"
	// LevelError reports a failure.
	LevelError Level = "error"
)

// Status is the outcome of a completed unit of work.
const (
	// StatusOK marks work that finished as asked.
	StatusOK = "ok"
	// StatusError marks work that failed.
	StatusError = "error"
)

// Event is one record. Its fields are a closed set rather than a free-form
// attribute bag: every producer is in this repository, and a fixed shape is
// what lets a reader render columns in a stable order and aggregate without
// guessing at types.
type Event struct {
	Time    time.Time `json:"ts"`
	Level   Level     `json:"level"`
	Kind    Kind      `json:"kind"`
	Message string    `json:"msg"`

	// PID names the process that emitted the event, so a machine running
	// three servers can be told apart from one running a single server.
	PID int `json:"pid,omitempty"`

	// Tool, Status and DurationMS describe a completed call or pass. A
	// duration is a pointer because zero milliseconds is a real answer and
	// "not timed" is a different one.
	Tool       string   `json:"tool,omitempty"`
	Status     string   `json:"status,omitempty"`
	DurationMS *float64 `json:"duration_ms,omitempty"`

	Stage      string `json:"stage,omitempty"`
	Repository string `json:"repository,omitempty"`
	Generation string `json:"generation,omitempty"`

	// Results is what a tool returned, Symbols what a pass produced.
	Results *int64 `json:"results,omitempty"`
	Symbols *int64 `json:"symbols,omitempty"`

	// Error carries the rendered failure. Tool failures arrive as
	// "CODE: message", so the stable code survives without a second field.
	Error string `json:"error,omitempty"`
}

// Duration answers the observed duration and whether one was recorded.
func (event Event) Duration() (time.Duration, bool) {
	if event.DurationMS == nil {
		return 0, false
	}
	return time.Duration(*event.DurationMS * float64(time.Millisecond)), true
}

// Failed reports whether the event describes work that did not answer.
//
// A refusal is one of these: it returns no rows, so it takes the error path and
// is recorded here as one. Telling the two apart is the reader's job and needs
// the tool vocabulary, which this package does not have -- see ErrorCode and
// Summarize.
func (event Event) Failed() bool {
	return event.Status == StatusError || event.Level == LevelError
}

// ErrorCode is the stable code a failed tool call carries.
//
// It is read back out of Error rather than kept in a field of its own, which
// is the shape the writer chose on purpose: a tool failure is rendered as
// "CODE: message", so the classification already survives in the file. That
// makes it readable from logs written before anything knew to classify them,
// which is the only reason the five-day measurement in LUQUE-2235 can be
// checked against the code that answers it.
func (event Event) ErrorCode() string {
	// A code with no colon after it is the other shape the renderer produces:
	// ToolError.Error() answers the code alone when the message is empty. A
	// parser that required the separator would drop exactly those, and drop
	// them into the failure column, which is the thing this exists to stop.
	code := event.Error
	if separator := strings.Index(code, ":"); separator >= 0 {
		code = code[:separator]
	}
	if code == "" {
		return ""
	}
	for _, letter := range code {
		if (letter < 'A' || letter > 'Z') && letter != '_' {
			return ""
		}
	}
	return code
}

// WithDuration returns the event carrying elapsed, in milliseconds.
func (event Event) WithDuration(elapsed time.Duration) Event {
	milliseconds := float64(elapsed) / float64(time.Millisecond)
	event.DurationMS = &milliseconds
	return event
}

// WithResults returns the event carrying a returned-row count.
func (event Event) WithResults(count int) Event {
	value := int64(count)
	event.Results = &value
	return event
}

// WithSymbols returns the event carrying a produced-symbol count.
func (event Event) WithSymbols(count int64) Event {
	event.Symbols = &count
	return event
}

// DefaultMaxBytes is the size at which the live file is rotated. One rotation
// is kept, so the store costs at most twice this on disk.
const DefaultMaxBytes = 8 << 20

// rotatedSuffix names the single retained rotation.
const rotatedSuffix = ".1"

// Writer appends events to the store. A nil *Writer is a working sink that
// discards, so a caller that could not open the file needs no branch.
type Writer struct {
	mutex    sync.Mutex
	path     string
	file     *os.File
	size     int64
	maxBytes int64
	pid      int
}

// Open answers a Writer appending to path, creating the directory when it is
// missing.
func Open(path string) (*Writer, error) {
	return OpenWithLimit(path, DefaultMaxBytes)
}

// OpenWithLimit is Open with an explicit rotation threshold.
func OpenWithLimit(path string, maxBytes int64) (*Writer, error) {
	if path == "" {
		return nil, errors.New("eventlog: the path must not be empty")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("eventlog: the rotation threshold must be positive, got %d", maxBytes)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("eventlog: create the directory of %s: %w", path, err)
	}
	file, size, err := openAppend(path)
	if err != nil {
		return nil, err
	}
	return &Writer{path: path, file: file, size: size, maxBytes: maxBytes, pid: os.Getpid()}, nil
}

func openAppend(path string) (*os.File, int64, error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, 0, fmt.Errorf("eventlog: open %s: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, fmt.Errorf("eventlog: stat %s: %w", path, err)
	}
	return file, info.Size(), nil
}

// Path answers the file the Writer appends to, or the empty string when it
// discards.
func (writer *Writer) Path() string {
	if writer == nil {
		return ""
	}
	return writer.path
}

// Append writes one event. It never returns an error and never blocks on
// anything but its own mutex: a log that cannot be written must not be able to
// fail the work it describes. A record that does not fit is dropped, not
// truncated, so every line in the file stays parseable.
func (writer *Writer) Append(event Event) {
	if writer == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	if event.Level == "" {
		event.Level = LevelInfo
	}
	if event.PID == 0 {
		event.PID = writer.pid
	}
	line, err := json.Marshal(event)
	if err != nil {
		return
	}
	line = append(line, '\n')

	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if writer.file == nil {
		return
	}
	if writer.size+int64(len(line)) > writer.maxBytes {
		writer.rotateLocked()
	}
	written, err := writer.file.Write(line)
	if err != nil {
		// The destination is gone or full. Stop writing rather than emit a
		// half line that would break every later read.
		writer.file.Close()
		writer.file = nil
		return
	}
	writer.size += int64(written)
}

// rotateLocked moves the live file aside and starts a new one. A second
// process may still hold the renamed file and keep appending to it; that is
// why Read always reads the rotation before the live file instead of assuming
// the rotation is closed.
func (writer *Writer) rotateLocked() {
	writer.file.Close()
	writer.file = nil
	if err := os.Rename(writer.path, writer.path+rotatedSuffix); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Rotation failed, so the file would grow without bound. Truncating
		// loses less than giving up on the record entirely.
		if err := os.Remove(writer.path); err != nil {
			return
		}
	}
	file, size, err := openAppend(writer.path)
	if err != nil {
		return
	}
	writer.file = file
	writer.size = size
}

// Close releases the file.
func (writer *Writer) Close() error {
	if writer == nil {
		return nil
	}
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	if writer.file == nil {
		return nil
	}
	file := writer.file
	writer.file = nil
	return file.Close()
}

// ReadOptions narrows a read. The zero value reads everything the store holds.
type ReadOptions struct {
	// Since drops events older than this instant.
	Since time.Time
	// Kinds keeps only these kinds. Empty keeps every kind.
	Kinds []Kind
	// Tool keeps only events naming this tool. Empty keeps every tool.
	Tool string
	// FailuresOnly keeps only events that did not answer.
	FailuresOnly bool
	// Limit keeps at most this many of the newest matching events. Zero or
	// less keeps all of them.
	Limit int
}

func (options ReadOptions) keep(event Event) bool {
	if !options.Since.IsZero() && event.Time.Before(options.Since) {
		return false
	}
	if options.Tool != "" && event.Tool != options.Tool {
		return false
	}
	if options.FailuresOnly && !event.Failed() {
		return false
	}
	if len(options.Kinds) == 0 {
		return true
	}
	for _, kind := range options.Kinds {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

// Read answers the matching events in chronological order. A store that does
// not exist yet is not an error: it is a machine that has not run anything.
//
// A line that does not parse is skipped rather than failing the read. The file
// is written by several processes at once and may hold a record from a version
// that knew fields this one does not; refusing to show the rest would turn a
// harmless unknown into an outage of the only view a reader has.
func Read(path string, options ReadOptions) ([]Event, error) {
	if path == "" {
		return nil, errors.New("eventlog: the path must not be empty")
	}
	var events []Event
	for _, candidate := range []string{path + rotatedSuffix, path} {
		read, err := readFile(candidate, options)
		if err != nil {
			return nil, err
		}
		events = append(events, read...)
	}
	sort.SliceStable(events, func(first, second int) bool {
		return events[first].Time.Before(events[second].Time)
	})
	if options.Limit > 0 && len(events) > options.Limit {
		events = events[len(events)-options.Limit:]
	}
	return events, nil
}

func readFile(path string, options ReadOptions) ([]Event, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("eventlog: open %s: %w", path, err)
	}
	defer file.Close()

	var events []Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if !options.keep(event) {
			continue
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("eventlog: read %s: %w", path, err)
	}
	return events, nil
}
