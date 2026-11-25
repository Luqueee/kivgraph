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

type positionFiles map[hotsnapshot.FileID][]callableRange

func (files *positionFiles) resolve(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot, request SourceRequest, path string, cache map[string]*sourceFile) (hotsnapshot.SymbolID, error) {
	if *request.SnapshotID != snapshot.Metadata().ID {
		return 0, NewToolError(CodeSnapshotUnavailable, "snapshot_id changed; repeat the reference query")
	}
	repoID, ok := snapshot.RepositoryByName(request.Repository)
	if !ok {
		return 0, NewToolError(CodeRepositoryNotFound, fmt.Sprintf("unknown repository %q", request.Repository))
	}
	table := snapshot.Strings()
	pathID, known := table.Lookup(path)
	id, ok := snapshot.FileByRepoPath(hotsnapshot.RepoPathKey{Repository: repoID, Path: pathID})
	if !known || !ok {
		return 0, NewToolError(CodeSymbolNotFound, fmt.Sprintf("no indexed file at %s:%s", request.Repository, path))
	}
	ranges, ready := (*files)[id]
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
		var bindings map[bindingRange]bool
		err := snapshot.VisitSymbols(ctx, 0, hotsnapshot.SymbolID(snapshot.Metadata().Counts.Symbols), func(symbolID hotsnapshot.SymbolID, s hotsnapshot.SymbolRecord) error {
			if s.File != id || s.StartLine == 0 || s.EndLine < s.StartLine {
				return nil
			}
			kind, _ := table.String(s.Kind)
			switch kind {
			case "func", "function", "method", "constructor":
			case "variable", "constant":
				if language != "typescript" && language != "javascript" {
					return nil
				}
				if bindings == nil {
					lang := syntax.Language(language)
					if filepath.Ext(path) == ".tsx" {
						lang = syntax.LanguageTSX
					}
					var err error
					bindings, err = functionBindings(ctx, lang, source.lines)
					if err != nil {
						return err
					}
				}
				name, _ := table.String(s.Name)
				if !bindings[bindingRange{name, s.StartLine, s.EndLine}] {
					return nil
				}
			default:
				return nil
			}
			if uint64(s.EndLine) > uint64(len(source.lines)) {
				return fmt.Errorf("indexed callable at %s:%d-%d exceeds the verified file", path, s.StartLine, s.EndLine)
			}
			ranges = append(ranges, callableRange{symbolID, s.StartLine, s.EndLine})
			return nil
		})
		if err != nil {
			return 0, WrapToolError(CodeSnapshotUnavailable, "inspect indexed callable ranges", err)
		}
		if *files == nil {
			*files = make(positionFiles)
		}
		(*files)[id] = ranges
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
