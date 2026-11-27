package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/syntax"
)

type callableRange struct {
	id         hotsnapshot.SymbolID
	start, end uint32
}

// positionFiles caches callable ranges for the requested files of one batch.
type positionFiles struct {
	requests   []SourceRequest
	selectors  []symbolSelector
	candidates map[hotsnapshot.FileID][]callableCandidate
	ranges     map[hotsnapshot.FileID][]callableRange
}

type callableCandidate struct {
	id         hotsnapshot.SymbolID
	name       hotsnapshot.InternedString
	kind       string
	start, end uint32
}

func (files *positionFiles) resolve(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, request SourceRequest, path string, cache map[string]*sourceFile) (hotsnapshot.SymbolID, error) {
	if request.Publication != publicationToken(snapshot.Metadata()) {
		return 0, NewToolError(CodeSnapshotUnavailable, "publication changed; repeat the reference query")
	}
	repoID, id, err := locatePositionFile(snapshot, request.Repository, path)
	if err != nil {
		return 0, err
	}
	table := snapshot.Strings()
	ranges, ready := files.ranges[id]
	if !ready {
		repo, _ := snapshot.Repository(repoID)
		root, _ := table.String(repo.Path)
		file, _ := snapshot.File(id)
		key := root + "\x00" + path
		source := cache[key]
		if source == nil {
			source = loadSourceFile(root, path, file.ContentDigest)
			cache[key] = source
		}
		if source.err != nil {
			return 0, WrapToolError(CodeSnapshotUnavailable, "cannot verify position source", source.err)
		}
		if !source.fresh {
			return 0, NewToolError(CodeSnapshotUnavailable, "position source differs from the snapshot or has no indexed digest")
		}
		language, _ := table.String(file.Language)
		candidates, err := files.candidatesOf(ctx, snapshot, id)
		if err != nil {
			return 0, WrapToolError(CodeSnapshotUnavailable, "inspect indexed callable ranges", err)
		}
		var bindings map[bindingRange]bool
		last := uint64(source.physicalLines())
		for _, c := range candidates {
			switch c.kind {
			case "func", "function", "method", "static_method", "constructor":
			case "variable", "constant":
				if language != "typescript" && language != "javascript" {
					continue
				}
				if bindings == nil {
					lang := syntax.Language(language)
					if filepath.Ext(path) == ".tsx" {
						lang = syntax.LanguageTSX
					}
					bindings, err = functionBindings(ctx, lang, source.lines)
					if err != nil {
						return 0, WrapToolError(CodeSnapshotUnavailable, "inspect indexed callable ranges", err)
					}
				}
				name, _ := table.String(c.name)
				if !bindings[bindingRange{name, c.start, c.end}] {
					continue
				}
			default:
				continue
			}
			if uint64(c.end) > last {
				return 0, WrapToolError(CodeSnapshotUnavailable, "inspect indexed callable ranges",
					fmt.Errorf("indexed callable at %s:%d-%d exceeds the verified file", path, c.start, c.end))
			}
			ranges = append(ranges, callableRange{c.id, c.start, c.end})
		}
		if files.ranges == nil {
			files.ranges = make(map[hotsnapshot.FileID][]callableRange)
		}
		files.ranges[id] = ranges
	}
	// An innermost range must be contained in every matching range, not just shorter.
	var lo, hi uint32 = 0, ^uint32(0)
	matches := 0
	for _, r := range ranges {
		if r.start <= *request.Line && *request.Line <= r.end {
			lo = max(lo, r.start)
			hi = min(hi, r.end)
			matches++
		}
	}
	if matches == 0 {
		return 0, NewToolError(CodeSymbolNotFound, fmt.Sprintf("no indexed function contains %s:%d", path, *request.Line))
	}
	var found hotsnapshot.SymbolID
	n := 0
	for _, r := range ranges {
		if r.start == lo && r.end == hi {
			found = r.id
			n++
		}
	}
	if n != 1 {
		return 0, NewToolError(CodeAmbiguousSymbol, fmt.Sprintf("multiple enclosing functions at %s:%d; use a symbol selector", path, *request.Line))
	}
	return found, nil
}

