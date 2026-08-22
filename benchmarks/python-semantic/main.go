// Command python-semantic audits the exactness of the Python path over the
// fixtures of `testdata/python`, in both of the modes the path offers.
//
// The Python path has two producers and they promise different things, so a
// single number would hide the interesting half. `fallback` runs the bundled
// AST worker `python-worker/index.py`, whose payload declares
// `authoritative: false`; the normaliser therefore stamps every relation it
// reports as CANDIDATE (`internal/pythonloader/loader.go:93` and
// `internal/facts/semantic.go:295`). Publishing one exact edge from that arm
// would break the promise, so this harness measures the absence as a
// first-class property instead of asserting it in a comment. `exact` runs
// `python-worker/pyright_index.py` against a Pyright language server, whose
// payload declares `authoritative: true`, and there the question is the usual
// one: how many of its exact edges are wrong.
//
// The ground truth is written by hand from the fixture sources. Every
// expectation carries the file and the line it comes from; comparing an index
// against its own previous output proves nothing.
//
// The artifacts are deterministic: no timestamps and no machine paths, so two
// runs on the same host produce byte identical files and a regression is
// visible in the diff. The two toolchain versions are the only host dependent
// fields, and they are recorded because a measurement of Pyright that does not
// name Pyright is not reproducible.
//
// Exit status is about the measurement, never about the verdict: it is zero
// whenever the audit ran and wrote its artifacts, and the verdict lives in the
// gate token, which is printed on stdout and written into both files. A broken
// contract is `PYTHON_SEMANTIC_FAIL` there, not a missing report.
//
// Usage:
//
//	go run ./benchmarks/python-semantic
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
	fixtureRoot     = "testdata/python"
	outputDirectory = "benchmarks/python-semantic"
	command         = "go run ./benchmarks/python-semantic"
	// analyzerCommand is what the Pyright adapter spawns when the indexer is
	// configured as `auto`; the arm is skipped when it is not on the PATH.
	analyzerCommand = "pyright-langserver"
	pythonCommand   = "python3"

	gatePass   = "PYTHON_SEMANTIC_PASS"
	gateLimits = "PYTHON_SEMANTIC_PASS_WITH_LIMITS"
	gateFail   = "PYTHON_SEMANTIC_FAIL"
)

// expectedEdge is one relation the ground truth requires, written by hand from
// the fixture sources. Ends are qualified names: `coverage` declares
// `Vehicle.drive` and `Car.drive`, so a bare name would not identify an end.
type expectedEdge struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Target string `json:"target"`
}

// expectedUnresolved is a failure the fixture forces the path to declare. Only
// the import failures are listed: an import that leaves the fixture cannot
// resolve in either arm, while a bare name resolves differently -- Pyright
// reaches typeshed and reports TARGET_NOT_INDEXED where the AST worker reports
// NAME_NOT_RESOLVED -- and an expectation that depends on the arm is not a
// ground truth. Package and Symbol are matched when non empty.
type expectedUnresolved struct {
	Reason  string `json:"reason"`
	Package string `json:"package"`
	Symbol  string `json:"symbol"`
}

type auditCase struct {
	Name       string               `json:"name"`
	Fixture    string               `json:"fixture"`
	Edges      []expectedEdge       `json:"expected_edges"`
	Unresolved []expectedUnresolved `json:"expected_unresolved"`
}

// armSpec is one configuration of the Python path.
type armSpec struct {
	Name string
	// Mode is PythonAnalyzerMode: `fallback` or `exact`.
	Mode string
	// Analyzer is PythonAnalyzer; only the exact mode reads it.
	Analyzer string
	// RequiresZeroExact marks the arm whose payload is not authoritative, so
	// publishing an exact edge between symbols would be a broken promise.
	RequiresZeroExact bool
}

type caseMetrics struct {
	ExpectedEdges int `json:"expected_edges"`
	// TruePositives counts expected relations the arm published with the
	// expected kind at any confidence: the question of whether it sees the
	// relation at all. ExactTruePositives is the subset it published as
	// exact, which is what separates the two arms.
	TruePositives      int `json:"true_positives"`
	ExactTruePositives int `json:"exact_true_positives"`
	// FalseNegatives counts expected relations the arm did not publish with
	// the expected kind at any confidence.
	FalseNegatives int `json:"false_negatives"`
	// FalseExactEdges counts published exact relations whose (source,
	// target) pair the fixture sources do not contain at all. This is the
	// contract of `AGENTS.md`: an EXACT edge needs sufficient evidence, so a
	// fabricated relation is a defect and not a threshold.
	FalseExactEdges int `json:"false_exact_edges"`
	// KindMismatches counts published relations whose pair the ground truth
	// contains under another kind. The relation exists and the label is
	// coarser or different, which is a classification gap rather than a
	// fabricated edge, so it is counted apart instead of being folded into
	// the contract. An exact occurrence is prefixed `EXACT` in the listing.
	KindMismatches int `json:"kind_mismatches"`
	// ExtraCandidatePairs counts pairs the source does not contain published
	// below exact confidence. CANDIDATE is allowed to be wrong -- that is
	// what the word means -- so this is not the contract, and it is still
	// the number that says how much guessing the fallback arm does.
	ExtraCandidatePairs int `json:"extra_candidate_pairs"`
	// ExactEdges and CandidateEdges count published occurrences between two
	// symbols of the set. Package and file level edges are counted apart,
	// because they are not part of the symbol ground truth.
	ExactEdges     int `json:"exact_edges"`
	CandidateEdges int `json:"candidate_edges"`
	NonSymbolEdges int `json:"non_symbol_edges"`
	// ModuleSourcedUses counts use relations -- everything but IMPORTS_SYMBOL
	// and REEXPORTS, which a module does declare -- whose source symbol is a
	// module. A call attributed to the file instead of to the function that
	// makes it answers `find_references` at the wrong granularity.
	ModuleSourcedUses   int `json:"module_sourced_uses"`
	FunctionSourcedUses int `json:"function_sourced_uses"`
	Symbols             int `json:"symbols"`
	ExpectedUnresolved  int `json:"expected_unresolved"`
	UnresolvedMatched   int `json:"unresolved_matched"`
	UnresolvedDeclared  int `json:"unresolved_declared"`
	// ImportsWithoutEvidence counts IMPORTS_SYMBOL edges published with no
	// evidence key. The canonical set allows it, and the Rust audit does not
	// have to: it is recorded so the difference is visible.
	ImportsWithoutEvidence int `json:"imports_without_evidence"`
}

type reasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

