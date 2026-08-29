// Package telemetry reports that a version of Kivgraph ran on a machine for
// the first time, once, and nothing else.
//
// What it measures and why it is not called *installations* is in
// `docs/adr/0083-a-download-is-not-a-person.md`; the fields it sends are
// documented in `docs/development/analytics.md` and published on
// https://kivgraph.dev/telemetry/. The endpoint that receives it is
// `landing/src/install-report.mjs`.
//
// Three properties hold everywhere in this package, and each of them is a
// failure that would be invisible without a test:
//
//   - **nothing reaches stdout.** `kivgraph serve` runs the MCP surface over
//     stdio, where one stray byte corrupts the session. The daemon serves the
//     same surface over HTTP, where it does not, and this code is shared: it
//     obeys the stricter of the two everywhere. The notice goes to the writer
//     the caller passes, which is stderr;
//   - **the marker is a create, not a read.** stdio starts bursts -- ADR 0069
//     measured `69` starts of `serve` with `8` alive at once -- so a
//     read-then-write would let a whole burst find the marker absent and
//     report before any of them had created it. One install would become as
//     many pings as the client happened to spawn;
//   - **nothing here can fail a command.** Every error is dropped. A machine
//     with no network runs Kivgraph exactly as one with it.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Endpoint is where a ping goes.
const Endpoint = "https://kivgraph.dev/api/telemetry/first-run"

// DisableEnv turns the whole package off when it is set to "0".
//
// One variable, on the reader's side, and it is checked before anything is
// written: a machine that has opted out leaves no marker either, so opting
// back in reports the version it is running rather than nothing.
const DisableEnv = "KIVGRAPH_TELEMETRY"

// sendTimeout bounds a ping. It is short because nothing waits for it and a
// slow endpoint must not keep a goroutine alive across a session.
const sendTimeout = 2 * time.Second

// Options is what a caller has to know to report a first run.
type Options struct {
	// StateDirectory is where the marker lives. It is the state directory and
	// not the bundle root because an update replaces the bundle: a marker
	// there would fire again on every update, and the number would stop being
	// "the first run of a version".
	StateDirectory string
	// Version is the compiled version.Value.
	Version string
	// Transport is "stdio" or "daemon": which arrangement is about to serve.
	Transport string
	// Executable is os.Executable(); the channel is read from its layout.
	Executable string
	// Notice receives the one-time line telling the reader what was sent.
	// Never os.Stdout.
	Notice io.Writer

	// The seams, all optional. They exist so a test can drive the whole
	// function without a network, a home directory or a real clock.
	Endpoint string
	Client   *http.Client
	Getenv   func(string) string
}

// ping is the payload the endpoint validates. The field names and their closed
// sets are the contract; `landing/src/install-report.mjs` refuses anything
// else, including a sixth field.
type ping struct {
	Emitter   string `json:"emitter"`
	Version   string `json:"version"`
	Platform  string `json:"platform"`
	Channel   string `json:"channel"`
	Transport string `json:"transport"`
}

// Announce reports the first run of this version, or does nothing.
//
// It returns whether a ping was sent, which is for tests and for a caller that
// wants to log it; every caller in this repository ignores it. It is
// synchronous: a caller that must not wait runs it in a goroutine, and the
// timeout above is what bounds that goroutine.
func Announce(ctx context.Context, options Options) bool {
	getenv := options.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	// Checked first, before the marker: an opted-out machine leaves no trace
	// of this package at all.
	if getenv(DisableEnv) == "0" {
		return false
	}
	if options.StateDirectory == "" || options.Version == "" {
		return false
	}
	// A binary that is not part of a release does not report, and the marker
	// is not claimed either -- installing a release later should report it.
	//
	// This is not tidiness. Nothing in the layout distinguishes a developer's
	// `go build` from a CI job's, and this repository's own CI builds and runs
	// the binary on every push across five platforms: counting those would
	// make the number mostly us. The cost is that a `go install` user is
	// invisible, which is a declared hole rather than a silent one.
	channel := channelOf(options.Executable)
	if channel == "" {
		return false
	}

	created, err := claimTheVersion(options.StateDirectory, options.Version)
	if err != nil || !created {
		return false
	}

	body := ping{
		Emitter:   "binary",
		Version:   options.Version,
		Platform:  Platform(),
		Channel:   channel,
		Transport: options.Transport,
	}
	notice(options.Notice, body)
	return send(ctx, options, body)
}

