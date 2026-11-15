//go:build windows

package update

// archiveExtension is the container the release publishes for this platform.
// Windows gets a zip because that is what its own tools read: a reader who
// downloads the release opens it with Explorer or Expand-Archive, and neither
// of those reads a gzipped tarball. See ADR 0079.
const archiveExtension = ".zip"
