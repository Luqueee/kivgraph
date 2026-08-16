// Command rust-semantic audits the exactness of the Rust path over the
// fixtures of LUQUE-1813.
//
// It answers the two questions the gate asks separately: how many exact edges
// are wrong (compared against a ground truth written by hand), and whether the
// published set holds the canonical invariants (every edge with both ends and
// its evidence inside the set).
//
// The artifacts are deterministic: no timestamps and no machine paths, so a
// rerun on another host produces byte identical files and a regression is
// visible in the diff.
//
// Usage:
//
//	go run ./benchmarks/rust-semantic
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/indexer"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const (
	fixtureRoot     = "testdata/rust"
	outputDirectory = "benchmarks/rust-semantic"
	command         = "go run ./benchmarks/rust-semantic"
	// gate is the token this corpus can justify. The fixtures are three
	// crates: they prove the contracts, not the scale, and calling that a
	// full pass would be reporting a promise instead of a measurement.
	gate = "RUST_SEMANTIC_PASS_WITH_LIMITS"
)

// expectedEdge is one relation the ground truth requires, written by hand from
// the fixture sources.
type expectedEdge struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Target string `json:"target"`
	// CrossRepository marks an edge whose ends are indexed separately.
	CrossRepository bool `json:"cross_repository"`
}

type expectedUnresolved struct {
	Reason string `json:"reason"`
	Crate  string `json:"crate"`
}

type auditCase struct {
	Name         string               `json:"name"`
	Repositories []string             `json:"repositories"`
	Edges        []expectedEdge       `json:"expected_edges"`
	Unresolved   []expectedUnresolved `json:"expected_unresolved"`
}

type caseMetrics struct {
	ExpectedEdges      int `json:"expected_edges"`
	TruePositives      int `json:"true_positives"`
	FalseNegatives     int `json:"false_negatives"`
	FalseExactEdges    int `json:"false_exact_edges"`
	ExactEdges         int `json:"exact_edges"`
	Symbols            int `json:"symbols"`
	ExpectedUnresolved int `json:"expected_unresolved"`
	UnresolvedMatched  int `json:"unresolved_matched"`
}

type caseResult struct {
	Name              string      `json:"name"`
	Metrics           caseMetrics `json:"metrics"`
	MissingEdges      []string    `json:"missing_edges"`
	UnexpectedExact   []string    `json:"unexpected_exact_edges"`
	MissingUnresolved []string    `json:"missing_unresolved"`
	InvariantFailures []string    `json:"invariant_failures"`
}

