package workspace

import (
	"fmt"
	"sort"
	"strings"
)

// topologicalTypeScriptOrder orders the given programs so that every
// referenced project precedes each project that references it, and builds
// the reverse index of dependents that TypeScriptProjectGraph.Dependents
// serves.
//
// The map key, and every path in TypeScriptProgram.References, must already
// be the absolute, clean tsconfig path LUQUE-0606 assigns each project;
// this function performs no path normalization of its own.
//
// Ties are broken lexicographically: whenever more than one project is
// ready to be placed at a given point, the one with the smallest config
// path is chosen first. The algorithm never depends on Go's randomized map
// iteration order, so two calls over an equal input always produce the
// same, byte-identical order and dependents index.
//
// dependents holds one entry for every key of programs, even when a project
// has no dependents; such an entry's value is nil. Every entry's slice is
// sorted and free of duplicates, even when a project lists the same
// reference more than once.
//
// A project reference cycle is an error, since TypeScript itself forbids
// cycles between referenced projects; this includes a project that
// references itself, a cycle of length one. The error names every config
// path on the cycle, in the deterministic order it was walked. A reference
// to a config path absent from programs is also an error; it names both the
// referencing project and the missing target.
func topologicalTypeScriptOrder(programs map[string]TypeScriptProgram) ([]string, map[string][]string, error) {
	configPaths := make([]string, 0, len(programs))
	for configPath := range programs {
		configPaths = append(configPaths, configPath)
	}
	sort.Strings(configPaths)

	dependents := make(map[string][]string, len(configPaths))
	pendingReferenceCount := make(map[string]int, len(configPaths))
	for _, configPath := range configPaths {
		dependents[configPath] = nil
		pendingReferenceCount[configPath] = 0
	}

	for _, configPath := range configPaths {
		seenReferencePaths := make(map[string]bool, len(programs[configPath].References))
		for _, reference := range programs[configPath].References {
			if _, discovered := programs[reference]; !discovered {
				return nil, nil, fmt.Errorf("project %q references %q, which was not discovered", configPath, reference)
			}
			if seenReferencePaths[reference] {
				continue
			}
			seenReferencePaths[reference] = true
			pendingReferenceCount[configPath]++
			dependents[reference] = append(dependents[reference], configPath)
		}
	}
	for _, configPath := range configPaths {
		sort.Strings(dependents[configPath])
	}

	order := make([]string, 0, len(configPaths))
	emittedConfigPaths := make(map[string]bool, len(configPaths))
	readyConfigPaths := make([]string, 0, len(configPaths))
	for _, configPath := range configPaths {
		if pendingReferenceCount[configPath] == 0 {
			readyConfigPaths = append(readyConfigPaths, configPath)
		}
	}

	for len(readyConfigPaths) > 0 {
		sort.Strings(readyConfigPaths)
		nextConfigPath := readyConfigPaths[0]
		readyConfigPaths = readyConfigPaths[1:]
		order = append(order, nextConfigPath)
		emittedConfigPaths[nextConfigPath] = true
		for _, dependent := range dependents[nextConfigPath] {
			pendingReferenceCount[dependent]--
			if pendingReferenceCount[dependent] == 0 {
				readyConfigPaths = append(readyConfigPaths, dependent)
			}
		}
	}

	if len(order) != len(configPaths) {
		cycle := findTypeScriptReferenceCycle(programs, configPaths, emittedConfigPaths)
		return nil, nil, fmt.Errorf("project reference cycle detected: %s", strings.Join(cycle, " -> "))
	}

	return order, dependents, nil
}

// findTypeScriptReferenceCycle returns one reference cycle among the
// projects topologicalTypeScriptOrder could not place, as a path that
// starts and ends on the same config path. Candidates, and the references
// of each candidate visited, are walked in lexicographic order, so the
// result is deterministic.
//
// Every project left out of emittedConfigPaths has at least one reference
// that also never got emitted -- that is why topologicalTypeScriptOrder's
// Kahn's-algorithm pass stalled on it -- so a depth-first search restricted
// to that subgraph is guaranteed to find a cycle.
func findTypeScriptReferenceCycle(programs map[string]TypeScriptProgram, configPaths []string, emittedConfigPaths map[string]bool) []string {
	visited := make(map[string]bool, len(configPaths))
	onStack := make(map[string]bool, len(configPaths))
	var stack []string
	var cycle []string

	var visit func(configPath string) bool
	visit = func(configPath string) bool {
		visited[configPath] = true
		onStack[configPath] = true
		stack = append(stack, configPath)

		references := append([]string(nil), programs[configPath].References...)
		sort.Strings(references)
		seenReferencePaths := make(map[string]bool, len(references))
		for _, reference := range references {
			if seenReferencePaths[reference] || emittedConfigPaths[reference] {
				continue
			}
			seenReferencePaths[reference] = true
			if onStack[reference] {
				cutIndex := 0
				for index, stacked := range stack {
					if stacked == reference {
						cutIndex = index
						break
					}
				}
				cycle = append(append([]string(nil), stack[cutIndex:]...), reference)
				return true
			}
			if !visited[reference] && visit(reference) {
				return true
			}
		}

		stack = stack[:len(stack)-1]
		onStack[configPath] = false
		return false
	}

	for _, configPath := range configPaths {
		if emittedConfigPaths[configPath] || visited[configPath] {
			continue
		}
		if visit(configPath) {
			return cycle
		}
	}

	// topologicalTypeScriptOrder only calls this once Kahn's algorithm has
	// stalled, which -- per the invariant documented above -- always yields
	// a cycle the search above finds. This is an unreachable defensive
	// fallback in case that invariant is ever broken by a future change.
	unresolvedConfigPaths := make([]string, 0, len(configPaths))
	for _, configPath := range configPaths {
		if !emittedConfigPaths[configPath] {
			unresolvedConfigPaths = append(unresolvedConfigPaths, configPath)
		}
	}
	return unresolvedConfigPaths
}