type caseResult struct {
	Name    string      `json:"name"`
	Metrics caseMetrics `json:"metrics"`
	// IndexError is what the pass reported when it refused to produce a fact
	// set for this fixture at all. It is a defect of the arm and not a
	// limitation of the corpus, so it is kept beside the metrics rather than
	// aborting the run: the other arm and the other fixture still have
	// something to say.
	IndexError               string        `json:"index_error"`
	MissingEdges             []string      `json:"missing_edges"`
	FalseExactEdges          []string      `json:"false_exact_edges"`
	KindMismatches           []string      `json:"kind_mismatches"`
	ExtraCandidatePairs      []string      `json:"extra_candidate_pairs"`
	MissingUnresolved        []string      `json:"missing_unresolved"`
	InvariantFailures        []string      `json:"invariant_failures"`
	UnresolvedReasons        []reasonCount `json:"unresolved_reasons"`
	UnresolvedImportPackages []string      `json:"unresolved_import_packages"`
}

type armResult struct {
	Name              string       `json:"name"`
	AnalyzerMode      string       `json:"analyzer_mode"`
	Producer          string       `json:"producer"`
	Authoritative     bool         `json:"authoritative_payload"`
	Skipped           bool         `json:"skipped"`
	SkipReason        string       `json:"skip_reason"`
	RequiresZeroExact bool         `json:"requires_zero_exact"`
	ZeroExactHeld     bool         `json:"zero_exact_held"`
	Cases             []caseResult `json:"cases"`
	Totals            caseMetrics  `json:"totals"`
}

type environment struct {
	Python         string `json:"python"`
	PythonAnalyzer string `json:"python_analyzer"`
}

