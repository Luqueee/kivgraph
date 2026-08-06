//go:build !ladybug || !cgo

package ladybug

import (
	"context"

	"github.com/Luqueee/ladygraph/internal/facts"
)

// LoadReport records what a canonical load wrote.
type LoadReport struct {
	Tables    map[string]int64
	Nodes     int64
	Edges     int64
	StagingMS float64
	CopyMS    float64
}

// CanonicalProbe is one read executed against a freshly built graph.
type CanonicalProbe struct {
	Name      string
	SymbolKey string
	TargetKey string
	EdgeTable string
	MinRows   int64
}

// CanonicalProbeResult is the outcome of one probe.
type CanonicalProbeResult struct {
	Probe  string
	Rows   int64
	Passed bool
	Detail string
}

// LoadCanonical reports that the native build is required for a canonical load.
func LoadCanonical(ctx context.Context, path string, _ facts.Set, _ CanonicalLoadOptions) (LoadReport, error) {
	if err := validatePath(path); err != nil {
		return LoadReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return LoadReport{}, &Error{Op: "load canonical", Err: err}
	}
	return LoadReport{}, &Error{Op: "load canonical", Err: ErrUnavailable}
}

// CanonicalTableCounts reports that the native build is required to read counts.
func CanonicalTableCounts(ctx context.Context, path string) (map[string]int64, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: "canonical table counts", Err: err}
	}
	return nil, &Error{Op: "canonical table counts", Err: ErrUnavailable}
}

// RunCanonicalProbes reports that the native build is required to run probes.
func RunCanonicalProbes(ctx context.Context, path string, _ []CanonicalProbe) ([]CanonicalProbeResult, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: "run canonical probes", Err: err}
	}
	return nil, &Error{Op: "run canonical probes", Err: ErrUnavailable}
}
