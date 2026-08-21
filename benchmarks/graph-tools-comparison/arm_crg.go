package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// code-review-graph's answers, in the three shapes `query` and `impact` use.
const (
	crgStatusOK        = "ok"
	crgStatusAmbiguous = "ambiguous"
)

// crgNode is one row of a `query` answer: a result, or a candidate a refusal
// named. `qualified_name` is spelled `<absolute file>::<bare name>`, which is
// the form the refusal's own hint tells a caller to re-ask with.
type crgNode struct {
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	FilePath      string `json:"file_path"`
	ParentName    string `json:"parent_name"`
}

// crgAnswer is a `query` answer. `target` is the node the tool resolved the
// argument to, which is what makes a silent resolution checkable: a bare name
// that matches one node is answered without comment, and the only way to see
// which declaration was answered about is to read it back.
type crgAnswer struct {
	Status      string    `json:"status"`
	Summary     string    `json:"summary"`
	Target      string    `json:"target"`
	ResultCount int       `json:"result_count"`
	Results     []crgNode `json:"results"`
	Candidates  []crgNode `json:"candidates"`
}

// crgImpactAnswer is what the `impact` subcommand answers. It is the only
// transitive view the tool has: every `query` pattern is one hop.
type crgImpactAnswer struct {
	Status        string   `json:"status"`
	Summary       string   `json:"summary"`
	ImpactedFiles []string `json:"impacted_files"`
	TotalImpacted int      `json:"total_impacted"`
	Truncated     bool     `json:"truncated"`
	NodesOmitted  int      `json:"nodes_omitted"`
}

// buildCRG builds one repository's graph and returns what it cost in
// milliseconds.
//
// Each repository gets its own directory under dataDir, because `build` is a
// full build: it replaces whatever graph the data directory already held. One
// shared directory across 37 repositories would leave the last one standing and
// silently answer every earlier question with `not_found`. The per-repository
// path is recorded in `$HOME/.code-review-graph/registry.json`, which is how
// `query` -- which takes no `--data-dir` -- finds the right graph again, and why
// the isolated HOME has to be the same one the queries run under.
func buildCRG(ctx context.Context, binary, repoPath, dataDir, home string) (float64, error) {
	directory := filepath.Join(dataDir, filepath.Base(repoPath))
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return 0, fmt.Errorf("create %s: %w", directory, err)
	}
	command := exec.CommandContext(ctx, binary, "build", "--repo", repoPath, "--data-dir", directory, "-q")
	command.Env = append(os.Environ(), "HOME="+home)
	started := time.Now()
	output, err := command.CombinedOutput()
	elapsed := float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		return elapsed, fmt.Errorf("code-review-graph build %s: %w (%s)",
			repoPath, err, strings.TrimSpace(string(output)))
	}
	return elapsed, nil
}

// measureCRG answers the three families in code-review-graph's own vocabulary:
// `query callers_of` for references, the `impact` subcommand for the transitive
// family, and `query file_summary` for an outline.
//
// dataDir is not passed to any of them. A query resolves its graph through the
// registry in HOME rather than through a flag, so the isolation that matters
// here is the home directory, and the data directory only had to be right when
// buildCRG registered it.
func measureCRG(ctx context.Context, tokens *counter, repos repositories, captures map[string]string,
	binary, corpusRoot, dataDir, home string, q question) (*armResult, error) {
	repoPath := filepath.Join(corpusRoot, q.Subject.Dir)
	switch q.Family {
	case familyReferences:
		return crgReferences(ctx, tokens, repos, captures, binary, repoPath, home, q)
	case familyImpact:
		return crgImpact(ctx, tokens, repos, captures, binary, repoPath, home, q)
	case familyOutline:
		return crgOutline(ctx, tokens, captures, binary, repoPath, home, q)
	case familyConsumers:
		return &armResult{Unsupported: true, Note: "built with `--repo`, one repository per graph, so it has no cross-repository dimension to report"}, nil
	case familyDependencies:
		return &armResult{Unsupported: true, Note: "its graph is built around blast radius, which is the incoming direction"}, nil
	case familyLocate:
		return &armResult{Unsupported: true, Note: "`callers_of` is its only symbol entry point and it answers callers"}, nil
	case familyBodies:
		return &armResult{Unsupported: true, Note: "code-review-graph returns impacted files, not source"}, nil
	case familyFacts:
		return &armResult{Unsupported: true, Note: "it reports impact, not a declaration record"}, nil
	}
	return nil, fmt.Errorf("unknown family %q", q.Family)
}

