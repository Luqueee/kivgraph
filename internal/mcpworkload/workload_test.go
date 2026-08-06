package mcpworkload

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestGenerateUsesRequiredDistribution(t *testing.T) {
	workload, err := Generate(context.Background(), Config{Calls: 1_000, Seed: 7, Corpus: DefaultCorpus()})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	want := map[Operation]int{
		FindSymbol:             400,
		GetSymbol:              250,
		FindReferences:         200,
		FindCrossRepoConsumers: 100,
		GetBlastRadius:         50,
	}
	if !reflect.DeepEqual(workload.Distribution, want) {
		t.Fatalf("distribution = %#v, want %#v", workload.Distribution, want)
	}
	if len(workload.Requests) != 1_000 || workload.Calls != 1_000 {
		t.Fatalf("calls = %d/%d, want 1000", workload.Calls, len(workload.Requests))
	}
}

func TestGenerateIsDeterministicForSameSeed(t *testing.T) {
	config := Config{Calls: 100, Seed: 42, Corpus: DefaultCorpus()}
	first, err := Generate(context.Background(), config)
	if err != nil {
		t.Fatalf("first Generate() error = %v", err)
	}
	second, err := Generate(context.Background(), config)
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first workload: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second workload: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("same seed generated different JSON workloads")
	}
}

func TestGenerateChangesSequenceForDifferentSeed(t *testing.T) {
	first, err := Generate(context.Background(), Config{Calls: 100, Seed: 41, Corpus: DefaultCorpus()})
	if err != nil {
		t.Fatalf("first Generate() error = %v", err)
	}
	second, err := Generate(context.Background(), Config{Calls: 100, Seed: 42, Corpus: DefaultCorpus()})
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	if reflect.DeepEqual(first.Requests, second.Requests) {
		t.Fatal("different seeds generated the same request sequence")
	}
	if !reflect.DeepEqual(first.Distribution, second.Distribution) {
		t.Fatalf("different seeds changed distribution: first=%#v second=%#v", first.Distribution, second.Distribution)
	}
}

func TestGenerateArgumentsMatchToolContracts(t *testing.T) {
	workload, err := Generate(context.Background(), Config{Calls: 100, Seed: 42, Corpus: DefaultCorpus()})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	for _, request := range workload.Requests {
		if request.Sequence < 0 || request.Sequence >= len(workload.Requests) {
			t.Fatalf("invalid sequence %d", request.Sequence)
		}
		switch request.Operation {
		case FindSymbol:
			if request.Arguments["name"] == "" || request.Arguments["mode"] != "exact" || request.Arguments["limit"] != 50 {
				t.Fatalf("find_symbol arguments = %#v", request.Arguments)
			}
		case GetSymbol:
			if request.Arguments["stable_key"] == "" {
				t.Fatalf("get_symbol arguments = %#v", request.Arguments)
			}
		case FindReferences:
			if request.Arguments["stable_key"] == "" || request.Arguments["direction"] != "incoming" || request.Arguments["limit"] != 50 {
				t.Fatalf("find_references arguments = %#v", request.Arguments)
			}
		case FindCrossRepoConsumers:
			if request.Arguments["stable_key"] == "" || request.Arguments["limit"] != 50 {
				t.Fatalf("find_cross_repo_consumers arguments = %#v", request.Arguments)
			}
		case GetBlastRadius:
			if request.Arguments["stable_key"] == "" || request.Arguments["depth"] != 3 || request.Arguments["max_nodes"] != 5000 || request.Arguments["limit"] != 50 {
				t.Fatalf("get_blast_radius arguments = %#v", request.Arguments)
			}
		default:
			t.Fatalf("unsupported operation %q", request.Operation)
		}
	}
}

func TestGenerateHandlesSmallCallCounts(t *testing.T) {
	for calls := 1; calls <= 19; calls++ {
		workload, err := Generate(context.Background(), Config{Calls: calls, Seed: 42, Corpus: DefaultCorpus()})
		if err != nil {
			t.Fatalf("calls=%d Generate() error = %v", calls, err)
		}
		if len(workload.Requests) != calls {
			t.Fatalf("calls=%d generated %d requests", calls, len(workload.Requests))
		}
	}
}

func TestGenerateRejectsInvalidConfig(t *testing.T) {
	cases := []Config{
		{Calls: -1, Corpus: DefaultCorpus()},
		{Calls: 1, Corpus: Corpus{Probes: []Probe{{Name: "", StableKey: "symbol-1"}}}},
		{Calls: 1, Corpus: Corpus{Probes: []Probe{{Name: "symbol", StableKey: ""}}}},
		{Calls: 1, Corpus: Corpus{Probes: []Probe{{Name: "symbol", StableKey: "symbol-1"}, {Name: "other", StableKey: "symbol-1"}}}},
	}
	for index, config := range cases {
		if _, err := Generate(context.Background(), config); err == nil {
			t.Errorf("case %d Generate() error = nil", index)
		}
	}
}

func TestGenerateHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Generate(ctx, Config{Calls: 100, Seed: 42, Corpus: DefaultCorpus()})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Generate() error = %v, want context.Canceled", err)
	}
}
