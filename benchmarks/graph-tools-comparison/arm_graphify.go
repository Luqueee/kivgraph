package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// graphify is the one arm that writes into the tree it reads: `graphify update
// <path>` puts `graphify-out/` inside <path>. Every path this file hands the
// binary therefore goes through graphifyPrivate first, so a wrong flag fails
// loudly instead of leaving a build artefact in the measured corpus.
//
// It is also the one arm with no cross-repository graph: `update` builds one
// `graph.json` per repository, so a question whose answer lives in another
// repository is out of reach by construction. graphify does ship
// `global add`/`merge-graphs` for that, and this arm does not use them, because
// the build helper the harness calls is per-repository; the reference notes say
// so where it costs recall.
const graphifyBuildTimeout = 15 * time.Minute

// measureGraphify answers the three families with the three commands graphify
// offers for them: `affected` for what touches a declaration, `affected
// --depth` for the transitive form of the same question, and `explain` for what
// a file declares.
func measureGraphify(ctx context.Context, tokens *counter, repos repositories, captures map[string]string,
	binary, corpusCopyRoot, home string, q question) (*armResult, error) {
	repository := filepath.Join(corpusCopyRoot, q.Subject.Dir)
	if err := graphifyPrivate(repository); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", home, err)
	}
	graph := filepath.Join(repository, "graphify-out", "graph.json")
	switch q.Family {
	case familyReferences:
		return graphifyReferences(ctx, tokens, repos, captures, binary, graph, home, q)
	case familyImpact:
		return graphifyImpact(ctx, tokens, repos, captures, binary, graph, home, q)
	case familyOutline:
		return graphifyOutline(ctx, tokens, captures, binary, graph, home, q)
	}
	return nil, fmt.Errorf("unknown family %q", q.Family)
}

// buildGraphify builds one repository's graph and returns what it cost in
// milliseconds. `update` is the no-LLM path: AST extraction plus clustering,
// no API key, and it writes `graphify-out/` inside repoPath.
func buildGraphify(ctx context.Context, binary, repoPath, home string) (float64, error) {
	if err := graphifyPrivate(repoPath); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return 0, fmt.Errorf("create %s: %w", home, err)
	}
	buildContext, cancel := context.WithTimeout(ctx, graphifyBuildTimeout)
	defer cancel()
	command := exec.CommandContext(buildContext, binary, "update", repoPath)
	// The working directory matters as much as HOME: every subcommand defaults
	// `--graph` to `graphify-out/graph.json` relative to the current directory,
	// so running from the isolated home means a missing flag cannot reach a
	// graph that belongs to something else.
	command.Dir = home
	command.Env = append(os.Environ(), "HOME="+home)
	started := time.Now()
	output, err := command.CombinedOutput()
	elapsed := float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		return elapsed, fmt.Errorf("graphify update %s: %w (%s)", repoPath, err, strings.TrimSpace(string(output)))
	}
	if _, err := os.Stat(filepath.Join(repoPath, "graphify-out", "graph.json")); err != nil {
		return elapsed, fmt.Errorf("graphify update %s wrote no graph: %w", repoPath, err)
	}
	return elapsed, nil
}

