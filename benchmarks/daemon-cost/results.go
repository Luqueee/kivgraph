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
	"slices"
	"strings"

	"github.com/Luqueee/kivgraph/internal/procstat"
)

type results struct {
	Benchmark     string `json:"benchmark"`
	Date          string `json:"date"`
	SchemaVersion string `json:"schema_version"`
	Commit        string `json:"commit"`
	Digest        string `json:"digest"`
	SnapshotID    uint64 `json:"snapshot_id"`
	Calls         int    `json:"calls"`
	Warmup        int    `json:"warmup"`
	Seed          int64  `json:"seed"`
	// Transport names which of the daemon's two doors the clients used. Without
	// it two runs of the same corpus and client count are indistinguishable in
	// the file, and the socket number would be quoted for a path no editor can
	// take.
	Transport   string       `json:"transport"`
	Environment environment  `json:"environment"`
	Snapshot    snapshotFile `json:"snapshot"`
	Points      []point      `json:"points"`
	// Slopes are what no single client count can answer. A daemon that saves at
	// two clients and not at eight would look like a win in any one row.
	Slopes      slopes   `json:"slopes"`
	Limitations []string `json:"limitations"`
}

// point is one client count measured on both arms.
type point struct {
	Clients    int        `json:"clients"`
	Arms       []arm      `json:"arms"`
	Comparison comparison `json:"comparison"`
}

type environment struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	CPUs          int    `json:"cpus"`
	Kernel        string `json:"kernel"`
	ServerVersion string `json:"server_version"`
	// PrivateDirtySupported says whether this platform reports the per-process
	// private half at all. Without it this benchmark cannot answer its own
	// question: the resident size of a process that maps a shared file counts
	// pages the machine pays for once.
	PrivateDirtySupported bool `json:"private_dirty_supported"`
}

type snapshotFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type processSample struct {
	Index            int   `json:"index"`
	PID              int   `json:"pid"`
	ResidentBytes    int64 `json:"resident_bytes"`
	ProportionalByte int64 `json:"proportional_bytes"`
	SharedCleanByte  int64 `json:"shared_clean_bytes"`
	PrivateDirtyByte int64 `json:"private_dirty_bytes"`
	PeakBytes        int64 `json:"peak_bytes"`
}

type armTotals struct {
	ResidentBytes     int64 `json:"resident_bytes"`
	ProportionalByte  int64 `json:"proportional_bytes"`
	SharedCleanByte   int64 `json:"shared_clean_bytes"`
	PrivateDirtyByte  int64 `json:"private_dirty_bytes"`
	PeakBytes         int64 `json:"peak_bytes"`
	WorstPrivateDirty int64 `json:"worst_private_dirty_bytes"`
}

