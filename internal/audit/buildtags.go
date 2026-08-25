package audit

import (
	"bufio"
	"go/build/constraint"
	"os"
	"sort"
	"strings"
)

// buildTagsOf reads the build constraints of the given files and returns the
// tags they name, sorted and deduplicated.
//
// A remedy that says "grant the tag" and cannot name it is a remedy nobody can
// apply, and the tag is right there in the first lines of the files the go
// command refused. It is parsed with the compiler's own parser: a constraint
// is an expression, not a word, and `//go:build integration && !windows`
// names two tags of which only one is asked for.
func buildTagsOf(files []string) []string {
	tags := make(map[string]struct{})
	for _, file := range files {
		for _, tag := range constraintTagsOf(file) {
			tags[tag] = struct{}{}
		}
	}
	collected := make([]string, 0, len(tags))
	for tag := range tags {
		collected = append(collected, tag)
	}
	sort.Strings(collected)
	return collected
}

// constraintTagsOf reads the `//go:build` line of one file. Only the header is
// read: a constraint after the first non-comment, non-blank line is not one.
func constraintTagsOf(file string) []string {
	handle, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer handle.Close()

	scanner := bufio.NewScanner(handle)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			if !constraint.IsGoBuild(line) {
				continue
			}
			expression, err := constraint.Parse(line)
			if err != nil {
				return nil
			}
			return tagsOfExpression(expression)
		}
		// The header is over: the first line of code, or the package clause,
		// ends the region where a constraint counts.
		return nil
	}
	return nil
}

// tagsOfExpression walks the constraint expression. Evaluating it would not
// do: an evaluator short-circuits, so half of `a && b` would go unnamed.
func tagsOfExpression(expression constraint.Expr) []string {
	switch typed := expression.(type) {
	case *constraint.TagExpr:
		return []string{typed.Tag}
	case *constraint.NotExpr:
		return tagsOfExpression(typed.X)
	case *constraint.AndExpr:
		return append(tagsOfExpression(typed.X), tagsOfExpression(typed.Y)...)
	case *constraint.OrExpr:
		return append(tagsOfExpression(typed.X), tagsOfExpression(typed.Y)...)
	default:
		return nil
	}
}
