// Package toolchain manages optional analyzers owned by a Kivgraph
// installation. Managed tools live in Kivgraph state, never in a repository.
package toolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/mod/semver"

	"github.com/Luqueee/kivgraph/internal/filelock"
)

const (
	// Pyright is the first managed analyzer. The command family is intentionally
	// generic so future analyzers can use the same state and activation model.
	Pyright = "pyright"
	// DefaultPyrightVersion is pinned so an install does not silently change
	// the semantic provider used by a later graph generation.
	DefaultPyrightVersion = "1.1.413"
	manifestFile          = "kivgraph-toolchain.json"
	manifestSchema        = 2
	installTimeout        = 10 * time.Minute
)

// ToolStatus describes one managed tool without claiming that an absent tool
// is an error. A broken installation is named separately from a missing one.
type ToolStatus struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	Version    string `json:"version,omitempty"`
	Root       string `json:"root,omitempty"`
	Executable string `json:"executable,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

// Manifest is the durable description of an installed managed tool.
type Manifest struct {
	Schema     int    `json:"schema"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Package    string `json:"package"`
	Executable string `json:"executable"`
	SHA256     string `json:"sha256"`
}

// Names returns the managed tool vocabulary exposed by this build.
func Names() []string { return []string{Pyright} }

// ValidateName checks the generic command's tool argument.
func ValidateName(name string) error {
	if name != Pyright {
		return fmt.Errorf("unknown managed tool %q: want %s", name, Pyright)
	}
	return nil
}

// ValidateVersion accepts one exact stable semantic version, without a
// leading v. Keeping the path and package selector in this form avoids a
// floating npm range and makes the install reproducible at the command level.
func ValidateVersion(version string) error {
	if version == "" {
		return errors.New("version must not be empty")
	}
	if strings.HasPrefix(version, "v") || !semver.IsValid("v"+version) ||
		semver.Canonical("v"+version) != "v"+version || semver.Prerelease("v"+version) != "" {
		return fmt.Errorf("version %q is not an exact stable semantic version", version)
	}
	return nil
}

// StateRoot returns the directory where managed tools belong for one
// installation or profile universe.
func StateRoot(stateDirectory string) string {
	return filepath.Join(stateDirectory, "toolchains")
}

// PyrightRoot returns the version-independent Pyright directory.
func PyrightRoot(stateDirectory string) string {
	return filepath.Join(StateRoot(stateDirectory), Pyright)
}

// PyrightAnalyzerCommand returns the bundled adapter command configured to
// launch one managed Pyright language server.
func PyrightAnalyzerCommand(executable string) string {
	return "kivgraph-python-pyright --analyzer " + shellQuote(executable)
}

// IsManagedPyrightCommand reports whether command points into this
// installation's managed Pyright directory.
func IsManagedPyrightCommand(command, stateDirectory string) bool {
	if strings.TrimSpace(command) == "" || strings.TrimSpace(stateDirectory) == "" {
		return false
	}
	args, err := SplitCommandLine(command)
	if err != nil || len(args) < 3 || args[0] != "kivgraph-python-pyright" {
		return false
	}
	root, err := filepath.Abs(PyrightRoot(stateDirectory))
	if err != nil {
		return false
	}
	for index := 0; index+1 < len(args); index++ {
		if args[index] != "--analyzer" {
			continue
		}
		executable, err := filepath.Abs(args[index+1])
		if err != nil {
			return false
		}
		relative, err := filepath.Rel(root, executable)
		return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	}
	return false
}

// SplitCommandLine parses the small shell-like command vocabulary accepted by
// analyzer configuration. It removes quoting used for paths with spaces while
// preserving ordinary command arguments and platform path separators.
func SplitCommandLine(command string) ([]string, error) {
	return splitCommandLine(command, runtime.GOOS == "windows")
}