// graphifyPrivate refuses a path outside a temporary directory. graphify's own
// documentation calls `graphify-out/` an artefact of the directory it indexes,
// so the only safe target is the private copy of the corpus; this is the check
// that makes "only ever the copy" a property of the code rather than of the
// flag the harness happened to pass.
func graphifyPrivate(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		resolved = path
	}
	resolved = filepath.Clean(resolved)
	for _, root := range []string{"/private/tmp", "/private/var/folders", "/tmp", "/var/folders", os.TempDir()} {
		root = filepath.Clean(root)
		if resolved == root || strings.HasPrefix(resolved, root+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("graphify writes graphify-out/ into the directory it reads, "+
		"so it may only run against the private copy: %s is not under a temporary directory", path)
}

// graphifyDirection is the finding that decides how every `affected` answer in
// this benchmark must be read, and it is repeated on each result rather than
// left in a write-up, because a reader scoring the numbers needs it there.
//
// graphify serialises `graph.json` with `"directed": false`. networkx therefore
// loads an undirected Graph, which has no `in_edges`, and affected.py's
// fallback iterates `graph.edges()` keeping the ones whose *stored* orientation
// ends at the seed. In an undirected networkx graph that orientation is the
// node insertion order, i.e. the order the extractor walked the files -- so
// `affected` returns the neighbours declared before the seed and misses the
// ones declared after it. It is a neighbour set, not reverse reachability.
const graphifyDirection = "graph.json is written `directed: false`, so networkx loads it undirected, " +
	"affected.py falls back to scanning `graph.edges()` for edges whose stored orientation ends at the seed, " +
	"and that orientation is node insertion order rather than call direction: the answer is a neighbour set at " +
	"the given depth, not reverse reachability"

// graphifyReferences asks by bare name first, which is the only spelling a
// caller has, and narrows to the subject's own file only when graphify refuses.
func graphifyReferences(ctx context.Context, tokens *counter, repos repositories, captures map[string]string,
	binary, graph, home string, q question) (*armResult, error) {
	arm := &armResult{}
	notes := []string{graphifyDirection}
	answer := graphifyRun(ctx, tokens, captures, q.ID+"-graphify-affected", home, binary,
		"affected", q.Subject.Symbol, "--graph", graph)
	arm.add(answer)

	if graphifyRefused(answer.Text) {
		note, err := graphifyNarrow(arm, graph, q)
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
		answer = graphifyRun(ctx, tokens, captures, q.ID+"-graphify-affected-narrowed", home, binary,
			"affected", graphifySymbolID(q.Subject.Path, q.Subject.Symbol), "--graph", graph)
		arm.add(answer)
	}

	var claimed []string
	if graphifyRefused(answer.Text) {
		// The node id did not resolve either. `query` is the documented
		// fallback and it always answers, but it is a depth-2 BFS neighbourhood
		// seeded by heuristic label matching, so it crosses homonyms and mixes
		// callers with callees.
		notes = append(notes, "the node id did not resolve either, so the answer comes from `query`, "+
			"a depth-2 BFS neighbourhood seeded by label matching that crosses homonyms and mixes callers with callees")
		answer = graphifyRun(ctx, tokens, captures, q.ID+"-graphify-query", home, binary,
			"query", q.Ask, "--graph", graph)
		arm.add(answer)
		claimed = graphifyQueryFiles(answer.Text)
	} else {
		claimed = graphifyAffectedFiles(answer.Text)
	}

	arm.Claimed = graphifyClaimedFiles(repos, q, claimed)
	arm.Score = scoreAgainst(arm.Claimed, repos.canonicalAll(q.Truth))
	if note := graphifyPerRepositoryNote(q); note != "" {
		notes = append(notes, note)
	}
	arm.Note = strings.Join(notes, "; ")
	return arm, nil
}

// graphifyImpact asks the same command at the question's depth. `affected` is
// graphify's transitive form -- `--depth` is its only knob and it defaults to
// 2 -- so impact and references differ here by one flag, exactly as the tool
// intends.
func graphifyImpact(ctx context.Context, tokens *counter, repos repositories, captures map[string]string,
	binary, graph, home string, q question) (*armResult, error) {
	arm := &armResult{}
	depth := strconv.Itoa(q.Depth)
	notes := []string{"`affected --depth " + depth + "` (the flag's own default is 2); " + graphifyDirection}
	answer := graphifyRun(ctx, tokens, captures, q.ID+"-graphify-affected", home, binary,
		"affected", q.Subject.Symbol, "--depth", depth, "--graph", graph)
	arm.add(answer)

	if graphifyRefused(answer.Text) {
		note, err := graphifyNarrow(arm, graph, q)
		if err != nil {
			return nil, err
		}
		notes = append(notes, note)
		answer = graphifyRun(ctx, tokens, captures, q.ID+"-graphify-affected-narrowed", home, binary,
			"affected", graphifySymbolID(q.Subject.Path, q.Subject.Symbol), "--depth", depth, "--graph", graph)
		arm.add(answer)
	}

	var claimed []string
	if graphifyRefused(answer.Text) {
		notes = append(notes, "the node id did not resolve either, so the answer comes from `query`, "+
			"a depth-2 BFS neighbourhood rather than a reachable set")
		answer = graphifyRun(ctx, tokens, captures, q.ID+"-graphify-query", home, binary,
			"query", q.Ask, "--graph", graph)
		arm.add(answer)
		claimed = graphifyQueryFiles(answer.Text)
	} else {
		claimed = graphifyAffectedFiles(answer.Text)
	}

	arm.Claimed = graphifyClaimedFiles(repos, q, claimed)
	arm.Score = scoreAgainst(arm.Claimed, repos.canonicalAll(q.Truth))
	arm.Note = strings.Join(notes, "; ")
	return arm, nil
}

// graphifyOutline asks what one file declares. No command takes a file path, so
// the ask is `explain <file node id>`: the file node's `contains` edges are the
// declarations, and `explain` is the only command that prints them. The id is
// the documented `{parent_dir}_{stem}` form, and the answer is checked against
// the `Source:` line it prints, so a mis-resolution is a recorded fact rather
// than a wrong score.
func graphifyOutline(ctx context.Context, tokens *counter, captures map[string]string,
	binary, graph, home string, q question) (*armResult, error) {
	arm := &armResult{}
	answer := graphifyRun(ctx, tokens, captures, q.ID+"-graphify-explain", home, binary,
		"explain", graphifyFileID(q.Subject.Path), "--graph", graph)
	arm.add(answer)
	if strings.Contains(answer.Text, "No node matching") {
		arm.Note = "`explain` did not resolve the file node id `" + graphifyFileID(q.Subject.Path) +
			"`; no graphify command accepts a file path"
		arm.Score = scoreAgainst(nil, q.Truth)
		return arm, nil
	}

	notes := []string{"`explain <file node id>` reads the file node's `contains` edges, " +
		"because no graphify command takes a file path; the id is `{parent_dir}_{stem}`"}
	if source := graphifyExplainSource(answer.Text); source != "" && source != q.Subject.Path {
		notes = append(notes, "it resolved to "+source+" rather than "+q.Subject.Path)
	}
	names, hidden := graphifyContained(answer.Text)
	if hidden > 0 {
		notes = append(notes, fmt.Sprintf("`explain` prints at most 20 connections sorted by neighbour degree "+
			"and hid %d more here, so imports crowd out the declarations on a large file", hidden))
	}
	arm.Claimed = names
	arm.Score = scoreAgainst(names, q.Truth)
	arm.Note = strings.Join(notes, "; ")
	return arm, nil
}

// graphifyNarrow records an ambiguity refusal and describes the narrowed re-ask.
//
// The refusal names nothing -- it is the single line `No unique node match for
// X` -- so the count comes from replaying resolve_seed's own rule against the
// graph graphify built: a substring match on the node label. That rule is why
// the refusals here are wider than the homonyms: `expBackoffJitter` is declared
// once in this corpus and still refuses, because its own tests' names contain
// it.
func graphifyNarrow(arm *armResult, graph string, q question) (string, error) {
	candidates, err := graphifyCandidates(graph, q.Subject.Symbol)
	if err != nil {
		return "", err
	}
	arm.Ambiguous = candidates
	return fmt.Sprintf("`affected %q` refused with `No unique node match`, naming none of the %d nodes "+
		"whose label contains the name (graphify resolves by label substring, so tests named after the "+
		"declaration refuse it too); re-asked as the node id `%s`, which is the `{parent_dir}_{stem}_{name}` "+
		"form the extractor builds and the shipped skill does not document, because graphify has no "+
		"path-qualified addressing",
		q.Subject.Symbol, candidates, graphifySymbolID(q.Subject.Path, q.Subject.Symbol)), nil
}

// graphifyPerRepositoryNote says so when the answer could not have been found:
// `update` builds one graph per repository, so truth in another repository is
// unreachable however good the traversal is.
func graphifyPerRepositoryNote(q question) string {
	subject := q.Subject.Dir + "/"
	elsewhere := 0
	for _, file := range q.Truth {
		if !strings.HasPrefix(file, subject) {
			elsewhere++
		}
	}
	if elsewhere == 0 {
		return ""
	}
	return fmt.Sprintf("%d of %d truth files are outside %s and no per-repository graph can hold them; "+
		"graphify's cross-repository path is `global add`/`merge-graphs`, which this arm does not build",
		elsewhere, len(q.Truth), q.Subject.Repo)
}

// graphifyRun prices one graphify call. HOME carries the isolation: graphify
// appends every query to `$HOME/.cache/graphify-queries.log`, and the working
// directory is that same home so an omitted `--graph` cannot find a graph.
func graphifyRun(ctx context.Context, tokens *counter, captures map[string]string,
	capture, home, binary string, arguments ...string) observation {
	return runCLI(ctx, tokens, captures, capture, home, map[string]string{"HOME": home}, binary, arguments...)
}

func graphifyRefused(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "No unique node match")
}

