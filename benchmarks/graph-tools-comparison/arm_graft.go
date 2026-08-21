package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// measureGraft answers the three families through graft's CLI, which is the
// surface its own documentation leads with and which exposes the flags the MCP
// tools wrap: `--depth` for a transitive walk and `skeleton` for one file.
//
// The context directory lives outside the corpus, so nothing is written beside
// the code being read.
func measureGraft(
	ctx context.Context, tokens *counter, repos repositories, captures map[string]string,
	binary, contextDir, corpusRoot string, q question,
) (*armResult, error) {
	switch q.Family {
	case familyReferences:
		return graftCallers(ctx, tokens, repos, captures, binary, contextDir, corpusRoot, q, 1)
	case familyImpact:
		return graftCallers(ctx, tokens, repos, captures, binary, contextDir, corpusRoot, q, q.Depth)
	case familyOutline:
		return graftSkeleton(ctx, tokens, captures, binary, contextDir, corpusRoot, q)
	}
	return nil, fmt.Errorf("unknown family %q", q.Family)
}

// graftCallers traces callers of the bare name. graft takes a name and not a
// declaration, which is the whole of its ambiguity behaviour: when several
// declarations share the name it drops the cross-file callers and says it may
// undercount, rather than guessing. That refusal is priced like any answer.
func graftCallers(
	ctx context.Context, tokens *counter, repos repositories, captures map[string]string,
	binary, contextDir, corpusRoot string, q question, depth int,
) (*armResult, error) {
	arm := &armResult{}
	arguments := []string{"--dir", contextDir, "callers", q.Subject.Symbol, corpusRoot, "--json"}
	if depth > 1 {
		arguments = append(arguments, "--depth", fmt.Sprint(depth))
	}
	answer := runCLI(ctx, tokens, captures, q.ID+"-graft-callers", contextDir, nil, binary, arguments...)
	arm.add(answer)

	files, ambiguous, note := graftCallerFiles(answer.Text)
	arm.Ambiguous = ambiguous
	arm.Note = note
	claimed := make([]string, 0, len(files))
	for _, file := range files {
		claimed = append(claimed, repos.canonical(file))
	}
	arm.Claimed = withoutDeclaring(claimed, repos.canonical(q.Subject.corpusPath()))
	arm.Score = scoreAgainst(arm.Claimed, repos.canonicalAll(q.Truth))
	return arm, nil
}

// graftSkeleton asks for one file's API surface, which is graft's own name for
// the outline question.
func graftSkeleton(
	ctx context.Context, tokens *counter, captures map[string]string,
	binary, contextDir, corpusRoot string, q question,
) (*armResult, error) {
	arm := &armResult{}
	answer := runCLI(ctx, tokens, captures, q.ID+"-graft-skeleton", contextDir, nil,
		binary, "--dir", contextDir, "skeleton", q.Subject.corpusPath(), corpusRoot)
	arm.add(answer)
	arm.Claimed = graftSkeletonNames(answer.Text)
	arm.Score = scoreAgainst(arm.Claimed, q.Truth)
	return arm, nil
}

var (
	// A JSON answer carries the file of each caller; a text one spells it in
	// parentheses after the arrow. Both shapes appear, depending on whether the
	// walk found anything, so both are read.
	graftCallerLine  = regexp.MustCompile(`calls (?:←|<-|→|->) .*?\(([^()]+):L\d+`)
	graftAmbiguity   = regexp.MustCompile(`(\d+) definitions? share the name`)
	graftSkeletonRow = regexp.MustCompile(`^\s*(?:L\d+(?:-L\d+)?\s+)?(?:export\s+)?(?:async\s+)?(?:function|const|class|interface|type|enum|fn|func|struct|method|variable)\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

// graftCallerFiles reads the caller files out of an answer in either shape.
//
// The JSON one nests them: a `matches` entry per declaration the name resolved
// to, and a `hits` entry per reference, each carrying its own `path` and the
// `depth` at which the walk found it. Reading `matches` rather than one flat
// list is what keeps an ambiguous name honest -- graft answers about every
// declaration it matched, and all of them are counted, because the caller asked
// with a name and that is what a name got.
func graftCallerFiles(text string) ([]string, int, string) {
	ambiguous, note := 0, ""
	if match := graftAmbiguity.FindStringSubmatch(text); match != nil {
		ambiguous = atoi(match[1])
		note = "refused the cross-file callers of an ambiguous name and said it may undercount"
	}
	decoded := struct {
		Matches []struct {
			Symbol struct {
				Path string `json:"path"`
			} `json:"symbol"`
			Hits []struct {
				Path     string `json:"path"`
				Relation string `json:"relation"`
				Depth    int    `json:"depth"`
			} `json:"hits"`
		} `json:"matches"`
	}{}
	if json.Unmarshal([]byte(text), &decoded) == nil && len(decoded.Matches) > 0 {
		out := []string{}
		for _, match := range decoded.Matches {
			for _, hit := range match.Hits {
				if hit.Path != "" {
					out = append(out, hit.Path)
				}
			}
		}
		if len(decoded.Matches) > 1 && note == "" {
			note = fmt.Sprintf("answered about %d declarations sharing the name", len(decoded.Matches))
			ambiguous = len(decoded.Matches)
		}
		return out, ambiguous, note
	}
	out := []string{}
	for _, line := range strings.Split(text, "\n") {
		if match := graftCallerLine.FindStringSubmatch(line); match != nil {
			out = append(out, match[1])
		}
	}
	return out, ambiguous, note
}

// graftSkeletonNames reads the declaration names out of a signatures-only view.
func graftSkeletonNames(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, line := range strings.Split(text, "\n") {
		match := graftSkeletonRow.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if !seen[match[1]] {
			seen[match[1]] = true
			out = append(out, match[1])
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func atoi(in string) int {
	out := 0
	for _, char := range in {
		if char < '0' || char > '9' {
			return out
		}
		out = out*10 + int(char-'0')
	}
	return out
}
