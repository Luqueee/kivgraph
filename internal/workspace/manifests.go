package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxTypeScriptManifestBytes = 16 << 20

// TypeScriptDiscovery contains the manifests and project relationships found
// in one registered repository.
type TypeScriptDiscovery struct {
	PackageManifests []string
	Projects         []TypeScriptProject
	Workspaces       []TypeScriptWorkspaceDeclaration
}

// TypeScriptProject describes one tsconfig and its resolved project
// references. ConfigPath and References are absolute paths.
type TypeScriptProject struct {
	ConfigPath string
	References []string
}

// TypeScriptWorkspaceFormat identifies the file format that declares a
// workspace.
type TypeScriptWorkspaceFormat string

const (
	TypeScriptWorkspacePackageJSON TypeScriptWorkspaceFormat = "package.json"
	TypeScriptWorkspacePNPM        TypeScriptWorkspaceFormat = "pnpm-workspace.yaml"
)

// TypeScriptWorkspaceDeclaration describes package glob patterns declared by
// a package.json or pnpm-workspace.yaml file.
type TypeScriptWorkspaceDeclaration struct {
	ManifestPath string
	Format       TypeScriptWorkspaceFormat
	Patterns     []string
}

func isTypeScriptConfigName(name string) bool {
	return strings.HasPrefix(name, "tsconfig.") && strings.HasSuffix(name, ".json")
}

func readTypeScriptManifest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxTypeScriptManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTypeScriptManifestBytes {
		return nil, fmt.Errorf("manifest exceeds %d bytes", maxTypeScriptManifestBytes)
	}
	return data, nil
}

func parsePackageWorkspace(path string) ([]string, bool, error) {
	data, err := readTypeScriptManifest(path)
	if err != nil {
		return nil, false, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, false, fmt.Errorf("parse JSON: %w", err)
	}
	raw, exists := fields["workspaces"]
	if !exists {
		return nil, false, nil
	}
	patterns, err := decodeWorkspacePatterns(raw)
	if err != nil {
		return nil, false, err
	}
	return patterns, true, nil
}

func decodeWorkspacePatterns(raw json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, fmt.Errorf("workspaces must be an array or an object with packages")
	}
	switch trimmed[0] {
	case '[':
		var patterns []string
		if err := json.Unmarshal(trimmed, &patterns); err != nil {
			return nil, fmt.Errorf("workspaces array: %w", err)
		}
		return patterns, nil
	case '{':
		var object struct {
			Packages json.RawMessage `json:"packages"`
		}
		if err := json.Unmarshal(trimmed, &object); err != nil {
			return nil, fmt.Errorf("workspaces object: %w", err)
		}
		if len(object.Packages) == 0 || bytes.Equal(bytes.TrimSpace(object.Packages), []byte("null")) {
			return nil, fmt.Errorf("workspaces object must contain packages")
		}
		var patterns []string
		if err := json.Unmarshal(object.Packages, &patterns); err != nil {
			return nil, fmt.Errorf("workspaces.packages: %w", err)
		}
		return patterns, nil
	default:
		return nil, fmt.Errorf("workspaces must be an array or an object with packages")
	}
}

func parsePNPMWorkspace(path string) ([]string, error) {
	data, err := readTypeScriptManifest(path)
	if err != nil {
		return nil, err
	}
	var document map[string]yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	node, exists := document["packages"]
	if !exists {
		// pnpm permits a workspace file to contain only configuration such as
		// minimumReleaseAgeExclude; package patterns then use pnpm's defaults.
		return []string{}, nil
	}
	if node.Tag == "!!null" {
		return nil, nil
	}
	var patterns []string
	if err := node.Decode(&patterns); err != nil {
		return nil, fmt.Errorf("parse YAML packages: %w", err)
	}
	return patterns, nil
}

func normalizeWorkspacePatterns(root string, patterns []string) ([]string, error) {
	if patterns == nil {
		return nil, fmt.Errorf("workspace packages must not be null")
	}
	normalized := make([]string, len(patterns))
	for index, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return nil, fmt.Errorf("workspace packages[%d]: must not be empty", index)
		}
		candidate := strings.TrimPrefix(pattern, "!")
		if candidate == "" {
			return nil, fmt.Errorf("workspace packages[%d]: invalid negation", index)
		}
		candidate = filepath.FromSlash(candidate)
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(root, candidate)
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			return nil, fmt.Errorf("workspace packages[%d]: make path absolute: %w", index, err)
		}
		if !pathWithin(root, filepath.Clean(absolute)) {
			return nil, fmt.Errorf("workspace packages[%d]: path %q escapes repository realpath %q", index, pattern, root)
		}
		normalized[index] = filepath.ToSlash(pattern)
	}
	return normalized, nil
}

