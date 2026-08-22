// Command dart-semantic audits the exactness of the Dart path over the
// fixtures of testdata/dart.
//
// It answers the two questions the gate asks separately: how many exact edges
// are wrong (compared against a ground truth written by hand from the fixture
// sources), and whether the published set holds the canonical invariants
// (every exact edge with an exact provenance, every reference with its
// observation, every unresolved row with a subject).
//
// The artifacts are deterministic: no timestamps and no machine paths, so a
// rerun on another host produces byte identical files and a regression is
// visible in the diff.
//
// Usage:
//
//	go run ./benchmarks/dart-semantic
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
	"time"

	"github.com/Luqueee/kivgraph/internal/dartloader"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/indexer"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const (
	fixtureRoot     = "testdata/dart"
	outputDirectory = "benchmarks/dart-semantic"
	command         = "go run ./benchmarks/dart-semantic"
	// dartCommand is the analyzer driver. The Analysis Server ships with the
	// SDK, so the measurement depends on the `dart` of the PATH and says so
	// when it is missing instead of reporting a zero.
	dartCommand = "dart"
	// analysisBudget is generous on purpose: the Analysis Server loads the
	// whole package plus its SDK summary before it answers navigation.
	analysisBudget = 3 * time.Minute
	// gate is the token this corpus can justify. The fixtures are two pub
	// packages: they prove the contracts, not the scale, and calling that a
	// full pass would be reporting a promise instead of a measurement.
	gate = "DART_SEMANTIC_PASS_WITH_LIMITS"
	// gateFailure is what the same corpus reports when the measurement finds
	// a false exact edge, a missing relation, a broken invariant or a failure
	// the pass never declared. A pass token over a failed audit would be the
	// promise this task exists to retire.
	gateFailure = "DART_SEMANTIC_FAIL"
)

// expectedEdge is one relation the ground truth requires, written by hand from
// the fixture sources.
type expectedEdge struct {
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Target string `json:"target"`
	// CrossRepository marks an edge whose ends are indexed separately. No
	// Dart fixture has one yet; the field keeps the identity shared with the
	// Rust harness so a future provider case reads the same.
	CrossRepository bool `json:"cross_repository,omitempty"`
}

// expectedUnresolved is one failure the ground truth requires the pass to
// declare. It is keyed by the reason, the file that observed it and the kind
// of the target the Analysis Server named: the requested package of a Dart
// unresolved row is the absolute path of the analyzer target, which is a
// machine path and never enters an artifact.
type expectedUnresolved struct {
	Reason string `json:"reason"`
	File   string `json:"file"`
	Symbol string `json:"symbol"`
}

type auditCase struct {
	Name       string               `json:"name"`
	Fixture    string               `json:"fixture"`
	Edges      []expectedEdge       `json:"expected_edges"`
	Unresolved []expectedUnresolved `json:"expected_unresolved"`
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
	// UnresolvedDeclared is every failure row the pass published, not only
	// the ones the ground truth asked for: a fact lost in silence is the
	// defect this audit exists to catch.
	UnresolvedDeclared int `json:"unresolved_declared"`
	// DirectiveEdgesWithoutEvidence counts the import, export and part edges
	// published from a directive rather than from an observed reference.
	// They carry no evidence key, so the reference invariant cannot ask them
	// for one; the count keeps the gap visible instead of assumed.
	DirectiveEdgesWithoutEvidence int `json:"directive_edges_without_evidence"`
	// EdgesSourcedAtModule counts the reference relations whose source is the
	// synthetic module symbol of a file instead of a declaration. A directive
	// legitimately belongs to its compilation unit; a call, a type use or a
	// class header never does, so this number is the mechanical signature of
	// the attribution defect the findings name.
	EdgesSourcedAtModule int `json:"edges_sourced_at_module"`
	// SelfReferenceEdges counts the relations whose two ends are the same
	// symbol: a declaration is not a use of itself.
	SelfReferenceEdges int `json:"self_reference_edges"`
	// SymbolsSpanningOnlyTheirName counts the published declarations whose
	// range covers exactly the identifier. A Dart declaration is longer than
	// its name, so every row here is a span the producer lost.
	SymbolsSpanningOnlyTheirName int `json:"symbols_spanning_only_their_name"`
}

