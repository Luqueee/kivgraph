package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/procstat"
)

// results is the published artifact. It has to carry enough to be read years
// later without this checkout: the command, the commit, the machine, the corpus,
// the seed, the metrics and what the run could not answer.
type results struct {
	Benchmark     string `json:"benchmark"`
	Date          string `json:"date"`
	SchemaVersion string `json:"schema_version"`
	Commit        string `json:"commit"`
	Digest        string `json:"digest"`
	Command       string `json:"command"`
	SnapshotID    uint64 `json:"snapshot_id"`
	Calls         int    `json:"calls"`
	Warmup        int    `json:"warmup"`
	Seed          int64  `json:"seed"`

	Environment environment  `json:"environment"`
	Snapshot    snapshotFile `json:"snapshot"`
	Points      []point      `json:"points"`
	// Slopes are the answer. No single client count can give it: an
	// arrangement that saved at two clients and not at eight would look like a
	// win in any one row, and the row a reader picks would decide the ADR.
	Slopes      slopes   `json:"slopes"`
	Verdict     verdict  `json:"verdict"`
	Limitations []string `json:"limitations"`
}

// point is one client count measured on all three arms.
type point struct {
	Clients     int         `json:"clients"`
	Arms        []arm       `json:"arms"`
	RelayVSServ comparison  `json:"relay_vs_serve"`
	RelayTax    comparison  `json:"relay_vs_daemon"`
	Waits       waitSummary `json:"waits"`
}

type environment struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	CPUs          int    `json:"cpus"`
	Kernel        string `json:"kernel"`
	ServerVersion string `json:"server_version"`
	// PrivateDirtySupported says whether this platform reports the per-process
	// private half at all. Without it the benchmark cannot answer its own
	// question: the resident size of a process mapping a shared file counts
	// pages the machine pays for once.
	PrivateDirtySupported bool `json:"private_dirty_supported"`
}

type snapshotFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type processSample struct {
	Index            int    `json:"index"`
	PID              int    `json:"pid"`
	Role             string `json:"role"`
	ResidentBytes    int64  `json:"resident_bytes"`
	ProportionalByte int64  `json:"proportional_bytes"`
	SharedCleanByte  int64  `json:"shared_clean_bytes"`
	PrivateDirtyByte int64  `json:"private_dirty_bytes"`
	PeakBytes        int64  `json:"peak_bytes"`
}

type armTotals struct {
	ResidentBytes     int64 `json:"resident_bytes"`
	ProportionalByte  int64 `json:"proportional_bytes"`
	SharedCleanByte   int64 `json:"shared_clean_bytes"`
	PrivateDirtyByte  int64 `json:"private_dirty_bytes"`
	PeakBytes         int64 `json:"peak_bytes"`
	WorstPrivateDirty int64 `json:"worst_private_dirty_bytes"`
}

// latency describes the answered calls. The percentiles are pointers because a
// run that asks nothing has no percentile, and a zero would read as an instant
// answer rather than as no answer at all.
type latency struct {
	Calls int      `json:"calls"`
	P50MS *float64 `json:"p50_ms,omitempty"`
	P95MS *float64 `json:"p95_ms,omitempty"`
	P99MS *float64 `json:"p99_ms,omitempty"`
}

type arm struct {
	Name       string `json:"name"`
	SnapshotID uint64 `json:"snapshot_id"`
	Symbols    int    `json:"symbols"`
	Sessions   int    `json:"sessions"`
	// Clients are the per-client processes: a `serve` each in the serve arm, a
	// relay each in the relay arm, and none in the daemon arm, where a client
	// is a socket and not a process.
	Clients []processSample `json:"clients"`
	// Server is the one process behind them, absent in the serve arm because
	// there is nothing behind a `serve`.
	Server *processSample `json:"server,omitempty"`
	// ClientTotals is the per-client half on its own. In the relay arm it is
	// the number the ADR gated itself on: what a relay costs that a direct HTTP
	// session would not.
	ClientTotals armTotals `json:"client_totals"`
	// Totals is the whole arrangement, clients and server together, which is
	// what the machine actually pays.
	Totals     armTotals `json:"totals"`
	Latency    latency   `json:"latency"`
	CallErrors int       `json:"call_errors"`
	// FirstAnswersMS is what each client waited to be answered, in connection
	// order. An idle run times no answer, so this is empty rather than zeros.
	FirstAnswersMS []float64 `json:"first_answers_ms"`
	// NewClientMS is what one more client waits to be answered once the arm is
	// already running. Absent when the run asks nothing.
	NewClientMS *float64 `json:"new_client_ms,omitempty"`
	// NewClientConnectMS is spawn plus handshake for that client, measured
	// under every load. It is the field ADR 0084's third question reads.
	NewClientConnectMS float64 `json:"new_client_connect_ms"`
}

