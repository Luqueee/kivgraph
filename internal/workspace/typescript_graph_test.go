package workspace

import (
	"path/filepath"
	"testing"
)

// newGraphTestProgram builds a minimal TypeScriptProgram for
// topologicalTypeScriptOrder tests: only ConfigPath, Directory and
// References are populated, since those are the only fields the ordering
// algorithm reads.
func newGraphTestProgram(configPath string, references ...string) TypeScriptProgram {
	return TypeScriptProgram{
		ConfigPath: configPath,
		Directory:  filepath.Dir(configPath),
		References: append([]string(nil), references...),
	}
}

// graphTestPrograms indexes the given programs by their ConfigPath, the
// same shape topologicalTypeScriptOrder receives from
// newTypeScriptProjectGraph.
func graphTestPrograms(programs ...TypeScriptProgram) map[string]TypeScriptProgram {
	indexed := make(map[string]TypeScriptProgram, len(programs))
	for _, program := range programs {
		indexed[program.ConfigPath] = program
	}
	return indexed
}

// assertGraphDependents checks that dependents has an entry for configPath
// -- present even when empty, per topologicalTypeScriptOrder's contract --
// and that its content matches want exactly.
func assertGraphDependents(t *testing.T, dependents map[string][]string, configPath string, want []string) {
	t.Helper()
	got, ok := dependents[configPath]
	if !ok {
		t.Fatalf("dependents has no entry for %q", configPath)
	}
	if !equalStrings(got, want) {
		t.Fatalf("dependents[%q] = %#v, want %#v", configPath, got, want)
	}
}

func TestTopologicalTypeScriptOrderLinearChain(t *testing.T) {
	leaf := "/repo/c/tsconfig.json"
	mid := "/repo/b/tsconfig.json"
	root := "/repo/a/tsconfig.json"
	programs := graphTestPrograms(
		newGraphTestProgram(root, mid),
		newGraphTestProgram(mid, leaf),
		newGraphTestProgram(leaf),
	)

	order, dependents, err := topologicalTypeScriptOrder(programs)
	if err != nil {
		t.Fatalf("topologicalTypeScriptOrder() error = %v", err)
	}

	wantOrder := []string{leaf, mid, root}
	if !equalStrings(order, wantOrder) {
		t.Fatalf("order = %#v, want %#v", order, wantOrder)
	}
	assertGraphDependents(t, dependents, leaf, []string{mid})
	assertGraphDependents(t, dependents, mid, []string{root})
	assertGraphDependents(t, dependents, root, nil)
}

func TestTopologicalTypeScriptOrderDiamond(t *testing.T) {
	base := "/repo/base/tsconfig.json"
	left := "/repo/left/tsconfig.json"
	right := "/repo/right/tsconfig.json"
	top := "/repo/top/tsconfig.json"
	programs := graphTestPrograms(
		newGraphTestProgram(base),
		newGraphTestProgram(left, base),
		newGraphTestProgram(right, base),
		newGraphTestProgram(top, left, right),
	)

	order, dependents, err := topologicalTypeScriptOrder(programs)
	if err != nil {
		t.Fatalf("topologicalTypeScriptOrder() error = %v", err)
	}

	wantOrder := []string{base, left, right, top}
	if !equalStrings(order, wantOrder) {
		t.Fatalf("order = %#v, want %#v", order, wantOrder)
	}

	indexOf := func(configPath string) int {
		for index, candidate := range order {
			if candidate == configPath {
				return index
			}
		}
		t.Fatalf("%q missing from order %#v", configPath, order)
		return -1
	}
	if indexOf(base) >= indexOf(left) {
		t.Fatalf("base must precede left in order %#v", order)
	}
	if indexOf(base) >= indexOf(right) {
		t.Fatalf("base must precede right in order %#v", order)
	}
	if indexOf(left) >= indexOf(top) {
		t.Fatalf("left must precede top in order %#v", order)
	}
	if indexOf(right) >= indexOf(top) {
		t.Fatalf("right must precede top in order %#v", order)
	}

	assertGraphDependents(t, dependents, base, []string{left, right})
	assertGraphDependents(t, dependents, left, []string{top})
	assertGraphDependents(t, dependents, right, []string{top})
	assertGraphDependents(t, dependents, top, nil)
}