type caseResult struct {
	Name               string      `json:"name"`
	Metrics            caseMetrics `json:"metrics"`
	MissingEdges       []string    `json:"missing_edges"`
	UnexpectedExact    []string    `json:"unexpected_exact_edges"`
	MissingUnresolved  []string    `json:"missing_unresolved"`
	ObservedUnresolved []string    `json:"observed_unresolved"`
	InvariantFailures  []string    `json:"invariant_failures"`
}

type report struct {
	Command  string       `json:"command"`
	Fixtures []string     `json:"fixtures"`
	Cases    []caseResult `json:"cases"`
	Totals   caseMetrics  `json:"totals"`
	Gate     string       `json:"gate"`
	// Findings are the defects this pass observed in the Dart path. Each one
	// names the file and the line that produces it and the number in this
	// artifact that measures it.
	Findings    []string `json:"findings"`
	Limitations []string `json:"limitations"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "dart-semantic: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if _, err := exec.LookPath(dartCommand); err != nil {
		return fmt.Errorf("the %q executable is required to measure the Dart path: %w", dartCommand, err)
	}
	// The SDK directory is only a fallback command for the loader, so a
	// discovery failure is not fatal: it is reported and the PATH entry is
	// used on its own.
	sdkPath, sdkErr := dartloader.SDKRoot(dartCommand)
	measured := report{
		Command: command,
		Fixtures: []string{
			fixtureRoot + "/basic",
			fixtureRoot + "/advanced",
		},
		Findings: []string{
			"El origen de una referencia es ahora la declaración que la contiene, no el símbolo de módulo del archivo. `initialize` anuncia `hierarchicalDocumentSymbolSupport`, así que `textDocument/documentSymbol` responde `DocumentSymbol` con hijos y con el rango del cuerpo; antes respondía `SymbolInformation` planos cuyo `location.range` cubre sólo el identificador, `enclosing` no encontraba ninguna declaración que contuviera la ocurrencia y caía al módulo. Publicaba `EXTENDS models.dart -> Vehicle` para `class ElectricVehicle extends Vehicle`: una arista `EXACT` con el origen equivocado. Lo miden `edges_sourced_at_module` y `symbols_spanning_only_their_name`.",
			"Una declaración no se referencia a sí misma. La guarda comparaba desplazamientos, y el elemento de una directiva `library` vive en el desplazamiento 0 mientras la región está sobre el nombre, así que cuatro bucles pasaban como exactos. Ahora compara identidades; lo mide `self_reference_edges`.",
			"Un cuerpo con flecha no es una asignación: `String asText() => value.toString()` publicaba `ASSIGNS_FUNCTION` porque cualquier `=` del prefijo valía. Ahora se exige un `=` que no forme `=>`, `==` ni un operador compuesto, y un `=>` seguido de la expresión completa cuenta como retorno.",
			"Un `enum`, un `mixin` y un `extension type` usados como tipo dan `TYPE_USES`. El Analysis Server 1.40.1 manda `kind: \"UNKNOWN\"` para esos objetivos, así que la clase no la puede decidir el kind y la decide la posición: en `describe(VehicleKind kind)` la ocurrencia anota el nombre que la sigue. La referencia ya venía resuelta; esto sólo elige la relación, que es lo que hace cada rama de `dartReferenceKind`.",
			"El campo de representación de un `extension type` se publica. Ninguna de las dos fuentes de declaraciones lo lista y el Analysis Server resuelve su uso a un `PARAMETER`, así que todo uso apuntaba fuera del grafo aunque el campo es nameable como `id.value`; `appendExtensionTypeRepresentations` lo lee de la cabecera de la propia declaración.",
			"Un `extension type` ya no compite con su archivo por la identidad de módulo. El LSP lo reporta como `SymbolKind.Namespace` (3) y `dartKind` mapeaba 2, 3 y 4 a `module`; `NormalizeSemantic` indexa `moduleKeys` por ese kind y gana el último del orden por `ID`, así que en un archivo cuya ruta ordene después de `module` la declaración le quitaba al archivo su identidad. Reproducido en un paquete temporal con `src/feature.dart` -- una biblioteca con `part 'piece.dart';` y un `extension type UserId(int value)` --, donde la arista publicada era `PART_OF src.piece -> UserId`; con el mapeo corregido apunta al módulo del archivo.",
			"Una declaración observada por las dos fuentes se publicaba dos veces. Una fila del outline del analizador sin localización de elemento caía en el inicio de la declaración mientras la del LSP caía en el identificador, así que la deduplicación por posición no las unía; ahora la fila sin localización resuelve el desplazamiento de su propio nombre, y las filas que comparten identidad se colapsan.",
			"Las aristas de directiva (`IMPORTS_SYMBOL`, `REEXPORTS`, `PART_OF`) ya llevan `evidence_key`, que es lo que `AGENTS.md` exige de una arista canónica. La pasada anterior las publicó sin él y aplazó el arreglo por dos razones que resultaron falsas al medirlas: el payload no lo comparten los cinco lenguajes sino **dos** -- `facts.SemanticPayload` sólo lo usan `pythonloader` y `dartloader`; Go, TypeScript y Rust tienen su propio normalizador--, y los dos productores de Python **ya enviaban el fin** de cada import en su helper `point()`. No hubo cambio de protocolo: el decodificador Go no tenía campos donde ponerlo y lo tiraba. `directive_edges_without_evidence` pasa de `4` a `0`, y `imports_without_evidence` de Python de `7`/`12` a `0`.",
			"Lo que el `evidence_key` **no** cambia hoy es la respuesta servida, y conviene no venderlo de más: comparadas las dos versiones del binario sobre el mismo fixture, la fila de `find_references` para un `PART_OF` es idéntica byte a byte. La causa es que `hotsnapshot.EvidenceRecord` no proyecta la posición -- lleva clave, los dos ficheros, clase y procedencia--, así que ninguna tool puede abrir el vano de una evidencia en ninguno de los cinco lenguajes; la fila usa el rango del símbolo y `evidence_kind` sale de la procedencia. Queda declarado y no se arregla aquí: proyectarlo sube la versión del formato de filas del snapshot y ningún consumidor lo pide.",
			"Una relación `part` se observa desde sus dos extremos -- `part 'piece.dart';` y `part of 'feature.dart';` --, y **no** producía dos aristas: `NormalizeSemantic` ya deduplicaba por identidad desde el commit que trajo Dart, así que el hallazgo anterior era falso y se midió para comprobarlo (`PART_OF` = `1` arista con `2` filas de payload). Lo que sí faltaba era decidir *cuál* de las dos observaciones lleva la evidencia. Una arista, porque dos directivas declaran una relación -- a diferencia de dos llamadas, que son dos sitios que un agente tiene que visitar--, y la evidencia va en el archivo parte, que es el origen de la arista, aunque el otro extremo se observe primero. `SemanticPart` gana `File`: `LibraryFile` y `PartFile` nombran los extremos de la relación, no dónde está el texto.",
			"Una comparación ya no se clasifica como `PASSES_AS_CALLBACK`. Eran dos defectos ortogonales y ninguno arreglaba al otro: en `if (other == handler)` el `(` lo abre un keyword y no un callee, y en `register(other == handler)` el `(` sí abre una llamada pero la ocurrencia es operando de la comparación. Se arreglan con dos reglas -- `comparedInDartPrefix` y `opensDartArgument`, que busca el corchete sin cerrar más interno y mira qué identificador lo precede--, y el fixture las ejercita: `lib/language_features.dart` publica `REFERENCES prefersFallback -> fallback` y `PASSES_AS_CALLBACK choose -> fallback` desde las dos formas. Escribir el fixture destapó un tercer caso que nadie había nombrado: `final same = (other == handler)` salía `ASSIGNS_FUNCTION`, porque asignar el resultado de una comparación no es asignar la función.",
			"Las `36` aristas exactas publicadas caen en `31` identidades: la identidad de una arista es `clase nombre -> nombre`, así que dos relaciones de la misma clase entre homónimos -- el `OVERRIDES drive -> drive` que cubre a la vez la superclase y la interfaz -- colapsan en una fila. No hay ninguna identidad observada que la verdad no espere.",
		},
		Limitations: []string{
			"El corpus son dos paquetes pub de un solo repositorio: prueba los contratos, no la escala ni el camino cross-repository.",
			"La medición depende del SDK que respalda el `dart` del PATH: el Analysis Server viaja con él, y esta pasada usó el 1.40.1 del Dart SDK 3.13.1.",
			"La identidad de una arista es `clase nombre -> nombre`, así que dos relaciones de la misma clase entre homónimos colapsan en una fila: `OVERRIDES drive -> drive` cubre a la vez la superclase y la interfaz de `testdata/dart/basic/lib/models.dart:7`.",
			"Un objetivo que el Analysis Server resuelve fuera del conjunto publicado -- el SDK, un parámetro, un parámetro de tipo, un prefijo de import -- no es una arista: se retiene como `UNRESOLVED` con motivo `DART_TARGET_NOT_INDEXED` (`internal/dartloader/loader.go:635-648`).",
		},
	}
	if sdkErr != nil {
		measured.Limitations = append(measured.Limitations,
			"El directorio del SDK no se pudo descubrir desde el ejecutable `dart`; la medición usó únicamente el comando del PATH.")
		sdkPath = ""
	}
	invariantFailures := 0
	for _, testCase := range auditCases() {
		result, err := measureCase(ctx, testCase, sdkPath)
		if err != nil {
			return err
		}
		measured.Cases = append(measured.Cases, result)
		invariantFailures += len(result.InvariantFailures)
		measured.Totals.ExpectedEdges += result.Metrics.ExpectedEdges
		measured.Totals.TruePositives += result.Metrics.TruePositives
		measured.Totals.FalseNegatives += result.Metrics.FalseNegatives
		measured.Totals.FalseExactEdges += result.Metrics.FalseExactEdges
		measured.Totals.ExactEdges += result.Metrics.ExactEdges
		measured.Totals.Symbols += result.Metrics.Symbols
		measured.Totals.ExpectedUnresolved += result.Metrics.ExpectedUnresolved
		measured.Totals.UnresolvedMatched += result.Metrics.UnresolvedMatched
		measured.Totals.UnresolvedDeclared += result.Metrics.UnresolvedDeclared
		measured.Totals.DirectiveEdgesWithoutEvidence += result.Metrics.DirectiveEdgesWithoutEvidence
		measured.Totals.EdgesSourcedAtModule += result.Metrics.EdgesSourcedAtModule
		measured.Totals.SelfReferenceEdges += result.Metrics.SelfReferenceEdges
		measured.Totals.SymbolsSpanningOnlyTheirName += result.Metrics.SymbolsSpanningOnlyTheirName
	}
	// The gate says what the measurement says. False exact edges are the
	// contract of AGENTS.md and not a threshold, so a single one turns the
	// token into a failure instead of a pass with limits.
	failures := measured.Totals.FalseExactEdges + measured.Totals.FalseNegatives + invariantFailures +
		(measured.Totals.ExpectedUnresolved - measured.Totals.UnresolvedMatched)
	measured.Gate = gate
	if failures != 0 {
		measured.Gate = gateFailure
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
	if measured.Totals.FalseExactEdges+measured.Totals.FalseNegatives != 0 {
		return fmt.Errorf("audit failed: %d false exact edges, %d missing edges",
			measured.Totals.FalseExactEdges, measured.Totals.FalseNegatives)
	}
	if invariantFailures != 0 {
		return fmt.Errorf("audit failed: %d canonical invariant failures", invariantFailures)
	}
	if measured.Totals.UnresolvedMatched != measured.Totals.ExpectedUnresolved {
		return fmt.Errorf("audit failed: %d of %d expected failures declared",
			measured.Totals.UnresolvedMatched, measured.Totals.ExpectedUnresolved)
	}
	return nil
}

