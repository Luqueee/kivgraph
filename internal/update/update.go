// Package update replaces an installed bundle with a newer published release,
// and reports that one exists without installing it.
package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/Luqueee/kivgraph/internal/executable"
)

const (
	Repository     = "Luqueee/kivgraph"
	DefaultAPIBase = "https://api.github.com"
	checksumsName  = "SHA256SUMS"
	// bundleDirName is both the installed bundle directory and the single root
	// entry of the release archive. runtime.GOOS and runtime.GOARCH are
	// compile-time constants, so these names resolve to the platform this
	// binary was built for.
	bundleDirName = "kivgraph-" + runtime.GOOS + "-" + runtime.GOARCH
	// archiveExtension is per platform: Windows publishes a zip, because a
	// reader there opens a release with Explorer or Expand-Archive and
	// neither reads a gzipped tarball.
	archiveName      = bundleDirName + archiveExtension
	maxDownloadBytes = 512 << 20
	maxBundleBytes   = 512 << 20
	maxArchiveFiles  = 10000
)

// releaseTargets lists the platforms with a published distribution bundle. A
// platform outside this set has no asset to download, so Run refuses it before
// touching the network.
var releaseTargets = []string{"linux/amd64", "darwin/arm64", "windows/amd64"}

// checkReleaseTarget reports whether goos/goarch has a published bundle.
func checkReleaseTarget(goos, goarch string) error {
	target := goos + "/" + goarch
	for _, supported := range releaseTargets {
		if supported == target {
			return nil
		}
	}
	return fmt.Errorf("updates are only available for %s, got %s", strings.Join(releaseTargets, ", "), target)
}

// Options controls a release lookup and, unless CheckOnly is true, an atomic
// replacement of the currently installed bundle.
type Options struct {
	Client         *http.Client
	APIBaseURL     string
	Token          string
	CurrentVersion string
	ExecutablePath string
	CheckOnly      bool
	// Channel selects the release stream. An empty value follows the current
	// version: prerelease binaries stay on the development stream, while a
	// stable binary follows stable releases.
	Channel string
}

// Result describes the release selected by the update check.
type Result struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	Updated         bool
	Channel         string
}