type report struct {
	Command     string      `json:"command"`
	Fixtures    []string    `json:"fixtures"`
	Environment environment `json:"environment"`
	Arms        []armResult `json:"arms"`
	Gate        string      `json:"gate"`
	Limitations []string    `json:"limitations"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "python-semantic: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if _, err := exec.LookPath(pythonCommand); err != nil {
		return fmt.Errorf("%s is required to measure the Python path: %w", pythonCommand, err)
	}
	measured := report{
		Command:  command,
		Fixtures: []string{fixtureRoot + "/basic", fixtureRoot + "/coverage"},
		Environment: environment{
			Python:         toolVersion(ctx, pythonCommand, "--version"),
			PythonAnalyzer: analyzerVersion(ctx),
		},
		Limitations: baseLimitations(),
	}

	analyzerAvailable := true
	analyzerAbsence := ""
	if _, err := exec.LookPath(analyzerCommand); err != nil {
		analyzerAvailable = false
		analyzerAbsence = fmt.Sprintf("%s is not on the PATH: %v", analyzerCommand, err)
	}

	for _, spec := range armSpecs() {
		arm := armResult{
			Name:              spec.Name,
			AnalyzerMode:      spec.Mode,
			Producer:          producerOf(spec),
			Authoritative:     !spec.RequiresZeroExact,
			RequiresZeroExact: spec.RequiresZeroExact,
			Cases:             make([]caseResult, 0, 2),
		}
		if spec.Mode == "exact" && !analyzerAvailable {
			arm.Skipped = true
			arm.SkipReason = analyzerAbsence
			measured.Arms = append(measured.Arms, arm)
			continue
		}
		for _, testCase := range auditCases() {
			result, err := measureCase(ctx, spec, testCase)
			if err != nil {
				return err
			}
			arm.Cases = append(arm.Cases, result)
			arm.Totals = addMetrics(arm.Totals, result.Metrics)
		}
		arm.ZeroExactHeld = arm.Totals.ExactEdges == 0
		measured.Arms = append(measured.Arms, arm)
	}

	measured.Gate, measured.Limitations = verdict(measured)

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
	return nil
}

func armSpecs() []armSpec {
	return []armSpec{
		{Name: "fallback", Mode: "fallback", RequiresZeroExact: true},
		{Name: "exact", Mode: "exact", Analyzer: "auto"},
	}
}

func producerOf(spec armSpec) string {
	if spec.Mode == "exact" {
		return "python-worker/pyright_index.py + " + analyzerCommand
	}
	return "python-worker/index.py"
}

func baseLimitations() []string {
	return []string{
		"El corpus son dos fixtures de un solo paquete cada uno: prueba los contratos, no la escala.",
		"La verdad de referencia se escribió leyendo `testdata/python/basic` y `testdata/python/coverage`; cada expectativa cita su archivo y su línea en `auditCases`.",
		"Una arista se cuenta como falsa exacta sólo cuando el par (origen, destino) no existe en el fuente. Un par que existe publicado con otra clase se cuenta aparte como discrepancia de clase: la relación está, la etiqueta es más gruesa, y llamarlo arista fabricada mentiría sobre el contrato.",
		"Ningún productor Python emite `OVERRIDES` ni `REEXPORTS`. El acceso por atributo lo resuelve el brazo exacto -- `box.get()` y `runner.run()` dan arista -- y el fallback no: sólo recorre nodos `ast.Name` en posición de lectura, y sin analizador no podría nombrar el objetivo sin adivinarlo.",
		"`Callable` aparece en `coverage/pkg/service.py:22` como anotación y el valor que se le pasa en la línea 31 es un `lambda`, que no es un símbolo declarado: no hay `PASSES_AS_CALLBACK` que esperar en este corpus.",
		"`CALLS_DIRECT pkg.service.build -> pkg.service.convert` no se publica porque `convert` está `@overload`ada: la definición que devuelve Pyright cae dentro del módulo y sobre ninguna declaración indexada, así que el productor se niega a nombrar el módulo como objetivo. La relación no se pierde -- queda retenida como `TARGET_NOT_INDEXED` con su archivo y su posición -- y publicarla exigiría resolver la sobrecarga concreta, no elegir el único candidato que queda.",
		"La verdad de referencia se extendió el `2026-08-22` con dos filas -- `REFERENCES pkg.models.Box.get -> pkg.models.Box.value` y `REFERENCES pkg.models.Vehicle.drive -> pkg.models.Vehicle.name` --, después de medir. Las dos están en el fuente (`pkg/models.py:17` sobre `:14`, y `:24` sobre `:21`) y la primera versión de la verdad no las enumeró porque el productor no visitaba un atributo y nada podía observarlas. Se declara porque extender una verdad después de medirla es exactamente lo que hay que decir: no se quitó ninguna expectativa ni se relajó ningún criterio.",
		"La medición del brazo `exact` depende de la versión de Pyright instalada y de la typeshed que trae; la del brazo `fallback` sólo del `python3` del PATH.",
	}
}

// auditCases is the ground truth, written from the fixture sources rather than
// from a previous run: comparing an index against itself proves nothing. Both
// arms are measured against the same expectations, because the fixture says
// the same thing to both.
func auditCases() []auditCase {
	return []auditCase{
		{
			Name:    "basic",
			Fixture: "basic",
			Edges: []expectedEdge{
				// pkg/__init__.py:1 `from .models import Vehicle`.
				{Kind: "IMPORTS_SYMBOL", Source: "pkg", Target: "pkg.models.Vehicle"},
				// pkg/__init__.py:3 `__all__ = ["Vehicle"]` declares the
				// public surface of the package.
				{Kind: "REEXPORTS", Source: "pkg", Target: "pkg.models.Vehicle"},
				// pkg/models.py:6 `class ElectricVehicle(Vehicle):`.
				{Kind: "EXTENDS", Source: "pkg.models.ElectricVehicle", Target: "pkg.models.Vehicle"},
				// pkg/service.py:1 `from .models import Vehicle`.
				{Kind: "IMPORTS_SYMBOL", Source: "pkg.service", Target: "pkg.models.Vehicle"},
				// pkg/service.py:5 `return Vehicle()`, inside build_vehicle
				// declared at pkg/service.py:4.
				{Kind: "CALLS_DIRECT", Source: "pkg.service.build_vehicle", Target: "pkg.models.Vehicle"},
			},
			// Nothing in this fixture is imported from outside it, so the
			// path has no import failure to declare.
			Unresolved: []expectedUnresolved{},
		},
		{
			Name:    "coverage",
			Fixture: "coverage",
			Edges: []expectedEdge{
				// pkg/__init__.py:1 `from .models import Box, Car, Vehicle`.
				{Kind: "IMPORTS_SYMBOL", Source: "pkg", Target: "pkg.models.Box"},
				{Kind: "IMPORTS_SYMBOL", Source: "pkg", Target: "pkg.models.Car"},
				{Kind: "IMPORTS_SYMBOL", Source: "pkg", Target: "pkg.models.Vehicle"},
				// pkg/__init__.py:2 `from .service import build`.
				{Kind: "IMPORTS_SYMBOL", Source: "pkg", Target: "pkg.service.build"},
				// pkg/__init__.py:4 `__all__ = ["Box", "Car", "Vehicle", "build"]`.
				{Kind: "REEXPORTS", Source: "pkg", Target: "pkg.models.Box"},
				{Kind: "REEXPORTS", Source: "pkg", Target: "pkg.models.Car"},
				{Kind: "REEXPORTS", Source: "pkg", Target: "pkg.models.Vehicle"},
				{Kind: "REEXPORTS", Source: "pkg", Target: "pkg.service.build"},
				// pkg/models.py:12 `class Box(Generic[T]):` names the type
				// variable declared at pkg/models.py:5. The base is the
				// subscript, so what the class names is `T` itself.
				{Kind: "REFERENCES", Source: "pkg.models.Box", Target: "pkg.models.T"},
				// pkg/models.py:13 `def __init__(self, value: T) -> None:`.
				{Kind: "TYPE_USES", Source: "pkg.models.Box.__init__", Target: "pkg.models.T"},
				// pkg/models.py:16 `def get(self) -> T:`.
				{Kind: "TYPE_USES", Source: "pkg.models.Box.get", Target: "pkg.models.T"},
				// pkg/models.py:17 `return self.value`, over the attribute
				// assigned at pkg/models.py:14. Added on 2026-08-22, after the
				// producer started resolving attribute occurrences: the relation
				// was always in the source and the first truth did not enumerate
				// it, because nothing could observe it. The limitations say so.
				{Kind: "REFERENCES", Source: "pkg.models.Box.get", Target: "pkg.models.Box.value"},
				// pkg/models.py:24 `return self.name`, over the class attribute
				// declared at pkg/models.py:21. Added the same day and for the
				// same reason.
				{Kind: "REFERENCES", Source: "pkg.models.Vehicle.drive", Target: "pkg.models.Vehicle.name"},
				// pkg/models.py:27 `class Car(Vehicle):`, over the class
				// declared at pkg/models.py:20.
				{Kind: "EXTENDS", Source: "pkg.models.Car", Target: "pkg.models.Vehicle"},
				// pkg/models.py:28 `def drive(self) -> str:` redeclares the
				// method of pkg/models.py:23.
				{Kind: "OVERRIDES", Source: "pkg.models.Car.drive", Target: "pkg.models.Vehicle.drive"},
				// pkg/contracts.pyi:8 `def make_runner() -> Runner: ...`,
				// over the protocol declared at pkg/contracts.pyi:4. The
				// stub is the contract and it is a source like any other.
				{Kind: "TYPE_USES", Source: "pkg.contracts.make_runner", Target: "pkg.contracts.Runner"},
				// pkg/service.py:6 `from .contracts import Runner, make_runner`.
				{Kind: "IMPORTS_SYMBOL", Source: "pkg.service", Target: "pkg.contracts.Runner"},
				{Kind: "IMPORTS_SYMBOL", Source: "pkg.service", Target: "pkg.contracts.make_runner"},
				// pkg/service.py:7 `from .models import Box, Car, Named, Vehicle`.
				{Kind: "IMPORTS_SYMBOL", Source: "pkg.service", Target: "pkg.models.Box"},
				{Kind: "IMPORTS_SYMBOL", Source: "pkg.service", Target: "pkg.models.Car"},
				{Kind: "IMPORTS_SYMBOL", Source: "pkg.service", Target: "pkg.models.Named"},
				{Kind: "IMPORTS_SYMBOL", Source: "pkg.service", Target: "pkg.models.Vehicle"},
				// pkg/service.py:22 `def run_callback(callback:
				// Callable[[Vehicle], str], vehicle: Vehicle) -> str:` names
				// Vehicle twice, once inside the Callable and once as the
				// type of the second parameter.
				{Kind: "TYPE_USES", Source: "pkg.service.run_callback", Target: "pkg.models.Vehicle"},
				// pkg/service.py:27 `vehicle = Car()`, inside build declared
				// at pkg/service.py:26.
				{Kind: "CALLS_DIRECT", Source: "pkg.service.build", Target: "pkg.models.Car"},
				// pkg/service.py:28 `box: Box[Vehicle] = Box(vehicle)`: the
				// annotation uses both, the right hand side calls Box.
				{Kind: "TYPE_USES", Source: "pkg.service.build", Target: "pkg.models.Box"},
				{Kind: "TYPE_USES", Source: "pkg.service.build", Target: "pkg.models.Vehicle"},
				{Kind: "CALLS_DIRECT", Source: "pkg.service.build", Target: "pkg.models.Box"},
				// pkg/service.py:29 `named: Named = vehicle`, over the
				// protocol declared at pkg/models.py:8.
				{Kind: "TYPE_USES", Source: "pkg.service.build", Target: "pkg.models.Named"},
				// pkg/service.py:30 `runner: Runner = make_runner()`.
				{Kind: "TYPE_USES", Source: "pkg.service.build", Target: "pkg.contracts.Runner"},
				{Kind: "CALLS_DIRECT", Source: "pkg.service.build", Target: "pkg.contracts.make_runner"},
				// pkg/service.py:31 `return convert(box.get().drive()) +
				// runner.run() + run_callback(lambda item: item.name, named)`.
				// `convert` is the implementation of pkg/service.py:18, the
				// one the two @overload stubs of lines 11 and 15 describe.
				{Kind: "CALLS_DIRECT", Source: "pkg.service.build", Target: "pkg.service.convert"},
				{Kind: "CALLS_DIRECT", Source: "pkg.service.build", Target: "pkg.service.run_callback"},
				// The chained call: `box` is annotated `Box[Vehicle]` at line
				// 28, so `box.get()` is pkg/models.py:16 and its result is a
				// Vehicle, whose `drive` is pkg/models.py:23.
				{Kind: "CALLS_DIRECT", Source: "pkg.service.build", Target: "pkg.models.Box.get"},
				{Kind: "CALLS_DIRECT", Source: "pkg.service.build", Target: "pkg.models.Vehicle.drive"},
				// `runner` is annotated `Runner` at line 30, so `runner.run()`
				// is the protocol method of pkg/contracts.pyi:5.
				{Kind: "CALLS_DIRECT", Source: "pkg.service.build", Target: "pkg.contracts.Runner.run"},
				// The lambda of line 31 is passed where `Callable[[Vehicle],
				// str]` is expected, so `item` is a Vehicle and `item.name`
				// is the attribute declared at pkg/models.py:21. The lambda
				// is not a declaration, so the reference belongs to build.
				{Kind: "REFERENCES", Source: "pkg.service.build", Target: "pkg.models.Vehicle.name"},
			},
			Unresolved: []expectedUnresolved{
				// pkg/models.py:1 and pkg/service.py:1
				// `from __future__ import annotations`.
				{Reason: "IMPORT_NOT_RESOLVED", Package: "__future__", Symbol: "annotations"},
				// pkg/models.py:3 `from typing import Generic, Protocol, TypeVar`.
				{Reason: "IMPORT_NOT_RESOLVED", Package: "typing", Symbol: "Generic"},
				{Reason: "IMPORT_NOT_RESOLVED", Package: "typing", Symbol: "Protocol"},
				{Reason: "IMPORT_NOT_RESOLVED", Package: "typing", Symbol: "TypeVar"},
				// pkg/service.py:3 `from collections.abc import Callable`.
				{Reason: "IMPORT_NOT_RESOLVED", Package: "collections.abc", Symbol: "Callable"},
				// pkg/service.py:4 `from typing import overload`.
				{Reason: "IMPORT_NOT_RESOLVED", Package: "typing", Symbol: "overload"},
			},
		},
	}
}