type latency struct {
	Calls int     `json:"calls"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
}

type arm struct {
	Name       string          `json:"name"`
	SnapshotID uint64          `json:"snapshot_id"`
	Symbols    int             `json:"symbols"`
	Sessions   int             `json:"sessions"`
	Processes  []processSample `json:"processes"`
	Totals     armTotals       `json:"totals"`
	Latency    latency         `json:"latency"`
	CallErrors int             `json:"call_errors"`
	// FirstAnswersMS is what each client waited to be answered, in the order
	// they connected. In the daemon arm the shape is the finding: only the
	// first pays for a load.
	FirstAnswersMS []float64 `json:"first_answers_ms"`
	// NewClientMS is what one more client waits once the arm is already
	// running, which is what a second editor window sees.
	NewClientMS float64 `json:"new_client_ms"`
}

type comparison struct {
	// PrivateDirtyShare is the daemon arm's private half over the processes
	// arm's. Below one is a saving; above one the daemon costs more.
	PrivateDirtyShare float64 `json:"private_dirty_share"`
	PrivateDirtySaved int64   `json:"private_dirty_saved_bytes"`
	PeakShare         float64 `json:"peak_share"`
	P99Ratio          float64 `json:"p99_ratio"`
	// NewClientSpeedup is the processes arm's wait over the daemon arm's. Above
	// one means a new client is answered sooner by a running daemon.
	NewClientSpeedup float64 `json:"new_client_speedup"`
}

// armSlope is a straight line fitted to one arm's private half against the
// number of clients.
//
// The intercept is the fixed cost -- one load of the snapshot -- and the slope is
// what each additional client adds. That pair is the whole question: the
// processes arm should slope by a whole server, and if the daemon's slope
// approached the same figure the command would buy nothing.
type armSlope struct {
	Name string `json:"name"`
	// PerClientBytes is the least-squares slope over the measured sweep.
	PerClientBytes float64 `json:"private_dirty_bytes_per_client"`
	// FixedBytes is the fitted intercept: what the arm costs before any client.
	FixedBytes float64 `json:"private_dirty_fixed_bytes"`
	// Marginals are the step-by-step differences, so a reader sees whether the
	// line is a fair summary or hides a knee.
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
	// SlopeRatio is the daemon's per-client cost over the processes arm's. Near
	// zero means flat in the number of clients; near one means the daemon pays
	// per client what a whole process does, and the command is pointless.
	SlopeRatio float64 `json:"slope_ratio"`
	// CrossoverClients is where the two fitted lines meet, or zero when they do
	// not inside a plausible range. Below it the daemon costs more, because its
	// fixed half is paid with nothing to amortise it over.
	CrossoverClients float64 `json:"crossover_clients"`
}

func compare(processes, served arm) comparison {
	result := comparison{
		PrivateDirtySaved: processes.Totals.PrivateDirtyByte - served.Totals.PrivateDirtyByte,
		P99Ratio:          ratio(served.Latency.P99MS, processes.Latency.P99MS),
		NewClientSpeedup:  ratio(processes.NewClientMS, served.NewClientMS),
	}
	result.PrivateDirtyShare = ratio(float64(served.Totals.PrivateDirtyByte), float64(processes.Totals.PrivateDirtyByte))
	result.PeakShare = ratio(float64(served.Totals.PeakBytes), float64(processes.Totals.PeakBytes))
	return result
}

func ratio(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

// slopesOf fits each arm's private half against the client count.
//
// Two measured counts are the minimum: one point has no slope, and reporting a
// zero would read as "flat" when it means "not measured".
func slopesOf(out results) slopes {
	fitted := slopes{}
	for _, name := range []string{"processes", "daemon"} {
		xs := make([]float64, 0, len(out.Points))
		ys := make([]float64, 0, len(out.Points))
		steps := make([]marginal, 0, len(out.Points))
		var previous *point
		for index := range out.Points {
			measured := out.Points[index]
			one, ok := armByName(measured, name)
			if !ok {
				continue
			}
			xs = append(xs, float64(measured.Clients))
			ys = append(ys, float64(one.Totals.PrivateDirtyByte))
			if previous != nil {
				before, okBefore := armByName(*previous, name)
				if okBefore && measured.Clients != previous.Clients {
					steps = append(steps, marginal{
						From: previous.Clients,
						To:   measured.Clients,
						PerClient: (one.Totals.PrivateDirtyByte - before.Totals.PrivateDirtyByte) /
							int64(measured.Clients-previous.Clients),
					})
				}
			}
			previous = &out.Points[index]
		}
		if len(xs) < 2 {
			fitted.Arms = append(fitted.Arms, armSlope{Name: name, Marginals: steps})
			continue
		}
		slope, intercept := leastSquares(xs, ys)
		fitted.Arms = append(fitted.Arms, armSlope{
			Name:           name,
			PerClientBytes: slope,
			FixedBytes:     intercept,
			Marginals:      steps,
		})
	}
	if len(fitted.Arms) == 2 {
		fitted.SlopeRatio = ratio(fitted.Arms[1].PerClientBytes, fitted.Arms[0].PerClientBytes)
		fitted.CrossoverClients = crossover(fitted.Arms[0], fitted.Arms[1])
	}
	return fitted
}

func leastSquares(xs, ys []float64) (slope, intercept float64) {
	count := float64(len(xs))
	var sumX, sumY, sumXY, sumXX float64
	for index := range xs {
		sumX += xs[index]
		sumY += ys[index]
		sumXY += xs[index] * ys[index]
		sumXX += xs[index] * xs[index]
	}
	denominator := count*sumXX - sumX*sumX
	if denominator == 0 {
		return 0, sumY / count
	}
	slope = (count*sumXY - sumX*sumY) / denominator
	intercept = (sumY - slope*sumX) / count
	return slope, intercept
}

// crossover is where the two lines meet. A negative or absurd answer is
// reported as zero rather than as a client count nobody would run: the lines are
// fitted to a sweep, and extrapolating them past it is not a measurement.
func crossover(processes, served armSlope) float64 {
	difference := processes.PerClientBytes - served.PerClientBytes
	if difference == 0 {
		return 0
	}
	at := (served.FixedBytes - processes.FixedBytes) / difference
	if at <= 0 || at > 1024 {
		return 0
	}
	return at
}

// limitations are emitted from what the run observed, never written by hand.
func limitations(out results) []string {
	notes := make([]string, 0, 4)
	if out.Commit == "" {
		notes = append(notes,
			"the commit could not be read, so this run's provenance is incomplete: it was launched from outside a git checkout")
	}
	if !out.Environment.PrivateDirtySupported {
		notes = append(notes,
			"this platform does not report the per-process private half, so the totals here are resident sizes that count a shared mapped file once per process; the comparison cannot be concluded from them")
	}
	if len(out.Points) < 2 {
		notes = append(notes, "one client count was measured, so no slope was fitted and the sweep cannot say how either arm grows")
	}
	if !slices.Contains(measuredClients(out), 1) {
		notes = append(notes,
			"the one-client count was not measured, which is where the daemon's fixed half is visible with nothing to amortise it over")
	}
	for _, measured := range out.Points {
		for _, one := range measured.Arms {
			if one.CallErrors > 0 {
				notes = append(notes, fmt.Sprintf(
					"%d clients, %s arm: %d of %d calls were refused, so its latencies include error paths",
					measured.Clients, one.Name, one.CallErrors, one.Latency.Calls))
			}
		}
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
	output, err := exec.Command("uname", "-sr").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func serverVersion(server string) string {
	output, err := exec.Command(server, "version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func currentCommit() string {
	output, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func armByName(measured point, name string) (arm, bool) {
	for _, candidate := range measured.Arms {
		if candidate.Name == name {
			return candidate, true
		}
	}
	return arm{}, false
}

// measuredClients is the sweep as run, part of a run's identity: the same
// corpus measured at other counts is a different run.
func measuredClients(out results) []int {
	counts := make([]int, 0, len(out.Points))
	for _, measured := range out.Points {
		counts = append(counts, measured.Clients)
	}
	return counts
}

// computeDigest is the identity of a run's inputs, not of its measurements.
//
// The same corpus, generation, client count and workload seed must produce the
// same string, so two runs can be compared and a changed number cannot pass as
// the same experiment. Byte counts and latencies are deliberately excluded: they
// are what the run measures, and hashing them would make every run unique and
// the digest worthless.
func computeDigest(out results) (string, error) {
	identity := struct {
		Schema     string       `json:"schema"`
		SnapshotID uint64       `json:"snapshot_id"`
		Snapshot   snapshotFile `json:"snapshot"`
		Clients    []int        `json:"clients"`
		Calls      int          `json:"calls"`
		Warmup     int          `json:"warmup"`
		Seed       int64        `json:"seed"`
		// Transport is part of the identity because the two doors are not the
		// same experiment. Leaving it out would make a socket run and an HTTP
		// run over the same corpus collide on one digest, and a comparison
		// between them would look like a comparison of a run against itself.
		Transport string `json:"transport"`
		Symbols   int    `json:"symbols"`
		OS        string `json:"os"`
		Arch      string `json:"arch"`
	}{
		Schema:     out.SchemaVersion,
		SnapshotID: out.SnapshotID,
		Snapshot:   out.Snapshot,
		Clients:    measuredClients(out),
		Calls:      out.Calls,
		Warmup:     out.Warmup,
		Seed:       out.Seed,
		Transport:  out.Transport,
		OS:         out.Environment.OS,
		Arch:       out.Environment.Arch,
	}
	if len(out.Points) > 0 {
		if one, ok := armByName(out.Points[0], "processes"); ok {
			identity.Symbols = one.Symbols
		}
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("marshal run identity: %w", err)
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
		return fmt.Errorf("marshal results: %w", err)
	}
	path := filepath.Join(directory, "results.json")
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func printSummary(out results) {
	fmt.Printf("snapshot %d, %d calls, %d discarded, seed %d\n",
		out.SnapshotID, out.Calls, out.Warmup, out.Seed)
	for _, measured := range out.Points {
		fmt.Printf(" %d clients\n", measured.Clients)
		for _, one := range measured.Arms {
			fmt.Printf("  %-10s procs %d  private_dirty %s  resident %s  shared_clean %s  peak %s  p99 %.2f ms  worst first answer %.0f ms  new client %.0f ms\n",
				one.Name, len(one.Processes),
				megabytes(one.Totals.PrivateDirtyByte), megabytes(one.Totals.ResidentBytes),
				megabytes(one.Totals.SharedCleanByte), megabytes(one.Totals.PeakBytes),
				one.Latency.P99MS, worstFirstAnswer(one.FirstAnswersMS), one.NewClientMS)
		}
		fmt.Printf("  private_dirty share %.3f (saved %s), peak share %.3f, p99 ratio %.3f, new client %.2fx sooner\n",
			measured.Comparison.PrivateDirtyShare, megabytes(measured.Comparison.PrivateDirtySaved),
			measured.Comparison.PeakShare, measured.Comparison.P99Ratio,
			measured.Comparison.NewClientSpeedup)
	}
	for _, fit := range out.Slopes.Arms {
		fmt.Printf("  slope %-10s %s per client, fixed %s\n",
			fit.Name, megabytesFloat(fit.PerClientBytes), megabytesFloat(fit.FixedBytes))
		for _, step := range fit.Marginals {
			fmt.Printf("    %d -> %d clients: %s per client\n", step.From, step.To, megabytes(step.PerClient))
		}
	}
	fmt.Printf("  slope ratio %.3f", out.Slopes.SlopeRatio)
	if out.Slopes.CrossoverClients > 0 {
		fmt.Printf(", the arms cross at %.2f clients", out.Slopes.CrossoverClients)
	}
	fmt.Println()
	for _, note := range out.Limitations {
		fmt.Printf("  limitation: %s\n", note)
	}
	fmt.Printf("  digest %s\n", out.Digest)
}

func megabytes(value int64) string {
	return fmt.Sprintf("%.1f MB", float64(value)/(1<<20))
}

func megabytesFloat(value float64) string {
	return fmt.Sprintf("%.1f MB", value/(1<<20))
}
