package eventlog

import (
	"sort"
	"time"
)

// ToolSummary is what one tool did over the events it was summarised from.
//
// Percentiles are here because the store keeps every individual call rather
// than a running sum: internal/metrics deliberately retains latency as count,
// total and maximum to avoid an unbounded sample list in a live process, which
// buys a mean and a worst case and nothing in between. A reader over the file
// pays for the sample once, so it can answer the question a mean hides -- a
// tool whose median is fast and whose tail is not.
type ToolSummary struct {
	Tool     string        `json:"tool"`
	Calls    int           `json:"calls"`
	OK       int           `json:"ok"`
	Failed   int           `json:"failed"`
	Mean     time.Duration `json:"mean_ns"`
	Median   time.Duration `json:"median_ns"`
	P95      time.Duration `json:"p95_ns"`
	Max      time.Duration `json:"max_ns"`
	Last     time.Time     `json:"last"`
	LastFail string        `json:"last_failure,omitempty"`
}

// SuccessRate answers the share of calls that answered, in the range 0..1. A
// tool with no calls has no rate, so it answers 0 alongside false.
func (summary ToolSummary) SuccessRate() (float64, bool) {
	if summary.Calls == 0 {
		return 0, false
	}
	return float64(summary.OK) / float64(summary.Calls), true
}

// Summary is the whole aggregation, ordered so a reader sees the busiest tool
// first and so two runs over the same events render identically.
type Summary struct {
	Tools  []ToolSummary `json:"tools"`
	Calls  int           `json:"calls"`
	OK     int           `json:"ok"`
	Failed int           `json:"failed"`
	First  time.Time     `json:"first,omitempty"`
	Last   time.Time     `json:"last,omitempty"`
}

// Summarize aggregates the tool events among events. Events of another kind are
// ignored, so a caller may hand it an unfiltered read.
func Summarize(events []Event) Summary {
	type accumulator struct {
		summary   ToolSummary
		durations []time.Duration
	}
	byTool := make(map[string]*accumulator)
	summary := Summary{}

	for _, event := range events {
		if event.Kind != KindTool || event.Tool == "" {
			continue
		}
		entry := byTool[event.Tool]
		if entry == nil {
			entry = &accumulator{summary: ToolSummary{Tool: event.Tool}}
			byTool[event.Tool] = entry
		}
		entry.summary.Calls++
		summary.Calls++
		if event.Failed() {
			entry.summary.Failed++
			summary.Failed++
			if event.Error != "" {
				entry.summary.LastFail = event.Error
			}
		} else {
			entry.summary.OK++
			summary.OK++
		}
		if event.Time.After(entry.summary.Last) {
			entry.summary.Last = event.Time
		}
		if summary.First.IsZero() || event.Time.Before(summary.First) {
			summary.First = event.Time
		}
		if event.Time.After(summary.Last) {
			summary.Last = event.Time
		}
		if elapsed, timed := event.Duration(); timed {
			entry.durations = append(entry.durations, elapsed)
		}
	}

	summary.Tools = make([]ToolSummary, 0, len(byTool))
	for _, entry := range byTool {
		sort.Slice(entry.durations, func(first, second int) bool {
			return entry.durations[first] < entry.durations[second]
		})
		total := time.Duration(0)
		for _, elapsed := range entry.durations {
			total += elapsed
		}
		if count := len(entry.durations); count > 0 {
			entry.summary.Mean = total / time.Duration(count)
			entry.summary.Median = percentile(entry.durations, 0.50)
			entry.summary.P95 = percentile(entry.durations, 0.95)
			entry.summary.Max = entry.durations[count-1]
		}
		summary.Tools = append(summary.Tools, entry.summary)
	}
	sort.Slice(summary.Tools, func(first, second int) bool {
		left, right := summary.Tools[first], summary.Tools[second]
		if left.Calls != right.Calls {
			return left.Calls > right.Calls
		}
		return left.Tool < right.Tool
	})
	return summary
}

// percentile answers the nearest-rank percentile of an ascending sample. It is
// the definition a small sample deserves: with four calls there is no honest
// interpolation to make, and a reported value that no call ever took would be
// worse than a value one of them did.
func percentile(ascending []time.Duration, share float64) time.Duration {
	count := len(ascending)
	if count == 0 {
		return 0
	}
	index := int(float64(count)*share+0.5) - 1
	if index < 0 {
		index = 0
	}
	if index >= count {
		index = count - 1
	}
	return ascending[index]
}