func measureCase(ctx context.Context, spec armSpec, testCase auditCase) (caseResult, error) {
	root, err := os.MkdirTemp("", "kivgraph-python-audit-*")
	if err != nil {
		return caseResult{}, fmt.Errorf("create audit directory: %w", err)
	}
	defer os.RemoveAll(root)
	// The workspace layer refuses a path with a symlink component, and the
	// temporary directory of macOS is one.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	source := filepath.Join(fixtureRoot, testCase.Fixture)
	target := filepath.Join(root, testCase.Fixture)
	if err := os.CopyFS(target, os.DirFS(source)); err != nil {
		return caseResult{}, fmt.Errorf("copy fixture %q: %w", source, err)
	}
	repositories := []workspace.Repository{{
		Name: testCase.Fixture, Path: target, RealPath: target, Languages: []string{"python"},
	}}

	set, _, err := indexer.Full(ctx, indexer.FullOptions{
		Repositories:       repositories,
		PythonAnalyzerMode: spec.Mode,
		PythonAnalyzer:     spec.Analyzer,
		PythonPath:         pythonCommand,
		// The producers are looked up relative to the working directory, and
		// the fixture copy is not the repository that ships them.
		WorkingDirectory: ".",
	})
	result := caseResult{
		Name:                     testCase.Name,
		MissingEdges:             make([]string, 0),
		FalseExactEdges:          make([]string, 0),
		KindMismatches:           make([]string, 0),
		ExtraCandidatePairs:      make([]string, 0),
		MissingUnresolved:        make([]string, 0),
		InvariantFailures:        make([]string, 0),
		UnresolvedReasons:        make([]reasonCount, 0),
		UnresolvedImportPackages: make([]string, 0),
	}
	result.Metrics.ExpectedEdges = len(testCase.Edges)
	result.Metrics.ExpectedUnresolved = len(testCase.Unresolved)
	if err != nil {
		// The pass refused this fixture. Everything the fixture asserts is
		// lost, and saying so is the measurement: an arm that cannot index a
		// valid package publishes nothing, which is not the same as
		// publishing nothing wrong.
		result.IndexError = err.Error()
		result.Metrics.FalseNegatives = len(testCase.Edges)
		for _, edge := range testCase.Edges {
			result.MissingEdges = append(result.MissingEdges, edgeIdentity(edge.Kind, edge.Source, edge.Target))
		}
		for _, want := range testCase.Unresolved {
			result.MissingUnresolved = append(result.MissingUnresolved,
				strings.TrimSpace(want.Reason+" "+want.Package+" "+want.Symbol))
		}
		sort.Strings(result.MissingEdges)
		sort.Strings(result.MissingUnresolved)
		return result, nil
	}
	if err := set.Validate(); err != nil {
		result.InvariantFailures = append(result.InvariantFailures, err.Error())
	}
	result.InvariantFailures = append(result.InvariantFailures, checkInvariants(set)...)

	symbols := make(map[string]facts.Symbol, len(set.Symbols))
	for _, symbol := range set.Symbols {
		symbols[symbol.Key] = symbol
	}
	result.Metrics.Symbols = len(set.Symbols)
	result.Metrics.UnresolvedDeclared = len(set.Unresolved)

	// Coverage and exactness are two questions, so every published relation
	// between two symbols is recorded with whether any occurrence of it was
	// exact. Coverage is asked of both arms -- the fallback one is required to
	// see the relation and forbidden to call it exact -- while the contract of
	// `AGENTS.md` is asked only of the exact occurrences.
	observed := make(map[string]bool)
	observedPairs := make(map[string][]string)
	for _, edge := range set.Edges {
		sourceSymbol, hasSource := symbols[edge.SourceKey]
		targetSymbol, hasTarget := symbols[edge.TargetKey]
		if !hasSource || !hasTarget {
			// A package or file level edge: real, and not part of the symbol
			// ground truth.
			result.Metrics.NonSymbolEdges++
			continue
		}
		if edge.Kind == facts.ImportsSymbol && edge.EvidenceKey == "" {
			result.Metrics.ImportsWithoutEvidence++
		}
		switch edge.Kind {
		case facts.ImportsSymbol, facts.Reexports, facts.Exports:
		default:
			if sourceSymbol.Kind == "module" {
				result.Metrics.ModuleSourcedUses++
			} else {
				result.Metrics.FunctionSourcedUses++
			}
		}
		exact := edge.Confidence.Exact()
		if exact {
			result.Metrics.ExactEdges++
		} else if edge.Confidence == facts.Candidate {
			result.Metrics.CandidateEdges++
		}
		pair := pairIdentity(sourceSymbol.QualifiedName, targetSymbol.QualifiedName)
		identity := edgeIdentity(string(edge.Kind), sourceSymbol.QualifiedName, targetSymbol.QualifiedName)
		observed[identity] = observed[identity] || exact
		observedPairs[pair] = appendUnique(observedPairs[pair], string(edge.Kind))
	}

	expected := make(map[string]struct{}, len(testCase.Edges))
	expectedPairs := make(map[string]struct{}, len(testCase.Edges))
	for _, edge := range testCase.Edges {
		expected[edgeIdentity(edge.Kind, edge.Source, edge.Target)] = struct{}{}
		expectedPairs[pairIdentity(edge.Source, edge.Target)] = struct{}{}
	}
	for _, edge := range testCase.Edges {
		identity := edgeIdentity(edge.Kind, edge.Source, edge.Target)
		exact, published := observed[identity]
		if published {
			result.Metrics.TruePositives++
			if exact {
				result.Metrics.ExactTruePositives++
			}
			continue
		}
		result.Metrics.FalseNegatives++
		result.MissingEdges = append(result.MissingEdges, identity)
	}
	for pair, kinds := range observedPairs {
		_, knownPair := expectedPairs[pair]
		for _, kind := range kinds {
			identity := edgeIdentity(kind, pairSource(pair), pairTarget(pair))
			if _, wanted := expected[identity]; wanted {
				continue
			}
			exact := observed[identity]
			label := identity
			if exact {
				label = "EXACT " + identity
			}
			if knownPair {
				// The pair is in the source and the label is not: the
				// relation exists and its class is coarser or different.
				result.Metrics.KindMismatches++
				result.KindMismatches = append(result.KindMismatches, label)
				continue
			}
			if exact {
				result.Metrics.FalseExactEdges++
				result.FalseExactEdges = append(result.FalseExactEdges, identity)
				continue
			}
			// A pair the source does not contain, published below exact. It
			// is not the contract -- CANDIDATE is allowed to be wrong, that
			// is what the word means -- and it is still worth counting.
			result.Metrics.ExtraCandidatePairs++
			result.ExtraCandidatePairs = append(result.ExtraCandidatePairs, identity)
		}
	}

	for _, want := range testCase.Unresolved {
		matched := false
		for _, entry := range set.Unresolved {
			if entry.Reason != want.Reason {
				continue
			}
			if want.Package != "" && entry.RequestedPackage != want.Package {
				continue
			}
			if want.Symbol != "" && entry.RequestedSymbol != want.Symbol {
				continue
			}
			matched = true
			break
		}
		if matched {
			result.Metrics.UnresolvedMatched++
			continue
		}
		result.MissingUnresolved = append(result.MissingUnresolved,
			strings.TrimSpace(want.Reason+" "+want.Package+" "+want.Symbol))
	}
	result.UnresolvedReasons = countReasons(set)
	result.UnresolvedImportPackages = importPackages(set)

	sort.Strings(result.MissingEdges)
	sort.Strings(result.FalseExactEdges)
	sort.Strings(result.KindMismatches)
	sort.Strings(result.ExtraCandidatePairs)
	sort.Strings(result.MissingUnresolved)
	sort.Strings(result.InvariantFailures)
	return result, nil
}

