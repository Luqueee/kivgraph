//go:build !linux && !darwin

package supervisor

import (
	"errors"
	"testing"
)

// TestAnUnsupportedPlatformSaysSo keeps the absence declared. A caller deciding
// whether to register clients against a daemon needs to know the daemon will
// have no owner here, and a zero value would read as "installed".
func TestAnUnsupportedPlatformSaysSo(t *testing.T) {
	spec := Spec{Executable: "/opt/kivgraph/bin/kivgraph", StateDirectory: t.TempDir()}

	if _, err := Install(spec); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("Install() error = %v, want ErrUnsupportedPlatform", err)
	}
	report, err := Status(spec)
	if err != nil {
		t.Fatalf("Status() error = %v, want no error: an unsupported platform is an answer", err)
	}
	if report.State != StateUnsupported {
		t.Fatalf("Status() = %q, want %q", report.State, StateUnsupported)
	}
	if report.Label == "" {
		t.Fatal("Status() named no label, so the report says nothing an operator can use")
	}
}
