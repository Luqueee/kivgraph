package goloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/ladygraph/internal/goworkspace"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

// PrecisionGate is the gate emitted when the measurement is clean.
const PrecisionGate = "GO_SEMANTIC_PASS"

// PrecisionFailGate is emitted when any requirement is not met.
const PrecisionFailGate = "GO_SEMANTIC_FAIL"

// PrecisionMetrics are the counts required by LUQUE-0813.
type PrecisionMetrics struct {
	ExpectedEdges  int     `json:"expected_edges"`
	TruePositives  int     `json:"true_positives"`
	FalsePositives int     `json:"false_positives"`
	FalseNegatives int     `json:"false_negatives"`
	Precision      float64 `json:"precision"`
	Recall         float64 `json:"recall"`
	// FalseExactEdges are edges the fixtures prove wrong. The gate needs zero.
	FalseExactEdges               int `json:"false_exact_edges"`
	ExpectedUnresolved            int `json:"expected_unresolved"`
	UnresolvedCorrectlyClassified int `json:"unresolved_correctly_classified"`
	UnresolvedMisclassified       int `json:"unresolved_misclassified"`
}

// PrecisionCaseReport is the measurement of one fixture repository.
type PrecisionCaseReport struct {
	Name                 string           `json:"name"`
	Metrics              PrecisionMetrics `json:"metrics"`
	MissingEdges         []string         `json:"missing_edges"`
	UnexpectedEdges      []string         `json:"unexpected_edges"`
	MissingUnresolved    []string         `json:"missing_unresolved"`
	UnexpectedUnresolved []string         `json:"unexpected_unresolved"`
}

// PrecisionReport is the complete measurement.
type PrecisionReport struct {
	Fixtures []string              `json:"fixtures"`
	Cases    []PrecisionCaseReport `json:"cases"`
	Totals   PrecisionMetrics      `json:"totals"`
	Gate     string                `json:"gate"`
}

type precisionCase struct {
	name string
	// fixture is the directory holding the repositories.
	fixture string
	// workspaceRepositories enter the synthetic go.work.
	workspaceRepositories []string
	// registryRepositories build the module registry; a duplicated module
	// path is visible here even when the workspace could only use one.
	registryRepositories []string
	consumer             string
	// expectedEdges are `source -> KIND -> package.target` entries.
	expectedEdges []string
	// expectedUnresolved are `REASON:module:symbol` entries.
	expectedUnresolved []string
}

func precisionCases() []precisionCase {
	positive := "cross-repository"
	negative := "cross-repository-negative"
	return []precisionCase{
		{
			name:                  "consumer-a",
			fixture:               positive,
			workspaceRepositories: []string{"shared-library", "consumer-a", "consumer-b"},
			registryRepositories:  []string{"shared-library", "consumer-a", "consumer-b"},
			consumer:              "consumer-a",
			expectedEdges: []string{
				"main -> TYPE_USES -> example.com/ladygraph-fixture/shared/api.Shape",
				"main -> REFERENCES -> example.com/ladygraph-fixture/shared/api.Shape.Width",
				"main -> REFERENCES -> example.com/ladygraph-fixture/shared/api.Answer",
				"main -> CALLS_DIRECT -> example.com/ladygraph-fixture/shared/api.Compute",
				"main -> REFERENCES -> example.com/ladygraph-fixture/shared/api.Shape.Width",
				"main -> CALLS_DIRECT -> example.com/ladygraph-fixture/shared/api.Shape.Area",
				"main -> CALLS_DIRECT -> example.com/ladygraph-fixture/shared/api.Register",
				"main -> PASSES_AS_CALLBACK -> example.com/ladygraph-fixture/shared/api.Compute",
			},
			expectedUnresolved: nil,
		},
		{
			name:                  "consumer-b",
			fixture:               positive,
			workspaceRepositories: []string{"shared-library", "consumer-a", "consumer-b"},
			registryRepositories:  []string{"shared-library", "consumer-a", "consumer-b"},
			consumer:              "consumer-b",
			expectedEdges: []string{
				"main -> CALLS_DIRECT -> example.com/ladygraph-fixture/shared/api.Compute",
				"main -> REFERENCES -> example.com/ladygraph-fixture/shared/api.Answer",
				"main -> CALLS_DIRECT -> example.com/ladygraph-fixture/legacy.Legacy",
			},
			expectedUnresolved: nil,
		},
		{
			name:                  "consumer-negative",
			fixture:               negative,
			workspaceRepositories: []string{"decoy", "mirror", "twin-a", "consumer"},
			registryRepositories:  []string{"decoy", "mirror", "twin-a", "twin-b", "consumer"},
			consumer:              "consumer",
			expectedEdges: []string{
				"main -> TYPE_USES -> example.com/ladygraph-fixture/decoy/api.Shape",
				"main -> REFERENCES -> example.com/ladygraph-fixture/decoy/api.Shape.Width",
				"main -> CALLS_DIRECT -> example.com/ladygraph-fixture/decoy/api.Shape.Area",
				"main -> CALLS_DIRECT -> example.com/ladygraph-fixture/decoy/api.Compute",
				"main -> CALLS_DIRECT -> example.com/ladygraph-fixture/decoy/api.Register",
			},
			expectedUnresolved: []string{
				"AMBIGUOUS_MODULE_PROVIDER:example.com/ladygraph-fixture/twin:Compute",
				"REPLACE_CONFLICT:example.com/ladygraph-fixture/pinned:",
			},
		},
	}
}