// checkInvariants runs the canonical rules that do not depend on a ground
// truth: an exact edge needs an exact provenance, an evidence key must resolve
// inside the set, and an unresolved entry needs a subject.
//
// It deliberately does not require every reference edge to carry evidence, the
// way the Rust audit does. The Python normaliser publishes IMPORTS_SYMBOL with
// no evidence key at `internal/facts/semantic.go:362`, and the canonical
// `Set.Validate` allows an absent key while rejecting a dangling one. Turning
// that into an invariant failure would report a design decision as corruption,
// so it is counted in `imports_without_evidence` instead and named as a
// limitation.
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
		if edge.EvidenceKey != "" {
			if _, exists := evidence[edge.EvidenceKey]; !exists {
				failures = append(failures, string(edge.Kind)+" references unknown evidence")
			}
			continue
		}
		switch edge.Kind {
		case facts.References, facts.CallsDirect, facts.TypeUses, facts.Extends,
			facts.Implements, facts.Overrides, facts.PassesAsCallback,
			facts.AssignsFunction, facts.ReturnsFunction:
			failures = append(failures, string(edge.Kind)+" without evidence")
		}
	}
	for _, entry := range set.Unresolved {
		if strings.TrimSpace(entry.Reason) == "" || strings.TrimSpace(entry.RequestedPackage) == "" {
			failures = append(failures, "unresolved entry without a subject")
		}
	}
	return failures
}

func countReasons(set facts.Set) []reasonCount {
	counts := make(map[string]int)
	for _, entry := range set.Unresolved {
		counts[entry.Reason]++
	}
	reasons := make([]string, 0, len(counts))
	for reason := range counts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	result := make([]reasonCount, 0, len(reasons))
	for _, reason := range reasons {
		result = append(result, reasonCount{Reason: reason, Count: counts[reason]})
	}
	return result
}

