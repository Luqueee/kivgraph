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
	// GateClients is the client count the gate is decided on. The others are
	// measured and reported: the question is what sharing buys as N grows, and
	// a single N cannot answer it -- the shared part is amortised over the
	// processes holding it, so the ratio moves with N and a lone number reads
	// as a property of the design.
	GateClients int          `json:"gate_clients"`
	Calls       int          `json:"calls"`
	Warmup      int          `json:"warmup"`
	Seed        int64        `json:"seed"`
	Environment environment  `json:"environment"`
	Snapshot    snapshotFile `json:"snapshot"`
	Thresholds  thresholds   `json:"thresholds"`
	Points      []point      `json:"points"`
	Gate        gate         `json:"gate"`
	Limitations []string     `json:"limitations"`
}

// point is one client count measured on both arms.
type point struct {
	Clients    int        `json:"clients"`
	Arms       []arm      `json:"arms"`
	Comparison comparison `json:"comparison"`
	Checks     []check    `json:"checks"`
}

type environment struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	CPUs          int    `json:"cpus"`
	Kernel        string `json:"kernel"`
	ServerVersion string `json:"server_version"`
	// ProportionalSupported says whether this platform can divide a shared
	// page between the processes holding it. Without it the comparison is
	// still real, but the total is a sum of resident sizes that double-count
	// the file, so the gate is not emitted.
	ProportionalSupported bool `json:"proportional_supported"`
}

type snapshotFile struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type thresholds struct {
	ResidentShare  float64 `json:"resident_share"`
	PrivateDirty   int64   `json:"private_dirty_bytes"`
	P99Regression  float64 `json:"p99_regression"`
	MeasuredOnly   bool    `json:"measured_only"`
	GateSwitchName string  `json:"gate_switch_name"`
}

type processSample struct {
	Index            int     `json:"index"`
	PID              int     `json:"pid"`
	ResidentBytes    int64   `json:"resident_bytes"`
	ProportionalByte int64   `json:"proportional_bytes"`
	SharedCleanByte  int64   `json:"shared_clean_bytes"`
	PrivateDirtyByte int64   `json:"private_dirty_bytes"`
	PeakBytes        int64   `json:"peak_bytes"`
	FirstAnswerMS    float64 `json:"first_answer_ms"`
}

type armTotals struct {
	ResidentBytes      int64   `json:"resident_bytes"`
	ProportionalByte   int64   `json:"proportional_bytes"`
	SharedCleanByte    int64   `json:"shared_clean_bytes"`
	PrivateDirtyByte   int64   `json:"private_dirty_bytes"`
	WorstPrivateDirty  int64   `json:"worst_private_dirty_bytes"`
	WorstFirstAnswerMS float64 `json:"worst_first_answer_ms"`
}