type report struct {
	Command     string       `json:"command"`
	Fixtures    []string     `json:"fixtures"`
	Cases       []caseResult `json:"cases"`
	Totals      caseMetrics  `json:"totals"`
	Gate        string       `json:"gate"`
	Limitations []string     `json:"limitations"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "rust-semantic: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if _, err := exec.LookPath("rust-analyzer"); err != nil {
		return fmt.Errorf("rust-analyzer is required to measure the Rust path: %w", err)
	}
	measured := report{
		Command: command,
		Fixtures: []string{
			fixtureRoot + "/workspace",
			fixtureRoot + "/cross-repository",
		},
		Gate: gate,
		Limitations: []string{
			"El corpus son cuatro fixtures de crates: prueba los contratos, no la escala.",
			"PASSES_AS_CALLBACK, ASSIGNS_FUNCTION y RETURNS_FUNCTION exigen un destino invocable indexado en la misma pasada; hacia otro repositorio la clase degrada a REFERENCES.",
			"IMPLEMENTS, EXTENDS y OVERRIDES se derivan de la forma del impl y del bound: relationships viaja vacío.",
			"La medición depende de la versión de rust-analyzer instalada y de su sysroot.",
		},
	}
	for _, testCase := range auditCases() {
		result, err := measureCase(ctx, testCase)
		if err != nil {
			return err
		}
		measured.Cases = append(measured.Cases, result)
		measured.Totals.ExpectedEdges += result.Metrics.ExpectedEdges
		measured.Totals.TruePositives += result.Metrics.TruePositives
		measured.Totals.FalseNegatives += result.Metrics.FalseNegatives
		measured.Totals.FalseExactEdges += result.Metrics.FalseExactEdges
		measured.Totals.ExactEdges += result.Metrics.ExactEdges
		measured.Totals.Symbols += result.Metrics.Symbols
		measured.Totals.ExpectedUnresolved += result.Metrics.ExpectedUnresolved
		measured.Totals.UnresolvedMatched += result.Metrics.UnresolvedMatched
	}

	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return fmt.Errorf("create %q: %w", outputDirectory, err)
	}
	encoded, err := json.MarshalIndent(measured, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDirectory, "results.json"), append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDirectory, "report.md"), []byte(render(measured)), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}

	fmt.Println(measured.Gate)
	if failed := measured.Totals.FalseExactEdges + measured.Totals.FalseNegatives; failed != 0 {
		return fmt.Errorf("audit failed: %d false exact edges, %d missing edges",
			measured.Totals.FalseExactEdges, measured.Totals.FalseNegatives)
	}
	for _, result := range measured.Cases {
		if len(result.InvariantFailures) != 0 {
			return fmt.Errorf("case %q broke a canonical invariant", result.Name)
		}
		if result.Metrics.UnresolvedMatched != result.Metrics.ExpectedUnresolved {
			return fmt.Errorf("case %q declared %d of %d expected failures",
				result.Name, result.Metrics.UnresolvedMatched, result.Metrics.ExpectedUnresolved)
		}
	}
	return nil
}

// auditCases is the ground truth, written from the fixture sources rather than
// from a previous run: comparing an index against itself proves nothing.
func auditCases() []auditCase {
	return []auditCase{
		{
			Name:         "workspace",
			Repositories: []string{"workspace"},
			Edges: []expectedEdge{
				{Kind: "CALLS_DIRECT", Source: "run", Target: "double"},
				{Kind: "TYPE_USES", Source: "run", Target: "Value"},
				{Kind: "CALLS_DIRECT", Source: "helper_user", Target: "private_helper"},
				{Kind: "IMPORTS_SYMBOL", Source: "crate", Target: "double"},
				{Kind: "IMPORTS_SYMBOL", Source: "crate", Target: "Value"},
				// `use support::{...}` names the provider crate root, and the
				// struct literal names the field it fills.
				{Kind: "IMPORTS_SYMBOL", Source: "crate", Target: "crate"},
				{Kind: "REFERENCES", Source: "run", Target: "inner"},
				// El módulo de traits: relaciones estructurales y los usos
				// que las acompañan.
				{Kind: "IMPLEMENTS", Source: "Circle", Target: "Named"},
				{Kind: "IMPLEMENTS", Source: "Circle", Target: "Drawable"},
				{Kind: "EXTENDS", Source: "Drawable", Target: "Named"},
				{Kind: "OVERRIDES", Source: "name", Target: "name"},
				{Kind: "OVERRIDES", Source: "draw", Target: "draw"},
				{Kind: "REFERENCES", Source: "crate", Target: "shapes"},
				{Kind: "REFERENCES", Source: "draw", Target: "radius"},
				{Kind: "REFERENCES", Source: "new", Target: "radius"},
				{Kind: "TYPE_USES", Source: "shapes", Target: "Circle"},
			},
			Unresolved: []expectedUnresolved{
				{Reason: "CRATE_PROVIDER_NOT_FOUND", Crate: "core"},
				// `-> Self` names the implementation block, which SCIP
				// mentions and never defines.
				{Reason: "DEFINITION_NOT_INDEXED", Crate: "support"},
			},
		},
		{
			Name:         "cross-repository",
			Repositories: []string{"provider", "consumer"},
			Edges: []expectedEdge{
				{Kind: "CALLS_DIRECT", Source: "run", Target: "double", CrossRepository: true},
				{Kind: "TYPE_USES", Source: "run", Target: "Value", CrossRepository: true},
				{Kind: "IMPORTS_SYMBOL", Source: "crate", Target: "double", CrossRepository: true},
				{Kind: "IMPORTS_SYMBOL", Source: "crate", Target: "Value", CrossRepository: true},
				{Kind: "IMPORTS_SYMBOL", Source: "crate", Target: "crate", CrossRepository: true},
				{Kind: "REFERENCES", Source: "run", Target: "inner", CrossRepository: true},
			},
			Unresolved: []expectedUnresolved{
				{Reason: "CRATE_PROVIDER_NOT_FOUND", Crate: "core"},
			},
		},
		{
			Name:         "cross-repository-negative",
			Repositories: []string{"consumer"},
			Unresolved: []expectedUnresolved{
				{Reason: "CRATE_PROVIDER_NOT_FOUND", Crate: "support"},
			},
		},
		{
			// Function values: naming a function is not calling it, and the
			// three shapes are three different relations. The constant is
			// here to prove the negative -- an argument that is not callable
			// must stay a plain reference.
			Name:         "function-values",
			Repositories: []string{"values"},
			Edges: []expectedEdge{
				{Kind: "PASSES_AS_CALLBACK", Source: "passes_double", Target: "double"},
				{Kind: "CALLS_DIRECT", Source: "passes_double", Target: "apply"},
				{Kind: "ASSIGNS_FUNCTION", Source: "binds_double", Target: "double"},
				{Kind: "RETURNS_FUNCTION", Source: "picks_double", Target: "double"},
				{Kind: "RETURNS_FUNCTION", Source: "returns_explicitly", Target: "double"},
				{Kind: "CALLS_DIRECT", Source: "passes_limit", Target: "takes_limit"},
				{Kind: "REFERENCES", Source: "passes_limit", Target: "LIMIT"},
				{Kind: "REFERENCES", Source: "binds_limit", Target: "LIMIT"},
			},
			Unresolved: []expectedUnresolved{
				{Reason: "CRATE_PROVIDER_NOT_FOUND", Crate: "core"},
			},
		},
	}
}

func measureCase(ctx context.Context, testCase auditCase) (caseResult, error) {
	root, err := os.MkdirTemp("", "kivgraph-rust-audit-*")
	if err != nil {
		return caseResult{}, fmt.Errorf("create audit directory: %w", err)
	}
	defer os.RemoveAll(root)
	// The workspace layer refuses a path with a symlink component, and the
	// temporary directory of macOS is one.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}

	source := filepath.Join(fixtureRoot, "workspace")
	switch testCase.Name {
	case "workspace":
	case "function-values":
		source = filepath.Join(fixtureRoot, "function-values")
	default:
		source = filepath.Join(fixtureRoot, "cross-repository")
	}
	repositories := make([]workspace.Repository, 0, len(testCase.Repositories))
	for _, name := range testCase.Repositories {
		target := filepath.Join(root, name)
		from := source
		if testCase.Name != "workspace" && testCase.Name != "function-values" {
			from = filepath.Join(source, name)
		}
		if err := os.CopyFS(target, os.DirFS(from)); err != nil {
			return caseResult{}, fmt.Errorf("copy fixture %q: %w", from, err)
		}
		repositories = append(repositories, workspace.Repository{
			Name: name, Path: target, RealPath: target, Languages: []string{"rust"},
		})
	}
	// The negative case indexes the consumer alone, but its path dependency
	// still has to exist on disk for cargo to resolve the workspace.
	if testCase.Name == "cross-repository-negative" {
		if err := os.CopyFS(filepath.Join(root, "provider"), os.DirFS(filepath.Join(source, "provider"))); err != nil {
			return caseResult{}, fmt.Errorf("copy provider sources: %w", err)
		}
	}

	set, _, err := indexer.Full(ctx, indexer.FullOptions{
		Repositories:        repositories,
		RustAnalyzer:        "rust-analyzer",
		RustTargetDirectory: filepath.Join(root, "target"),
		RustBuildScripts:    true,
		RustProcMacros:      true,
		RustSysroot:         "discover",
		WorkingDirectory:    root,
	})
	if err != nil {
		return caseResult{}, fmt.Errorf("index case %q: %w", testCase.Name, err)
	}

	result := caseResult{Name: testCase.Name}
	if err := set.Validate(); err != nil {
		result.InvariantFailures = append(result.InvariantFailures, err.Error())
	}
	result.InvariantFailures = append(result.InvariantFailures, checkInvariants(set)...)

	symbols := make(map[string]facts.Symbol, len(set.Symbols))
	for _, symbol := range set.Symbols {
		symbols[symbol.Key] = symbol
	}
	result.Metrics.Symbols = len(set.Symbols)
	result.Metrics.ExpectedEdges = len(testCase.Edges)
	result.Metrics.ExpectedUnresolved = len(testCase.Unresolved)

	observed := make(map[string]struct{})
	for _, edge := range set.Edges {
		if !edge.Confidence.Exact() {
			continue
		}
		source, hasSource := symbols[edge.SourceKey]
		target, hasTarget := symbols[edge.TargetKey]
		if !hasSource || !hasTarget {
			// A package or file level edge: counted as exact, not part of
			// the symbol ground truth.
			continue
		}
		result.Metrics.ExactEdges++
		cross := source.RepositoryKey != target.RepositoryKey
		observed[edgeIdentity(string(edge.Kind), source.Name, target.Name, cross)] = struct{}{}
	}
	expected := make(map[string]struct{}, len(testCase.Edges))
	for _, edge := range testCase.Edges {
		identity := edgeIdentity(edge.Kind, edge.Source, edge.Target, edge.CrossRepository)
		expected[identity] = struct{}{}
		if _, exists := observed[identity]; exists {
			result.Metrics.TruePositives++
			continue
		}
		result.Metrics.FalseNegatives++
		result.MissingEdges = append(result.MissingEdges, identity)
	}
	for identity := range observed {
		if _, exists := expected[identity]; exists {
			continue
		}
		result.Metrics.FalseExactEdges++
		result.UnexpectedExact = append(result.UnexpectedExact, identity)
	}

	for _, want := range testCase.Unresolved {
		matched := false
		for _, entry := range set.Unresolved {
			if entry.Reason == want.Reason && entry.RequestedPackage == want.Crate {
				matched = true
				break
			}
		}
		if matched {
			result.Metrics.UnresolvedMatched++
			continue
		}
		result.MissingUnresolved = append(result.MissingUnresolved, want.Reason+" "+want.Crate)
	}

	sort.Strings(result.MissingEdges)
	sort.Strings(result.UnexpectedExact)
	sort.Strings(result.MissingUnresolved)
	sort.Strings(result.InvariantFailures)
	return result, nil
}

// checkInvariants runs the canonical rules that do not depend on a ground
// truth: an exact edge needs an exact provenance, a reference edge needs its
// observation, and an unresolved entry needs a subject.
func checkInvariants(set facts.Set) []string {
	failures := make([]string, 0)
	evidence := make(map[string]struct{}, len(set.Evidence))
	for _, entry := range set.Evidence {
		evidence[entry.Key] = struct{}{}
	}
	for _, edge := range set.Edges {
		if edge.Confidence.Exact() && !edge.Provenance.Exact() {
			failures = append(failures, fmt.Sprintf("%s claims %s from %s", edge.Kind, edge.Confidence, edge.Provenance))
		}
		switch edge.Kind {
		case facts.References, facts.CallsDirect, facts.TypeUses, facts.ImportsSymbol, facts.Reexports:
			if edge.EvidenceKey == "" {
				failures = append(failures, string(edge.Kind)+" without evidence")
				continue
			}
			if _, exists := evidence[edge.EvidenceKey]; !exists {
				failures = append(failures, string(edge.Kind)+" references unknown evidence")
			}
		}
	}
	for _, entry := range set.Unresolved {
		if strings.TrimSpace(entry.Reason) == "" || strings.TrimSpace(entry.RequestedPackage) == "" {
			failures = append(failures, "unresolved entry without a subject")
		}
	}
	return failures
}

func edgeIdentity(kind, source, target string, cross bool) string {
	scope := "local"
	if cross {
		scope = "cross"
	}
	return fmt.Sprintf("%s %s -> %s (%s)", kind, source, target, scope)
}

func render(measured report) string {
	var out strings.Builder
	out.WriteString("# Exactitud semántica Rust\n\n")
	out.WriteString("Auditoría de LUQUE-1816 sobre los fixtures de LUQUE-1813.\n")
	out.WriteString("Se regenera con `" + measured.Command + "`.\n\n")
	out.WriteString("## Fixtures\n\n")
	for _, fixture := range measured.Fixtures {
		out.WriteString("- `" + fixture + "`\n")
	}
	out.WriteString("\n## Totales\n\n")
	fmt.Fprintf(&out, "- ocurrencias de arista exacta entre símbolos: %d\n", measured.Totals.ExactEdges)
	fmt.Fprintf(&out, "- aristas esperadas: %d\n", measured.Totals.ExpectedEdges)
	fmt.Fprintf(&out, "- true positives: %d\n", measured.Totals.TruePositives)
	fmt.Fprintf(&out, "- false negatives: %d\n", measured.Totals.FalseNegatives)
	fmt.Fprintf(&out, "- false exact edges: %d\n", measured.Totals.FalseExactEdges)
	fmt.Fprintf(&out, "- símbolos publicados: %d\n", measured.Totals.Symbols)
	fmt.Fprintf(&out, "- no resueltas declaradas: %d/%d\n\n",
		measured.Totals.UnresolvedMatched, measured.Totals.ExpectedUnresolved)

	out.WriteString("## Casos\n\n")
	out.WriteString("| Caso | Esperadas | TP | FN | Falsas exactas | Símbolos | No resueltas |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, result := range measured.Cases {
		fmt.Fprintf(&out, "| %s | %d | %d | %d | %d | %d | %d/%d |\n",
			result.Name, result.Metrics.ExpectedEdges, result.Metrics.TruePositives,
			result.Metrics.FalseNegatives, result.Metrics.FalseExactEdges,
			result.Metrics.Symbols, result.Metrics.UnresolvedMatched, result.Metrics.ExpectedUnresolved)
	}
	for _, result := range measured.Cases {
		if len(result.MissingEdges)+len(result.UnexpectedExact)+len(result.MissingUnresolved)+len(result.InvariantFailures) == 0 {
			continue
		}
		out.WriteString("\n### " + result.Name + "\n\n")
		for _, entry := range result.MissingEdges {
			out.WriteString("- falta: " + entry + "\n")
		}
		for _, entry := range result.UnexpectedExact {
			out.WriteString("- exacta inesperada: " + entry + "\n")
		}
		for _, entry := range result.MissingUnresolved {
			out.WriteString("- no resuelta ausente: " + entry + "\n")
		}
		for _, entry := range result.InvariantFailures {
			out.WriteString("- invariante rota: " + entry + "\n")
		}
	}
	out.WriteString("\n## Limitaciones\n\n")
	for _, limitation := range measured.Limitations {
		out.WriteString("- " + limitation + "\n")
	}
	out.WriteString("\n## Gate\n\n```text\n" + measured.Gate + "\n```\n")
	return out.String()
}
