package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// measureKivgraph answers the three families the way the tool's own description
// tells a caller to.
//
// It asks for the default view, not the cheaper `files` one, even though the
// questions here are about files and `files` answers them for a third of the
// tokens (measured in benchmarks/graft-comparison). Every other arm returns its
// line-level answer because that is all it has; taking the discount only we can
// take would compare our summary against their detail and call the difference a
// saving. The discount is reported in the write-up instead.
func measureKivgraph(
	ctx context.Context, tokens *counter, repos repositories, kiv *server, q question,
) (*armResult, error) {
	switch q.Family {
	case familyReferences:
		return kivgraphReferences(ctx, tokens, repos, kiv, q)
	case familyImpact:
		return kivgraphImpact(ctx, tokens, repos, kiv, q)
	case familyOutline:
		return kivgraphOutline(ctx, tokens, kiv, q)
	}
	return nil, fmt.Errorf("unknown family %q", q.Family)
}

// kivgraphReferences asks by bare name first, which is what the description
// says suffices, and narrows only when the name is ambiguous -- paying for the
// refusal, because the refusal is what named the candidates.
func kivgraphReferences(
	ctx context.Context, tokens *counter, repos repositories, kiv *server, q question,
) (*armResult, error) {
	arm := &armResult{}
	arguments := map[string]any{"name": q.Subject.Symbol, "direction": "incoming"}
	answer := kiv.call(ctx, tokens, q.ID+"-kivgraph-by-name", "find_references", arguments)
	arm.add(answer)
	if answer.Failed && strings.Contains(answer.Error, "AMBIGUOUS_SYMBOL") {
		arm.Ambiguous = declarationsNamed(answer.Error)
		arguments = map[string]any{
			"qualified_name": q.Subject.Name, "repository": q.Subject.Repo,
			"path": q.Subject.Path, "direction": "incoming",
		}
		answer = kiv.call(ctx, tokens, q.ID+"-kivgraph-p1", "find_references", arguments)
		arm.add(answer)
	}
	claimed := []string{}
	for page := 1; !answer.Failed; page++ {
		files, cursor, err := kivgraphReferenceFiles(answer.Text)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, files...)
		if cursor == "" {
			break
		}
		arguments["cursor"] = cursor
		answer = kiv.call(ctx, tokens,
			fmt.Sprintf("%s-kivgraph-p%d", q.ID, page+1), "find_references", arguments)
		arm.add(answer)
	}
	arm.Claimed = withoutDeclaring(repos.canonicalAll(claimed), repos.canonical(q.Subject.corpusPath()))
	arm.Score = scoreAgainst(arm.Claimed, repos.canonicalAll(q.Truth))
	return arm, nil
}

// kivgraphImpact asks the blast radius at the question's depth. The answer
// declares which kinds it left out by default, and that declaration is repeated
// in the note rather than silently absorbed into the score.
func kivgraphImpact(
	ctx context.Context, tokens *counter, repos repositories, kiv *server, q question,
) (*armResult, error) {
	arm := &armResult{}
	answer := kiv.call(ctx, tokens, q.ID+"-kivgraph-blast", "get_blast_radius", map[string]any{
		"qualified_name": q.Subject.Name, "repository": q.Subject.Repo,
		"path": q.Subject.Path, "depth": q.Depth,
	})
	arm.add(answer)
	if answer.Failed {
		arm.Note = "refused: " + answer.Error
		arm.Score = scoreAgainst(nil, repos.canonicalAll(q.Truth))
		return arm, nil
	}
	files, _, err := kivgraphReferenceFiles(answer.Text)
	if err != nil {
		return nil, err
	}
	arm.Claimed = withoutDeclaring(repos.canonicalAll(files), repos.canonical(q.Subject.corpusPath()))
	arm.Score = scoreAgainst(arm.Claimed, repos.canonicalAll(q.Truth))
	if excluded := excludedKinds(answer.Text); excluded != "" {
		arm.Note = "excludes " + excluded + " by default, and says so in the payload"
	}
	return arm, nil
}

// kivgraphOutline asks what one file declares. The compact answer spells an
// entry as `name@line`, and a nested name -- `handleCreate.mentionable` -- is a
// local inside a declaration rather than a declaration of the file, so the
// top-level set is the undotted one.
func kivgraphOutline(ctx context.Context, tokens *counter, kiv *server, q question) (*armResult, error) {
	arm := &armResult{}
	answer := kiv.call(ctx, tokens, q.ID+"-kivgraph-outline", "get_file_outline", map[string]any{
		"repository": q.Subject.Repo, "path": q.Subject.Path,
	})
	arm.add(answer)
	if answer.Failed {
		arm.Note = "refused: " + answer.Error
		arm.Score = scoreAgainst(nil, q.Truth)
		return arm, nil
	}
	names, err := kivgraphOutlineNames(answer.Text)
	if err != nil {
		return nil, err
	}
	arm.Claimed = names
	arm.Score = scoreAgainst(names, q.Truth)
	return arm, nil
}

// referencePage is the shape find_references and get_blast_radius answer in once
// ADR 0046 hoisted every field that repeated: the subject once, the repository
// once, and rows grouped by whatever they share. One group collapses into
// `results.files`; several stay under `results.groups`.
type referencePage struct {
	NextCursor *string `json:"next_cursor"`
	Results    struct {
		Repository string         `json:"repository"`
		Files      []referenceRow `json:"files"`
		Groups     []struct {
			Repository string         `json:"repository"`
			Files      []referenceRow `json:"files"`
		} `json:"groups"`
		KindsExcluded []string `json:"kinds_default_excluded"`
	} `json:"results"`
}

