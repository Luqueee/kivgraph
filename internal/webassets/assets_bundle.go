//go:build webassets

package webassets

import (
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// New returns a handler for the versioned web bundle copied next to a
// distribution binary. The source-tree candidate is useful for local tagged
// builds; a missing bundle deliberately falls back to a visible 503 response.
func New() http.Handler {
	roots := assetRoots()
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serveBundle(writer, request, roots)
	})
}

func assetRoots() []string {
	roots := make([]string, 0, 5)
	if executable, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executable)
		roots = append(roots,
			filepath.Join(executableDir, "..", "web"),
			filepath.Join(executableDir, "web"),
		)
	}
	if workingDirectory, err := os.Getwd(); err == nil {
		for directory := workingDirectory; ; directory = filepath.Dir(directory) {
			roots = append(roots, filepath.Join(directory, "web", "dist"))
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
		}
	}
	return roots
}

// contentSecurityPolicy lets the viewer load from its own origin and nowhere
// else.
//
// The viewer is local and read-only, and for a while it was neither private
// nor able to work offline: a dependency fetched its fonts from a CDN on every
// load, and nothing said so. With this the browser refuses such a request and
// names it in the console. `blob:` is the text worker the canvas starts, and
// the inline styles are the ones React writes on elements.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"worker-src 'self' blob:; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self' blob: data:; " +
	"object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

func serveBundle(writer http.ResponseWriter, request *http.Request, roots []string) {
	writer.Header().Set("Content-Security-Policy", contentSecurityPolicy)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	root := findAssetRoot(roots)
	if root == "" {
		serveUnavailable(writer, request)
		return
	}

	target, relative, ok := assetPath(root, request.URL.Path)
	if !ok {
		http.NotFound(writer, request)
		return
	}

	file, err := os.Open(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(writer, request)
			return
		}
		http.Error(writer, "web asset unavailable", http.StatusInternalServerError)
		return
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			slog.Default().Debug("close web asset", "path", target, "error", closeErr)
		}
	}()

	info, err := file.Stat()
	if err != nil {
		http.Error(writer, "web asset unavailable", http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.NotFound(writer, request)
		return
	}
	if contentType := mime.TypeByExtension(filepath.Ext(target)); contentType != "" {
		writer.Header().Set("Content-Type", contentType)
	}
	if strings.HasPrefix(relative, "assets/") {
		writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		writer.Header().Set("Cache-Control", "no-store")
	}
	http.ServeContent(writer, request, filepath.Base(target), info.ModTime(), file)
}

func findAssetRoot(roots []string) string {
	for _, root := range roots {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolvedRoot)
		if err != nil || !info.IsDir() {
			continue
		}
		index, err := os.Stat(filepath.Join(resolvedRoot, "index.html"))
		if err != nil || index.IsDir() {
			continue
		}
		return resolvedRoot
	}
	return ""
}

func assetPath(root, requestPath string) (target, relative string, ok bool) {
	relative = strings.TrimPrefix(requestPath, "/")
	if relative == "" {
		relative = "index.html"
	}
	if !fs.ValidPath(relative) {
		return "", "", false
	}

	candidate := filepath.Join(root, filepath.FromSlash(relative))
	resolvedTarget, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", "", false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", false
	}
	relativeResolved, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || filepath.IsAbs(relativeResolved) || relativeResolved == ".." || strings.HasPrefix(relativeResolved, ".."+string(filepath.Separator)) {
		return "", "", false
	}
	return resolvedTarget, relative, true
}
