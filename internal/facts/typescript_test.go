package facts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

var typeScriptGolden = filepath.Join("..", "..", "testdata", "protocol", "ts-facts-v1")

func loadPayload(t *testing.T, name string) TypeScriptPayload {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(typeScriptGolden, name))
	if err != nil {
		t.Fatalf("read golden payload: %v", err)
	}
	payload, err := DecodeTypeScriptPayload(data)
	if err != nil {
		t.Fatalf("DecodeTypeScriptPayload() error = %v", err)
	}
	return payload
}

// TestNormalizeTypeScriptConsumesRealWorkerOutput uses payloads produced by
// `pnpm facts` in ts-worker, so the wire contract is checked against the code
// that emits it and not against a hand written sample.
func TestNormalizeTypeScriptConsumesRealWorkerOutput(t *testing.T) {
	payload := loadPayload(t, "shared-library.json")
	root := filepath.Join("/repositories", "shared-library")

	set, report, err := NormalizeTypeScript(context.Background(), payload, root)
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if len(set.Repositories) != 1 || set.Repositories[0].Name != "shared-library" {
		t.Fatalf("repositories = %#v", set.Repositories)
	}
	if len(set.Packages) != 1 || set.Packages[0].Name != "@luque-fixture/shared" {
		t.Fatalf("packages = %#v", set.Packages)
	}
	if set.Packages[0].RootPath != root {
		t.Fatalf("package root = %q, want the caller supplied root", set.Packages[0].RootPath)
	}

	byQualifiedName := make(map[string]Symbol, len(set.Symbols))
	for _, symbol := range set.Symbols {
		if symbol.Key == "" || symbol.CanonicalIdentity == "" {
			t.Fatalf("symbol without identity: %#v", symbol)
		}
		if symbol.Language != LanguageTypeScript {
			t.Fatalf("symbol language = %q", symbol.Language)
		}
		byQualifiedName[symbol.QualifiedName] = symbol
	}
	for _, name := range []string{"value", "compute", "helper"} {
		if _, declared := byQualifiedName[name]; !declared {
			t.Fatalf("symbol %q missing: %v", name, byQualifiedName)
		}
	}

	references := 0
	for _, edge := range set.Edges {
		switch edge.Kind {
		case Defines:
			if edge.Provenance != TypeScriptChecker {
				t.Fatalf("DEFINES edge = %#v", edge)
			}
		case ContainsPackage, ContainsFile:
		default:
			references++
			if !edge.Confidence.Exact() || edge.EvidenceKey == "" {
				t.Fatalf("reference edge = %#v", edge)
			}
		}
	}
	if references == 0 {
		t.Fatalf("no local reference edge was produced: %#v", set.Edges)
	}
	if report.EdgesWithoutTarget != 0 {
		t.Fatalf("dropped targets = %d", report.EdgesWithoutTarget)
	}
}

func TestNormalizeTypeScriptKeepsUnresolvedFacts(t *testing.T) {
	payload := loadPayload(t, "consumer-a.json")
	set, _, err := NormalizeTypeScript(context.Background(), payload, "/repositories/consumer-a")
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(set.Unresolved) != 1 {
		t.Fatalf("unresolved = %#v", set.Unresolved)
	}
	entry := set.Unresolved[0]
	if entry.Reason != "PACKAGE_PROVIDER_NOT_FOUND" ||
		entry.RequestedPackage != "@luque-fixture/shared" ||
		entry.Language != LanguageTypeScript {
		t.Fatalf("unresolved entry = %#v", entry)
	}
	if entry.FileKey != FileKey("consumer-a", "src/direct.ts") {
		t.Fatalf("unresolved file = %q", entry.FileKey)
	}
}

func TestNormalizeTypeScriptIsDeterministicAndPortable(t *testing.T) {
	payload := loadPayload(t, "shared-library.json")
	first, _, err := NormalizeTypeScript(context.Background(), payload, "/repositories/shared-library")
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	second, _, err := NormalizeTypeScript(context.Background(), payload, "/repositories/shared-library")
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	for index := range first.Edges {
		if first.Edges[index] != second.Edges[index] {
			t.Fatalf("edge %d differs between runs", index)
		}
	}

	// Keys must not depend on where the repository is checked out.
	moved, _, err := NormalizeTypeScript(context.Background(), payload, "/elsewhere/shared-library")
	if err != nil {
		t.Fatalf("NormalizeTypeScript() error = %v", err)
	}
	for index := range first.Symbols {
		if first.Symbols[index].Key != moved.Symbols[index].Key {
			t.Fatalf("symbol key changed with the checkout path")
		}
	}
	for index := range first.Files {
		if first.Files[index].Key != moved.Files[index].Key {
			t.Fatalf("file key changed with the checkout path")
		}
	}
}

func TestDecodeTypeScriptPayloadRejectsForeignVersions(t *testing.T) {
	if _, err := DecodeTypeScriptPayload([]byte(`{"version":99}`)); !errors.Is(err, ErrInvalidFacts) {
		t.Fatalf("DecodeTypeScriptPayload() error = %v, want ErrInvalidFacts", err)
	}
	if _, err := DecodeTypeScriptPayload([]byte("not json")); err == nil {
		t.Fatalf("DecodeTypeScriptPayload() must reject malformed input")
	}
	if _, _, err := NormalizeTypeScript(context.Background(), TypeScriptPayload{
		Version: TypeScriptWireVersion,
	}, "/repositories/x"); !errors.Is(err, ErrInvalidFacts) {
		t.Fatalf("NormalizeTypeScript() must reject a payload without repository")
	}
}