type comparison struct {
	// PrivateDirtyShare is the first arm's private half over the second's.
	// Below one is a saving.
	PrivateDirtyShare float64 `json:"private_dirty_share"`
	PrivateDirtySaved int64   `json:"private_dirty_saved_bytes"`
	PeakShare         float64 `json:"peak_share"`
}

// waitSummary is the third question: what a client waits to be usable, on each
// arrangement, at this client count.
type waitSummary struct {
	ServeConnectMS  float64  `json:"serve_connect_ms"`
	RelayConnectMS  float64  `json:"relay_connect_ms"`
	DaemonConnectMS float64  `json:"daemon_connect_ms"`
	ServeAnswerMS   *float64 `json:"serve_answer_ms,omitempty"`
	RelayAnswerMS   *float64 `json:"relay_answer_ms,omitempty"`
	DaemonAnswerMS  *float64 `json:"daemon_answer_ms,omitempty"`
}

// armSlope is a straight line fitted to one arm against the number of clients.
//
// The intercept is the fixed cost and the slope is what each further client
// adds. The pair is the question: the serve arm should slope by a whole server,
// and a relay whose slope approached it would buy nothing.
type armSlope struct {
	Name string `json:"name"`
	// PerClientBytes is the least-squares slope of the whole arrangement.
	PerClientBytes float64 `json:"private_dirty_bytes_per_client"`
	// FixedBytes is the fitted intercept: the cost before any client.
	FixedBytes float64 `json:"private_dirty_fixed_bytes"`
	// ClientPerClientBytes is the same slope over the per-client processes
	// alone. In the relay arm this is the relay's own floor with the daemon's
	// growth taken out of it.
	ClientPerClientBytes float64 `json:"client_private_dirty_bytes_per_client"`
	// Marginals are the step-by-step differences, so a reader can see whether
	// the line is a fair summary or hides a knee.
	Marginals []marginal `json:"marginals"`
}

// marginal is what going from one measured count to the next added.
type marginal struct {
	From      int   `json:"from_clients"`
	To        int   `json:"to_clients"`
	PerClient int64 `json:"private_dirty_bytes_per_client"`
}

type slopes struct {
	Arms []armSlope `json:"arms"`
	// RelayOverServe is the relay arrangement's per-client cost over the serve
	// arrangement's. Near one means the relay pays per client what a whole
	// server does, and the change buys nothing.
	RelayOverServe float64 `json:"relay_over_serve"`
	// RelayOverDaemon is the same against a direct HTTP session, which is the
	// cheapest a client could ever be. It is the price of keeping the stdio
	// entry the .mcpb format forces.
	RelayOverDaemon float64 `json:"relay_over_daemon"`
	// SavedBytesPerClient is the gap the relay opens against a `serve`, per
	// client. This is the figure the ADR's gate reads.
	SavedBytesPerClient float64 `json:"saved_bytes_per_client"`
}

// verdict is the gate ADR 0084 wrote for itself, evaluated rather than left to
// a reader: commit 2 of LUQUE-2233 exists only if the floor opens a gap.
//
// It states a threshold and the measurement, and it does not hide the case it
// cannot decide. A benchmark that only printed the bytes would leave the gate
// to whoever quoted it, which is how the `4 MB` figure this ADR had to retract
// got written in the first place.
type verdict struct {
	// ThresholdBytesPerClient is what the gap has to clear. It is a choice, not
	// a measurement, and it is recorded so a later run can be judged the same
	// way rather than against a number someone remembers.
	ThresholdBytesPerClient float64 `json:"threshold_bytes_per_client"`
	SavedBytesPerClient     float64 `json:"saved_bytes_per_client"`
	// Proceed is whether the saving clears the threshold. False is a real
	// answer here: it closes the ficha and keeps the stdio server.
	Proceed bool   `json:"proceed"`
	Reason  string `json:"reason"`
}