func splitCommandLine(command string, windows bool) ([]string, error) {
	runes := []rune(command)
	args := make([]string, 0)
	var current strings.Builder
	var quote rune
	escaped := false
	token := false
	for index := 0; index < len(runes); index++ {
		character := runes[index]
		if escaped {
			current.WriteRune(character)
			escaped = false
			token = true
			continue
		}
		if quote == '\'' {
			if character == '\'' {
				quote = 0
				token = true
			} else {
				current.WriteRune(character)
				token = true
			}
			continue
		}
		if quote == '"' {
			if character == '"' {
				quote = 0
				token = true
				continue
			}
			if !windows && character == '\\' && index+1 < len(runes) && strings.ContainsRune("\"\\$`", runes[index+1]) {
				escaped = true
				continue
			}
			current.WriteRune(character)
			token = true
			continue
		}
		switch {
		case character == '\\':
			if windows {
				current.WriteRune(character)
				token = true
				continue
			}
			if index+1 == len(runes) {
				return nil, errors.New("unterminated escape")
			}
			if unicode.IsSpace(runes[index+1]) || runes[index+1] == '\\' || runes[index+1] == '\'' || runes[index+1] == '"' {
				escaped = true
			} else {
				current.WriteRune(character)
			}
			token = true
		case character == '\'' || character == '"':
			quote = character
			token = true
		case unicode.IsSpace(character):
			if token {
				args = append(args, current.String())
				current.Reset()
				token = false
			}
		default:
			current.WriteRune(character)
			token = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if token {
		args = append(args, current.String())
	}
	return args, nil
}

// Status returns the status of every managed analyzer in this build.
func Status(stateDirectory string) ([]ToolStatus, error) {
	status, err := pyrightStatus(stateDirectory, "")
	if err != nil {
		return nil, err
	}
	return []ToolStatus{status}, nil
}

// FindNPM locates the package manager used for installing Pyright.
func FindNPM() (string, error) {
	for _, name := range []string{"npm", "npm.cmd"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("npm is unavailable; install Node.js 22 or later and ensure npm is on PATH")
}

// Install installs one supported analyzer into stateDirectory. npmPath is
// injectable for tests; an empty value discovers npm on PATH.
func Install(ctx context.Context, stateDirectory, name, version, npmPath string) (ToolStatus, error) {
	if err := ValidateName(name); err != nil {
		return ToolStatus{}, err
	}
	if err := ValidateVersion(version); err != nil {
		return ToolStatus{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, installTimeout)
		defer cancel()
	}
	if npmPath == "" {
		var err error
		npmPath, err = FindNPM()
		if err != nil {
			return ToolStatus{}, err
		}
	}
	stateDirectory, err := resolveStateDirectory(stateDirectory)
	if err != nil {
		return ToolStatus{}, err
	}
	toolRoot := PyrightRoot(stateDirectory)
	if err := os.MkdirAll(toolRoot, 0o700); err != nil {
		return ToolStatus{}, fmt.Errorf("create Pyright state %q: %w", toolRoot, err)
	}
	lock, held, err := filelock.Acquire(filepath.Join(StateRoot(stateDirectory), ".install.lock"))
	if err != nil {
		return ToolStatus{}, fmt.Errorf("lock toolchain state: %w", err)
	}
	if !held {
		return ToolStatus{}, errors.New("another toolchain operation is already running")
	}
	defer func() { _ = lock.Release() }()

	existing, err := pyrightStatus(stateDirectory, version)
	if err != nil {
		return ToolStatus{}, err
	}
	if existing.State == "installed" && existing.Version == version {
		return existing, nil
	}

	temporary, err := os.MkdirTemp(toolRoot, ".install-*")
	if err != nil {
		return ToolStatus{}, fmt.Errorf("prepare Pyright install: %w", err)
	}
	defer os.RemoveAll(temporary)
	arguments := []string{
		"install", "--prefix", temporary, "--no-save", "--no-package-lock",
		"--ignore-scripts", "--no-audit", "--no-fund", Pyright + "@" + version,
	}
	command := exec.CommandContext(ctx, npmPath, arguments...)
	command.Dir = temporary
	output, err := command.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail != "" {
			return ToolStatus{}, fmt.Errorf("npm install %s@%s: %w: %s", Pyright, version, err, detail)
		}
		return ToolStatus{}, fmt.Errorf("npm install %s@%s: %w", Pyright, version, err)
	}

	packageVersion, err := installedPackageVersion(temporary)
	if err != nil {
		return ToolStatus{}, fmt.Errorf("inspect installed Pyright: %w", err)
	}
	if packageVersion != version {
		return ToolStatus{}, fmt.Errorf("npm installed Pyright %q, want %q", packageVersion, version)
	}
	executable, err := pyrightExecutable(temporary)
	if err != nil {
		return ToolStatus{}, err
	}
	digest, err := treeDigest(filepath.Join(temporary, "node_modules"))
	if err != nil {
		return ToolStatus{}, fmt.Errorf("digest installed Pyright: %w", err)
	}
	manifest := Manifest{
		Schema:     manifestSchema,
		Name:       Pyright,
		Version:    version,
		Package:    Pyright + "@" + version,
		Executable: filepath.ToSlash(executable),
		SHA256:     digest,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ToolStatus{}, fmt.Errorf("encode Pyright manifest: %w", err)
	}
	manifestData = append(manifestData, '\n')
	if err := os.WriteFile(filepath.Join(temporary, manifestFile), manifestData, 0o600); err != nil {
		return ToolStatus{}, fmt.Errorf("write Pyright manifest: %w", err)
	}

	target := filepath.Join(toolRoot, version)
	if info, statErr := os.Stat(target); statErr == nil {
		if !info.IsDir() {
			return ToolStatus{}, fmt.Errorf("pyright target %q is not a directory", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return ToolStatus{}, fmt.Errorf("replace broken Pyright install: %w", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return ToolStatus{}, fmt.Errorf("inspect Pyright target %q: %w", target, statErr)
	}
	if err := os.Rename(temporary, target); err != nil {
		return ToolStatus{}, fmt.Errorf("publish Pyright install: %w", err)
	}
	status, err := pyrightStatus(stateDirectory, version)
	if err != nil {
		return ToolStatus{}, fmt.Errorf("verify Pyright install: %w", err)
	}
	if status.State != "installed" {
		return ToolStatus{}, fmt.Errorf("verify Pyright install: %s", status.Detail)
	}
	return status, nil
}

// Remove deletes only the managed state for one supported analyzer.
func Remove(stateDirectory, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	stateDirectory, err := resolveStateDirectory(stateDirectory)
	if err != nil {
		return err
	}
	root := PyrightRoot(stateDirectory)
	lock, held, err := filelock.Acquire(filepath.Join(StateRoot(stateDirectory), ".install.lock"))
	if err != nil {
		return fmt.Errorf("lock toolchain state: %w", err)
	}
	if !held {
		return errors.New("another toolchain operation is already running")
	}
	defer func() { _ = lock.Release() }()
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove managed %s state %q: %w", name, root, err)
	}
	return nil
}

func pyrightStatus(stateDirectory, requestedVersion string) (ToolStatus, error) {
	stateDirectory, err := resolveStateDirectory(stateDirectory)
	if err != nil {
		return ToolStatus{}, err
	}
	root := PyrightRoot(stateDirectory)
	status := ToolStatus{Name: Pyright, Root: root}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		status.State = "missing"
		status.Detail = "not installed"
		return status, nil
	}
	if err != nil {
		return ToolStatus{}, fmt.Errorf("inspect Pyright state %q: %w", root, err)
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && ValidateVersion(entry.Name()) == nil {
			versions = append(versions, entry.Name())
		}
	}
	if len(versions) == 0 {
		status.State = "broken"
		status.Detail = "no versioned installation found"
		return status, nil
	}
	sort.Slice(versions, func(i, j int) bool {
		return semver.Compare("v"+versions[i], "v"+versions[j]) > 0
	})
	selectedVersion := versions[0]
	if requestedVersion != "" {
		selectedVersion = requestedVersion
		found := false
		for _, candidate := range versions {
			if candidate == selectedVersion {
				found = true
				break
			}
		}
		if !found {
			status.State = "broken"
			status.Version = selectedVersion
			status.Detail = "requested version was not published"
			return status, nil
		}
	}
	versionRoot := filepath.Join(root, selectedVersion)
	manifestData, err := os.ReadFile(filepath.Join(versionRoot, manifestFile))
	if err != nil {
		status.State = "broken"
		status.Version = selectedVersion
		status.Detail = fmt.Sprintf("read manifest: %v", err)
		return status, nil
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		status.State = "broken"
		status.Version = selectedVersion
		status.Detail = fmt.Sprintf("decode manifest: %v", err)
		return status, nil
	}
	status.Version = manifest.Version
	status.Executable = filepath.Join(versionRoot, filepath.FromSlash(manifest.Executable))
	status.SHA256 = manifest.SHA256
	switch {
	case manifest.Schema != manifestSchema:
		status.State = "broken"
		status.Detail = fmt.Sprintf("manifest schema %d is unsupported", manifest.Schema)
	case manifest.Name != Pyright:
		status.State = "broken"
		status.Detail = fmt.Sprintf("manifest names %q", manifest.Name)
	case manifest.Version != selectedVersion || ValidateVersion(manifest.Version) != nil:
		status.State = "broken"
		status.Detail = "manifest version does not match its directory"
	case manifest.Executable == "" || filepath.IsAbs(filepath.FromSlash(manifest.Executable)) ||
		strings.HasPrefix(filepath.Clean(filepath.FromSlash(manifest.Executable)), ".."+string(filepath.Separator)):
		status.State = "broken"
		status.Detail = "manifest executable escapes its installation"
	case !isExecutable(status.Executable):
		status.State = "broken"
		status.Detail = fmt.Sprintf("executable %q is missing", status.Executable)
	default:
		digest, digestErr := treeDigest(filepath.Join(versionRoot, "node_modules"))
		if digestErr != nil {
			status.State = "broken"
			status.Detail = fmt.Sprintf("digest installation: %v", digestErr)
		} else if digest != manifest.SHA256 {
			status.State = "broken"
			status.Detail = "installed files do not match the manifest digest"
		} else {
			status.State = "installed"
		}
	}
	return status, nil
}

func resolveStateDirectory(stateDirectory string) (string, error) {
	if strings.TrimSpace(stateDirectory) == "" {
		return "", errors.New("toolchain state directory must not be empty")
	}
	resolved, err := filepath.Abs(stateDirectory)
	if err != nil {
		return "", fmt.Errorf("resolve toolchain state: %w", err)
	}
	return resolved, nil
}

func installedPackageVersion(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "node_modules", Pyright, "package.json"))
	if err != nil {
		return "", err
	}
	var packageJSON struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &packageJSON); err != nil {
		return "", err
	}
	if packageJSON.Version == "" {
		return "", errors.New("package.json has no version")
	}
	return packageJSON.Version, nil
}

