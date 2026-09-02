package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestResolveRepositoriesIncludesDerivedExternalDartPackages(t *testing.T) {
	root := testsupport.TempDir(t)
	external := filepath.Join(root, "external-package")
	if err := os.MkdirAll(filepath.Join(root, ".dart_tool"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	config := `{"configVersion":2,"packages":[{"name":"external","rootUri":"../external-package/"}]}`
	if err := os.WriteFile(filepath.Join(root, ".dart_tool", "package_config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	repositories, err := ResolveRepositories(FullOptions{
		Repositories:        []workspace.Repository{{Name: "app", Path: root, RealPath: root, Languages: []string{"dart"}}},
		DartIncludeExternal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 2 {
		t.Fatalf("effective repositories = %#v, want app and external provider", repositories)
	}
	provider := repositories[1]
	if !provider.Derived || provider.Path != external || !strings.HasPrefix(provider.Name, "dart-package:external:") {
		t.Fatalf("external provider = %#v, want a derived Dart package", provider)
	}
}
