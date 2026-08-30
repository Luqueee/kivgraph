package main

import (
	"fmt"
	"io"
	"strings"
)

// keyValueRow is the small presentation seam shared by human-readable command
// reports. JSON and redirected output keep their existing line-oriented shape;
// an interactive terminal gets the same facts with a layout that is easier to
// scan.
type keyValueRow struct {
	Key        string
	Value      string
	ValueStyle string
}

func renderKeyValueTable(title string, rows []keyValueRow) string {
	var output strings.Builder
	if title != "" {
		fmt.Fprintln(&output, title)
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row.Key))
	}
	for _, row := range rows {
		fmt.Fprintf(&output, "  %*s: %s\n", width, row.Key, row.Value)
	}
	return output.String()
}

func writeKeyValueTable(writer io.Writer, title string, rows []keyValueRow) {
	if !integrationTUIIsInteractive(writer) {
		_, _ = io.WriteString(writer, renderKeyValueTable(title, rows))
		return
	}

	paint := styleFor(writer)
	fmt.Fprintf(writer, "%s%s%s\n", paint.bold, title, paint.reset)
	width := 0
	for _, row := range rows {
		width = max(width, len(row.Key))
	}
	for _, row := range rows {
		value := row.Value
		if row.ValueStyle != "" {
			value = row.ValueStyle + value + paint.reset
		}
		fmt.Fprintf(writer, "  %*s: %s\n", width, row.Key, value)
	}
}

func passFailStyle(passed bool, paint style) string {
	if passed {
		return paint.success
	}
	return paint.error
}