func pyrightExecutable(root string) (string, error) {
	base := filepath.Join(root, "node_modules", ".bin", "pyright-langserver")
	candidates := []string{base}
	if runtime.GOOS == "windows" {
		candidates = []string{base + ".cmd", base + ".exe", base}
	}
	for _, candidate := range candidates {
		if isExecutable(candidate) {
			relative, err := filepath.Rel(root, candidate)
			if err != nil {
				return "", fmt.Errorf("relativize Pyright executable: %w", err)
			}
			return relative, nil
		}
	}
	return "", fmt.Errorf("pyright install has no pyright-langserver executable under %q", filepath.Join(root, "node_modules", ".bin"))
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

func treeDigest(root string) (string, error) {
	hash := sha256.New()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, path := range paths {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return "", err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		hash.Write([]byte(filepath.ToSlash(relative)))
		hash.Write([]byte{0})
		fmt.Fprintf(hash, "mode:%o\x00", info.Mode().Perm())
		switch {
		case info.IsDir():
			hash.Write([]byte("directory\x00"))
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return "", err
			}
			hash.Write([]byte("symlink\x00" + target))
		case info.Mode().IsRegular():
			file, err := os.Open(path)
			if err != nil {
				return "", err
			}
			_, copyErr := io.Copy(hash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return "", copyErr
			}
			if closeErr != nil {
				return "", closeErr
			}
		default:
			return "", fmt.Errorf("unsupported file type at %q", path)
		}
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func shellQuote(value string) string {
	if runtime.GOOS == "windows" {
		return `"` + value + `"`
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "`", "\\`").Replace(value)
	return `"` + escaped + `"`
}
