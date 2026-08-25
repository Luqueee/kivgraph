package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestResolveTypeScriptSources(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, root string) parsedTypeScriptConfig
		wantSources func(root string) []string
	}{
		{
			name: "default include applies when neither files nor include is declared",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				writeDiscoveryFile(t, filepath.Join(root, "a.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "sub", "b.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "sub", "deep", "c.txt"), "text")
				return parsedTypeScriptConfig{
					ConfigPath: filepath.Join(root, "tsconfig.json"),
					Directory:  root,
				}
			},
			wantSources: func(root string) []string {
				return []string{filepath.Join(root, "a.ts"), filepath.Join(root, "sub", "b.ts")}
			},
		},
		{
			name: "explicit files entries bypass exclude",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				pinned := filepath.Join(root, "node_modules", "pinned", "index.ts")
				writeDiscoveryFile(t, pinned, "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "src", "index.ts"), "export {}")
				return parsedTypeScriptConfig{
					ConfigPath: filepath.Join(root, "tsconfig.json"),
					Directory:  root,
					HasFiles:   true,
					Files:      []string{pinned},
					HasInclude: true,
					Include:    []string{filepath.Join(root, "src", "**", "*.ts")},
				}
			},
			wantSources: func(root string) []string {
				return []string{
					filepath.Join(root, "node_modules", "pinned", "index.ts"),
					filepath.Join(root, "src", "index.ts"),
				}
			},
		},
		{
			name: "include with ** nests multiple levels",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				writeDiscoveryFile(t, filepath.Join(root, "a", "b", "c", "deep.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "shallow.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "notes.txt"), "text")
				return parsedTypeScriptConfig{
					ConfigPath: filepath.Join(root, "tsconfig.json"),
					Directory:  root,
					HasInclude: true,
					Include:    []string{filepath.Join(root, "**", "*.ts")},
				}
			},
			wantSources: func(root string) []string {
				return []string{filepath.Join(root, "a", "b", "c", "deep.ts"), filepath.Join(root, "shallow.ts")}
			},
		},
		{
			name: "exclude prunes a subtree",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				writeDiscoveryFile(t, filepath.Join(root, "keep", "kept.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "excluded", "dropped.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "excluded", "nested", "alsoDropped.ts"), "export {}")
				return parsedTypeScriptConfig{
					ConfigPath: filepath.Join(root, "tsconfig.json"),
					Directory:  root,
					HasExclude: true,
					Exclude:    []string{filepath.Join(root, "excluded")},
				}
			},
			wantSources: func(root string) []string {
				return []string{filepath.Join(root, "keep", "kept.ts")}
			},
		},
		{
			name: "node_modules is excluded by default",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				writeDiscoveryFile(t, filepath.Join(root, "src", "index.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "node_modules", "dep", "index.ts"), "export {}")
				return parsedTypeScriptConfig{
					ConfigPath: filepath.Join(root, "tsconfig.json"),
					Directory:  root,
				}
			},
			wantSources: func(root string) []string {
				return []string{filepath.Join(root, "src", "index.ts")}
			},
		},
		{
			name: "allowJs adds every JavaScript extension the compiler reads",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				writeDiscoveryFile(t, filepath.Join(root, "index.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "legacy.js"), "module.exports = {}")
				writeDiscoveryFile(t, filepath.Join(root, "tool.mjs"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "tool.cjs"), "module.exports = {}")
				writeDiscoveryFile(t, filepath.Join(root, "styles.css"), "body {}")
				return parsedTypeScriptConfig{
					ConfigPath:      filepath.Join(root, "tsconfig.json"),
					Directory:       root,
					CompilerOptions: map[string]any{"allowJs": true},
				}
			},
			wantSources: func(root string) []string {
				return []string{
					filepath.Join(root, "index.ts"),
					filepath.Join(root, "legacy.js"),
					filepath.Join(root, "tool.cjs"),
					filepath.Join(root, "tool.mjs"),
				}
			},
		},
		{
			// The other half of the same contract: ".mts" and ".cts" are
			// TypeScript with no option asked for, and a ".mjs" beside them
			// is a source only once the project says "allowJs".
			name: "mts and cts are claimed without allowJs and mjs is not",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				writeDiscoveryFile(t, filepath.Join(root, "module.mts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "script.cts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "tool.mjs"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "tool.cjs"), "module.exports = {}")
				return parsedTypeScriptConfig{
					ConfigPath: filepath.Join(root, "tsconfig.json"),
					Directory:  root,
				}
			},
			wantSources: func(root string) []string {
				return []string{
					filepath.Join(root, "module.mts"),
					filepath.Join(root, "script.cts"),
				}
			},
		},
		{
			name: "explicit extension pattern does not also match other extensions",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				writeDiscoveryFile(t, filepath.Join(root, "src", "component.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "src", "component.tsx"), "export {}")
				return parsedTypeScriptConfig{
					ConfigPath: filepath.Join(root, "tsconfig.json"),
					Directory:  root,
					HasInclude: true,
					Include:    []string{filepath.Join(root, "src", "**", "*.ts")},
				}
			},
			wantSources: func(root string) []string {
				return []string{filepath.Join(root, "src", "component.ts")}
			},
		},
		{
			name: "deduplicates a file reached by both files and include",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				shared := filepath.Join(root, "src", "shared.ts")
				writeDiscoveryFile(t, shared, "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "src", "only-include.ts"), "export {}")
				return parsedTypeScriptConfig{
					ConfigPath: filepath.Join(root, "tsconfig.json"),
					Directory:  root,
					HasFiles:   true,
					Files:      []string{shared},
					HasInclude: true,
					Include:    []string{filepath.Join(root, "src", "**", "*.ts")},
				}
			},
			wantSources: func(root string) []string {
				return []string{
					filepath.Join(root, "src", "only-include.ts"),
					filepath.Join(root, "src", "shared.ts"),
				}
			},
		},
		{
			name: "result is sorted lexicographically",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				writeDiscoveryFile(t, filepath.Join(root, "zebra.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "mango.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "apple.ts"), "export {}")
				return parsedTypeScriptConfig{
					ConfigPath: filepath.Join(root, "tsconfig.json"),
					Directory:  root,
				}
			},
			wantSources: func(root string) []string {
				return []string{
					filepath.Join(root, "apple.ts"),
					filepath.Join(root, "mango.ts"),
					filepath.Join(root, "zebra.ts"),
				}
			},
		},
		{
			name: "default excludes also prune outDir and declarationDir",
			setup: func(t *testing.T, root string) parsedTypeScriptConfig {
				writeDiscoveryFile(t, filepath.Join(root, "src", "index.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "dist", "generated.d.ts"), "export {}")
				writeDiscoveryFile(t, filepath.Join(root, "types", "extra.d.ts"), "export {}")
				return parsedTypeScriptConfig{
					ConfigPath: filepath.Join(root, "tsconfig.json"),
					Directory:  root,
					CompilerOptions: map[string]any{
						"outDir":         filepath.Join(root, "dist"),
						"declarationDir": filepath.Join(root, "types"),
					},
				}
			},
			wantSources: func(root string) []string {
				return []string{filepath.Join(root, "src", "index.ts")}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			configuration := test.setup(t, root)
			got, err := resolveTypeScriptSources(configuration, root)
			if err != nil {
				t.Fatalf("resolveTypeScriptSources() error = %v", err)
			}
			want := test.wantSources(root)
			if !equalStrings(got, want) {
				t.Fatalf("resolveTypeScriptSources() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestResolveTypeScriptSourcesRejectsInvalidFiles(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(t *testing.T, root string) string
		wantErrContains string
		wantErrIs       error
	}{
		{
			name: "missing files entry",
			setup: func(t *testing.T, root string) string {
				return filepath.Join(root, "missing.ts")
			},
			wantErrContains: "missing.ts",
			wantErrIs:       os.ErrNotExist,
		},
		{
			name: "files entry is not a regular file",
			setup: func(t *testing.T, root string) string {
				directory := filepath.Join(root, "adirectory")
				writeDiscoveryFile(t, filepath.Join(directory, "placeholder.ts"), "export {}")
				return directory
			},
			wantErrContains: "is not a regular file",
		},
		{
			name: "files entry escapes the repository root",
			setup: func(t *testing.T, root string) string {
				outside := testsupport.TempDir(t)
				escaped := filepath.Join(outside, "escaped.ts")
				writeDiscoveryFile(t, escaped, "export {}")
				return escaped
			},
			wantErrContains: "escapes repository root",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			declaredFile := test.setup(t, root)
			configuration := parsedTypeScriptConfig{
				ConfigPath: filepath.Join(root, "tsconfig.json"),
				Directory:  root,
				HasFiles:   true,
				Files:      []string{declaredFile},
			}
			_, err := resolveTypeScriptSources(configuration, root)
			if err == nil || !strings.Contains(err.Error(), test.wantErrContains) {
				t.Fatalf("resolveTypeScriptSources() error = %v, want substring %q", err, test.wantErrContains)
			}
			if test.wantErrIs != nil && !errors.Is(err, test.wantErrIs) {
				t.Fatalf("resolveTypeScriptSources() error = %v, want errors.Is(%v)", err, test.wantErrIs)
			}
		})
	}
}

func TestResolveTypeScriptSourcesSkipsSymlinkedDirectories(t *testing.T) {
	root := testsupport.TempDir(t)
	external := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(external, "leaked.ts"), "export {}")
	writeDiscoveryFile(t, filepath.Join(root, "kept.ts"), "export {}")
	link := filepath.Join(root, "linked")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	configuration := parsedTypeScriptConfig{
		ConfigPath: filepath.Join(root, "tsconfig.json"),
		Directory:  root,
	}
	got, err := resolveTypeScriptSources(configuration, root)
	if err != nil {
		t.Fatalf("resolveTypeScriptSources() error = %v", err)
	}
	want := []string{filepath.Join(root, "kept.ts")}
	if !equalStrings(got, want) {
		t.Fatalf("resolveTypeScriptSources() = %#v, want %#v", got, want)
	}
}

func TestResolveTypeScriptSourcesNodeModulesStaysExcludedEvenViaExplicitInclude(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "node_modules", "pkg", "index.ts"), "export {}")
	writeDiscoveryFile(t, filepath.Join(root, "src", "index.ts"), "export {}")

	configuration := parsedTypeScriptConfig{
		ConfigPath: filepath.Join(root, "tsconfig.json"),
		Directory:  root,
		HasInclude: true,
		Include: []string{
			filepath.Join(root, "node_modules", "pkg", "**", "*.ts"),
			filepath.Join(root, "src", "**", "*.ts"),
		},
	}
	got, err := resolveTypeScriptSources(configuration, root)
	if err != nil {
		t.Fatalf("resolveTypeScriptSources() error = %v", err)
	}
	want := []string{filepath.Join(root, "src", "index.ts")}
	if !equalStrings(got, want) {
		t.Fatalf("resolveTypeScriptSources() = %#v, want %#v", got, want)
	}
}