// graphifyClaimedFiles canonicalises repository-relative answers and drops the
// declaring file the question already named.
func graphifyClaimedFiles(repos repositories, q question, paths []string) []string {
	claimed := make([]string, 0, len(paths))
	for _, path := range paths {
		claimed = append(claimed, repos.canonical(q.Subject.Dir+"/"+path))
	}
	return withoutDeclaring(claimed, repos.canonical(q.Subject.corpusPath()))
}

// `- withRetry() [calls] internal/infrastructure/postgres/retry.go:L49`
var graphifyAffectedLine = regexp.MustCompile(`^-\s+(.+?)\s+\[([^\]]+)\]\s+(\S.*?)\s*$`)

var graphifyLocationSuffix = regexp.MustCompile(`:L\d+(-L?\d+)?$`)

func graphifyAffectedFiles(text string) []string {
	files := []string{}
	for _, line := range strings.Split(text, "\n") {
		match := graphifyAffectedLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if file := graphifyLocationSuffix.ReplaceAllString(match[3], ""); file != "-" {
			files = append(files, file)
		}
	}
	return files
}

// `NODE handleFetch() [src=src/cluster/worker/ipc/channel.ipc.ts loc=L105 community=33]`
var graphifyQuerySource = regexp.MustCompile(`^NODE\s+.*\[src=([^\s\]]+)`)

