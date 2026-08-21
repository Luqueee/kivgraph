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
	case familyConsumers:
		return kivgraphConsumers(ctx, tokens, repos, kiv, q)
	case familyDependencies:
		return kivgraphDependencies(ctx, tokens, repos, kiv, q)
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
	// An outline pages like every other answer, and a file with more
	// declarations than one page holds is exactly where an outline is worth
	// asking for: reading only the first page capped recall at the page size
	// and blamed the tool for the harness stopping early.
	arguments := map[string]any{"repository": q.Subject.Repo, "path": q.Subject.Path}
	labels := []string{}
	for page := 1; ; page++ {
		answer := kiv.call(ctx, tokens, fmt.Sprintf("%s-kivgraph-outline-p%d", q.ID, page), "get_file_outline", arguments)
		arm.add(answer)
		if answer.Failed {
			arm.Note = "refused: " + answer.Error
			arm.Score = scoreAgainst(nil, q.Truth)
			return arm, nil
		}
		pageLabels, cursor, err := kivgraphOutlineLabels(answer.Text)
		if err != nil {
			return nil, err
		}
		labels = append(labels, pageLabels...)
		if cursor == nil {
			break
		}
		arguments["cursor"] = *cursor
	}
	names := declarationNames(labels)
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
	NextCursor *string `json:"next_cursor"`
	Results    struct {
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

// kivgraphOutlineLabels returns one page of raw labels and its cursor. The
// labels are not names yet: see declarationNames for why turning one into the
// other is the harness's job and not the tool's.
func kivgraphOutlineLabels(text string) ([]string, *string, error) {
	page := outlinePage{}
	if err := json.Unmarshal([]byte(text), &page); err != nil {
		return nil, nil, fmt.Errorf("parse kivgraph outline: %w", err)
	}
	out := []string{}
	collect := func(kind string, files []outlineFile) {
		if outlineBindingKinds[kind] {
			return
		}
		for _, file := range files {
			for _, entry := range file.At {
				if label := outlineLabel(entry); label != "" {
					name, _, _ := strings.Cut(label, "@")
					out = append(out, name)
				}
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
	return out, page.NextCursor, nil
}

// declarationNames reduces qualified labels to the unit an outline is compared
// in. This is the harness honouring its own rule -- the answer is compared,
// never the spelling -- and it did not before: the `.`-nesting rule alone read
// a Go or TypeScript answer correctly, where a top-level name is already bare,
// and scored a Rust one at zero because every label there carries its module
// path, so `audio::range::parse_range` never equalled `parse_range`.
//
// The prefix is derived rather than assumed: whatever `::` segments every label
// shares is the enclosing scope the file itself is, so stripping it leaves the
// declaration. A label that is exactly that prefix is the file's own module,
// which is not something the file declares, and a label with a separator left
// after stripping is nested inside another declaration -- an `impl` block, a
// `mod tests`, a local -- and so is not top level either.
func declarationNames(labels []string) []string {
	prefix := enclosingScope(labels)
	seen := map[string]bool{}
	out := []string{}
	for _, label := range labels {
		name := label
		if prefix != "" {
			if name == prefix {
				continue
			}
			name = strings.TrimPrefix(name, prefix+"::")
		}
		if name == "" || strings.Contains(name, "::") || strings.Contains(name, ".") || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// enclosingScope is the label that names the scope the file itself is: the one
// every other label hangs under. It is found rather than assumed -- the label
// that is a `::` prefix of all the others -- so an answer whose labels are
// already bare, which is every Go and TypeScript answer here, reports no scope
// and reaches the nesting rule unchanged.
//
// The longest common prefix cannot be used for this. The file's own module row
// is one segment shorter than everything under it, so the common prefix of
// `audio::range` and `audio::range::parse_range` is `audio`, which left every
// real declaration looking nested and scored a correct answer at zero.
func enclosingScope(labels []string) string {
	for _, candidate := range labels {
		if !strings.Contains(candidate, "::") {
			continue
		}
		prefixes := true
		for _, other := range labels {
			if other == candidate || strings.HasPrefix(other, candidate+"::") {
				continue
			}
			prefixes = false
			break
		}
		if prefixes {
			return candidate
		}
	}
	return ""
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

// consumerPage is what find_cross_repo_consumers answers. The subject is stated
// once and consumers are grouped by everything they share, so a row carries only
// its repository, its package and where it is -- `at` being `path:line`, since a
// consumer in another repository has no hoisted path to inherit.
//
// The page also splits its own coverage: an exact symbol consumer is a resolved
// use, and a package level one is a repository that depends on the package
// without a resolved symbol behind it. Only the first answers this question, and
// the second is reported rather than folded in.
type consumerPage struct {
	NextCursor *string `json:"next_cursor"`
	Coverage   struct {
		Exact        int `json:"exact"`
		PackageLevel int `json:"package_level"`
	} `json:"coverage"`
	Results struct {
		Groups []struct {
			Category  string `json:"category"`
			EdgeKind  string `json:"edge_kind"`
			Consumers []struct {
				Repo string `json:"repo"`
				At   string `json:"at"`
			} `json:"consumers"`
		} `json:"groups"`
	} `json:"results"`
}

// kivgraphConsumers asks which repositories other than the declaring one use the
// subject. It counts the exact symbol rows: a package level row says a
// repository depends on the package, which is a true fact about a dependency and
// not a use of this declaration.
func kivgraphConsumers(
	ctx context.Context, tokens *counter, repos repositories, kiv *server, q question,
) (*armResult, error) {
	arm := &armResult{}
	arguments := map[string]any{
		"qualified_name": q.Subject.Name, "repository": q.Subject.Repo, "path": q.Subject.Path,
	}
	claimed, packageLevel := []string{}, 0
	for page := 1; ; page++ {
		answer := kiv.call(ctx, tokens,
			fmt.Sprintf("%s-kivgraph-consumers-p%d", q.ID, page), "find_cross_repo_consumers", arguments)
		arm.add(answer)
		if answer.Failed {
			arm.Note = "refused: " + answer.Error
			arm.Score = scoreAgainst(nil, repos.canonicalAll(q.Truth))
			return arm, nil
		}
		var decoded consumerPage
		if err := json.Unmarshal([]byte(answer.Text), &decoded); err != nil {
			return nil, fmt.Errorf("%s: decode consumers: %w", q.ID, err)
		}
		packageLevel += decoded.Coverage.PackageLevel
		for _, group := range decoded.Results.Groups {
			if group.Category != "exact_symbol" {
				continue
			}
			for _, consumer := range group.Consumers {
				path := consumer.At
				if cut := strings.LastIndex(path, ":"); cut > 0 {
					path = path[:cut]
				}
				claimed = append(claimed, repos.canonical(consumer.Repo+"/"+path))
			}
		}
		if decoded.NextCursor == nil {
			break
		}
		arguments["cursor"] = *decoded.NextCursor
	}
	arm.Claimed = claimed
	arm.Score = scoreAgainst(claimed, repos.canonicalAll(q.Truth))
	if packageLevel > 0 {
		arm.Note = fmt.Sprintf("%d package level row(s) reported separately and not counted as uses", packageLevel)
	}
	return arm, nil
}

// kivgraphDependencies asks what the subject reaches outward at the question's
// depth. The answer groups reached symbols by file under one hoisted repository,
// which is the same shape a reference page uses.
func kivgraphDependencies(
	ctx context.Context, tokens *counter, repos repositories, kiv *server, q question,
) (*armResult, error) {
	arm := &armResult{}
	arguments := map[string]any{
		"qualified_name": q.Subject.Name, "repository": q.Subject.Repo,
		"path": q.Subject.Path, "depth": q.Depth,
	}
	claimed := []string{}
	for page := 1; ; page++ {
		answer := kiv.call(ctx, tokens,
			fmt.Sprintf("%s-kivgraph-deps-p%d", q.ID, page), "trace_dependencies", arguments)
		arm.add(answer)
		if answer.Failed {
			arm.Note = "refused: " + answer.Error
			arm.Score = scoreAgainst(nil, repos.canonicalAll(q.Truth))
			return arm, nil
		}
		files, cursor, err := kivgraphReferenceFiles(answer.Text)
		if err != nil {
			return nil, err
		}
		claimed = append(claimed, files...)
		if cursor == "" {
			break
		}
		arguments["cursor"] = cursor
	}
	arm.Claimed = withoutDeclaring(repos.canonicalAll(claimed), repos.canonical(q.Subject.corpusPath()))
	arm.Score = scoreAgainst(arm.Claimed, repos.canonicalAll(q.Truth))
	return arm, nil
}
