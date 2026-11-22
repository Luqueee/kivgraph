package indexer

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// An `.astro` page imports like any module and nothing reads it. Its uses were
// in no edge and no failure; the file and the exported names it writes are
// declared, and nothing else is.
func TestDeclareUnreadComponentsNamesTheExportedNamesAFileWrites(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"src/pages/index.astro":        "---\nimport { loadPosts } from '../lib/posts'\nconst posts = loadPosts()\n---\n<Layout title=\"x\">{posts.map(render)}</Layout>\n",
		"src/pages/about.astro":        "---\n---\n<p>nothing of this package</p>\n",
		"src/components/Card.vue":      "<script setup>\nimport { render } from '../lib/posts'\n</script>\n",
		"src/components/Old.svelte":    "<script>privateHelper()</script>\n",
		"src/lib/posts.ts":             "export function loadPosts() {}\n",
		"node_modules/dep/Page.astro":  "loadPosts\n",
		".astro/generated/Page.astro":  "loadPosts\n",
		"vendored/Page.astro":          "loadPosts\n",
		"packages/other/package.json":  "{}",
		"packages/other/src/App.astro": "loadPosts\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	repositoryKey := facts.RepositoryKey("site")
	packageKey := facts.PackageKey(facts.LanguageTypeScript, "site", "site")
	fileKey := facts.FileKey("site", "src/lib/posts.ts")
	newSet := func() facts.Set {
		return facts.Set{
			Repositories: []facts.Repository{{Key: repositoryKey, Name: "site", RootPath: root, Languages: []facts.Language{facts.LanguageTypeScript}}},
			Packages:     []facts.Package{{Key: packageKey, RepositoryKey: repositoryKey, Language: facts.LanguageTypeScript, Name: "site"}},
			Files:        []facts.File{{Key: fileKey, RepositoryKey: repositoryKey, PackageKey: packageKey, Path: "src/lib/posts.ts", Language: facts.LanguageTypeScript}},
			Symbols: []facts.Symbol{
				{Key: "s1", RepositoryKey: repositoryKey, PackageKey: packageKey, FileKey: fileKey, Language: facts.LanguageTypeScript, Name: "loadPosts", Exported: true},
				{Key: "s2", RepositoryKey: repositoryKey, PackageKey: packageKey, FileKey: fileKey, Language: facts.LanguageTypeScript, Name: "render", Exported: true},
				{Key: "s3", RepositoryKey: repositoryKey, PackageKey: packageKey, FileKey: fileKey, Language: facts.LanguageTypeScript, Name: "privateHelper"},
				{Key: "s4", RepositoryKey: repositoryKey, PackageKey: packageKey, FileKey: fileKey, Language: facts.LanguageTypeScript, Name: "default", Exported: true},
			},
		}
	}
	repository := workspace.Repository{Name: "site", RealPath: root, Exclusions: []string{"vendored"}}

	set := newSet()
	declareUnreadComponents(&set, repository, root)
	var rows []string
	for _, unresolved := range set.Unresolved {
		if unresolved.Reason != UnresolvedComponentNotRead || unresolved.Language != facts.LanguageTypeScript {
			t.Errorf("row = %#v", unresolved)
		}
		rows = append(rows, strings.TrimPrefix(unresolved.FileKey, facts.FileKey("site", ""))+" "+unresolved.RequestedSymbol+" "+string(rune('0'+unresolved.Start.Line)))
	}
	want := []string{
		"src/components/Card.vue render 2",
		"src/pages/index.astro loadPosts 2",
		"src/pages/index.astro render 5",
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("rows = %v, want %v", rows, want)
	}
	// The files the rows name are published, and only those.
	var files []string
	for _, file := range set.Files {
		files = append(files, file.Path)
	}
	if want := []string{"src/components/Card.vue", "src/lib/posts.ts", "src/pages/index.astro"}; !reflect.DeepEqual(files, want) {
		t.Errorf("files = %v, want %v", files, want)
	}

	// Nothing exported: nothing to look for.
	bare := newSet()
	bare.Symbols = bare.Symbols[2:3]
	declareUnreadComponents(&bare, repository, root)
	if len(bare.Unresolved) != 0 || len(bare.Files) != 1 {
		t.Errorf("with nothing exported: %d rows, %d files", len(bare.Unresolved), len(bare.Files))
	}
	// A file too large to read for words is not read.
	large := newSet()
	if err := os.WriteFile(filepath.Join(root, "src/pages/huge.astro"), []byte("loadPosts\n"+strings.Repeat("x", maximumComponentFile)), 0o600); err != nil {
		t.Fatal(err)
	}
	declareUnreadComponents(&large, repository, root)
	for _, file := range large.Files {
		if file.Path == "src/pages/huge.astro" {
			t.Error("a file above the limit was read")
		}
	}
}
