package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/mcpworkload"
)

// statusAnswer is the part of graph_status this harness needs. Decoding only
// these fields is deliberate: a benchmark that unmarshalled the whole response
// would fail to compile every time an unrelated field moved.
type statusAnswer struct {
	SnapshotID uint64
	Symbols    int
}

// readStatus refuses a status that names no symbols rather than recording a
// zero. A run whose corpus size is unknown cannot be compared with another, and
// the first version of this file published `symbols: 0` because it only checked
// the snapshot id -- the count is nested under `results`, and a guess at the
// shape decoded to zero without failing.
func readStatus(ctx context.Context, session *sdkmcp.ClientSession) (statusAnswer, error) {
	text, err := callTool(ctx, session, "graph_status", map[string]any{})
	if err != nil {
		return statusAnswer{}, err
	}
	var decoded struct {
		SnapshotID *uint64 `json:"snapshot_id"`
		Results    struct {
			Symbols int `json:"symbols"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return statusAnswer{}, fmt.Errorf("decode graph_status: %w", err)
	}
	if decoded.SnapshotID == nil || decoded.Results.Symbols == 0 {
		return statusAnswer{}, fmt.Errorf("graph_status named no snapshot or no symbols: %s", clip(text, 200))
	}
	return statusAnswer{SnapshotID: *decoded.SnapshotID, Symbols: decoded.Results.Symbols}, nil
}

// harvestProbes draws the workload's subjects from the snapshot under test.
//
// A synthetic corpus would make the two arms comparable to each other and to
// nothing else: the latency of a query depends on how many references the symbol
// actually has. These come from find_symbol, so they exist, and they are sorted
// so two runs of the same generation drive the same workload.
func harvestProbes(ctx context.Context, session *sdkmcp.ClientSession) ([]mcpworkload.Probe, error) {
	// The full view, because the compact default hoists the fields every row
	// shares onto the page: this needs a stable key per row, which is exactly
	// what the compact shape is built to stop repeating.
	text, err := callTool(ctx, session, "find_symbol", map[string]any{
		"name":            "e",
		"mode":            "substring",
		"limit":           probeCount,
		"view":            "full",
		"response_format": "detailed",
	})
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Results []struct {
			Name      string `json:"name"`
			StableKey string `json:"stable_key"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return nil, fmt.Errorf("decode find_symbol: %w", err)
	}
	probes := make([]mcpworkload.Probe, 0, len(decoded.Results))
	for _, row := range decoded.Results {
		if row.Name == "" || row.StableKey == "" {
			continue
		}
		probes = append(probes, mcpworkload.Probe{Name: row.Name, StableKey: row.StableKey})
	}
	if len(probes) < 8 {
		return nil, fmt.Errorf("the snapshot yielded %d usable probes, want at least 8", len(probes))
	}
	sort.Slice(probes, func(i, j int) bool { return probes[i].StableKey < probes[j].StableKey })
	return probes, nil
}

// driveAll splits the workload across the sessions and runs them at once,
// because the question is what N clients cost together while all of them are
// being answered. In the daemon arm that concurrency is also the point: N
// sessions answered by one process is the arrangement under test.
func driveAll(ctx context.Context, sessions []*sdkmcp.ClientSession, requests []mcpworkload.Request) ([]int64, int, error) {
	if len(sessions) == 0 {
		return nil, 0, errors.New("no sessions to drive")
	}
	batches := make([][]mcpworkload.Request, len(sessions))
	for index, request := range requests {
		target := index % len(sessions)
		batches[target] = append(batches[target], request)
	}

	var (
		group     sync.WaitGroup
		mutex     sync.Mutex
		latencies = make([]int64, 0, len(requests))
		failures  int
		firstErr  error
	)
	for index, session := range sessions {
		group.Add(1)
		go func(session *sdkmcp.ClientSession, batch []mcpworkload.Request) {
			defer group.Done()
			local := make([]int64, 0, len(batch))
			localFailures := 0
			for _, request := range batch {
				started := time.Now()
				_, err := callTool(ctx, session, string(request.Operation), request.Arguments)
				local = append(local, time.Since(started).Nanoseconds())
				if err != nil {
					localFailures++
					mutex.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mutex.Unlock()
				}
			}
			mutex.Lock()
			latencies = append(latencies, local...)
			failures += localFailures
			mutex.Unlock()
		}(session, batches[index])
	}
	group.Wait()

	// A handful of refusals is data, not a broken run: some probes name a
	// symbol no reference points at. A run where most calls failed is measuring
	// error paths and must not be published as latency.
	if failures*2 > len(requests) {
		return nil, failures, fmt.Errorf("%d of %d calls failed, first: %w", failures, len(requests), firstErr)
	}
	return latencies, failures, nil
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

func latencyOf(durations []int64) latency {
	if len(durations) == 0 {
		return latency{}
	}
	sorted := append([]int64(nil), durations...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p50 := percentileMS(sorted, 0.50)
	p95 := percentileMS(sorted, 0.95)
	p99 := percentileMS(sorted, 0.99)
	return latency{Calls: len(sorted), P50MS: &p50, P95MS: &p95, P99MS: &p99}
}

func percentileMS(sorted []int64, fraction float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * fraction)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return float64(sorted[index]) / 1e6
}

// worstFirstAnswer is what the slowest client waited. A mean would hide the
// arm's shape: in the processes arm every client pays a full start, in the
// daemon arm only the first one does.
//
// A run that asks nothing has no first answer, and nil says so: a zero would
// read as the fastest arm ever measured.
func worstFirstAnswer(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	worst := 0.0
	for _, value := range values {
		if value > worst {
			worst = value
		}
	}
	return &worst
}
