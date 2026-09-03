package tools

import (
	"context"
	"testing"

	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProfileSelectionDoesNotBorrowDefaultFreshness(t *testing.T) {
	// Different profiles can independently publish the same generation number.
	aggregate, err := hotsnapshot.NewProfileSnapshotStore("default", map[string]*hotsnapshot.SnapshotStore{
		"default": graphStatusStore(t, 62),
		"other":   graphStatusStore(t, 62),
	})
	if err != nil {
		t.Fatal(err)
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "1"}, nil)
	RegisterGraphStatusWithObserverAndSnapshotStoreAndMetrics(server, nil, aggregate,
		func(context.Context) (HostStatus, error) {
			return HostStatus{ContentFreshness: &freshness.Status{Generation: 62, State: "fresh"}}, nil
		}, nil)
	a, b := sdkmcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(t.Context(), b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, profile := range []string{"other", "*", "default"} {
		t.Run(profile, func(t *testing.T) {
			result, err := cs.CallTool(t.Context(), &sdkmcp.CallToolParams{Name: "graph_status", Arguments: map[string]any{"profile": []string{profile}}})
			if err != nil || result.IsError {
				t.Fatalf("call: %v, %v", result, err)
			}
			status := decodeResponse[GraphStatus](t, result).Results
			if profile == "default" {
				if status.ContentFreshness == nil || status.ContentFreshness.State != "fresh" {
					t.Fatalf("default attestation missing: %#v", status)
				}
			} else if status.ContentFreshness != nil {
				t.Fatalf("borrowed default attestation: %#v", status)
			}
			if profile == "*" && len(status.Profiles) != 2 {
				t.Fatalf("profile discovery lost: %#v", status)
			}
		})
	}
}
