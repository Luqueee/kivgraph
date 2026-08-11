// Package logging provides the process logger used by Ladygraph commands.
package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
)

// New returns a JSON logger writing one structured event per record to output.
// A nil output discards records, which keeps callers safe in tests and in
// optional integrations.
func New(output io.Writer) *slog.Logger {
	if output == nil {
		output = io.Discard
	}
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// NewCommandWriter adapts a command's stderr writes to structured records.
//
// A command writes progress and failures to the same io.Writer, and this
// adapter used to log every line as an error: a successful index reported
// each unit of work it finished at level ERROR, so the level said nothing
// about whether anything had gone wrong. Progress is INFO; a caller that
// knows a line is a failure says so through WriteLevel.
//
// The line is the record's message rather than an attribute of a fixed
// "command stderr" message: a log nobody can grep for the error text is a
// log that hides it.
func NewCommandWriter(logger *slog.Logger) *CommandWriter {
	return &CommandWriter{logger: logger}
}

// CommandWriter is the stderr of a one-shot command.
type CommandWriter struct {
	logger *slog.Logger
}

// Write records one progress line.
func (writer *CommandWriter) Write(data []byte) (int, error) {
	if writer.logger == nil {
		return 0, errors.New("logging: nil logger")
	}
	if message := strings.TrimSpace(string(data)); message != "" {
		writer.logger.Info(message)
	}
	return len(data), nil
}

// WriteLevel records one line at a level the caller chose.
func (writer *CommandWriter) WriteLevel(level slog.Level, message string) {
	if writer.logger == nil {
		return
	}
	if trimmed := strings.TrimSpace(message); trimmed != "" {
		writer.logger.Log(context.Background(), level, trimmed)
	}
}