func importPackages(set facts.Set) []string {
	seen := make(map[string]struct{})
	packages := make([]string, 0)
	for _, entry := range set.Unresolved {
		if entry.Reason != "IMPORT_NOT_RESOLVED" {
			continue
		}
		if _, exists := seen[entry.RequestedPackage]; exists {
			continue
		}
		seen[entry.RequestedPackage] = struct{}{}
		packages = append(packages, entry.RequestedPackage)
	}
	sort.Strings(packages)
	return packages
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func edgeIdentity(kind, source, target string) string {
	return fmt.Sprintf("%s %s -> %s", kind, source, target)
}

func pairIdentity(source, target string) string {
	return source + " -> " + target
}

func pairSource(pair string) string {
	if index := strings.Index(pair, " -> "); index >= 0 {
		return pair[:index]
	}
	return pair
}

func pairTarget(pair string) string {
	if index := strings.Index(pair, " -> "); index >= 0 {
		return pair[index+len(" -> "):]
	}
	return ""
}

func addMetrics(left, right caseMetrics) caseMetrics {
	left.ExpectedEdges += right.ExpectedEdges
	left.TruePositives += right.TruePositives
	left.ExactTruePositives += right.ExactTruePositives
	left.FalseNegatives += right.FalseNegatives
	left.FalseExactEdges += right.FalseExactEdges
	left.KindMismatches += right.KindMismatches
	left.ExtraCandidatePairs += right.ExtraCandidatePairs
	left.ExactEdges += right.ExactEdges
	left.CandidateEdges += right.CandidateEdges
	left.NonSymbolEdges += right.NonSymbolEdges
	left.ModuleSourcedUses += right.ModuleSourcedUses
	left.FunctionSourcedUses += right.FunctionSourcedUses
	left.Symbols += right.Symbols
	left.ExpectedUnresolved += right.ExpectedUnresolved
	left.UnresolvedMatched += right.UnresolvedMatched
	left.UnresolvedDeclared += right.UnresolvedDeclared
	left.ImportsWithoutEvidence += right.ImportsWithoutEvidence
	return left
}

// verdict decides the gate token and appends the limitations the run observed
// to the ones it declares up front. A broken contract is a FAIL token, not a
// missing report: the measurement happened either way and hiding it would be
// the one outcome worse than a failure.
func verdict(measured report) (string, []string) {
	limitations := measured.Limitations
	failures := make([]string, 0)
	observed := make([]string, 0)
	for _, arm := range measured.Arms {
		if arm.Skipped {
			observed = append(observed, fmt.Sprintf(
				"El brazo `%s` no se midió: %s. La ausencia se declara, no se cuenta como cero.", arm.Name, arm.SkipReason))
			continue
		}
		indexed := true
		for _, result := range arm.Cases {
			if result.IndexError == "" {
				continue
			}
			indexed = false
			failures = append(failures, fmt.Sprintf(
				"El brazo `%s` no pudo indexar el fixture `%s`: %s.", arm.Name, result.Name, result.IndexError))
		}
		if arm.Totals.FalseExactEdges != 0 {
			failures = append(failures, fmt.Sprintf(
				"El brazo `%s` publicó %d aristas exactas cuyo par no existe en el fuente.", arm.Name, arm.Totals.FalseExactEdges))
		}
		if arm.RequiresZeroExact && !arm.ZeroExactHeld {
			failures = append(failures, fmt.Sprintf(
				"El brazo `%s` no es autoritativo y publicó %d aristas exactas entre símbolos.", arm.Name, arm.Totals.ExactEdges))
		}
		if indexed && !arm.RequiresZeroExact && arm.Totals.ExactEdges == 0 {
			failures = append(failures, fmt.Sprintf(
				"El brazo `%s` se declara autoritativo y no publicó ninguna arista exacta entre símbolos.", arm.Name))
		}
		if indexed && arm.Totals.UnresolvedMatched != arm.Totals.ExpectedUnresolved {
			failures = append(failures, fmt.Sprintf(
				"El brazo `%s` declaró %d de %d fallos esperados.", arm.Name, arm.Totals.UnresolvedMatched, arm.Totals.ExpectedUnresolved))
		}
		for _, result := range arm.Cases {
			if len(result.InvariantFailures) != 0 {
				failures = append(failures, fmt.Sprintf(
					"El caso `%s` del brazo `%s` rompió %d invariante(s) canónica(s).", result.Name, arm.Name, len(result.InvariantFailures)))
			}
		}
		if arm.Totals.FalseNegatives != 0 {
			observed = append(observed, fmt.Sprintf(
				"El brazo `%s` no publicó %d de %d relaciones del fuente; están enumeradas por nombre en la sección del brazo.",
				arm.Name, arm.Totals.FalseNegatives, arm.Totals.ExpectedEdges))
		}
		if arm.Totals.KindMismatches != 0 {
			observed = append(observed, fmt.Sprintf(
				"El brazo `%s` publicó %d relaciones existentes con otra clase que la que dice el fuente.", arm.Name, arm.Totals.KindMismatches))
		}
		if arm.Totals.ModuleSourcedUses != 0 {
			observed = append(observed, fmt.Sprintf(
				"El brazo `%s` atribuyó %d usos al símbolo de módulo y %d a una función o clase.",
				arm.Name, arm.Totals.ModuleSourcedUses, arm.Totals.FunctionSourcedUses))
		}
		if arm.Totals.ImportsWithoutEvidence != 0 {
			observed = append(observed, fmt.Sprintf(
				"El brazo `%s` publicó %d aristas `IMPORTS_SYMBOL` sin clave de evidencia (`internal/facts/semantic.go:362`).",
				arm.Name, arm.Totals.ImportsWithoutEvidence))
		}
	}
	limitations = append(limitations, observed...)
	if len(failures) != 0 {
		return gateFail, append(limitations, failures...)
	}
	if len(observed) != 0 {
		return gateLimits, limitations
	}
	return gatePass, limitations
}

func toolVersion(ctx context.Context, name string, args ...string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return "unavailable"
	}
	output, err := exec.CommandContext(ctx, path, args...).CombinedOutput()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(output)), "\n", 2)[0])
}

// analyzerVersion reports the Pyright build the exact arm measures. The
// language server itself refuses `--version` -- it wants a transport -- so the
// CLI of the same package answers for it.
func analyzerVersion(ctx context.Context) string {
	if _, err := exec.LookPath(analyzerCommand); err != nil {
		return "unavailable"
	}
	return toolVersion(ctx, "pyright", "--version")
}