type latency struct {
	Calls int     `json:"calls"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
}

type arm struct {
	Name           string          `json:"name"`
	SnapshotID     uint64          `json:"snapshot_id"`
	Symbols        int             `json:"symbols"`
	ServedFromFile bool            `json:"served_from_file"`
	Processes      []processSample `json:"processes"`
	Totals         armTotals       `json:"totals"`
	Latency        latency         `json:"latency"`
	CallErrors     int             `json:"call_errors"`
}

type comparison struct {
	// ResidentShare is the mapped arm's cost over the derived arm's, on
	// whichever measure the platform can defend: proportional when it divides
	// shared pages, resident otherwise.
	ResidentShare   float64 `json:"resident_share"`
	ResidentMeasure string  `json:"resident_measure"`
	PrivateShare    float64 `json:"private_dirty_share"`
	P99Ratio        float64 `json:"p99_ratio"`
	FirstAnswerMS   struct {
		Mapped  float64 `json:"mapped"`
		Derived float64 `json:"derived"`
	} `json:"worst_first_answer_ms"`
}

type gate struct {
	// Evaluated says the checks ran. A harness that stopped evaluating them
	// would pass silently, which is worse than a threshold missed.
	Evaluated bool     `json:"evaluated"`
	Enforced  bool     `json:"enforced"`
	Passed    bool     `json:"passed"`
	Sentinel  string   `json:"sentinel,omitempty"`
	Checks    []check  `json:"checks"`
	Refusals  []string `json:"refusals"`
}

type check struct {
	Name   string  `json:"name"`
	Passed bool    `json:"passed"`
	Got    float64 `json:"got"`
	Want   float64 `json:"want"`
	Detail string  `json:"detail"`
}

func observeEnvironment(server string) environment {
	return environment{
		OS:                    runtime.GOOS,
		Arch:                  runtime.GOARCH,
		CPUs:                  runtime.NumCPU(),
		Kernel:                kernelRelease(),
		ServerVersion:         serverVersion(server),
		ProportionalSupported: procstat.ProportionalSupported(),
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

func pointByClients(out results, clients int) (point, bool) {
	for _, candidate := range out.Points {
		if candidate.Clients == clients {
			return candidate, true
		}
	}
	return point{}, false
}

func compare(mapped, derived arm) comparison {
	var out comparison
	out.ResidentMeasure = "resident"
	mappedTotal, derivedTotal := mapped.Totals.ResidentBytes, derived.Totals.ResidentBytes
	if procstat.ProportionalSupported() {
		out.ResidentMeasure = "proportional"
		mappedTotal, derivedTotal = mapped.Totals.ProportionalByte, derived.Totals.ProportionalByte
	}
	out.ResidentShare = ratio(float64(mappedTotal), float64(derivedTotal))
	out.PrivateShare = ratio(float64(mapped.Totals.PrivateDirtyByte), float64(derived.Totals.PrivateDirtyByte))
	out.P99Ratio = ratio(mapped.Latency.P99MS, derived.Latency.P99MS)
	out.FirstAnswerMS.Mapped = mapped.Totals.WorstFirstAnswerMS
	out.FirstAnswerMS.Derived = derived.Totals.WorstFirstAnswerMS
	return out
}

func ratio(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

// limitations are emitted from what the run observed, never written by hand.
func limitations(out results) []string {
	var found []string
	if !out.Environment.ProportionalSupported {
		found = append(found, "this platform cannot divide a shared page between the "+
			"processes holding it, so the totals sum resident sizes and count the mapped "+
			"file once per server; the gate is not emitted")
	}
	for _, measured := range out.Points {
		mapped, hasMapped := armByName(measured, "mapped")
		derived, hasDerived := armByName(measured, "derived")
		where := fmt.Sprintf("at %d clients", measured.Clients)
		if hasMapped && !mapped.ServedFromFile {
			found = append(found, where+": the mapped arm did not serve the published file, so the two arms measured the same thing")
		}
		if hasDerived && derived.ServedFromFile {
			found = append(found, where+": the derived arm served the published file, so the two arms measured the same thing")
		}
		if hasMapped && hasDerived && mapped.Symbols != derived.Symbols {
			found = append(found, fmt.Sprintf(
				"%s: the arms disagree on the graph: %d symbols mapped against %d derived",
				where, mapped.Symbols, derived.Symbols))
		}
		if hasMapped && mapped.CallErrors > 0 {
			found = append(found, fmt.Sprintf("%s: %d calls failed in the mapped arm", where, mapped.CallErrors))
		}
		if hasDerived && derived.CallErrors > 0 {
			found = append(found, fmt.Sprintf("%s: %d calls failed in the derived arm", where, derived.CallErrors))
		}
	}
	return found
}

// checksFor prices one point against the thresholds. Every point is checked, so
// a reader can see where the criteria start holding instead of only whether they
// hold at the one count the gate names.
func checksFor(measured point) []check {
	mapped, hasMapped := armByName(measured, "mapped")
	if !hasMapped {
		return nil
	}
	return []check{
		{
			Name:   "resident_share",
			Passed: measured.Comparison.ResidentShare > 0 && measured.Comparison.ResidentShare <= maximumResidentShare,
			Got:    measured.Comparison.ResidentShare,
			Want:   maximumResidentShare,
			Detail: "total " + measured.Comparison.ResidentMeasure + " of the mapped arm over the derived arm",
		},
		{
			Name:   "private_dirty_per_process",
			Passed: mapped.Totals.WorstPrivateDirty > 0 && mapped.Totals.WorstPrivateDirty <= maximumPrivateDirty,
			Got:    float64(mapped.Totals.WorstPrivateDirty),
			Want:   float64(maximumPrivateDirty),
			Detail: "the worst single server of the mapped arm",
		},
		{
			Name:   "p99_not_worse",
			Passed: measured.Comparison.P99Ratio > 0 && measured.Comparison.P99Ratio <= 1+maximumP99Regression,
			Got:    measured.Comparison.P99Ratio,
			Want:   1 + maximumP99Regression,
			Detail: "mapped p99 over derived p99",
		},
	}
}

// decide evaluates every check always, and enforces them only when asked.
//
// The distinction is the convention of benchmarks/AGENTS.md: a threshold that
// depends on a shared machine fails on a loaded runner over code that did not
// change, so the limit is enforced when someone asks for it and reported the
// rest of the time. What is always asserted is that the measurement happened.
func decide(out results) gate {
	measured, found := pointByClients(out, out.GateClients)
	_, hasMapped := armByName(measured, "mapped")
	_, hasDerived := armByName(measured, "derived")
	decision := gate{
		Evaluated: found && hasMapped && hasDerived,
		Enforced:  os.Getenv(gateEnvironmentSwitch) == "1",
	}
	if !decision.Evaluated {
		decision.Refusals = append(decision.Refusals, fmt.Sprintf(
			"both arms at %d clients are required, which is the count the gate is decided on",
			out.GateClients))
		return decision
	}

	decision.Checks = measured.Checks

	// A refusal is not a failed threshold: it says the run cannot answer.
	if !out.Environment.ProportionalSupported {
		decision.Refusals = append(decision.Refusals,
			"the platform cannot divide shared pages, so the resident share is not the machine's cost")
	}
	if len(out.Limitations) > 0 {
		decision.Refusals = append(decision.Refusals, out.Limitations...)
	}

	passed := true
	for _, item := range decision.Checks {
		if !item.Passed {
			passed = false
		}
	}
	decision.Passed = passed && len(decision.Refusals) == 0
	if decision.Passed && decision.Enforced {
		decision.Sentinel = gatePassSentinel
	}
	return decision
}

// computeDigest is the identity of a run's inputs, not of its measurements.
//
// The same corpus, generation, client count and workload seed must produce the
// same string, so two runs can be compared and a changed number cannot pass as
// the same experiment. Latencies and byte counts are deliberately excluded:
// they are what the run measures, and hashing them would make every run unique
// and the digest worthless.
func computeDigest(out results) (string, error) {
	identity := struct {
		Schema     string       `json:"schema"`
		SnapshotID uint64       `json:"snapshot_id"`
		Snapshot   snapshotFile `json:"snapshot"`
		Clients    []int        `json:"clients"`
		Calls      int          `json:"calls"`
		Warmup     int          `json:"warmup"`
		Seed       int64        `json:"seed"`
		Symbols    int          `json:"symbols"`
		OS         string       `json:"os"`
		Arch       string       `json:"arch"`
		Thresholds thresholds   `json:"thresholds"`
	}{
		Schema:     out.SchemaVersion,
		SnapshotID: out.SnapshotID,
		Snapshot:   out.Snapshot,
		Clients:    measuredClients(out),
		Calls:      out.Calls,
		Warmup:     out.Warmup,
		Seed:       out.Seed,
		OS:         out.Environment.OS,
		Arch:       out.Environment.Arch,
		Thresholds: out.Thresholds,
	}
	if measured, found := pointByClients(out, out.GateClients); found {
		if mapped, ok := armByName(measured, "mapped"); ok {
			identity.Symbols = mapped.Symbols
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
	out.Commit = currentCommit()
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
	fmt.Printf("snapshot %d, %d calls, %d discarded, seed %d, gate at %d clients\n",
		out.SnapshotID, out.Calls, out.Warmup, out.Seed, out.GateClients)
	for _, measured := range out.Points {
		fmt.Printf(" %d clients\n", measured.Clients)
		for _, item := range measured.Arms {
			fmt.Printf("  %-8s served_from_file=%-5v  resident %s  proportional %s  shared_clean %s  private_dirty %s  p99 %.2f ms  first answer %.0f ms\n",
				item.Name, item.ServedFromFile,
				megabytes(item.Totals.ResidentBytes), megabytes(item.Totals.ProportionalByte),
				megabytes(item.Totals.SharedCleanByte), megabytes(item.Totals.PrivateDirtyByte),
				item.Latency.P99MS, item.Totals.WorstFirstAnswerMS)
		}
		fmt.Printf("  %s share %.3f (want <= %.2f), worst private_dirty %s (want <= %s), p99 ratio %.3f\n",
			measured.Comparison.ResidentMeasure, measured.Comparison.ResidentShare, maximumResidentShare,
			megabytes(worstPrivate(measured)), megabytes(maximumPrivateDirty), measured.Comparison.P99Ratio)
		for _, item := range measured.Checks {
			state := "FAIL"
			if item.Passed {
				state = "ok"
			}
			fmt.Printf("  check %-26s %-4s got %.3f want %.3f\n", item.Name, state, item.Got, item.Want)
		}
	}
	for _, refusal := range out.Gate.Refusals {
		fmt.Printf("  refusal: %s\n", refusal)
	}
	switch {
	case out.Gate.Sentinel != "":
		fmt.Println("  " + out.Gate.Sentinel)
	case out.Gate.Passed:
		fmt.Printf("  every check passed at %d clients; set %s=1 to emit %s\n",
			out.GateClients, gateEnvironmentSwitch, gatePassSentinel)
	default:
		fmt.Printf("  gate not emitted (decided at %d clients)\n", out.GateClients)
	}
	fmt.Printf("  digest %s\n", out.Digest)
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

func worstPrivate(measured point) int64 {
	if mapped, ok := armByName(measured, "mapped"); ok {
		return mapped.Totals.WorstPrivateDirty
	}
	return 0
}

func megabytes(value int64) string {
	return fmt.Sprintf("%.1f MB", float64(value)/(1<<20))
}
