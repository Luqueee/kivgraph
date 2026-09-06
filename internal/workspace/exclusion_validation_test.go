package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoveryRejectsInvalidExclusionBeforeParsingManifests(t *testing.T) {
	tests := map[string]struct {
		manifest string
		discover func(context.Context, Repository) error
	}{
		"TypeScript": {manifest: "package.json", discover: func(ctx context.Context, repository Repository) error {
			_, err := DiscoverTypeScript(ctx, repository)
			return err
		}},
		"Go": {manifest: "go.mod", discover: func(ctx context.Context, repository Repository) error {
			_, err := DiscoverGo(ctx, repository)
			return err
		}},
		"Cargo": {manifest: "Cargo.toml", discover: func(ctx context.Context, repository Repository) error {
			_, err := DiscoverCargo(ctx, repository)
			return err
		}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatalf("EvalSymlinks() error = %v", err)
			}
			manifestPath := filepath.Join(root, test.manifest)
			if err := os.WriteFile(manifestPath, []byte("not valid ["), 0o600); err != nil {
				t.Fatalf("write malformed manifest %q: %v", manifestPath, err)
			}
			repository := Repository{RealPath: root, Exclusions: []string{"["}}
			err = test.discover(context.Background(), repository)
			if err == nil || !strings.Contains(err.Error(), "exclusions[0]") {
				t.Fatalf("%s discovery manifest=%q exclusions=%q error = %v, want exclusion error before manifest parsing", name, manifestPath, repository.Exclusions, err)
			}
		})
	}
}