// auditCases is the ground truth, written from the fixture sources rather than
// from a previous run: comparing an index against itself proves nothing.
//
// Every row cites the file and the line of the fixture that justifies it. A
// source is the innermost declaration that contains the occurrence, which is
// the synthetic module symbol -- named after the file -- for anything written
// at the top level of a compilation unit, such as a directive.
func auditCases() []auditCase {
	return []auditCase{
		{
			Name:    "basic",
			Fixture: "basic",
			Edges: []expectedEdge{
				// lib/service.dart:1 `import 'models.dart';` -- the
				// directive, and the URI the Analysis Server navigates.
				{Kind: "IMPORTS_SYMBOL", Source: "service.dart", Target: "models.dart"},
				{Kind: "REFERENCES", Source: "service.dart", Target: "models.dart"},
				// lib/service.dart:4 `Vehicle buildVehicle() => Vehicle();`
				// -- the return type is a type use, the constructor is a call.
				{Kind: "TYPE_USES", Source: "buildVehicle", Target: "Vehicle"},
				{Kind: "CALLS_DIRECT", Source: "buildVehicle", Target: "Vehicle"},
				// lib/models.dart:5 `class ElectricVehicle extends Vehicle
				// with Chargeable implements Transport` -- three headers,
				// three different relations.
				{Kind: "EXTENDS", Source: "ElectricVehicle", Target: "Vehicle"},
				{Kind: "EMBEDS", Source: "ElectricVehicle", Target: "Chargeable"},
				{Kind: "IMPLEMENTS", Source: "ElectricVehicle", Target: "Transport"},
				// lib/models.dart:7 `String drive() => 'electric';` under
				// `@override`: it overrides `Vehicle.drive` (lib/models.dart:2)
				// and `Transport.drive` (lib/models.dart:15). Both collapse
				// into one identity because the three members are homonyms.
				{Kind: "OVERRIDES", Source: "drive", Target: "drive"},
			},
			Unresolved: []expectedUnresolved{
				// `String` at lib/models.dart:2, :7 and :15, and `int` at
				// lib/models.dart:11: dart:core is not published, so the
				// reference is retained instead of invented.
				{Reason: "DART_TARGET_NOT_INDEXED", File: "lib/models.dart", Symbol: "CLASS"},
			},
		},
		{
			Name:    "advanced",
			Fixture: "advanced",
			Edges: []expectedEdge{
				// lib/library.dart:3 `part 'part.dart';` with
				// lib/part.dart:1 `part of 'library.dart';` -- one module
				// relation from both sides, plus the navigated URI of each.
				{Kind: "PART_OF", Source: "part.dart", Target: "library.dart"},
				{Kind: "REFERENCES", Source: "library.dart", Target: "part.dart"},
				{Kind: "REFERENCES", Source: "part.dart", Target: "library.dart"},
				// lib/library.dart:4 `export 'models.dart';` -- a Dart
				// export forwards another library's surface.
				{Kind: "REEXPORTS", Source: "library.dart", Target: "models.dart"},
				{Kind: "REFERENCES", Source: "library.dart", Target: "models.dart"},
				// lib/library.dart:6 `String libraryValue() =>
				// PartValue().value;` -- the constructor is a call and the
				// getter of lib/part.dart:4 is read, not assigned.
				{Kind: "CALLS_DIRECT", Source: "libraryValue", Target: "PartValue"},
				{Kind: "REFERENCES", Source: "libraryValue", Target: "value"},
				// lib/conditional.dart:1 `import 'models.dart' if
				// (dart.library.io) 'models.dart' as models;`
				{Kind: "IMPORTS_SYMBOL", Source: "conditional.dart", Target: "models.dart"},
				{Kind: "REFERENCES", Source: "conditional.dart", Target: "models.dart"},
				// lib/conditional.dart:3 `models.ExportedModel makeModel() =>
				// models.ExportedModel('conditional');` -- the return type is
				// a type use of lib/models.dart:1, the expression calls the
				// constructor of lib/models.dart:4.
				{Kind: "TYPE_USES", Source: "makeModel", Target: "ExportedModel"},
				{Kind: "CALLS_DIRECT", Source: "makeModel", Target: "ExportedModel"},
				// lib/models.dart:4 `ExportedModel(this.name);` -- the field
				// formal names the field of lib/models.dart:2.
				{Kind: "REFERENCES", Source: "ExportedModel", Target: "name"},
				// lib/language_features.dart:9 `final class Success<T> extends
				// Result<T>` -- the sealed class of :5.
				{Kind: "EXTENDS", Source: "Success", Target: "Result"},
				// lib/language_features.dart:12 `const Success(this.value);`
				// -- the field of :10.
				{Kind: "REFERENCES", Source: "Success", Target: "value"},
				// lib/language_features.dart:18 `String asText() =>
				// value.toString();` -- the representation field of the
				// extension type declared at :17.
				{Kind: "REFERENCES", Source: "asText", Target: "value"},
				// lib/language_features.dart:21 `describe(VehicleKind kind)`
				// -- the parameter type is a type use of the enum of :3.
				{Kind: "TYPE_USES", Source: "describe", Target: "VehicleKind"},
				// lib/language_features.dart:23 and :24 -- `VehicleKind.car`
				// and `VehicleKind.bike` name the enum as a qualifier and
				// read two of its constants. A qualifier is not a type use
				// and an enum constant is not a type.
				{Kind: "REFERENCES", Source: "describe", Target: "VehicleKind"},
				{Kind: "REFERENCES", Source: "describe", Target: "car"},
				{Kind: "REFERENCES", Source: "describe", Target: "bike"},
				// lib/language_features.dart:29 `handler == fallback` -- an
				// operand of a comparison. This shape and the argument below
				// look identical to a bracket test: both put a `(` before the
				// occurrence and a `)` after it, and both used to publish
				// PASSES_AS_CALLBACK.
				{Kind: "REFERENCES", Source: "prefersFallback", Target: "fallback"},
				// lib/language_features.dart:31 `runWith(fallback)` -- the
				// argument, where the parenthesis really does open a call.
				{Kind: "PASSES_AS_CALLBACK", Source: "choose", Target: "fallback"},
				// The same line calls both: `runWith(...)` and `fallback()`.
				{Kind: "CALLS_DIRECT", Source: "choose", Target: "runWith"},
				{Kind: "CALLS_DIRECT", Source: "choose", Target: "fallback"},
			},
			Unresolved: []expectedUnresolved{
				// `String` at lib/library.dart:6 and lib/models.dart:2.
				{Reason: "DART_TARGET_NOT_INDEXED", File: "lib/library.dart", Symbol: "CLASS"},
				{Reason: "DART_TARGET_NOT_INDEXED", File: "lib/models.dart", Symbol: "CLASS"},
				// `String` at lib/language_features.dart:18 and :21, `int` at
				// :17 and :21.
				{Reason: "DART_TARGET_NOT_INDEXED", File: "lib/language_features.dart", Symbol: "CLASS"},
				// `T` at lib/language_features.dart:9, :10 and :15, and `R`
				// at :15: a type parameter is not a published declaration.
				{Reason: "DART_TARGET_NOT_INDEXED", File: "lib/language_features.dart", Symbol: "TYPE_PARAMETER"},
				// `kind` at lib/language_features.dart:22: a parameter is not
				// a published declaration either.
				{Reason: "DART_TARGET_NOT_INDEXED", File: "lib/language_features.dart", Symbol: "PARAMETER"},
			},
		},
	}
}

