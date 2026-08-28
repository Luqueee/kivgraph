package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestServeParsesIntrospection covers the states of the flag on the one command
// that owns it: absent, present, and present beside the flag it has to coexist
// with. The default matters most -- introspection is opt-in, and a default that
// drifted would change what every existing client is served.
func TestServeParsesIntrospection(t *testing.T) {
	for name, testCase := range map[string]struct {
		arguments         []string
		wantConfigPath    string
		wantIntrospection bool
	}{
		"the default is off":       {arguments: []string{"--config", "foo.yaml"}, wantConfigPath: "foo.yaml"},
		"it can be asked for":      {arguments: []string{"--introspection", "--config", "foo.yaml"}, wantConfigPath: "foo.yaml", wantIntrospection: true},
		"it stands on its own":     {arguments: []string{"--introspection"}, wantIntrospection: true},
		"neither flag is required": {arguments: nil},
	} {
		t.Run(name, func(t *testing.T) {
			var configPath string
			var options serveOptions
			if err := serveFlagSet(&configPath, &options).Parse(testCase.arguments); err != nil {
				t.Fatalf("Parse(%v) error = %v", testCase.arguments, err)
			}
			if configPath != testCase.wantConfigPath {
				t.Errorf("--config = %q, want %q", configPath, testCase.wantConfigPath)
			}
			if options.Introspection != testCase.wantIntrospection {
				t.Errorf("--introspection = %t, want %t", options.Introspection, testCase.wantIntrospection)
			}
		})
	}
}

// TestServeHelpNamesIntrospection is the other half: a flag the help does not
// name is a flag nobody outside this repository can find. `serve --help` renders
// the same flag set the parser reads, so this is what proves the two agree.
func TestServeHelpNamesIntrospection(t *testing.T) {
	var configPath string
	var options serveOptions
	var help bytes.Buffer
	writeCommandHelp(&help, "serve", serveFlagSet(&configPath, &options))
	for _, want := range []string{"--introspection", "--config", "when no index is available"} {
		if !strings.Contains(help.String(), want) {
			t.Errorf("serve --help does not mention %q:\n%s", want, help.String())
		}
	}
}

// TestNoOtherCommandAcquiredIntrospection keeps the flag on the command that was
// scoped to have it. The daemon builds a server per accepted session and nothing
// asked for introspection there; a flag that leaked into another command's set
// would be a surface nobody designed.
func TestNoOtherCommandAcquiredIntrospection(t *testing.T) {
	for _, spec := range allCommands() {
		if spec.name() == "serve" || spec.flags == nil {
			continue
		}
		spec.flags().VisitAll(func(entry *flag.Flag) {
			if entry.Name == "introspection" {
				t.Errorf("%q declares --introspection, which belongs to serve alone", spec.name())
			}
		})
	}
}

// TestIntrospectionDeclinesTheRelay is the flag's one interaction with the rest
// of the command, and without it the flag would be a silent no-op on any
// machine with a daemon: the relay is what `serve` does by default, and a
// daemon nobody asked for introspection publishes no query tool at all when
// nothing is indexed -- which is exactly the state this flag exists to look
// past. Declining is what makes the flag mean something wherever it is set.
func TestIntrospectionDeclinesTheRelay(t *testing.T) {
	testsupport.SetHome(t, t.TempDir())
	if _, err := config.Initialize(config.InitOptions{}); err != nil {
		t.Fatalf("config.Initialize: %v", err)
	}
	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	// A daemon that is answering right now, so nothing but the flag can be
	// what declines: without it this endpoint is the relay's happy path.
	answering := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	defer answering.Close()
	directory := stateDirectory(loaded)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create state directory: %v", err)
	}
	encoded, err := json.Marshal(daemon.Endpoint{URL: answering.URL, Token: "t", PID: os.Getpid()})
	if err != nil {
		t.Fatalf("encode endpoint: %v", err)
	}
	if err := os.WriteFile(daemon.EndpointPath(directory), encoded, 0o600); err != nil {
		t.Fatalf("write endpoint: %v", err)
	}

	// The control, and the reason this test proves anything: without it a
	// decline could just as well mean the fixture was never reachable, and
	// the assertion below would pass with the flag removed. Running the
	// relaying half is not an option -- it would consume this process's own
	// stdio -- so the probe it depends on is what is asserted instead.
	if _, answers := reachableDaemon(context.Background(), loaded); !answers {
		t.Fatal("the fixture daemon is not reachable, so a decline would prove nothing")
	}

	relayed, err := relayToTheDaemon(context.Background(), "serve", "", loaded, nil, true)
	if err != nil {
		t.Fatalf("relayToTheDaemon() error = %v", err)
	}
	if relayed {
		t.Fatal("--introspection relayed to the daemon, so the flag did nothing")
	}
}