func parseTypeScriptProject(configPath, root string) (TypeScriptProject, error) {
	data, err := readTypeScriptManifest(configPath)
	if err != nil {
		return TypeScriptProject{}, err
	}
	data, err = decodeJSONC(data)
	if err != nil {
		return TypeScriptProject{}, err
	}
	var document struct {
		References json.RawMessage `json:"references"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return TypeScriptProject{}, fmt.Errorf("parse JSON: %w", err)
	}
	project := TypeScriptProject{ConfigPath: filepath.Clean(configPath)}
	if len(document.References) == 0 || bytes.Equal(bytes.TrimSpace(document.References), []byte("null")) {
		return project, nil
	}
	var references []json.RawMessage
	if err := json.Unmarshal(document.References, &references); err != nil {
		return TypeScriptProject{}, fmt.Errorf("references must be an array: %w", err)
	}
	project.References = make([]string, len(references))
	for index, rawReference := range references {
		var reference struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(rawReference, &reference); err != nil {
			return TypeScriptProject{}, fmt.Errorf("references[%d]: %w", index, err)
		}
		if strings.TrimSpace(reference.Path) == "" {
			return TypeScriptProject{}, fmt.Errorf("references[%d].path: must not be empty", index)
		}
		resolved, err := resolveProjectReference(filepath.Dir(configPath), root, reference.Path)
		if err != nil {
			return TypeScriptProject{}, fmt.Errorf("references[%d].path %q: %w", index, reference.Path, err)
		}
		project.References[index] = resolved
	}
	return project, nil
}

func resolveProjectReference(configDirectory, root, referencePath string) (string, error) {
	target := filepath.FromSlash(strings.TrimSpace(referencePath))
	if !filepath.IsAbs(target) {
		target = filepath.Join(configDirectory, target)
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("make path absolute: %w", err)
	}
	target = filepath.Clean(absolute)
	if !pathWithin(root, target) {
		return "", fmt.Errorf("path escapes repository realpath %q", root)
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("target does not exist or is inaccessible: %w", err)
	}
	if info.IsDir() {
		target = filepath.Join(target, "tsconfig.json")
		info, err = os.Stat(target)
		if err != nil {
			return "", fmt.Errorf("directory reference has no tsconfig.json: %w", err)
		}
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("target is not a regular file")
	}
	if symlink, err := FirstSymlink(target); err != nil {
		return "", fmt.Errorf("inspect symlinks: %w", err)
	} else if symlink != "" {
		return "", fmt.Errorf("target contains symlink component %q", symlink)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve target realpath: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("make target realpath absolute: %w", err)
	}
	resolved = filepath.Clean(resolved)
	if !pathWithin(root, resolved) {
		return "", fmt.Errorf("target realpath escapes repository realpath %q", root)
	}
	return resolved, nil
}

func decodeJSONC(data []byte) ([]byte, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	withoutComments := make([]byte, 0, len(data))
	const (
		jsonNormal = iota
		jsonString
		jsonLineComment
		jsonBlockComment
	)
	state := jsonNormal
	escaped := false
	for index := 0; index < len(data); index++ {
		current := data[index]
		switch state {
		case jsonNormal:
			switch {
			case current == '"':
				state = jsonString
				withoutComments = append(withoutComments, current)
			case current == '/' && index+1 < len(data) && data[index+1] == '/':
				state = jsonLineComment
				withoutComments = append(withoutComments, ' ')
				index++
			case current == '/' && index+1 < len(data) && data[index+1] == '*':
				state = jsonBlockComment
				withoutComments = append(withoutComments, ' ')
				index++
			default:
				withoutComments = append(withoutComments, current)
			}
		case jsonString:
			withoutComments = append(withoutComments, current)
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '"' {
				state = jsonNormal
			}
		case jsonLineComment:
			if current == '\n' {
				state = jsonNormal
				withoutComments = append(withoutComments, current)
			}
		case jsonBlockComment:
			if current == '*' && index+1 < len(data) && data[index+1] == '/' {
				state = jsonNormal
				withoutComments = append(withoutComments, ' ')
				index++
			} else if current == '\n' {
				withoutComments = append(withoutComments, current)
			}
		}
	}
	if state == jsonBlockComment {
		return nil, fmt.Errorf("unterminated block comment")
	}
	return removeJSONCTrailingCommas(withoutComments), nil
}

func removeJSONCTrailingCommas(data []byte) []byte {
	result := make([]byte, 0, len(data))
	inString := false
	escaped := false
	for index := 0; index < len(data); index++ {
		current := data[index]
		if inString {
			result = append(result, current)
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '"' {
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			result = append(result, current)
			continue
		}
		if current == ',' {
			next := index + 1
			for next < len(data) && (data[next] == ' ' || data[next] == '\t' || data[next] == '\r' || data[next] == '\n') {
				next++
			}
			if next < len(data) && (data[next] == ']' || data[next] == '}') {
				continue
			}
		}
		result = append(result, current)
	}
	return result
}
