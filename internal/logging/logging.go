// Package logging provides the process logger used by Ladygraph commands.
package logging

import (
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

// NewErrorWriter adapts legacy command stderr writes to structured error
// records. It lets command handlers keep their injected io.Writer contracts
// while the process entry point guarantees JSON on stderr.
func NewErrorWriter(logger *slog.Logger) io.Writer {
	return errorWriter{logger: logger}
}

type errorWriter struct {
	logger *slog.Logger
}

func (writer errorWriter) Write(data []byte) (int, error) {
	if writer.logger == nil {
		return 0, errors.New("logging: nil logger")
	}
	message := strings.TrimSpace(string(data))
	if message != "" {
		writer.logger.Error("command stderr", "message", message)
	}
	return len(data), nil
}