// crgReferences asks by bare name first, because that is what a caller has, and
// narrows only when the first answer was not about the subject -- paying for
// both calls, because being refused costs what it costs.
//
// There are two ways the first answer can miss. A name matching several nodes is
// refused with its candidates, and then the candidate list is what names the
// right one. A name matching one node is answered without comment even when that
// node is a homonym in another file, and then the `target` field is the only
// thing that says so; re-asking is the same narrowing either way.
func crgReferences(ctx context.Context, tokens *counter, repos repositories, captures map[string]string,
	binary, repoPath, home string, q question) (*armResult, error) {
	arm := &armResult{}
	file := filepath.Join(repoPath, q.Subject.Path)
	observed, answer, err := crgQuery(ctx, tokens, captures,
		q.ID+"-crg-by-name", binary, repoPath, home, "callers_of", q.Subject.Symbol)
	if err != nil {
		return nil, err
	}
	arm.add(observed)
	if observed.Failed {
		return crgRefused(arm, repos, q, "callers_of "+q.Subject.Symbol+" exited non-zero: "+observed.Error), nil
	}

	if answer.Status == crgStatusAmbiguous {
		arm.Ambiguous = len(answer.Candidates)
		candidate, found := crgCandidate(answer.Candidates, file, q.Subject.Symbol)
		if !found {
			return crgRefused(arm, repos, q, fmt.Sprintf(
				"refused between %d declarations and named none in %s", arm.Ambiguous, q.Subject.Path)), nil
		}
		observed, answer, err = crgQuery(ctx, tokens, captures,
			q.ID+"-crg-narrowed", binary, repoPath, home, "callers_of", candidate.QualifiedName)
	} else if !strings.HasPrefix(answer.Target, file+"::") {
		// The bare name resolved to something else, or to nothing. Re-ask in
		// the spelling the tool's own hint asks for, at the subject's own path.
		observed, answer, err = crgQuery(ctx, tokens, captures,
			q.ID+"-crg-narrowed", binary, repoPath, home, "callers_of", file+"::"+q.Subject.Symbol)
	} else {
		return crgClaimReferences(arm, repos, q, answer), nil
	}
	if err != nil {
		return nil, err
	}
	arm.add(observed)
	if observed.Failed {
		return crgRefused(arm, repos, q, "narrowed callers_of exited non-zero: "+observed.Error), nil
	}
	return crgClaimReferences(arm, repos, q, answer), nil
}

// crgClaimReferences scores whatever the last answer held. Repetition is kept:
// eleven callers in three files are eleven facts, and the scorer is what decides
// to compare sets.
func crgClaimReferences(arm *armResult, repos repositories, q question, answer crgAnswer) *armResult {
	claimed := make([]string, 0, len(answer.Results))
	for _, result := range answer.Results {
		claimed = append(claimed, repos.canonical(result.FilePath))
	}
	arm.Claimed = withoutDeclaring(claimed, repos.canonical(q.Subject.corpusPath()))
	arm.Score = scoreAgainst(arm.Claimed, repos.canonicalAll(q.Truth))
	switch {
	case answer.Status != crgStatusOK:
		arm.Note = "callers_of answered " + answer.Status + " for " + q.Subject.Symbol
	case len(arm.Claimed) == 0:
		arm.Note = "answered zero callers: `build` keeps one graph per repository, so a call site outside " +
			q.Subject.Repo + " was never in the graph this query read"
	}
	return arm
}