// referenceRow is one file in an answer. The header hoists a repository only
// when every row shares one, so a row that spans repositories carries its own
// under `repo`. Reading only the header was silently correct while every
// answer stayed inside one repository, and silently empty the moment one did
// not -- which is exactly what a cross-repository answer looks like.
type referenceRow struct {
	File  string            `json:"file"`
	Repo  string            `json:"repo"`
	At    []json.RawMessage `json:"at"`
	Count int               `json:"count"`
}

// kivgraphReferenceFiles reads one page as `repository/path` addresses, keeping
// one entry per fact. The `files` view carries no header repository because a
// list spanning repositories has nowhere to hoist one, so an empty header means
// the entry already names its own.
func kivgraphReferenceFiles(text string) ([]string, string, error) {
	page := referencePage{}
	if err := json.Unmarshal([]byte(text), &page); err != nil {
		return nil, "", fmt.Errorf("parse kivgraph page: %w", err)
	}
	out := []string{}
	collect := func(repository, file string, facts int) {
		address := repository + "/" + file
		if repository == "" {
			address = file
		}
		if facts == 0 {
			facts = 1
		}
		for range facts {
			out = append(out, address)
		}
	}
	// A row names its own repository when it has one; the header is the
	// fallback for the hoisted case.
	rowRepository := func(header, group string, row referenceRow) string {
		if row.Repo != "" {
			return row.Repo
		}
		if group != "" {
			return group
		}
		return header
	}
	for _, file := range page.Results.Files {
		collect(rowRepository(page.Results.Repository, "", file), file.File,
			max(len(file.At), file.Count))
	}
	for _, group := range page.Results.Groups {
		for _, file := range group.Files {
			collect(rowRepository(page.Results.Repository, group.Repository, file),
				file.File, max(len(file.At), file.Count))
		}
	}
	cursor := ""
	if page.NextCursor != nil {
		cursor = *page.NextCursor
	}
	return out, cursor, nil
}

func excludedKinds(text string) string {
	page := referencePage{}
	if err := json.Unmarshal([]byte(text), &page); err != nil {
		return ""
	}
	return strings.Join(page.Results.KindsExcluded, ", ")
}

// outlinePage is the shape get_file_outline answers in. One group collapses
// into `results.files`, several stay under `results.groups`, and an entry is a
// bare label when its group hoisted the kind and a tuple when it did not --
// `["withRetry@49-78", "func"]`. Reading only one of those four combinations is
// how this arm first scored zero on a file it had answered perfectly.
type outlinePage struct {
	Results struct {
		Kind   string        `json:"kind"`
		Files  []outlineFile `json:"files"`
		Groups []struct {
			Kind  string        `json:"kind"`
			Files []outlineFile `json:"files"`
		} `json:"groups"`
	} `json:"results"`
}

// outlineBindingKinds are the groups that are not declarations of the file. An
// `import` binds a name into it and an `export` re-exposes a declaration the
// same answer already carries -- `handleChannelIPC` arrives once as a function
// and once as `handleChannelIPC#2` under `export`. Counting them would answer
// "what does this file bind", which is a different question from the one asked,
// and the kind that says so is in the payload.
var outlineBindingKinds = map[string]bool{"import": true, "export": true}

type outlineFile struct {
	At []json.RawMessage `json:"at"`
}

// kivgraphOutlineNames reads the declaration names. A nested label --
// `handleCreate.mentionable` -- is a local inside a declaration rather than a
// declaration of the file, so the top-level set is the undotted one.
func kivgraphOutlineNames(text string) ([]string, error) {
	page := outlinePage{}
	if err := json.Unmarshal([]byte(text), &page); err != nil {
		return nil, fmt.Errorf("parse kivgraph outline: %w", err)
	}
	seen := map[string]bool{}
	out := []string{}
	collect := func(kind string, files []outlineFile) {
		if outlineBindingKinds[kind] {
			return
		}
		for _, file := range files {
			for _, entry := range file.At {
				label := outlineLabel(entry)
				name, _, _ := strings.Cut(label, "@")
				if name == "" || strings.Contains(name, ".") || seen[name] {
					continue
				}
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	collect(page.Results.Kind, page.Results.Files)
	for _, group := range page.Results.Groups {
		kind := group.Kind
		if kind == "" {
			kind = page.Results.Kind
		}
		collect(kind, group.Files)
	}
	return out, nil
}

// outlineLabel reads the declaration label out of an entry in either spelling.
func outlineLabel(entry json.RawMessage) string {
	var literal string
	if json.Unmarshal(entry, &literal) == nil {
		return literal
	}
	var tuple []string
	if json.Unmarshal(entry, &tuple) == nil && len(tuple) > 0 {
		return tuple[0]
	}
	return ""
}

// withoutDeclaring drops the file the question already named. Every arm knows
// where the subject lives, because the question said so, and counting it would
// inflate all of them equally.
func withoutDeclaring(claimed []string, declaring string) []string {
	out := make([]string, 0, len(claimed))
	for _, item := range claimed {
		if item == declaring {
			continue
		}
		out = append(out, item)
	}
	return out
}

// declarationsNamed reads how many declarations a refusal refused between, so
// the number means the same thing on every arm that refuses instead of guessing.
func declarationsNamed(message string) int {
	_, rest, found := strings.Cut(message, "declares ")
	if !found {
		return 0
	}
	count := 0
	for _, char := range rest {
		if char < '0' || char > '9' {
			break
		}
		count = count*10 + int(char-'0')
	}
	return count
}
