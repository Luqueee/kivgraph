package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoveryRejectsInvalidExclusionBeforeWalkingEmptyRepository(t *testing.T) {
	for name, discover := range map[string]func(context.Context, Repository) error{
		"TypeScript": func(ctx context.Context, repository Repository) error {
			_, err := DiscoverTypeScript(ctx, repository)
			return err
		},
		"Go": func(ctx context.Context, repository Repository) error {
			_, err := DiscoverGo(ctx, repository)
			return err
		},
		"Cargo": func(ctx context.Context, repository Repository) error {
			_, err := DiscoverCargo(ctx, repository)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatalf("EvalSymlinks() error = %v", err)
			}
			repository := Repository{RealPath: root, Exclusions: []string{"["}}
			err = discover(context.Background(), repository)
			if err == nil || !strings.Contains(err.Error(), "exclusions[0]") {
				t.Fatalf("%s discovery exclusions=%q error = %v, want invalid exclusion error", name, repository.Exclusions, err)
			}
		})
	}
}
