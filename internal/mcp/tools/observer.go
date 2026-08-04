package tools

import "time"

// Observer receives the elapsed time spent in an MCP tool handler.
type Observer func(toolName string, elapsed time.Duration)
