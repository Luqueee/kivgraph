package main

// The workload driver below is `benchmarks/daemon-cost`'s, copied rather than
// imported. That is not tidiness lost: the two benchmarks have to drive the
// *same* load for their figures to be readable side by side -- this one quotes
// daemon-cost's tables in its report -- and `benchmarks/daemon-cost` is a main
// package with nothing to import. Extracting it would edit the program that
// produced the published artifacts, and an artifact describes the code that
// produced it.

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
// zero. A run whose corpus size is unknown cannot be compared with another.
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

// harvestProbes draws the workload's subjects from the snapshot under test. A
// synthetic corpus would make the arms comparable to each other and to nothing
// else: the cost of a query depends on how many references the symbol has.
func harvestProbes(ctx context.Context, session *sdkmcp.ClientSession) ([]mcpworkload.Probe, error) {
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
// being answered.
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

func callTool(ctx context.Context, session *sdkmcp.ClientSession, name string, arguments map[string]any) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	response, err := session.CallTool(callCtx, &sdkmcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return "", fmt.Errorf("call %s: %w", name, err)
	}
	if response.IsError {
		return "", fmt.Errorf("call %s returned an error result: %s", name, firstText(response))
	}
	return firstText(response), nil
}

func firstText(response *sdkmcp.CallToolResult) string {
	for _, content := range response.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

func clip(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