// MeasurePrecision runs both fixtures and compares the produced facts with the
// ground truth of LUQUE-0811 and LUQUE-0812.
//
// fixtureRoot is the directory holding `cross-repository` and
// `cross-repository-negative`, normally `testdata/go`.
func MeasurePrecision(ctx context.Context, fixtureRoot string) (PrecisionReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root, err := filepath.Abs(fixtureRoot)
	if err != nil {
		return PrecisionReport{}, fmt.Errorf("resolve fixture root %q: %w", fixtureRoot, err)
	}

	report := PrecisionReport{
		Fixtures: []string{
			filepath.Join(fixtureRoot, "cross-repository"),
			filepath.Join(fixtureRoot, "cross-repository-negative"),
		},
	}
	for _, testCase := range precisionCases() {
		measured, err := measurePrecisionCase(ctx, root, testCase)
		if err != nil {
			return PrecisionReport{}, err
		}
		report.Cases = append(report.Cases, measured)
	}
	report.Totals = aggregatePrecision(report.Cases)
	report.Gate = PrecisionFailGate
	if report.Totals.FalseExactEdges == 0 &&
		report.Totals.FalseNegatives == 0 &&
		report.Totals.UnresolvedMisclassified == 0 {
		report.Gate = PrecisionGate
	}
	return report, nil
}