func measureCase(ctx context.Context, testCase auditCase, sdkPath string) (caseResult, error) {
	root, err := os.MkdirTemp("", "kivgraph-dart-audit-*")
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
	target := filepath.Join(root, testCase.Name)
	if err := os.CopyFS(target, os.DirFS(source)); err != nil {
		return caseResult{}, fmt.Errorf("copy fixture %q: %w", source, err)
	}
	repositories := []workspace.Repository{{
		Name: testCase.Name, Path: target, RealPath: target, Languages: []string{"dart"},
	}}

	set, _, err := indexer.Full(ctx, indexer.FullOptions{
		Repositories:            repositories,
		DartAnalyzer:            dartCommand,
		DartSDKPath:             sdkPath,
		DartWaitForAnalysis:     true,
		DartMaximumAnalysisTime: analysisBudget,
		WorkingDirectory:        root,
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
	result.Metrics.UnresolvedDeclared = len(set.Unresolved)
	for _, symbol := range set.Symbols {
		if symbol.Kind == "module" {
			continue
		}
		if symbol.End.Offset-symbol.Start.Offset == len(symbol.Name) {
			result.Metrics.SymbolsSpanningOnlyTheirName++
		}
	}
	observedText := make(map[string]string, len(set.Evidence))
	for _, entry := range set.Evidence {
		observedText[entry.Key] = strings.TrimSpace(entry.Text)
	}

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
		if directiveEdge(edge.Kind) {
			if edge.EvidenceKey == "" {
				result.Metrics.DirectiveEdgesWithoutEvidence++
			}
		} else if source.Kind == "module" && !directiveOccurrence(observedText[edge.EvidenceKey]) {
			result.Metrics.EdgesSourcedAtModule++
		}
		if edge.SourceKey == edge.TargetKey {
			result.Metrics.SelfReferenceEdges++
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

	paths := make(map[string]string, len(set.Files))
	for _, file := range set.Files {
		paths[file.Key] = file.Path
	}
	declared := make(map[string]int, len(set.Unresolved))
	for _, entry := range set.Unresolved {
		declared[unresolvedIdentity(entry.Reason, paths[entry.FileKey], entry.RequestedSymbol)]++
	}
	for _, want := range testCase.Unresolved {
		identity := unresolvedIdentity(want.Reason, want.File, want.Symbol)
		if declared[identity] > 0 {
			result.Metrics.UnresolvedMatched++
			continue
		}
		result.MissingUnresolved = append(result.MissingUnresolved, identity)
	}
	for identity, count := range declared {
		result.ObservedUnresolved = append(result.ObservedUnresolved, fmt.Sprintf("%s x%d", identity, count))
	}

	sort.Strings(result.MissingEdges)
	sort.Strings(result.UnexpectedExact)
	sort.Strings(result.MissingUnresolved)
	sort.Strings(result.ObservedUnresolved)
	sort.Strings(result.InvariantFailures)
	return result, nil
}

// checkInvariants runs the canonical rules that do not depend on a ground
// truth: an exact edge needs an exact provenance, a reference edge needs its
// observation, and an unresolved entry needs a subject.
//
// A directive edge is deliberately outside the observation rule: the Dart
// producer publishes it from the directive and not as a reference, so it
// carries no evidence key. That gap is counted and reported rather than
// asserted away.
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
		case facts.References, facts.CallsDirect, facts.TypeUses, facts.PassesAsCallback,
			facts.AssignsFunction, facts.ReturnsFunction, facts.Implements, facts.Extends,
			facts.Embeds, facts.Overrides:
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

// directiveEdge reports whether the relation is published from a Dart
// directive instead of an observed reference.
func directiveEdge(kind facts.EdgeKind) bool {
	switch kind {
	case facts.ImportsSymbol, facts.Exports, facts.Reexports, facts.PartOf:
		return true
	default:
		return false
	}
}

// directiveOccurrence reports whether the observed line is a Dart directive.
// A reference written there belongs to the compilation unit, so the module
// symbol is its rightful source and it is not evidence of misattribution.
func directiveOccurrence(text string) bool {
	for _, keyword := range []string{"import ", "export ", "part ", "part of ", "library "} {
		if strings.HasPrefix(text, keyword) {
			return true
		}
	}
	return false
}

func edgeIdentity(kind, source, target string, cross bool) string {
	scope := "local"
	if cross {
		scope = "cross"
	}
	return fmt.Sprintf("%s %s -> %s (%s)", kind, source, target, scope)
}

func unresolvedIdentity(reason, file, symbol string) string {
	if strings.TrimSpace(file) == "" {
		file = "(sin archivo)"
	}
	if strings.TrimSpace(symbol) == "" {
		symbol = "(sin clase)"
	}
	return fmt.Sprintf("%s %s %s", reason, file, symbol)
}

func render(measured report) string {
	var out strings.Builder
	out.WriteString("# Exactitud semántica Dart\n\n")
	out.WriteString("Auditoría de LUQUE-2201 sobre los fixtures de `testdata/dart`, con el método\n")
	out.WriteString("de LUQUE-1816: verdad de referencia escrita a mano leyendo los fuentes.\n")
	out.WriteString("Se regenera con `" + measured.Command + "`.\n\n")
	fmt.Fprintf(&out, "Resultado: `%s` -- %d falsas exactas, %d relaciones esperadas ausentes\n",
		measured.Gate, measured.Totals.FalseExactEdges, measured.Totals.FalseNegatives)
	fmt.Fprintf(&out, "sobre %d esperadas, y %d/%d fallos declarados. Los hallazgos están abajo.\n\n",
		measured.Totals.ExpectedEdges, measured.Totals.UnresolvedMatched, measured.Totals.ExpectedUnresolved)
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
	fmt.Fprintf(&out, "- no resueltas declaradas: %d/%d esperadas, %d filas publicadas\n",
		measured.Totals.UnresolvedMatched, measured.Totals.ExpectedUnresolved, measured.Totals.UnresolvedDeclared)
	fmt.Fprintf(&out, "- aristas de directiva sin `evidence_key`: %d\n", measured.Totals.DirectiveEdgesWithoutEvidence)
	fmt.Fprintf(&out, "- referencias cuyo origen es el módulo del archivo y no una declaración: %d\n", measured.Totals.EdgesSourcedAtModule)
	fmt.Fprintf(&out, "- aristas con los dos extremos en el mismo símbolo: %d\n", measured.Totals.SelfReferenceEdges)
	fmt.Fprintf(&out, "- declaraciones cuyo rango cubre sólo su nombre: %d\n\n", measured.Totals.SymbolsSpanningOnlyTheirName)

	out.WriteString("## Casos\n\n")
	out.WriteString("| Caso | Esperadas | TP | FN | Falsas exactas | Exactas | Símbolos | No resueltas |\n")
	out.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, result := range measured.Cases {
		fmt.Fprintf(&out, "| %s | %d | %d | %d | %d | %d | %d | %d/%d |\n",
			result.Name, result.Metrics.ExpectedEdges, result.Metrics.TruePositives,
			result.Metrics.FalseNegatives, result.Metrics.FalseExactEdges,
			result.Metrics.ExactEdges, result.Metrics.Symbols,
			result.Metrics.UnresolvedMatched, result.Metrics.ExpectedUnresolved)
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

	out.WriteString("\n## No resueltas observadas\n\n")
	out.WriteString("Cada fila es `motivo archivo clase-del-objetivo` con su número de\n")
	out.WriteString("ocurrencias. Están todas, no sólo las esperadas: un hecho perdido en\n")
	out.WriteString("silencio es el defecto que esta auditoría existe para no repetir.\n")
	for _, result := range measured.Cases {
		out.WriteString("\n### " + result.Name + "\n\n")
		if len(result.ObservedUnresolved) == 0 {
			out.WriteString("- ninguna\n")
			continue
		}
		for _, entry := range result.ObservedUnresolved {
			out.WriteString("- " + entry + "\n")
		}
	}

	out.WriteString("\n## Hallazgos\n\n")
	out.WriteString("Cada uno nombra el mecanismo que lo produce y el número de este artefacto\n")
	out.WriteString("que lo mide. Los que están corregidos se describen con el defecto que\n")
	out.WriteString("tenían, para que la cifra de esta pasada no se lea sin su historia; los que\n")
	out.WriteString("no, dicen por qué no se tocaron.\n\n")
	for _, finding := range measured.Findings {
		out.WriteString("- " + finding + "\n")
	}

	out.WriteString("\n## Limitaciones\n\n")
	for _, limitation := range measured.Limitations {
		out.WriteString("- " + limitation + "\n")
	}
	out.WriteString("\n## Gate\n\n```text\n" + measured.Gate + "\n```\n")
	return out.String()
}