func totalsOf(samples []processSample) armTotals {
	var totals armTotals
	for _, sample := range samples {
		totals.ResidentBytes += sample.ResidentBytes
		totals.ProportionalByte += sample.ProportionalByte
		totals.SharedCleanByte += sample.SharedCleanByte
		totals.PrivateDirtyByte += sample.PrivateDirtyByte
		totals.PeakBytes += sample.PeakBytes
		if sample.PrivateDirtyByte > totals.WorstPrivateDirty {
			totals.WorstPrivateDirty = sample.PrivateDirtyByte
		}
	}
	return totals
}

func compare(first, second arm) comparison {
	return comparison{
		PrivateDirtyShare: ratio(float64(first.Totals.PrivateDirtyByte), float64(second.Totals.PrivateDirtyByte)),
		PrivateDirtySaved: second.Totals.PrivateDirtyByte - first.Totals.PrivateDirtyByte,
		PeakShare:         ratio(float64(first.Totals.PeakBytes), float64(second.Totals.PeakBytes)),
	}
}

func ratio(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

func armByName(measured point, name string) (arm, bool) {
	for _, one := range measured.Arms {
		if one.Name == name {
			return one, true
		}
	}
	return arm{}, false
}

func measuredClients(out results) []int {
	counts := make([]int, 0, len(out.Points))
	for _, measured := range out.Points {
		counts = append(counts, measured.Clients)
	}
	sort.Ints(counts)
	return counts
}

func slopesOf(out results) slopes {
	computed := slopes{}
	for _, name := range armNames {
		xs := make([]float64, 0, len(out.Points))
		totals := make([]float64, 0, len(out.Points))
		clientsOnly := make([]float64, 0, len(out.Points))
		fitted := armSlope{Name: name}
		var previous *point
		for index := range out.Points {
			measured := out.Points[index]
			one, ok := armByName(measured, name)
			if !ok {
				continue
			}
			xs = append(xs, float64(measured.Clients))
			totals = append(totals, float64(one.Totals.PrivateDirtyByte))
			clientsOnly = append(clientsOnly, float64(one.ClientTotals.PrivateDirtyByte))
			if previous != nil {
				before, okBefore := armByName(*previous, name)
				if okBefore && measured.Clients != previous.Clients {
					step := measured.Clients - previous.Clients
					fitted.Marginals = append(fitted.Marginals, marginal{
						From:      previous.Clients,
						To:        measured.Clients,
						PerClient: (one.Totals.PrivateDirtyByte - before.Totals.PrivateDirtyByte) / int64(step),
					})
				}
			}
			previous = &out.Points[index]
		}
		fitted.PerClientBytes, fitted.FixedBytes = leastSquares(xs, totals)
		fitted.ClientPerClientBytes, _ = leastSquares(xs, clientsOnly)
		computed.Arms = append(computed.Arms, fitted)
	}

	serve, okServe := slopeByName(computed, armServe)
	relay, okRelay := slopeByName(computed, armRelay)
	served, okDaemon := slopeByName(computed, armDaemon)
	if okServe && okRelay {
		computed.RelayOverServe = ratio(relay.PerClientBytes, serve.PerClientBytes)
		computed.SavedBytesPerClient = serve.PerClientBytes - relay.PerClientBytes
	}
	if okRelay && okDaemon {
		computed.RelayOverDaemon = ratio(relay.PerClientBytes, served.PerClientBytes)
	}
	return computed
}

func slopeByName(computed slopes, name string) (armSlope, bool) {
	for _, one := range computed.Arms {
		if one.Name == name {
			return one, true
		}
	}
	return armSlope{}, false
}

// leastSquares fits y = slope*x + intercept. Fewer than two distinct x values
// determine no line, and a zero slope there would be an invented answer rather
// than a flat one.
func leastSquares(xs, ys []float64) (slope, intercept float64) {
	if len(xs) != len(ys) || len(xs) < 2 {
		return 0, 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for index := range xs {
		sumX += xs[index]
		sumY += ys[index]
		sumXY += xs[index] * ys[index]
		sumXX += xs[index] * xs[index]
	}
	n := float64(len(xs))
	denominator := n*sumXX - sumX*sumX
	if denominator == 0 {
		return 0, 0
	}
	slope = (n*sumXY - sumX*sumY) / denominator
	intercept = (sumY - slope*sumX) / n
	return slope, intercept
}

// verdictOf evaluates the gate. The threshold is one megabyte per client: below
// that the relay is trading three commits of work for a rounding error on the
// load that dominates, and ADR 0084 says in as many words that such a result
// closes the ficha.
func verdictOf(computed slopes, threshold float64) verdict {
	decided := verdict{
		ThresholdBytesPerClient: threshold,
		SavedBytesPerClient:     computed.SavedBytesPerClient,
		Proceed:                 computed.SavedBytesPerClient >= threshold,
	}
	if decided.Proceed {
		decided.Reason = fmt.Sprintf(
			"the relay costs %s per client against %s for a serve: %s saved, over the %s the gate asks for",
			megabytesFloat(relayPerClient(computed)), megabytesFloat(servePerClient(computed)),
			megabytesFloat(computed.SavedBytesPerClient), megabytesFloat(threshold))
		return decided
	}
	decided.Reason = fmt.Sprintf(
		"the relay costs %s per client against %s for a serve: %s saved, under the %s the gate asks for; LUQUE-2233 closes here and the stdio server stays",
		megabytesFloat(relayPerClient(computed)), megabytesFloat(servePerClient(computed)),
		megabytesFloat(computed.SavedBytesPerClient), megabytesFloat(threshold))
	return decided
}

func relayPerClient(computed slopes) float64 {
	fitted, _ := slopeByName(computed, armRelay)
	return fitted.PerClientBytes
}

func servePerClient(computed slopes) float64 {
	fitted, _ := slopeByName(computed, armServe)
	return fitted.PerClientBytes
}

// limitations names what this run could not answer, in the file rather than
// only in the prose: a reader who quotes a number out of the JSON gets the
// caveats with it.
func limitations(out results, cfg config) []string {
	notes := []string{
		"One machine, one kernel, one corpus. The bytes are not portable to another workspace: " +
			"the load a client puts on a server grows with the graph, and this one is declared above.",
		"The relay arm is a prototype with no fallback, no provisioning and no version check. " +
			"A finished relay is not cheaper than this one, so the floor measured here is a lower bound on the real one.",
		"The daemon arm is not a shippable arrangement for the installations that carry the volume: " +
			"the .mcpb manifest has no field for a url. It is here as the cheapest a client could be, " +
			"which is what says whether the relay's own tax is small.",
	}
	if !out.Environment.PrivateDirtySupported {
		notes = append(notes,
			"This platform does not report private dirty pages, so every figure here is resident size, "+
				"which counts shared pages the machine pays for once. The comparison is not sound.")
	}
	if strings.HasSuffix(out.Commit, "-dirty") {
		notes = append(notes,
			"The tree had uncommitted changes: `commit` carries `-dirty` and names code that is not in any history.")
	}
	// Both shapes `currentCommit` produces for an unreadable revision: a bare
	// "unknown" when `rev-parse` failed, and the "-unknown" suffix when only
	// the dirty check did. Matching one of them was how a run with no
	// attributable commit published no limitation saying so.
	if out.Commit == "unknown" || strings.HasSuffix(out.Commit, "-unknown") {
		notes = append(notes,
			"The commit could not be read, so these figures are not attributable to a revision.")
	}
	if failed := callErrorsIn(out); failed > 0 {
		notes = append(notes, fmt.Sprintf(
			"%d call(s) failed, and the driver times every call it makes, so those durations "+
				"are inside the percentiles. The driver is `benchmarks/daemon-cost`'s, copied "+
				"unchanged so the two sets of latencies stay comparable; diverging here would "+
				"end that. The memory figures are unaffected.", failed))
	}
	if out.Calls == 0 {
		notes = append(notes,
			"No calls: this is the idle load, which is the median session -- 48 of 51 real servers answer nothing -- "+
				"and not a light one.")
	}
	if cfg.Warmup == 0 && out.Calls > 0 {
		notes = append(notes,
			"No warmup, so the first calls of each arm pay for whatever the process had not touched yet. "+
				"That is the honest shape for a load of this size: a real session of eight calls has no warmup either.")
	}
	if len(measuredClients(out)) < 3 {
		notes = append(notes,
			"Fewer than three client counts: the fitted slope is a line through too few points to show a knee.")
	}
	return notes
}

func observeEnvironment(server string) environment {
	return environment{
		OS:                    runtime.GOOS,
		Arch:                  runtime.GOARCH,
		CPUs:                  runtime.NumCPU(),
		Kernel:                kernelRelease(),
		ServerVersion:         serverVersion(server),
		PrivateDirtySupported: procstat.ProportionalSupported(),
	}
}

func kernelRelease() string {
	output, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func serverVersion(server string) string {
	output, err := exec.Command(server, "version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

// currentCommit refuses to publish a bare revision for a tree that has changes
// in it. A run measures the code it ran, and an artifact whose commit names
// something else attributes its numbers to the wrong place.
func currentCommit() string {
	revision, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(string(revision))
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return commit + "-unknown"
	}
	if strings.TrimSpace(string(status)) != "" {
		return commit + "-dirty"
	}
	return commit
}

// computeDigest is the identity of the inputs -- corpus, generation, counts,
// seed -- and not of the measurements. Two runs of the same experiment share a
// digest, so a figure that differs cannot be passed off as the same run.
func computeDigest(out results) (string, error) {
	identity := struct {
		Benchmark string `json:"benchmark"`
		Schema    string `json:"schema_version"`
		Snapshot  int64  `json:"snapshot_bytes"`
		ID        uint64 `json:"snapshot_id"`
		Calls     int    `json:"calls"`
		Warmup    int    `json:"warmup"`
		Seed      int64  `json:"seed"`
		Clients   []int  `json:"clients"`
		Arms      []string
	}{
		Benchmark: out.Benchmark,
		Schema:    out.SchemaVersion,
		Snapshot:  out.Snapshot.Bytes,
		ID:        out.SnapshotID,
		Calls:     out.Calls,
		Warmup:    out.Warmup,
		Seed:      out.Seed,
		Clients:   measuredClients(out),
		Arms:      armNames,
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("digest: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func writeResults(directory string, out results) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", directory, err)
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	path := filepath.Join(directory, "results.json")
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func printSummary(out results) {
	fmt.Printf("relay-cost  commit=%s  snapshot=%d  symbols=%d  calls=%d\n",
		out.Commit, out.SnapshotID, symbolsOf(out), out.Calls)
	fmt.Printf("%-8s %-8s %-12s %-12s %-12s %-10s\n",
		"clients", "arm", "private", "clients-only", "peak", "connect")
	for _, measured := range out.Points {
		for _, one := range measured.Arms {
			fmt.Printf("%-8d %-8s %-12s %-12s %-12s %-10s\n",
				measured.Clients, one.Name,
				megabytes(one.Totals.PrivateDirtyByte),
				megabytes(one.ClientTotals.PrivateDirtyByte),
				megabytes(one.Totals.PeakBytes),
				milliseconds(&one.NewClientConnectMS))
		}
	}
	fmt.Println()
	for _, fitted := range out.Slopes.Arms {
		fmt.Printf("slope %-8s %s/client (fixed %s, clients only %s/client)\n",
			fitted.Name,
			megabytesFloat(fitted.PerClientBytes),
			megabytesFloat(fitted.FixedBytes),
			megabytesFloat(fitted.ClientPerClientBytes))
	}
	fmt.Printf("\nverdict: proceed=%t\n  %s\n", out.Verdict.Proceed, out.Verdict.Reason)
}

// callErrorsIn totals the refusals across every arm, so a limitation about
// them is stated only when there were some.
func callErrorsIn(out results) int {
	total := 0
	for _, measured := range out.Points {
		for _, one := range measured.Arms {
			total += one.CallErrors
		}
	}
	return total
}

func symbolsOf(out results) int {
	for _, measured := range out.Points {
		for _, one := range measured.Arms {
			if one.Symbols > 0 {
				return one.Symbols
			}
		}
	}
	return 0
}

func megabytes(value int64) string {
	return fmt.Sprintf("%.2f MB", float64(value)/(1024*1024))
}

func megabytesFloat(value float64) string {
	return fmt.Sprintf("%.2f MB", value/(1024*1024))
}

func milliseconds(value *float64) string {
	if value == nil {
		return "--"
	}
	return fmt.Sprintf("%.1f ms", *value)
}
