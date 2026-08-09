package indexing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeProjectCanonicalizesAcceptedInput(t *testing.T) {
	root := t.TempDir()
	projectRoot := filepath.Join(root, "project")
	if err := os.Mkdir(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := normalizeProject(Project{
		Name:      " demo ",
		Path:      "project",
		Languages: []string{" Go "},
	}, root)
	if err != nil {
		t.Fatalf("normalizeProject() error = %v", err)
	}
	if got.Name != "demo" || got.Path != projectRoot || len(got.Languages) != 1 || got.Languages[0] != "go" {
		t.Fatalf("normalizeProject() = %#v", got)
	}
}

func TestNormalizeProjectRejectsInvalidInputs(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "file")
	if err := os.WriteFile(filePath, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		in   Project
		want string
	}{
		{
			name: "empty name",
			in:   Project{Path: root, Languages: []string{"go"}},
			want: "project name must not be empty",
		},
		{
			name: "path separator in name",
			in:   Project{Name: "bad/name", Path: root, Languages: []string{"go"}},
			want: "not a valid repository identifier",
		},
		{
			name: "empty path",
			in:   Project{Name: "demo", Languages: []string{"go"}},
			want: "project path must not be empty",
		},
		{
			name: "relative path without working directory",
			in:   Project{Name: "demo", Path: ".", Languages: []string{"go"}},
			want: "working directory is unavailable",
		},
		{
			name: "missing path",
			in:   Project{Name: "demo", Path: filepath.Join(root, "missing"), Languages: []string{"go"}},
			want: "inspect project path",
		},
		{
			name: "file path",
			in:   Project{Name: "demo", Path: filePath, Languages: []string{"go"}},
			want: "is not a directory",
		},
		{
			name: "no languages",
			in:   Project{Name: "demo", Path: root},
			want: "must contain at least one language",
		},
		{
			name: "unsupported language",
			in:   Project{Name: "demo", Path: root, Languages: []string{"rust"}},
			want: "unsupported language",
		},
		{
			name: "duplicate language",
			in:   Project{Name: "demo", Path: root, Languages: []string{"go", " Go "}},
			want: "duplicate language",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeProject(test.in, "")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalizeProject() error = %v, want substring %q", err, test.want)
			}
		})
	}
}