func render(measured report) string {
	var out strings.Builder
	out.WriteString("# Exactitud semántica Python\n\n")
	out.WriteString("Auditoría del camino Python sobre los fixtures de `testdata/python`, en sus dos modos.\n")
	out.WriteString("Se regenera con `" + measured.Command + "`.\n\n")
	out.WriteString("La verdad de referencia está escrita a mano desde los fuentes del fixture, en\n")
	out.WriteString("`auditCases` de `main.go`, y cada expectativa cita el archivo y la línea de la\n")
	out.WriteString("que sale. Comparar un índice contra su propia salida anterior no demuestra nada.\n\n")
	out.WriteString("El código de salida habla de la medición, nunca del veredicto: es `0` siempre que\n")
	out.WriteString("la auditoría se ejecutó y escribió sus artefactos. El veredicto es el token del\n")
	out.WriteString("gate, que va en `stdout` y en los dos archivos. Dos ejecuciones seguidas producen\n")
	out.WriteString("`results.json` y `report.md` idénticos byte a byte; los dos únicos campos que\n")
	out.WriteString("dependen del host son las versiones de la sección Entorno.\n\n")

	out.WriteString("## Fixtures\n\n")
	for _, fixture := range measured.Fixtures {
		out.WriteString("- `" + fixture + "`\n")
	}
	out.WriteString("\n## Entorno\n\n")
	out.WriteString("- `python3`: `" + measured.Environment.Python + "`\n")
	out.WriteString("- analizador del brazo `exact`: `" + measured.Environment.PythonAnalyzer + "`\n")

	out.WriteString("\n## Totales\n\n")
	out.WriteString("`TP` cuenta las relaciones del fuente que el brazo publica con la clase esperada\n")
	out.WriteString("a cualquier confianza; `TP exactas` el subconjunto que publica como exacta. La\n")
	out.WriteString("diferencia entre las dos columnas es la promesa de cada brazo.\n\n")
	out.WriteString("| Brazo | Modo | Exactas | Candidatas | Esperadas | TP | TP exactas | FN | Falsas exactas | Clase distinta | Símbolos | No resueltas |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, arm := range measured.Arms {
		if arm.Skipped {
			fmt.Fprintf(&out, "| %s | %s | no medido | no medido | no medido | no medido | no medido | no medido | no medido | no medido | no medido | no medido |\n",
				arm.Name, arm.AnalyzerMode)
			continue
		}
		fmt.Fprintf(&out, "| %s | %s | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d/%d |\n",
			arm.Name, arm.AnalyzerMode, arm.Totals.ExactEdges, arm.Totals.CandidateEdges,
			arm.Totals.ExpectedEdges, arm.Totals.TruePositives, arm.Totals.ExactTruePositives,
			arm.Totals.FalseNegatives, arm.Totals.FalseExactEdges, arm.Totals.KindMismatches,
			arm.Totals.Symbols, arm.Totals.UnresolvedMatched, arm.Totals.ExpectedUnresolved)
	}

	out.WriteString("\n## Los dos brazos\n\n")
	out.WriteString("El camino Python tiene dos productores y no prometen lo mismo, así que un solo\n")
	out.WriteString("número escondería la mitad interesante.\n\n")
	for _, arm := range measured.Arms {
		out.WriteString("### Brazo `" + arm.Name + "`\n\n")
		out.WriteString("- `PythonAnalyzerMode`: `" + arm.AnalyzerMode + "`\n")
		out.WriteString("- productor: `" + arm.Producer + "`\n")
		fmt.Fprintf(&out, "- payload `authoritative`: `%t`\n", arm.Authoritative)
		if arm.Skipped {
			out.WriteString("- **no medido**: " + arm.SkipReason + "\n")
			out.WriteString("- la ausencia se declara y se salta; no se inventa un cero ni se cuenta como fallo del código.\n\n")
			continue
		}
		if arm.RequiresZeroExact {
			out.WriteString("- propiedad exigida: **ninguna arista exacta entre símbolos**, porque el payload\n")
			out.WriteString("  no es autoritativo y la confianza se decide en `internal/facts/semantic.go:295`.\n")
			fmt.Fprintf(&out, "- propiedad cumplida: `%t` (%d aristas exactas, %d candidatas)\n",
				arm.ZeroExactHeld, arm.Totals.ExactEdges, arm.Totals.CandidateEdges)
			fmt.Fprintf(&out, "- cobertura como `CANDIDATE`: %d de %d relaciones del fuente\n",
				arm.Totals.TruePositives, arm.Totals.ExpectedEdges)
		} else {
			out.WriteString("- propiedad exigida: **cero falsas exactas**, el contrato de `AGENTS.md`.\n")
			fmt.Fprintf(&out, "- propiedad cumplida: `%t` (%d falsas exactas de %d exactas)\n",
				arm.Totals.FalseExactEdges == 0, arm.Totals.FalseExactEdges, arm.Totals.ExactEdges)
			fmt.Fprintf(&out, "- cobertura exacta: %d de %d relaciones del fuente\n",
				arm.Totals.ExactTruePositives, arm.Totals.ExpectedEdges)
		}
		fmt.Fprintf(&out, "- usos atribuidos al módulo: %d; a una función o clase: %d\n",
			arm.Totals.ModuleSourcedUses, arm.Totals.FunctionSourcedUses)
		fmt.Fprintf(&out, "- `IMPORTS_SYMBOL` sin evidencia: %d\n", arm.Totals.ImportsWithoutEvidence)
		fmt.Fprintf(&out, "- relaciones con clase distinta a la del fuente: %d; pares ausentes del fuente por debajo de exacta: %d\n",
			arm.Totals.KindMismatches, arm.Totals.ExtraCandidatePairs)
		out.WriteString("\n#### Casos\n\n")
		out.WriteString("| Caso | Esperadas | TP | TP exactas | FN | Falsas exactas | Clase distinta | Exactas | Candidatas | Símbolos | No resueltas | Declaradas |\n")
		out.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for _, result := range arm.Cases {
			fmt.Fprintf(&out, "| %s | %d | %d | %d | %d | %d | %d | %d | %d | %d | %d/%d | %d |\n",
				result.Name, result.Metrics.ExpectedEdges, result.Metrics.TruePositives,
				result.Metrics.ExactTruePositives, result.Metrics.FalseNegatives,
				result.Metrics.FalseExactEdges, result.Metrics.KindMismatches,
				result.Metrics.ExactEdges, result.Metrics.CandidateEdges, result.Metrics.Symbols,
				result.Metrics.UnresolvedMatched, result.Metrics.ExpectedUnresolved,
				result.Metrics.UnresolvedDeclared)
		}
		for _, result := range arm.Cases {
			out.WriteString("\n#### " + arm.Name + " / " + result.Name + "\n\n")
			if result.IndexError != "" {
				out.WriteString("- **la pasada no produjo hechos para este fixture**: " + result.IndexError + "\n")
			}
			if len(result.UnresolvedReasons) != 0 {
				out.WriteString("- motivos no resueltos:")
				for index, entry := range result.UnresolvedReasons {
					if index != 0 {
						out.WriteString(",")
					}
					fmt.Fprintf(&out, " `%s`=%d", entry.Reason, entry.Count)
				}
				out.WriteString("\n")
			}
			if len(result.UnresolvedImportPackages) != 0 {
				out.WriteString("- paquetes de import no resueltos: ")
				for index, name := range result.UnresolvedImportPackages {
					if index != 0 {
						out.WriteString(", ")
					}
					out.WriteString("`" + name + "`")
				}
				out.WriteString("\n")
			}
			for _, entry := range result.MissingEdges {
				out.WriteString("- falta: " + entry + "\n")
			}
			for _, entry := range result.FalseExactEdges {
				out.WriteString("- **falsa exacta**: " + entry + "\n")
			}
			for _, entry := range result.KindMismatches {
				out.WriteString("- clase distinta: " + entry + "\n")
			}
			for _, entry := range result.ExtraCandidatePairs {
				out.WriteString("- par ausente del fuente, por debajo de exacta: " + entry + "\n")
			}
			for _, entry := range result.MissingUnresolved {
				out.WriteString("- no resuelta ausente: " + entry + "\n")
			}
			for _, entry := range result.InvariantFailures {
				out.WriteString("- invariante rota: " + entry + "\n")
			}
		}
		out.WriteString("\n")
	}

	out.WriteString(renderFindings(measured))

	out.WriteString("## Limitaciones\n\n")
	for _, limitation := range measured.Limitations {
		out.WriteString("- " + limitation + "\n")
	}
	out.WriteString("\n## Gate\n\n```text\n" + measured.Gate + "\n```\n")
	return out.String()
}

