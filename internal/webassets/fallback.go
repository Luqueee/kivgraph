package webassets

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"
)

const fallbackHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Kivgraph web bundle unavailable</title></head>
<body>
<h1>Web bundle unavailable</h1>
<p>This Kivgraph binary was built without the versioned web bundle. Build the web assets and package the binary with the <code>webassets</code> build tag.</p>
</body>
</html>
`

func serveUnavailable(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Content-Length", strconv.Itoa(len(fallbackHTML)))
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusServiceUnavailable)
	if request.Method == http.MethodHead {
		return
	}
	if _, err := io.WriteString(writer, fallbackHTML); err != nil {
		slog.Default().Debug("write unavailable web bundle response", "error", err)
	}
}