const (
	ChannelStable      = "stable"
	ChannelDevelopment = "dev"
)

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Prerelease bool          `json:"prerelease"`
	Draft      bool          `json:"draft"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type bundleManifest struct {
	Product string `json:"product"`
	Release string `json:"release"`
	Target  struct {
		OS   string `json:"os"`
		Arch string `json:"arch"`
	} `json:"target"`
	Source struct {
		Dirty bool `json:"dirty"`
	} `json:"source"`
}

// Run checks the latest GitHub release and optionally installs it. A release
// is installed only after the outer checksum, inner bundle checksums, manifest
// and executable version all agree.
func Run(ctx context.Context, options Options) (Result, error) {
	if ctx == nil {
		return Result{}, errors.New("update context must not be nil")
	}
	if err := checkReleaseTarget(runtime.GOOS, runtime.GOARCH); err != nil {
		return Result{}, err
	}

	current := strings.TrimSpace(options.CurrentVersion)
	if current == "" {
		return Result{}, errors.New("current Kivgraph version must not be empty")
	}
	currentSemver := semanticVersion(current)
	if !semver.IsValid(currentSemver) {
		return Result{}, fmt.Errorf("current Kivgraph version %q is not valid semver", current)
	}
	channel, err := resolveChannel(current, options.Channel)
	if err != nil {
		return Result{}, err
	}

	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Minute}
	}
	apiBaseURL := strings.TrimRight(options.APIBaseURL, "/")
	if apiBaseURL == "" {
		apiBaseURL = DefaultAPIBase
	}
	release, err := latestRelease(ctx, client, apiBaseURL, options.Token, current, channel)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		CurrentVersion: current,
		LatestVersion:  strings.TrimPrefix(release.TagName, "v"),
		Channel:        channel,
	}
	result.UpdateAvailable = semver.Compare(currentSemver, release.TagName) < 0
	if !result.UpdateAvailable || options.CheckOnly {
		return result, nil
	}

	executablePath, err := executablePath(options.ExecutablePath)
	if err != nil {
		return result, err
	}
	bundleRoot, err := currentBundleRoot(executablePath)
	if err != nil {
		return result, err
	}
	if err := installRelease(ctx, client, options.Token, release, result.LatestVersion, bundleRoot); err != nil {
		return result, err
	}
	result.Updated = true
	return result, nil
}

func latestRelease(ctx context.Context, client *http.Client, apiBaseURL, token, current, channel string) (githubRelease, error) {
	if channel == ChannelDevelopment {
		return latestDevelopmentRelease(ctx, client, apiBaseURL, token, current)
	}
	url := strings.TrimRight(apiBaseURL, "/") + "/repos/" + Repository + "/releases/latest"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return githubRelease{}, fmt.Errorf("create latest release request: %w", err)
	}
	setHeaders(request, token, current)
	response, err := client.Do(request)
	if err != nil {
		return githubRelease{}, fmt.Errorf("query latest release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			return githubRelease{}, fmt.Errorf("query latest release: HTTP %s: read response: %w", response.Status, readErr)
		}
		return githubRelease{}, fmt.Errorf("query latest release: HTTP %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var release githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decode latest release: %w", err)
	}
	if !strings.HasPrefix(release.TagName, "v") || !semver.IsValid(release.TagName) {
		return githubRelease{}, fmt.Errorf("latest release tag %q is not valid semver", release.TagName)
	}
	if release.Prerelease {
		return githubRelease{}, fmt.Errorf("latest stable release %q is marked prerelease", release.TagName)
	}
	if _, err := releaseAsset(release, archiveName); err != nil {
		return githubRelease{}, err
	}
	if _, err := releaseAsset(release, checksumsName); err != nil {
		return githubRelease{}, err
	}
	return release, nil
}

func latestDevelopmentRelease(ctx context.Context, client *http.Client, apiBaseURL, token, current string) (githubRelease, error) {
	url := strings.TrimRight(apiBaseURL, "/") + "/repos/" + Repository + "/releases?per_page=100"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return githubRelease{}, fmt.Errorf("create development release request: %w", err)
	}
	setHeaders(request, token, current)
	response, err := client.Do(request)
	if err != nil {
		return githubRelease{}, fmt.Errorf("query development releases: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr != nil {
			return githubRelease{}, fmt.Errorf("query development releases: HTTP %s: read response: %w", response.Status, readErr)
		}
		return githubRelease{}, fmt.Errorf("query development releases: HTTP %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var releases []githubRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&releases); err != nil {
		return githubRelease{}, fmt.Errorf("decode development releases: %w", err)
	}
	var selected githubRelease
	for _, release := range releases {
		if release.Draft || !release.Prerelease || !strings.HasPrefix(release.TagName, "v") || !semver.IsValid(release.TagName) {
			continue
		}
		if selected.TagName == "" || semver.Compare(selected.TagName, release.TagName) < 0 {
			selected = release
		}
	}
	if selected.TagName == "" {
		return githubRelease{}, errors.New("no published development release is available")
	}
	if _, err := releaseAsset(selected, archiveName); err != nil {
		return githubRelease{}, err
	}
	if _, err := releaseAsset(selected, checksumsName); err != nil {
		return githubRelease{}, err
	}
	return selected, nil
}

func resolveChannel(current, requested string) (string, error) {
	if requested == "" {
		if strings.Contains(strings.TrimPrefix(current, "v"), "-") {
			return ChannelDevelopment, nil
		}
		return ChannelStable, nil
	}
	if requested != ChannelStable && requested != ChannelDevelopment {
		return "", fmt.Errorf("update channel %q is invalid (want %s or %s)", requested, ChannelStable, ChannelDevelopment)
	}
	return requested, nil
}

func installRelease(ctx context.Context, client *http.Client, token string, release githubRelease, version, bundleRoot string) error {
	archive, err := releaseAsset(release, archiveName)
	if err != nil {
		return err
	}
	checksums, err := releaseAsset(release, checksumsName)
	if err != nil {
		return err
	}

	downloadDir, err := os.MkdirTemp("", "kivgraph-update-download-")
	if err != nil {
		return fmt.Errorf("create update download directory: %w", err)
	}
	defer os.RemoveAll(downloadDir)
	archivePath := filepath.Join(downloadDir, archiveName)
	checksumsPath := filepath.Join(downloadDir, checksumsName)
	if err := download(ctx, client, token, archive.BrowserDownloadURL, archivePath); err != nil {
		return fmt.Errorf("download %s: %w", archiveName, err)
	}
	if err := download(ctx, client, token, checksums.BrowserDownloadURL, checksumsPath); err != nil {
		return fmt.Errorf("download %s: %w", checksumsName, err)
	}
	if err := verifyChecksumFile(archivePath, checksumsPath, archiveName); err != nil {
		return fmt.Errorf("verify release checksum: %w", err)
	}

	parent := filepath.Dir(bundleRoot)
	stagingDir, err := os.MkdirTemp(parent, ".kivgraph-update-")
	if err != nil {
		return fmt.Errorf("create update staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDir)
	if err := extractArchive(archivePath, stagingDir); err != nil {
		return fmt.Errorf("extract release archive: %w", err)
	}
	stagedBundle := filepath.Join(stagingDir, bundleDirName)
	if err := validateBundle(stagedBundle, version); err != nil {
		return fmt.Errorf("validate release bundle: %w", err)
	}
	if err := replaceBundle(bundleRoot, stagedBundle); err != nil {
		return fmt.Errorf("replace installed bundle: %w", err)
	}
	return nil
}

func releaseAsset(release githubRelease, name string) (githubAsset, error) {
	for _, asset := range release.Assets {
		if asset.Name == name {
			if asset.BrowserDownloadURL == "" {
				return githubAsset{}, fmt.Errorf("release asset %q has no download URL", name)
			}
			return asset, nil
		}
	}
	return githubAsset{}, fmt.Errorf("latest release is missing asset %q", name)
}

func download(ctx context.Context, client *http.Client, token, url, destination string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create download request: %w", err)
	}
	setHeaders(request, token, "update")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", response.Status)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	written, copyErr := io.Copy(file, io.LimitReader(response.Body, maxDownloadBytes+1))
	if copyErr == nil && written > maxDownloadBytes {
		copyErr = fmt.Errorf("response exceeds %d bytes", maxDownloadBytes)
	}
	if copyErr == nil {
		copyErr = file.Sync()
	}
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return fmt.Errorf("close destination: %w", closeErr)
	}
	return nil
}

func setHeaders(request *http.Request, token, current string) {
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "kivgraph/"+current)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
}

func verifyChecksumFile(filePath, checksumPath, expectedName string) error {
	file, err := os.Open(checksumPath)
	if err != nil {
		return fmt.Errorf("open checksums: %w", err)
	}
	defer file.Close()

	var expected string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || strings.TrimPrefix(fields[1], "*") != expectedName {
			continue
		}
		if expected != "" {
			return fmt.Errorf("duplicate checksum entry for %q", expectedName)
		}
		expected = fields[0]
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}
	if len(expected) != sha256.Size*2 {
		return fmt.Errorf("checksum entry for %q is missing or malformed", expectedName)
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return fmt.Errorf("checksum entry for %q is malformed: %w", expectedName, err)
	}
	actual, err := fileSHA256(filePath)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %q: expected %s, got %s", expectedName, expected, actual)
	}
	return nil
}

// archiveEntry is one member of a release archive, reduced to what extraction
// needs. Both readers produce it so that the guards below run once. A
// containment check with two implementations is a containment check with two
// ways to be wrong, and this branch has already found one of those.
type archiveEntry struct {
	// name is the archive's own slash-separated name, unvalidated.
	name  string
	isDir bool
	// size is what the archive claims, which is why it bounds the copy rather
	// than being trusted as the amount written.
	size int64
	mode fs.FileMode
	open func() (io.ReadCloser, error)
}

// archiveExtractor writes entries under destination within the bounds this
// package documents: nothing outside the bundle directory, no duplicate name,
// no more than maxArchiveFiles members and no more than maxBundleBytes of
// them uncompressed.
type archiveExtractor struct {
	destination string
	seen        map[string]struct{}
	total       uint64
	entries     int
}

func (extractor *archiveExtractor) add(entry archiveEntry) error {
	extractor.entries++
	if extractor.entries > maxArchiveFiles {
		return fmt.Errorf("archive contains more than %d entries", maxArchiveFiles)
	}
	relative, err := archivePathName(entry.name)
	if err != nil {
		return err
	}
	if _, exists := extractor.seen[relative]; exists {
		return fmt.Errorf("archive contains duplicate entry %q", relative)
	}
	extractor.seen[relative] = struct{}{}
	pathOnDisk := filepath.Join(extractor.destination, filepath.FromSlash(relative))
	if entry.isDir {
		if err := os.MkdirAll(pathOnDisk, 0o755); err != nil {
			return fmt.Errorf("create directory %q: %w", relative, err)
		}
		return nil
	}
	if entry.size < 0 {
		return fmt.Errorf("archive entry %q has negative size", relative)
	}
	if uint64(entry.size) > maxBundleBytes-extractor.total {
		return fmt.Errorf("archive exceeds %d uncompressed bytes", maxBundleBytes)
	}
	extractor.total += uint64(entry.size)
	if err := os.MkdirAll(filepath.Dir(pathOnDisk), 0o755); err != nil {
		return fmt.Errorf("create parent for %q: %w", relative, err)
	}
	mode := entry.mode.Perm()
	if mode == 0 {
		mode = 0o644
	}
	source, err := entry.open()
	if err != nil {
		return fmt.Errorf("open archive entry %q: %w", relative, err)
	}
	output, err := os.OpenFile(pathOnDisk, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("create archive file %q: %w", relative, err)
	}
	// CopyN and not Copy: the size is the archive's own claim, so a member
	// that delivers more than it declared is truncated here rather than
	// counted after the fact, and one that delivers less is an error rather
	// than a short file nobody notices.
	_, copyErr := io.CopyN(output, source, entry.size)
	closeErr := output.Close()
	sourceErr := source.Close()
	if copyErr != nil {
		return fmt.Errorf("extract archive file %q: %w", relative, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close archive file %q: %w", relative, closeErr)
	}
	if sourceErr != nil {
		return fmt.Errorf("close archive entry %q: %w", relative, sourceErr)
	}
	return nil
}

// extractArchive unpacks a release archive, choosing the reader by the name
// the release published it under rather than by runtime.GOOS. The two are the
// same in production -- archiveName is built from the platform -- and keeping
// them separate is what lets each reader be tested everywhere instead of only
// on the machine that ships it.
func extractArchive(archivePath, destination string) error {
	extractor := &archiveExtractor{destination: destination, seen: make(map[string]struct{})}
	var err error
	if strings.HasSuffix(archivePath, ".zip") {
		err = readZipArchive(archivePath, extractor)
	} else {
		err = readTarArchive(archivePath, extractor)
	}
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(destination, bundleDirName)); err != nil {
		return fmt.Errorf("archive is missing %s: %w", bundleDirName, err)
	}
	return nil
}

func readTarArchive(archivePath string, extractor *archiveExtractor) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer reader.Close()
	tarReader := tar.NewReader(io.LimitReader(reader, maxBundleBytes+1))
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			return nil
		}
		if nextErr != nil {
			return fmt.Errorf("read archive entry: %w", nextErr)
		}
		info := header.FileInfo()
		// TypeRegA is the pre-1.11 spelling of a regular file, and the reader
		// has normalised it to TypeReg on the way in since then, so naming it
		// here only widens what this accepts by a value that cannot arrive.
		if !info.IsDir() && header.Typeflag != tar.TypeReg {
			return fmt.Errorf("archive entry %q has unsupported type %d", header.Name, header.Typeflag)
		}
		if err := extractor.add(archiveEntry{
			name:  header.Name,
			isDir: info.IsDir(),
			size:  header.Size,
			mode:  info.Mode(),
			open:  func() (io.ReadCloser, error) { return io.NopCloser(tarReader), nil },
		}); err != nil {
			return err
		}
	}
}

func readZipArchive(archivePath string, extractor *archiveExtractor) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip archive: %w", err)
	}
	defer reader.Close()
	for _, member := range reader.File {
		info := member.FileInfo()
		// A zip can carry a symlink or a device in its external attributes.
		// The tarball reader refuses those by type; this is the same refusal
		// spelled in the vocabulary this format uses.
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("archive entry %q has unsupported mode %v", member.Name, info.Mode())
		}
		if member.UncompressedSize64 > math.MaxInt64 {
			return fmt.Errorf("archive entry %q declares an impossible size", member.Name)
		}
		if err := extractor.add(archiveEntry{
			name:  member.Name,
			isDir: info.IsDir(),
			size:  int64(member.UncompressedSize64),
			mode:  info.Mode(),
			open:  member.Open,
		}); err != nil {
			return err
		}
	}
	return nil
}

func archivePathName(name string) (string, error) {
	name = strings.TrimPrefix(name, "./")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("archive contains unsafe path %q", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("archive contains unsafe path %q", name)
	}
	if !strings.HasPrefix(clean, bundleDirName+"/") && clean != bundleDirName {
		return "", fmt.Errorf("archive entry %q is outside %s", name, bundleDirName)
	}
	return clean, nil
}

// findBundleWorker returns file information for the worker shim the bundle
// carries. Windows stores
// it as `kivgraph-ts-worker.cmd` and every other platform as an extensionless
// script, so the search is over what the platform would run rather than over
// one spelled name.
func findBundleWorker(root string) (fs.FileInfo, error) {
	candidates := executable.Candidates("kivgraph-ts-worker")
	for _, name := range candidates {
		info, err := os.Stat(filepath.Join(root, "bin", name))
		if err == nil {
			return info, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat worker %q: %w", name, err)
		}
	}
	return nil, fmt.Errorf("bundle carries no worker under any of %v", candidates)
}

func validateBundle(root, expectedVersion string) error {
	manifestPath := filepath.Join(root, "manifest.json")
	manifestFile, err := os.Open(manifestPath)
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	var manifest bundleManifest
	decodeErr := json.NewDecoder(io.LimitReader(manifestFile, 1<<20)).Decode(&manifest)
	closeErr := manifestFile.Close()
	if decodeErr != nil {
		return fmt.Errorf("decode manifest: %w", decodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close manifest: %w", closeErr)
	}
	if manifest.Product != "kivgraph" || manifest.Release != expectedVersion {
		return fmt.Errorf("manifest release is %q, want %q", manifest.Release, expectedVersion)
	}
	if manifest.Target.OS != runtime.GOOS || manifest.Target.Arch != runtime.GOARCH {
		return fmt.Errorf("manifest target is %s/%s, want %s/%s", manifest.Target.OS, manifest.Target.Arch, runtime.GOOS, runtime.GOARCH)
	}
	if manifest.Source.Dirty {
		return errors.New("manifest was built from a dirty source tree")
	}
	if err := verifyBundleChecksums(root); err != nil {
		return err
	}
	// The binary carries the platform's program suffix, and "is executable" is
	// asked of the platform rather than of the mode bits. Windows keeps none:
	// Go reports 0666 for every plain file there, so `mode&0o111 == 0` was
	// true of a perfectly good kivgraph.exe and this check refused every
	// bundle it was given.
	binaryPath := filepath.Join(root, "bin", executable.Name("kivgraph"))
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("stat binary: %w", err)
	}
	if !executable.IsProgram(info) {
		return errors.New("bundle binary is not executable")
	}
	// The worker is looked up by candidate rather than by name: its shim is a
	// `.cmd` on Windows and an extensionless script elsewhere, so the name it
	// is stored under is not the one Name would write.
	workerInfo, err := findBundleWorker(root)
	if err != nil {
		return err
	}
	if !executable.IsProgram(workerInfo) {
		return errors.New("bundle worker is not executable")
	}
	command := exec.Command(binaryPath, "version")
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run bundled version: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	if got := strings.TrimSpace(string(output)); got != expectedVersion {
		return fmt.Errorf("bundle version is %q, want %q", got, expectedVersion)
	}
	return nil
}

func verifyBundleChecksums(root string) error {
	checksumPath := filepath.Join(root, checksumsName)
	file, err := os.Open(checksumPath)
	if err != nil {
		return fmt.Errorf("open bundle checksums: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	entries := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 {
			return fmt.Errorf("malformed bundle checksum line %q", scanner.Text())
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return fmt.Errorf("malformed bundle checksum %q: %w", fields[0], err)
		}
		relative := strings.TrimPrefix(fields[1], "*")
		clean := path.Clean(relative)
		if clean != relative || filepath.IsAbs(relative) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(relative, "\\") {
			return fmt.Errorf("unsafe bundle checksum path %q", relative)
		}
		filePath := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(filePath)
		if err != nil {
			return fmt.Errorf("stat bundle checksum path %q: %w", relative, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("bundle checksum path %q is not a regular file", relative)
		}
		actual, err := fileSHA256(filePath)
		if err != nil {
			return fmt.Errorf("hash bundle file %q: %w", relative, err)
		}
		if actual != fields[0] {
			return fmt.Errorf("bundle checksum mismatch for %q: expected %s, got %s", relative, fields[0], actual)
		}
		entries++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read bundle checksums: %w", err)
	}
	if entries == 0 {
		return errors.New("bundle checksums are empty")
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func executablePath(value string) (string, error) {
	if value == "" {
		var err error
		value, err = os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve current executable: %w", err)
		}
	}
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil {
		return "", fmt.Errorf("resolve current executable %q: %w", value, err)
	}
	return filepath.Abs(resolved)
}

func currentBundleRoot(executable string) (string, error) {
	binDir := filepath.Dir(executable)
	if filepath.Base(binDir) != "bin" {
		return "", fmt.Errorf("current executable %q is not inside a Kivgraph bundle", executable)
	}
	root := filepath.Dir(binDir)
	manifest, err := os.Stat(filepath.Join(root, "manifest.json"))
	if err != nil {
		return "", fmt.Errorf("current executable is not an installed release bundle: %w", err)
	}
	if manifest.IsDir() {
		return "", errors.New("bundle manifest is a directory")
	}
	return root, nil
}

func replaceBundle(currentRoot, stagedRoot string) error {
	if currentRoot == "" || currentRoot == string(filepath.Separator) || stagedRoot == "" {
		return errors.New("unsafe bundle replacement path")
	}
	if filepath.Dir(currentRoot) != filepath.Dir(stagedRoot) && filepath.Dir(currentRoot) != filepath.Dir(filepath.Dir(stagedRoot)) {
		return errors.New("staged bundle is not on the installation filesystem")
	}
	backupRoot := currentRoot + ".previous"
	if _, err := os.Lstat(backupRoot); err == nil {
		// A backup an earlier update left behind. On Windows that is the
		// ordinary outcome rather than a fault: the bundle it replaced held
		// the running binary, which could not be unlinked while it ran. It
		// can be now, because that process has since exited. Refusing here
		// would make the second update on a Windows machine impossible.
		if removeErr := os.RemoveAll(backupRoot); removeErr != nil {
			return fmt.Errorf("previous bundle %s exists and cannot be removed: %w", backupRoot, removeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect previous bundle: %w", err)
	}
	if err := os.Rename(currentRoot, backupRoot); err != nil {
		return fmt.Errorf("move current bundle to backup: %w", err)
	}
	installed := false
	defer func() {
		if installed {
			return
		}
		_ = os.RemoveAll(currentRoot)
		_ = os.Rename(backupRoot, currentRoot)
	}()
	if err := os.Rename(stagedRoot, currentRoot); err != nil {
		return fmt.Errorf("move staged bundle into place: %w", err)
	}
	installed = true
	if err := removeReplacedBundle(backupRoot); err != nil {
		return fmt.Errorf("remove previous bundle: %w", err)
	}
	return nil
}

func semanticVersion(value string) string {
	if strings.HasPrefix(value, "v") {
		return value
	}
	return "v" + value
}
