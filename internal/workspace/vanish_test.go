package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestVisitUnlessVanished(t *testing.T) {
	present := filepath.Join(t.TempDir(), "present")
	if err := os.WriteFile(present, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(t.TempDir(), "gone")
	other := errors.New("malformed")

	tests := []struct {
		name string
		path string
		err  error
		want error
	}{
		{"visit succeeds", present, nil, nil},
		{"a file that is gone is skipped", gone, fmt.Errorf("parse: %w", fs.ErrNotExist), nil},
		{"a missing reference from a file that is there is a failure", present, fmt.Errorf("parse: %w", fs.ErrNotExist), fs.ErrNotExist},
		{"another error is a failure whether or not the file is there", gone, other, other},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := visitUnlessVanished(test.path, func(string) error { return test.err })
			if !errors.Is(got, test.want) || (test.want == nil) != (got == nil) {
				t.Fatalf("visitUnlessVanished = %v, want %v", got, test.want)
			}
		})
	}
}