func measurePrecisionCase(
	ctx context.Context,
	root string,
	testCase precisionCase,
) (PrecisionCaseReport, error) {
	repository := func(name string) workspace.Repository {
		path := filepath.Join(root, testCase.fixture, name)
		return workspace.Repository{Name: name, Path: path, RealPath: path}
	}
	workspaceRepositories := make([]workspace.Repository, 0, len(testCase.workspaceRepositories))
	for _, name := range testCase.workspaceRepositories {
		workspaceRepositories = append(workspaceRepositories, repository(name))
	}
	registryRepositories := make([]workspace.Repository, 0, len(testCase.registryRepositories))
	for _, name := range testCase.registryRepositories {
		registryRepositories = append(registryRepositories, repository(name))
	}

	plan, err := goworkspace.BuildPlan(ctx, workspaceRepositories, goworkspace.Options{})
	if err != nil {
		return PrecisionCaseReport{}, fmt.Errorf("case %q workspace: %w", testCase.name, err)
	}
	workFile, cleanup, err := temporaryWorkspace(ctx, plan, workspaceRepositories)
	if err != nil {
		return PrecisionCaseReport{}, fmt.Errorf("case %q workspace file: %w", testCase.name, err)
	}
	defer cleanup()

	consumer := repository(testCase.consumer)
	result, err := Load(ctx, Options{Directory: consumer.Path, WorkFile: workFile})
	if err != nil {
		return PrecisionCaseReport{}, fmt.Errorf("case %q load: %w", testCase.name, err)
	}
	uses, err := ExtractUses(ctx, result, UseOptions{Repository: consumer.Name})
	if err != nil {
		return PrecisionCaseReport{}, fmt.Errorf("case %q uses: %w", testCase.name, err)
	}
	references, err := ClassifyReferences(ctx, result, uses)
	if err != nil {
		return PrecisionCaseReport{}, fmt.Errorf("case %q references: %w", testCase.name, err)
	}
	registry, err := NewModuleRegistry(ctx, registryRepositories)
	if err != nil {
		return PrecisionCaseReport{}, fmt.Errorf("case %q registry: %w", testCase.name, err)
	}
	cross, err := ResolveCrossRepository(ctx, uses, registry, CrossRepositoryOptions{
		ConsumerRepository: consumer.Name,
	})
	if err != nil {
		return PrecisionCaseReport{}, fmt.Errorf("case %q cross-repository: %w", testCase.name, err)
	}
	unresolved, err := ClassifyUnresolved(ctx, result, cross, UnresolvedOptions{
		Repository:         consumer.Name,
		WorkspaceConflicts: plan.Conflicts,
	})
	if err != nil {
		return PrecisionCaseReport{}, fmt.Errorf("case %q unresolved: %w", testCase.name, err)
	}

	observedEdges := observedPrecisionEdges(references, cross)
	observedUnresolved := make([]string, 0, len(unresolved))
	for _, entry := range unresolved {
		observedUnresolved = append(observedUnresolved,
			fmt.Sprintf("%s:%s:%s", entry.Reason, entry.RequestedModulePath, entry.RequestedSymbol))
	}

	missingEdges, unexpectedEdges := difference(testCase.expectedEdges, observedEdges)
	missingUnresolved, unexpectedUnresolved := difference(testCase.expectedUnresolved, observedUnresolved)
	truePositives := len(testCase.expectedEdges) - len(missingEdges)

	return PrecisionCaseReport{
		Name: testCase.name,
		Metrics: PrecisionMetrics{
			ExpectedEdges:                 len(testCase.expectedEdges),
			TruePositives:                 truePositives,
			FalsePositives:                len(unexpectedEdges),
			FalseNegatives:                len(missingEdges),
			Precision:                     ratio(truePositives, truePositives+len(unexpectedEdges)),
			Recall:                        ratio(truePositives, len(testCase.expectedEdges)),
			FalseExactEdges:               len(unexpectedEdges),
			ExpectedUnresolved:            len(testCase.expectedUnresolved),
			UnresolvedCorrectlyClassified: len(testCase.expectedUnresolved) - len(missingUnresolved),
			UnresolvedMisclassified:       len(missingUnresolved) + len(unexpectedUnresolved),
		},
		MissingEdges:         missingEdges,
		UnexpectedEdges:      unexpectedEdges,
		MissingUnresolved:    missingUnresolved,
		UnexpectedUnresolved: unexpectedUnresolved,
	}, nil
}

// observedPrecisionEdges renders every resolved cross-repository edge with the
// classification of its occurrence.
func observedPrecisionEdges(references []Reference, cross []CrossRepositoryReference) []string {
	kinds := make(map[position]ReferenceKind, len(references))
	for _, reference := range references {
		kinds[position{file: reference.FileName, offset: reference.Offset}] = reference.Kind
	}
	edges := make([]string, 0, len(cross))
	for _, reference := range cross {
		if reference.Status != CrossRepositoryResolved {
			continue
		}
		kind := kinds[position{file: reference.FileName, offset: reference.Offset}]
		edges = append(edges, fmt.Sprintf("%s -> %s -> %s.%s",
			reference.SourceQualifiedName, kind,
			reference.TargetPackagePath, reference.TargetQualifiedName))
	}
	return edges
}

