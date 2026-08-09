package workspace

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/testsupport"
)

func TestResolveTypeScriptConfigResolvesSimpleInheritance(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.base.json"), `{
		"compilerOptions": {"strict": true, "target": "ES2022"}
	}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{
		"extends": "./tsconfig.base.json",
		"compilerOptions": {"module": "NodeNext"}
	}`)

	resolved, err := resolveTypeScriptConfig(filepath.Join(root, "tsconfig.json"), root)
	if err != nil {
		t.Fatalf("resolveTypeScriptConfig() error = %v", err)
	}

	wantChain := []string{filepath.Join(root, "tsconfig.base.json")}
	if !equalStrings(resolved.ExtendsChain, wantChain) {
		t.Fatalf("ExtendsChain = %v, want %v", resolved.ExtendsChain, wantChain)
	}
	if resolved.CompilerOptions["strict"] != true {
		t.Fatalf("strict = %v, want true (inherited from the parent)", resolved.CompilerOptions["strict"])
	}
	if resolved.CompilerOptions["target"] != "ES2022" {
		t.Fatalf("target = %v, want ES2022 (inherited from the parent)", resolved.CompilerOptions["target"])
	}
	if resolved.CompilerOptions["module"] != "NodeNext" {
		t.Fatalf("module = %v, want NodeNext (declared by the child)", resolved.CompilerOptions["module"])
	}
	if resolved.HasFiles || resolved.HasInclude || resolved.HasExclude {
		t.Fatalf("Has{Files,Include,Exclude} = %v/%v/%v, want all false: nothing in the chain declares them",
			resolved.HasFiles, resolved.HasInclude, resolved.HasExclude)
	}
	if resolved.Files != nil || resolved.Include != nil || resolved.Exclude != nil {
		t.Fatalf("{Files,Include,Exclude} = %v/%v/%v, want all nil", resolved.Files, resolved.Include, resolved.Exclude)
	}
}

func TestResolveTypeScriptConfigArrayExtendsAppliesLastEntryWins(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.a.json"), `{
		"compilerOptions": {"target": "ES2018", "strict": false}
	}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.b.json"), `{
		"compilerOptions": {"target": "ES2022"}
	}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{
		"extends": ["./tsconfig.a.json", "./tsconfig.b.json"]
	}`)

	resolved, err := resolveTypeScriptConfig(filepath.Join(root, "tsconfig.json"), root)
	if err != nil {
		t.Fatalf("resolveTypeScriptConfig() error = %v", err)
	}

	if resolved.CompilerOptions["target"] != "ES2022" {
		t.Fatalf("target = %v, want ES2022 (the rightmost extends entry wins the conflict)", resolved.CompilerOptions["target"])
	}
	if resolved.CompilerOptions["strict"] != false {
		t.Fatalf("strict = %v, want false (only the leftmost parent declares it, so it survives)", resolved.CompilerOptions["strict"])
	}
	wantChain := []string{filepath.Join(root, "tsconfig.b.json"), filepath.Join(root, "tsconfig.a.json")}
	if !equalStrings(resolved.ExtendsChain, wantChain) {
		t.Fatalf("ExtendsChain = %v, want %v (nearest/highest precedence entry first)", resolved.ExtendsChain, wantChain)
	}
}

func TestResolveTypeScriptConfigAllowsDiamondSharedAncestor(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.base.json"), `{"compilerOptions": {"strict": true}}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.a.json"), `{
		"extends": "./tsconfig.base.json",
		"compilerOptions": {"target": "ES2018"}
	}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.b.json"), `{
		"extends": "./tsconfig.base.json",
		"compilerOptions": {"target": "ES2022"}
	}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{
		"extends": ["./tsconfig.a.json", "./tsconfig.b.json"]
	}`)

	resolved, err := resolveTypeScriptConfig(filepath.Join(root, "tsconfig.json"), root)
	if err != nil {
		t.Fatalf("resolveTypeScriptConfig() error = %v, want the shared ancestor to resolve without a false cycle", err)
	}
	if resolved.CompilerOptions["strict"] != true {
		t.Fatalf("strict = %v, want true (inherited through both branches from the shared ancestor)", resolved.CompilerOptions["strict"])
	}
	if resolved.CompilerOptions["target"] != "ES2022" {
		t.Fatalf("target = %v, want ES2022 (the rightmost branch still wins)", resolved.CompilerOptions["target"])
	}
}

func TestResolveTypeScriptConfigChildOverridesParentCompilerOptions(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.base.json"), `{
		"compilerOptions": {"strict": false, "target": "ES2018"}
	}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{
		"extends": "./tsconfig.base.json",
		"compilerOptions": {"strict": true}
	}`)

	resolved, err := resolveTypeScriptConfig(filepath.Join(root, "tsconfig.json"), root)
	if err != nil {
		t.Fatalf("resolveTypeScriptConfig() error = %v", err)
	}
	if resolved.CompilerOptions["strict"] != true {
		t.Fatalf("strict = %v, want true (the child overrides the parent)", resolved.CompilerOptions["strict"])
	}
	if resolved.CompilerOptions["target"] != "ES2018" {
		t.Fatalf("target = %v, want ES2018 (untouched, inherited from the parent)", resolved.CompilerOptions["target"])
	}
}

func TestResolveTypeScriptConfigChildIncludeReplacesParentInclude(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.base.json"), `{
		"include": ["src/**/*.ts"],
		"exclude": ["src/**/*.spec.ts"]
	}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{
		"extends": "./tsconfig.base.json",
		"include": ["lib/**/*.ts"]
	}`)

	resolved, err := resolveTypeScriptConfig(filepath.Join(root, "tsconfig.json"), root)
	if err != nil {
		t.Fatalf("resolveTypeScriptConfig() error = %v", err)
	}

	if !resolved.HasInclude {
		t.Fatalf("HasInclude = false, want true")
	}
	wantInclude := []string{filepath.Join(root, "lib/**/*.ts")}
	if !equalStrings(resolved.Include, wantInclude) {
		t.Fatalf("Include = %v, want %v (the child's include fully replaces the parent's, it does not merge with it)", resolved.Include, wantInclude)
	}
	// Exclude was never redeclared by the child, so it is still inherited
	// from the parent untouched.
	if !resolved.HasExclude {
		t.Fatalf("HasExclude = false, want true (inherited from the parent)")
	}
	wantExclude := []string{filepath.Join(root, "src/**/*.spec.ts")}
	if !equalStrings(resolved.Exclude, wantExclude) {
		t.Fatalf("Exclude = %v, want %v (inherited from the parent)", resolved.Exclude, wantExclude)
	}
}

func TestResolveTypeScriptConfigRebasesInheritedOutDirAgainstParentDirectory(t *testing.T) {
	root := testsupport.TempDir(t)
	baseDirectory := filepath.Join(root, "base")
	packageDirectory := filepath.Join(root, "packages", "app")
	writeDiscoveryFile(t, filepath.Join(baseDirectory, "tsconfig.json"), `{
		"compilerOptions": {"outDir": "./dist", "rootDir": "./src"}
	}`)
	writeDiscoveryFile(t, filepath.Join(packageDirectory, "tsconfig.json"), `{
		"extends": "../../base/tsconfig.json"
	}`)

	resolved, err := resolveTypeScriptConfig(filepath.Join(packageDirectory, "tsconfig.json"), root)
	if err != nil {
		t.Fatalf("resolveTypeScriptConfig() error = %v", err)
	}

	wantOutDir := filepath.Join(baseDirectory, "dist")
	if resolved.CompilerOptions["outDir"] != wantOutDir {
		t.Fatalf("outDir = %v, want %q (rebased against the declaring parent's directory, not the child's)", resolved.CompilerOptions["outDir"], wantOutDir)
	}
	wantRootDir := filepath.Join(baseDirectory, "src")
	if resolved.CompilerOptions["rootDir"] != wantRootDir {
		t.Fatalf("rootDir = %v, want %q (rebased against the declaring parent's directory, not the child's)", resolved.CompilerOptions["rootDir"], wantRootDir)
	}
}

func TestResolveTypeScriptConfigExtendsThroughNodeModules(t *testing.T) {
	root := testsupport.TempDir(t)
	packageDirectory := filepath.Join(root, "packages", "app")
	writeDiscoveryFile(t, filepath.Join(root, "node_modules", "@acme", "tsconfig-base", "tsconfig.json"), `{
		"compilerOptions": {"strict": true, "target": "ES2022"}
	}`)
	writeDiscoveryFile(t, filepath.Join(packageDirectory, "tsconfig.json"), `{"extends": "@acme/tsconfig-base"}`)

	resolved, err := resolveTypeScriptConfig(filepath.Join(packageDirectory, "tsconfig.json"), root)
	if err != nil {
		t.Fatalf("resolveTypeScriptConfig() error = %v", err)
	}

	wantChain := []string{filepath.Join(root, "node_modules", "@acme", "tsconfig-base", "tsconfig.json")}
	if !equalStrings(resolved.ExtendsChain, wantChain) {
		t.Fatalf("ExtendsChain = %v, want %v (found by walking up to the node_modules at the repository root)", resolved.ExtendsChain, wantChain)
	}
	if resolved.CompilerOptions["strict"] != true || resolved.CompilerOptions["target"] != "ES2022" {
		t.Fatalf("CompilerOptions = %#v, want strict/target inherited from the node_modules package", resolved.CompilerOptions)
	}
}

func TestResolveTypeScriptConfigExtendsWithoutJSONExtension(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.base.json"), `{"compilerOptions": {"strict": true}}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"extends": "./tsconfig.base"}`)

	resolved, err := resolveTypeScriptConfig(filepath.Join(root, "tsconfig.json"), root)
	if err != nil {
		t.Fatalf("resolveTypeScriptConfig() error = %v", err)
	}

	wantChain := []string{filepath.Join(root, "tsconfig.base.json")}
	if !equalStrings(resolved.ExtendsChain, wantChain) {
		t.Fatalf("ExtendsChain = %v, want %v (resolved by appending .json)", resolved.ExtendsChain, wantChain)
	}
	if resolved.CompilerOptions["strict"] != true {
		t.Fatalf("strict = %v, want true (inherited)", resolved.CompilerOptions["strict"])
	}
}

func TestResolveTypeScriptConfigDetectsExtendsCycle(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.a.json"), `{"extends": "./tsconfig.b.json"}`)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.b.json"), `{"extends": "./tsconfig.a.json"}`)

	_, err := resolveTypeScriptConfig(filepath.Join(root, "tsconfig.a.json"), root)
	if err == nil {
		t.Fatalf("resolveTypeScriptConfig() error = nil, want a cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("resolveTypeScriptConfig() error = %v, want it to mention the cycle", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(root, "tsconfig.a.json")) || !strings.Contains(err.Error(), filepath.Join(root, "tsconfig.b.json")) {
		t.Fatalf("resolveTypeScriptConfig() error = %v, want it to name both configs on the cycle", err)
	}
}

func TestResolveTypeScriptConfigLeavesPathsMappingRelativeToBaseURL(t *testing.T) {
	root := testsupport.TempDir(t)
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{
		"compilerOptions": {
			"baseUrl": ".",
			"paths": {"@app/*": ["src/*"], "@lib/*": ["../shared/*"]}
		}
	}`)

	resolved, err := resolveTypeScriptConfig(filepath.Join(root, "tsconfig.json"), root)
	if err != nil {
		t.Fatalf("resolveTypeScriptConfig() error = %v", err)
	}

	if resolved.CompilerOptions["baseUrl"] != root {
		t.Fatalf("baseUrl = %v, want %q (absolutized)", resolved.CompilerOptions["baseUrl"], root)
	}
	pathsValue, ok := resolved.CompilerOptions["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths = %#v, want a map", resolved.CompilerOptions["paths"])
	}
	appEntries, ok := pathsValue["@app/*"].([]any)
	if !ok || len(appEntries) != 1 || appEntries[0] != "src/*" {
		t.Fatalf(`paths["@app/*"] = %#v, want ["src/*"] untouched (paths is never absolutized)`, pathsValue["@app/*"])
	}
	libEntries, ok := pathsValue["@lib/*"].([]any)
	if !ok || len(libEntries) != 1 || libEntries[0] != "../shared/*" {
		t.Fatalf(`paths["@lib/*"] = %#v, want ["../shared/*"] untouched (paths is never absolutized)`, pathsValue["@lib/*"])
	}
}

func TestResolveTypeScriptConfigRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, root string) string // returns the configPath to resolve
		wantError  string
		wantErrorB string // second required substring, checked when non-empty
	}{
		{
			name: "config path not absolute",
			setup: func(t *testing.T, root string) string {
				writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{}`)
				return "tsconfig.json"
			},
			wantError: "must be absolute",
		},
		{
			name: "config path escapes repository root",
			setup: func(t *testing.T, root string) string {
				outside := testsupport.TempDir(t)
				writeDiscoveryFile(t, filepath.Join(outside, "tsconfig.json"), `{}`)
				return filepath.Join(outside, "tsconfig.json")
			},
			wantError: "escapes repository root",
		},
		{
			name: "unresolvable relative extends",
			setup: func(t *testing.T, root string) string {
				writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"extends": "./does-not-exist"}`)
				return filepath.Join(root, "tsconfig.json")
			},
			wantError:  "does-not-exist",
			wantErrorB: `"extends" entry`,
		},
		{
			name: "unresolvable node module extends",
			setup: func(t *testing.T, root string) string {
				writeDiscoveryFile(t, filepath.Join(root, "tsconfig.json"), `{"extends": "@acme/missing-package"}`)
				return filepath.Join(root, "tsconfig.json")
			},
			wantError:  "@acme/missing-package",
			wantErrorB: `"extends" entry`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := testsupport.TempDir(t)
			configPath := test.setup(t, root)
			_, err := resolveTypeScriptConfig(configPath, root)
			if err == nil {
				t.Fatalf("resolveTypeScriptConfig() error = nil, want an error containing %q", test.wantError)
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("resolveTypeScriptConfig() error = %v, want substring %q", err, test.wantError)
			}
			if test.wantErrorB != "" && !strings.Contains(err.Error(), test.wantErrorB) {
				t.Fatalf("resolveTypeScriptConfig() error = %v, want substring %q", err, test.wantErrorB)
			}
		})
	}
}