// renderFindings names the code that produces the numbers above. Only the
// findings of an arm that actually ran are written: a defect nobody observed is
// not a finding.
func renderFindings(measured report) string {
	var out strings.Builder
	out.WriteString("## Hallazgos\n\n")
	out.WriteString("Cada uno explica un número de las tablas y se nombra con archivo y línea.\n")
	out.WriteString("Los corregidos se describen con el defecto que tenían, para que la cifra de\n")
	out.WriteString("esta pasada no se lea sin su historia; los que no, dicen por qué.\n\n")
	for _, arm := range measured.Arms {
		if arm.Skipped {
			continue
		}
		out.WriteString("### Brazo `" + arm.Name + "`\n\n")
		if arm.RequiresZeroExact {
			out.WriteString("1. Un `from .x import Y` dentro de un `__init__.py` no resuelve.\n")
			out.WriteString("   `python-worker/index.py:185` calcula la base restando `node.level` a las\n")
			out.WriteString("   partes del módulo actual, que es correcto para `pkg.service` -- nivel 1 da\n")
			out.WriteString("   `pkg` -- y falso para el propio paquete: el módulo de `pkg/__init__.py` ya\n")
			out.WriteString("   **es** `pkg`, así que nivel 1 da base `models` en vez de `pkg.models`. Es\n")
			out.WriteString("   la causa de los `IMPORTS_SYMBOL` ausentes que salen de `pkg` en los dos\n")
			out.WriteString("   fixtures, y de los paquetes `models` y `service` en la lista de imports no\n")
			out.WriteString("   resueltos.\n")
			out.WriteString("2. `__all__` no se lee. Es una lista de constantes de texto y el recorrido\n")
			out.WriteString("   sólo mira nodos `ast.Name` en posición de lectura, así que ninguna arista\n")
			out.WriteString("   `REEXPORTS` se publica aunque `pkg/__init__.py` declare su superficie\n")
			out.WriteString("   pública.\n")
			out.WriteString("3. Una anotación subscrita degrada de `TYPE_USES` a `REFERENCES`.\n")
			out.WriteString("   `is_type_position` (`python-worker/index.py:244`) reconoce el nodo que **es**\n")
			out.WriteString("   la anotación, no uno anidado dentro de ella, así que en\n")
			out.WriteString("   `box: Box[Vehicle]` (`testdata/python/coverage/pkg/service.py:28`) ni `Box`\n")
			out.WriteString("   ni `Vehicle` cuentan como uso de tipo. La relación sigue ahí; la clase es\n")
			out.WriteString("   más gruesa.\n")
			out.WriteString("4. Una llamada por atributo no produce ninguna arista. `box.get()`,\n")
			out.WriteString("   `runner.run()` e `item.name` (`service.py:31`) son nodos `ast.Attribute` y el\n")
			out.WriteString("   recorrido no los visita, así que la llamada encadenada del fixture es\n")
			out.WriteString("   invisible en el grafo.\n\n")
			continue
		}
		out.WriteString("1. El adaptador pedía capacidades vacías y luego asumía la respuesta\n")
		out.WriteString("   anidada. `pyright_index.py` mandaba `\"capabilities\": {}` en el\n")
		out.WriteString("   `initialize`, así que Pyright contestaba `textDocument/documentSymbol` con\n")
		out.WriteString("   la forma plana `SymbolInformation[]`, que no lleva `children`; `visit` sólo\n")
		out.WriteString("   anida a partir de `children`, así que todo símbolo recibía el prefijo del\n")
		out.WriteString("   módulo y perdía su clase. `Vehicle.drive`\n")
		out.WriteString("   (`testdata/python/coverage/pkg/models.py:23`) y `Car.drive` (`:28`) daban\n")
		out.WriteString("   los dos `pkg.models.drive`, el normalizador publicaba dos `DEFINES` para\n")
		out.WriteString("   una clave y `facts.Set.Validate` rechazaba el conjunto entero: el fixture\n")
		out.WriteString("   `coverage` no se indexaba en absoluto. Ahora se anuncia\n")
		out.WriteString("   `hierarchicalDocumentSymbolSupport` y el fixture se indexa.\n")
		out.WriteString("2. Ninguna referencia salía de la función que la hace: el productor ponía\n")
		out.WriteString("   `sourceId: module_id` en todas, así que `find_references` contestaba a\n")
		out.WriteString("   granularidad de archivo. Peor, ese origen equivocado fabricaba una arista\n")
		out.WriteString("   exacta: `EXTENDS pkg.models -> pkg.models.Vehicle` sobre un fuente que\n")
		out.WriteString("   dice `class ElectricVehicle(Vehicle):`\n")
		out.WriteString("   (`testdata/python/basic/pkg/models.py:6`), y un módulo no hereda de nada.\n")
		out.WriteString("   Ahora la referencia se atribuye a la declaración que la encierra; es la\n")
		out.WriteString("   columna `usos atribuidos al módulo`.\n")
		out.WriteString("3. Las variables y los parámetros locales se publicaban como símbolos, así\n")
		out.WriteString("   que una función sostenía aristas hacia sus propios locales y hacia sí\n")
		out.WriteString("   misma. Ninguna existe en el fuente: eran dieciséis exactas falsas. Un\n")
		out.WriteString("   local no lo puede nombrar nadie desde fuera, que es la misma regla que el\n")
		out.WriteString("   camino de Go aplica a una declaración que no alcanza el ámbito de\n")
		out.WriteString("   paquete.\n")
		out.WriteString("4. Un objetivo que Pyright sitúa dentro de un archivo pero sobre ninguna\n")
		out.WriteString("   declaración indexada resolvía al módulo, porque es el único símbolo cuyo\n")
		out.WriteString("   rango cubre el archivo entero. Eso no es un objetivo resuelto: es uno que\n")
		out.WriteString("   no se pudo identificar, y publicarlo era ganar una arista `EXACT` por ser\n")
		out.WriteString("   el último candidato. Ahora se retiene como `TARGET_NOT_INDEXED`; el precio\n")
		out.WriteString("   está en las limitaciones, con la llamada a la función `@overload`ada que\n")
		out.WriteString("   deja de publicarse.\n")
		out.WriteString("5. Lo que sigue sin publicarse en este brazo: `__all__` no se lee, así que\n")
		out.WriteString("   no hay `REEXPORTS`; un acceso por atributo es un nodo `ast.Attribute` que\n")
		out.WriteString("   el recorrido no visita, así que `box.get()`, `runner.run()` e `item.name`\n")
		out.WriteString("   (`testdata/python/coverage/pkg/service.py:31`) no dan arista; y una\n")
		out.WriteString("   anotación subscrita degrada a `REFERENCES`, porque el nodo anidado dentro\n")
		out.WriteString("   de `Box[Vehicle]` no se reconoce como posición de tipo. La relación está;\n")
		out.WriteString("   la clase es más gruesa.\n\n")
	}
	return out.String()
}