// difference compares multisets so a repeated edge is not silently merged.
func difference(expected, observed []string) (missing, unexpected []string) {
	counts := make(map[string]int, len(expected))
	for _, entry := range expected {
		counts[entry]++
	}
	for _, entry := range observed {
		if counts[entry] > 0 {
			counts[entry]--
			continue
		}
		unexpected = append(unexpected, entry)
	}
	for entry, remaining := range counts {
		for index := 0; index < remaining; index++ {
			missing = append(missing, entry)
		}
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	return missing, unexpected
}

func mkdirTemporary() (string, error) {
	directory, err := os.MkdirTemp("", "ladygraph-go-precision-")
	if err != nil {
		return "", fmt.Errorf("create temporary workspace directory: %w", err)
	}
	return directory, nil
}

func removeAll(directory string) {
	_ = os.RemoveAll(directory)
}

func aggregatePrecision(cases []PrecisionCaseReport) PrecisionMetrics {
	totals := PrecisionMetrics{}
	for _, entry := range cases {
		totals.ExpectedEdges += entry.Metrics.ExpectedEdges
		totals.TruePositives += entry.Metrics.TruePositives
		totals.FalsePositives += entry.Metrics.FalsePositives
		totals.FalseNegatives += entry.Metrics.FalseNegatives
		totals.FalseExactEdges += entry.Metrics.FalseExactEdges
		totals.ExpectedUnresolved += entry.Metrics.ExpectedUnresolved
		totals.UnresolvedCorrectlyClassified += entry.Metrics.UnresolvedCorrectlyClassified
		totals.UnresolvedMisclassified += entry.Metrics.UnresolvedMisclassified
	}
	totals.Precision = ratio(totals.TruePositives, totals.TruePositives+totals.FalsePositives)
	totals.Recall = ratio(totals.TruePositives, totals.ExpectedEdges)
	return totals
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 1
	}
	return float64(numerator) / float64(denominator)
}

// temporaryWorkspace writes the synthetic workspace outside every repository.
func temporaryWorkspace(
	ctx context.Context,
	plan goworkspace.Plan,
	repositories []workspace.Repository,
) (string, func(), error) {
	directory, err := mkdirTemporary()
	if err != nil {
		return "", func() {}, err
	}
	target := filepath.Join(directory, "go.work")
	if _, err := goworkspace.Write(ctx, target, plan, repositories); err != nil {
		removeAll(directory)
		return "", func() {}, err
	}
	return target, func() { removeAll(directory) }, nil
}

// PrecisionSummary renders the totals as text lines, for reports and logs.
func PrecisionSummary(metrics PrecisionMetrics) []string {
	return []string{
		fmt.Sprintf("true positives: %d", metrics.TruePositives),
		fmt.Sprintf("false positives: %d", metrics.FalsePositives),
		fmt.Sprintf("false negatives: %d", metrics.FalseNegatives),
		fmt.Sprintf("precision: %.4f", metrics.Precision),
		fmt.Sprintf("recall: %.4f", metrics.Recall),
		fmt.Sprintf("false exact edges: %d", metrics.FalseExactEdges),
		fmt.Sprintf("unresolved correctly classified: %d/%d",
			metrics.UnresolvedCorrectlyClassified, metrics.ExpectedUnresolved),
	}
}

// PrecisionCaseRow renders one case as a Markdown table row.
func PrecisionCaseRow(entry PrecisionCaseReport) string {
	metrics := entry.Metrics
	cells := []string{
		entry.Name,
		fmt.Sprint(metrics.ExpectedEdges),
		fmt.Sprint(metrics.TruePositives),
		fmt.Sprint(metrics.FalsePositives),
		fmt.Sprint(metrics.FalseNegatives),
		fmt.Sprintf("%.4f", metrics.Precision),
		fmt.Sprintf("%.4f", metrics.Recall),
		fmt.Sprintf("%d/%d", metrics.UnresolvedCorrectlyClassified, metrics.ExpectedUnresolved),
	}
	return "| " + strings.Join(cells, " | ") + " |"
}