func TestCloneCompilerOptionsDeepCopiesNestedValues(t *testing.T) {
	if clone := cloneCompilerOptions(nil); clone != nil {
		t.Fatalf("cloneCompilerOptions(nil) = %#v, want nil", clone)
	}

	original := map[string]any{
		"strict": true,
		"paths": map[string]any{
			"@app/*": []any{"src/*"},
		},
		"typeRoots": []any{"./types"},
	}
	clone := cloneCompilerOptions(original)
	if !reflect.DeepEqual(clone, original) {
		t.Fatalf("clone = %#v, want a deep-equal copy of %#v", clone, original)
	}

	// Mutating the clone's nested map/slice values in place must never be
	// observable through the original: a shallow top-level copy would still
	// share these backing arrays and maps with it.
	clone["typeRoots"].([]any)[0] = "mutated"
	clone["paths"].(map[string]any)["@app/*"].([]any)[0] = "mutated"
	clone["paths"].(map[string]any)["@new/*"] = []any{"new/*"}

	if original["typeRoots"].([]any)[0] != "./types" {
		t.Fatalf("original typeRoots mutated through the clone: %#v", original["typeRoots"])
	}
	originalPaths := original["paths"].(map[string]any)
	if len(originalPaths) != 1 {
		t.Fatalf("original paths gained a key through the clone: %#v", originalPaths)
	}
	if originalPaths["@app/*"].([]any)[0] != "src/*" {
		t.Fatalf("original paths[@app/*] mutated through the clone: %#v", originalPaths["@app/*"])
	}
}
