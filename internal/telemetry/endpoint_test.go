package telemetry

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"testing"
)

// refusingTransport fails the test if anything is sent at all, wherever to.
type refusingTransport struct{ t *testing.T }

func (transport refusingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.t.Errorf("a build with no collector sent a request to %s", request.URL)
	return nil, http.ErrUseLastResponse
}

// This distribution has no collector, so the compiled endpoint is empty and a
// released binary -- the one shape that would report -- sends nothing, claims
// no marker and prints no notice, for either emitter.
func TestABuildWithNoCollectorReportsNothing(t *testing.T) {
	if Endpoint != "" {
		t.Skipf("this build reports to %s", Endpoint)
	}
	for name, announce := range map[string]func(context.Context, Options) bool{
		"Announce":                  Announce,
		"AnnounceSupervisorInstall": AnnounceSupervisorInstall,
	} {
		opts := options(t, "")
		opts.Client = &http.Client{Transport: refusingTransport{t}}
		if announce(context.Background(), opts) {
			t.Fatalf("%s() = true with no endpoint", name)
		}
		if entries, err := os.ReadDir(opts.StateDirectory); err != nil || len(entries) != 0 {
			t.Fatalf("%s left %v in the state directory (err %v), want nothing", name, entries, err)
		}
		if notice := opts.Notice.(*bytes.Buffer).String(); notice != "" {
			t.Fatalf("%s printed %q about a report it did not make", name, notice)
		}
	}
}