func graphifyQueryFiles(text string) []string {
	files := []string{}
	for _, line := range strings.Split(text, "\n") {
		if match := graphifyQuerySource.FindStringSubmatch(line); match != nil {
			files = append(files, match[1])
		}
	}
	return files
}

// `  --> withRetry() [contains] [EXTRACTED]`, or `<--` when the extractor
// happened to store the edge the other way round.
var graphifyConnection = regexp.MustCompile(`^\s*(?:-->|<--)\s+(.+?)\s+\[([^\]]+)\]`)

var graphifyHidden = regexp.MustCompile(`\.\.\. and (\d+) more`)

var graphifyExplainSourceLine = regexp.MustCompile(`^\s*Source:\s+(\S+)`)

// graphifyContained reads a file node's declarations off its `contains` edges,
// and how many connections `explain` refused to print.
func graphifyContained(text string) ([]string, int) {
	names := []string{}
	for _, line := range strings.Split(text, "\n") {
		match := graphifyConnection.FindStringSubmatch(line)
		if match == nil || match[2] != "contains" {
			continue
		}
		names = append(names, strings.TrimSuffix(match[1], "()"))
	}
	hidden := 0
	if match := graphifyHidden.FindStringSubmatch(text); match != nil {
		hidden, _ = strconv.Atoi(match[1])
	}
	return names, hidden
}

func graphifyExplainSource(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if match := graphifyExplainSourceLine.FindStringSubmatch(line); match != nil {
			return match[1]
		}
	}
	return ""
}

// graphifyCandidates counts the nodes graphify's resolver refused between, by
// replaying the rule that produced the refusal: a case-insensitive substring
// match on the node label.
func graphifyCandidates(graph, symbol string) (int, error) {
	data, err := os.ReadFile(graph)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", graph, err)
	}
	var parsed struct {
		Nodes []struct {
			Label string `json:"label"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return 0, fmt.Errorf("parse %s: %w", graph, err)
	}
	needle := strings.ToLower(symbol)
	candidates := 0
	for _, node := range parsed.Nodes {
		if strings.Contains(strings.ToLower(node.Label), needle) {
			candidates++
		}
	}
	return candidates, nil
}

// graphifyFileID and graphifySymbolID rebuild the node ids graphify's extractor
// assigns, because no command accepts a repository-relative path and the ids
// are the only unique handle the graph has. `{parent_dir}_{stem}` for a file,
// the same plus the declaration name for a declaration.
func graphifyFileID(repositoryPath string) string {
	return graphifyID(graphifyFileStem(repositoryPath))
}

func graphifySymbolID(repositoryPath, name string) string {
	return graphifyID(graphifyFileStem(repositoryPath), name)
}

func graphifyFileStem(repositoryPath string) string {
	base := filepath.Base(repositoryPath)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	parent := filepath.Base(filepath.Dir(repositoryPath))
	if parent == "." || parent == string(filepath.Separator) {
		return stem
	}
	return parent + "." + stem
}

var graphifyNonWord = regexp.MustCompile(`[^\p{L}\p{N}_]+`)

var graphifyRuns = regexp.MustCompile(`_+`)

func graphifyID(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.Trim(part, "_."); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	id := graphifyNonWord.ReplaceAllString(strings.Join(kept, "_"), "_")
	return strings.ToLower(strings.Trim(graphifyRuns.ReplaceAllString(id, "_"), "_"))
}