// TestTopologicalTypeScriptOrderIsDeterministic rebuilds the same graph from
// scratch on every iteration -- never reusing one map value -- because Go
// randomizes map iteration order per range, including across ranges over an
// equal map built again. If topologicalTypeScriptOrder ever leaked that
// randomness into its result, this would eventually observe a mismatch.
func TestTopologicalTypeScriptOrderIsDeterministic(t *testing.T) {
	root := "/repo/root/tsconfig.json"
	serviceA := "/repo/service-a/tsconfig.json"
	serviceB := "/repo/service-b/tsconfig.json"
	serviceC := "/repo/service-c/tsconfig.json"
	app := "/repo/app/tsconfig.json"

	buildPrograms := func() map[string]TypeScriptProgram {
		return graphTestPrograms(
			newGraphTestProgram(root),
			newGraphTestProgram(serviceA, root),
			newGraphTestProgram(serviceB, root),
			newGraphTestProgram(serviceC, root),
			newGraphTestProgram(app, serviceA, serviceB, serviceC),
		)
	}

	wantOrder := []string{root, serviceA, serviceB, serviceC, app}
	wantRootDependents := []string{serviceA, serviceB, serviceC}

	const iterations = 25
	for iteration := range iterations {
		order, dependents, err := topologicalTypeScriptOrder(buildPrograms())
		if err != nil {
			t.Fatalf("topologicalTypeScriptOrder() iteration %d error = %v", iteration, err)
		}
		if !equalStrings(order, wantOrder) {
			t.Fatalf("iteration %d order = %#v, want %#v", iteration, order, wantOrder)
		}
		assertGraphDependents(t, dependents, root, wantRootDependents)
	}
}

func TestTopologicalTypeScriptOrderBreaksTiesLexicographically(t *testing.T) {
	root := "/repo/root/tsconfig.json"
	zeta := "/repo/zeta/tsconfig.json"
	mid := "/repo/mid/tsconfig.json"
	beta := "/repo/beta/tsconfig.json"

	// Declared out of lexicographic order on purpose: the result must
	// reflect the sorted config paths, not this call order.
	programs := graphTestPrograms(
		newGraphTestProgram(root),
		newGraphTestProgram(zeta, root),
		newGraphTestProgram(mid, root),
		newGraphTestProgram(beta, root),
	)

	order, dependents, err := topologicalTypeScriptOrder(programs)
	if err != nil {
		t.Fatalf("topologicalTypeScriptOrder() error = %v", err)
	}

	// root is the only project ready at the start, so it is placed first
	// regardless of ties; beta, mid and zeta all become ready together once
	// root is placed, and must then be placed in lexicographic order.
	wantOrder := []string{root, beta, mid, zeta}
	if !equalStrings(order, wantOrder) {
		t.Fatalf("order = %#v, want %#v", order, wantOrder)
	}
	assertGraphDependents(t, dependents, root, []string{beta, mid, zeta})
}

// A project that becomes ready later can still be the smallest candidate. A
// plain FIFO queue would place the one that was enqueued first, so this is
// the case that actually distinguishes an ordered pick from insertion order.
func TestTopologicalTypeScriptOrderPrefersALateSmallerCandidate(t *testing.T) {
	mid := "/repo/mid/tsconfig.json"
	zeta := "/repo/zeta/tsconfig.json"
	alpha := "/repo/alpha/tsconfig.json"

	programs := graphTestPrograms(
		newGraphTestProgram(mid),
		newGraphTestProgram(zeta),
		newGraphTestProgram(alpha, mid),
	)

	order, _, err := topologicalTypeScriptOrder(programs)
	if err != nil {
		t.Fatalf("topologicalTypeScriptOrder() error = %v", err)
	}

	// mid and zeta are ready first; placing mid frees alpha, which sorts
	// before the already waiting zeta and must therefore come next.
	wantOrder := []string{mid, alpha, zeta}
	if !equalStrings(order, wantOrder) {
		t.Fatalf("order = %#v, want %#v", order, wantOrder)
	}
}