func locatePositionFile(snapshot *hotsnapshot.GraphSnapshot, repository, path string) (hotsnapshot.RepositoryID, hotsnapshot.FileID, error) {
	repoID, ok := snapshot.RepositoryByName(repository)
	if !ok {
		return 0, 0, NewToolError(CodeRepositoryNotFound, fmt.Sprintf("unknown repository %q", repository))
	}
	pathID, known := snapshot.Strings().Lookup(path)
	id, ok := snapshot.FileByRepoPath(hotsnapshot.RepoPathKey{Repository: repoID, Path: pathID})
	if !known || !ok {
		return 0, 0, NewToolError(CodeSymbolNotFound, fmt.Sprintf("no indexed file at %s:%s", repository, path))
	}
	return repoID, id, nil
}

// candidatesOf returns the symbols of one file. The first call scans the symbol
// table once for every file the batch names that is still unscanned. Names that
// do not resolve are left out; their own selector reports the error. A file the
// batch did not name (an evidence preview source has no requests) costs one
// further scan for itself, then stays cached.
func (files *positionFiles) candidatesOf(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, id hotsnapshot.FileID) ([]callableCandidate, error) {
	if found, ok := files.candidates[id]; ok {
		return found, nil
	}
	wanted := map[hotsnapshot.FileID][]callableCandidate{id: nil}
	if files.candidates == nil {
		publication := publicationToken(snapshot.Metadata())
		for i, request := range files.requests {
			if request.Line == nil || request.Publication != publication {
				continue
			}
			if _, fileID, err := locatePositionFile(snapshot, request.Repository, files.selectors[i].Path); err == nil {
				wanted[fileID] = nil
			}
		}
	}
	table := snapshot.Strings()
	err := snapshot.VisitSymbols(ctx, 0, hotsnapshot.SymbolID(snapshot.Metadata().Counts.Symbols), func(symbolID hotsnapshot.SymbolID, s hotsnapshot.SymbolRecord) error {
		if _, ok := wanted[s.File]; !ok || s.StartLine == 0 || s.EndLine < s.StartLine {
			return nil
		}
		kind, _ := table.String(s.Kind)
		wanted[s.File] = append(wanted[s.File], callableCandidate{symbolID, s.Name, kind, s.StartLine, s.EndLine})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if files.candidates == nil {
		files.candidates = wanted
	} else {
		files.candidates[id] = wanted[id]
	}
	return wanted[id], nil
}

type bindingRange struct {
	name       string
	start, end uint32
}

// The TS checker records arrow bindings as variables. Only match a direct
// function initializer against the indexed declaration's name and line range.
func functionBindings(ctx context.Context, lang syntax.Language, lines []string) (map[bindingRange]bool, error) {
	parser, err := syntax.NewParserManager(1)
	if err != nil {
		return nil, err
	}
	defer parser.Close()
	data := []byte(strings.Join(lines, "\n"))
	tree, err := parser.Parse(ctx, lang, data)
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	root, err := tree.RootNode()
	if err != nil {
		return nil, err
	}
	cursor := root.Walk()
	defer cursor.Close()
	result := make(map[bindingRange]bool)
	for {
		n := cursor.Node()
		if n.Kind() == "variable_declarator" && !n.HasError() {
			name, value := n.ChildByFieldName("name"), n.ChildByFieldName("value")
		unwrap:
			for value != nil {
				switch value.Kind() {
				case "parenthesized_expression", "satisfies_expression", "as_expression", "non_null_expression", "instantiation_expression":
					value = value.NamedChild(0)
				case "type_assertion":
					// In <Type>expr the type precedes the asserted expression.
					value = value.NamedChild(value.NamedChildCount() - 1)
				default:
					break unwrap
				}
			}
			if name != nil && name.Kind() == "identifier" && value != nil {
				switch value.Kind() {
				case "arrow_function", "function_expression", "generator_function":
					result[bindingRange{name.Utf8Text(data), uint32(n.StartPosition().Row + 1), uint32(n.EndPosition().Row + 1)}] = true
				}
			}
		}
		if cursor.GotoFirstChild() {
			continue
		}
		for !cursor.GotoNextSibling() {
			if !cursor.GotoParent() {
				return result, nil
			}
		}
	}
}