// claimTheVersion creates the marker and reports whether this call created it.
//
// `O_EXCL` is the whole function. Two processes racing here get exactly one
// `true`, which is what makes a burst of eight `serve` starts one ping.
func claimTheVersion(stateDirectory, version string) (bool, error) {
	directory := filepath.Join(stateDirectory, "first-run")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return false, err
	}
	marker := filepath.Join(directory, version)
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer file.Close()
	// The timestamp is for whoever finds the file and wonders what it is. The
	// file's existence is the fact; its contents are a courtesy.
	_, _ = fmt.Fprintf(file, "%s\n", time.Now().UTC().Format(time.RFC3339))
	return true, nil
}

// Platform names the build, in the vocabulary the release assets use.
//
// The same string appears in `kivgraph-linux-amd64.tar.gz` and in the download
// series of Layer 0, so the two layers can be read side by side rather than
// joined by hand.
func Platform() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// channelOf says how this executable arrived, read from what is around it, or
// returns "" when it did not arrive as a release at all.
//
// An MCP bundle unpacks as `<extension>/manifest.json` beside
// `<extension>/server/bin/kivgraph`, and a release archive as
// `<root>/manifest.json` beside `<root>/bin/kivgraph`. So the bundle manifest
// two directories up says "released", and an MCPB manifest three directories
// up -- the one with an `mcp_config` -- says which of the two.
//
// `archive` covers both a hand-extracted archive and one the installer placed,
// because nothing in the layout distinguishes them. It does not need to: the
// installer reports its own row, with `emitter: installer`.
func channelOf(executable string) string {
	if executable == "" {
		return ""
	}
	resolved, err := filepath.Abs(executable)
	if err != nil {
		return ""
	}
	if evaluated, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = evaluated
	}
	bundleRoot := filepath.Dir(filepath.Dir(resolved))
	if !isFile(filepath.Join(bundleRoot, "manifest.json")) {
		return ""
	}
	if isExtensionManifest(filepath.Join(filepath.Dir(bundleRoot), "manifest.json")) {
		return "mcpb"
	}
	return "archive"
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// isExtensionManifest reports whether path is an MCPB manifest.
//
// It reads a key rather than trusting the name, because a release archive
// extracted into a directory that happens to hold an unrelated
// `manifest.json` would otherwise be counted as a bundle install. `mcp_config`
// is the key manifest 0.3 has and the bundle manifest does not.
func isExtensionManifest(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var manifest struct {
		Server struct {
			MCPConfig json.RawMessage `json:"mcp_config"`
		} `json:"server"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false
	}
	return len(manifest.Server.MCPConfig) > 0
}

// notice tells the reader, once, what just happened.
//
// It names the variable rather than describing a setting, so the answer to
// "how do I stop this" is on the same line as the thing that prompted the
// question.
func notice(writer io.Writer, body ping) {
	if writer == nil {
		return
	}
	fmt.Fprintf(writer,
		"kivgraph: first run of %s on this machine: reporting version, platform (%s), channel (%s) and transport (%s), and nothing else.\n"+
			"kivgraph: no identifier is created and nothing about your code is sent. https://kivgraph.dev/telemetry/\n"+
			"kivgraph: set %s=0 to turn it off.\n",
		body.Version, body.Platform, body.Channel, body.Transport, DisableEnv)
}

// send posts the ping and drops every failure.
func send(ctx context.Context, options Options, body ping) bool {
	endpoint := options.Endpoint
	if endpoint == "" {
		endpoint = Endpoint
	}
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: sendTimeout}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return false
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	// Drained so the connection can be reused, and discarded because the
	// endpoint answers 204 to everything and there is nothing in it to read.
	_, _ = io.Copy(io.Discard, response.Body)
	return true
}