// crgImpact asks the blast radius at the question's depth.
//
// `impact` is the only transitive view the tool has, and it takes changed files
// rather than declarations: `--files <path>::<name>` reports zero changed nodes,
// so the subject can only be named by the file that holds it. What comes back is
// therefore the reachable set of the whole file, which is a wider question than
// the one asked, and the note says so rather than letting the precision imply a
// tool that answered badly about a declaration.
func crgImpact(ctx context.Context, tokens *counter, repos repositories, captures map[string]string,
	binary, repoPath, home string, q question) (*armResult, error) {
	arm := &armResult{}
	observed := runCLI(ctx, tokens, captures, q.ID+"-crg-impact", repoPath,
		map[string]string{"HOME": home}, binary,
		"impact", "--repo", repoPath, "--files", q.Subject.Path, "--depth", strconv.Itoa(q.Depth))
	arm.add(observed)
	if observed.Failed {
		return crgRefused(arm, repos, q, "impact exited non-zero: "+observed.Error), nil
	}
	var answer crgImpactAnswer
	if err := json.Unmarshal([]byte(observed.Text), &answer); err != nil {
		return nil, fmt.Errorf("parse impact %s: %w", q.ID, err)
	}
	claimed := make([]string, 0, len(answer.ImpactedFiles))
	for _, path := range answer.ImpactedFiles {
		claimed = append(claimed, repos.canonical(path))
	}
	arm.Claimed = withoutDeclaring(claimed, repos.canonical(q.Subject.corpusPath()))
	arm.Score = scoreAgainst(arm.Claimed, repos.canonicalAll(q.Truth))
	arm.Note = fmt.Sprintf(
		"impact takes changed files, never a declaration -- `--files %s::%s` reports zero changed nodes -- "+
			"so this is the whole file's reachable set at the selectable --depth %d: %d nodes over %d files",
		q.Subject.Path, q.Subject.Name, q.Depth, answer.TotalImpacted, len(answer.ImpactedFiles))
	if answer.Truncated {
		arm.Note += fmt.Sprintf("; truncated, %d nodes omitted", answer.NodesOmitted)
	}
	return arm, nil
}

// crgOutline asks what one file declares. `file_summary` returns the file's own
// node alongside them, and a node with a parent is a member of a declaration
// rather than a declaration of the file, so the top-level set is neither.
func crgOutline(ctx context.Context, tokens *counter, captures map[string]string,
	binary, repoPath, home string, q question) (*armResult, error) {
	arm := &armResult{}
	observed, answer, err := crgQuery(ctx, tokens, captures,
		q.ID+"-crg-outline", binary, repoPath, home, "file_summary", q.Subject.Path)
	if err != nil {
		return nil, err
	}
	arm.add(observed)
	if observed.Failed {
		arm.Note = "file_summary exited non-zero: " + observed.Error
		arm.Score = scoreAgainst(nil, q.Truth)
		return arm, nil
	}
	names := make([]string, 0, len(answer.Results))
	for _, result := range answer.Results {
		if result.Kind == "File" || result.ParentName != "" {
			continue
		}
		names = append(names, result.Name)
	}
	arm.Claimed = names
	arm.Score = scoreAgainst(names, q.Truth)
	arm.Note = "file_summary returns the file's own node beside its declarations; only the declarations are claimed"
	if answer.Status != crgStatusOK {
		arm.Note = "file_summary answered " + answer.Status + " for " + q.Subject.Path
	}
	return arm, nil
}

// crgQuery prices one `query` call and reads it back. A refusal and a non-zero
// exit both travel in the observation, because both are answers a caller pays
// for; only output that is not the JSON the tool documents is an error.
func crgQuery(ctx context.Context, tokens *counter, captures map[string]string,
	capture, binary, repoPath, home, pattern, target string) (observation, crgAnswer, error) {
	observed := runCLI(ctx, tokens, captures, capture, repoPath,
		map[string]string{"HOME": home}, binary, "query", "--repo", repoPath, pattern, target)
	if observed.Failed {
		return observed, crgAnswer{}, nil
	}
	var answer crgAnswer
	if err := json.Unmarshal([]byte(observed.Text), &answer); err != nil {
		return observed, crgAnswer{}, fmt.Errorf("parse %s %s: %w", pattern, capture, err)
	}
	return observed, answer, nil
}

// crgCandidate picks the declaration the question asked about out of the ones a
// refusal named. The subject's file decides it, and its name breaks a tie
// between two nodes in that file -- a `describe:getRequiredField@L38` in a test
// is a candidate the tool offers for the same bare name.
func crgCandidate(candidates []crgNode, file, symbol string) (crgNode, bool) {
	best, bestRank := crgNode{}, 0
	for _, candidate := range candidates {
		rank := 0
		switch {
		case candidate.FilePath == file:
			rank = 2
		case candidate.FilePath != "" && strings.HasSuffix(file, "/"+candidate.FilePath):
			rank = 1
		default:
			continue
		}
		if candidate.Name == symbol || strings.HasSuffix(candidate.QualifiedName, "::"+symbol) {
			rank += 2
		}
		if rank > bestRank {
			best, bestRank = candidate, rank
		}
	}
	return best, bestRank > 0
}

// crgRefused records an answer that never named files. It is scored, not marked
// unsupported: the tool answers this family, and being told nothing about a
// question it accepts is a result about the tool.
func crgRefused(arm *armResult, repos repositories, q question, note string) *armResult {
	arm.Note = note
	arm.Score = scoreAgainst(nil, repos.canonicalAll(q.Truth))
	return arm
}