func TestTopologicalTypeScriptOrderDetectsCycles(t *testing.T) {
	a := "/repo/a/tsconfig.json"
	b := "/repo/b/tsconfig.json"
	c := "/repo/c/tsconfig.json"

	testCases := []struct {
		name      string
		programs  map[string]TypeScriptProgram
		wantError string
	}{
		{
			name: "two project cycle",
			programs: graphTestPrograms(
				newGraphTestProgram(a, b),
				newGraphTestProgram(b, a),
			),
			wantError: "project reference cycle detected: " + a + " -> " + b + " -> " + a,
		},
		{
			name: "three project cycle",
			programs: graphTestPrograms(
				newGraphTestProgram(a, b),
				newGraphTestProgram(b, c),
				newGraphTestProgram(c, a),
			),
			wantError: "project reference cycle detected: " + a + " -> " + b + " -> " + c + " -> " + a,
		},
		{
			name: "self reference",
			programs: graphTestPrograms(
				newGraphTestProgram(a, a),
			),
			wantError: "project reference cycle detected: " + a + " -> " + a,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			order, dependents, err := topologicalTypeScriptOrder(testCase.programs)
			if err == nil {
				t.Fatalf("topologicalTypeScriptOrder() error = nil, want %q", testCase.wantError)
			}
			if err.Error() != testCase.wantError {
				t.Fatalf("topologicalTypeScriptOrder() error = %q, want %q", err.Error(), testCase.wantError)
			}
			if order != nil {
				t.Fatalf("order = %#v, want nil", order)
			}
			if dependents != nil {
				t.Fatalf("dependents = %#v, want nil", dependents)
			}
		})
	}
}

func TestTopologicalTypeScriptOrderRejectsADanglingReference(t *testing.T) {
	app := "/repo/app/tsconfig.json"
	missing := "/repo/missing/tsconfig.json"
	programs := graphTestPrograms(
		newGraphTestProgram(app, missing),
	)

	order, dependents, err := topologicalTypeScriptOrder(programs)
	if err == nil {
		t.Fatalf("topologicalTypeScriptOrder() error = nil, want a dangling reference error")
	}
	wantError := `project "` + app + `" references "` + missing + `", which was not discovered`
	if err.Error() != wantError {
		t.Fatalf("topologicalTypeScriptOrder() error = %q, want %q", err.Error(), wantError)
	}
	if order != nil {
		t.Fatalf("order = %#v, want nil", order)
	}
	if dependents != nil {
		t.Fatalf("dependents = %#v, want nil", dependents)
	}
}

func TestTopologicalTypeScriptOrderDeduplicatesReferences(t *testing.T) {
	target := "/repo/b/tsconfig.json"
	referencer := "/repo/a/tsconfig.json"
	programs := graphTestPrograms(
		newGraphTestProgram(referencer, target, target, target),
		newGraphTestProgram(target),
	)

	order, dependents, err := topologicalTypeScriptOrder(programs)
	if err != nil {
		t.Fatalf("topologicalTypeScriptOrder() error = %v", err)
	}

	wantOrder := []string{target, referencer}
	if !equalStrings(order, wantOrder) {
		t.Fatalf("order = %#v, want %#v", order, wantOrder)
	}
	// The three duplicate references must collapse into a single dependents
	// entry, not one per occurrence.
	assertGraphDependents(t, dependents, target, []string{referencer})
	assertGraphDependents(t, dependents, referencer, nil)
}

func TestTopologicalTypeScriptOrderIsolatedProjectsHaveNoDependents(t *testing.T) {
	a := "/repo/a/tsconfig.json"
	b := "/repo/b/tsconfig.json"
	c := "/repo/c/tsconfig.json"
	programs := graphTestPrograms(
		newGraphTestProgram(a),
		newGraphTestProgram(b),
		newGraphTestProgram(c),
	)

	order, dependents, err := topologicalTypeScriptOrder(programs)
	if err != nil {
		t.Fatalf("topologicalTypeScriptOrder() error = %v", err)
	}

	wantOrder := []string{a, b, c}
	if !equalStrings(order, wantOrder) {
		t.Fatalf("order = %#v, want %#v", order, wantOrder)
	}
	if len(dependents) != 3 {
		t.Fatalf("dependents has %d entries, want 3", len(dependents))
	}
	assertGraphDependents(t, dependents, a, nil)
	assertGraphDependents(t, dependents, b, nil)
	assertGraphDependents(t, dependents, c, nil)
}
