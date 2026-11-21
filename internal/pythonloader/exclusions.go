package pythonloader

import (
	"fmt"
	"path/filepath"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// withoutExcluded removes from a payload what the repository's `exclusions`
// say is not part of it.
//
// The producers walk the repository themselves and were never told about the
// registry, so a directory excluded for Go and TypeScript was still indexed for
// Python. It is applied here, to the payload, and not passed to the producer:
// the matcher is one function with its own grammar (`**`, `./`, absolute
// paths), a second implementation in a script would be a second rule, and an
// external producer would not know the flag.
//
// A reference from a file that stays to a declaration that goes is not dropped
// with it. The source makes that use, so it becomes an unresolved occurrence
// that says the target is outside the index, which is what bounds an answer
// about the name.
func withoutExcluded(payload facts.SemanticPayload, root string, exclusions []string) (facts.SemanticPayload, error) {
	if len(exclusions) == 0 {
		return payload, nil
	}
	decided := map[string]bool{}
	excluded := func(file string) (bool, error) {
		if answer, known := decided[file]; known {
			return answer, nil
		}
		answer, err := workspace.MatchesExclusion(root, filepath.Join(root, filepath.FromSlash(file)), exclusions)
		if err != nil {
			return false, fmt.Errorf("apply exclusions to %q: %w", file, err)
		}
		decided[file] = answer
		return answer, nil
	}

	files := payload.Files[:0:0]
	for _, file := range payload.Files {
		out, err := excluded(file.Path)
		if err != nil {
			return facts.SemanticPayload{}, err
		}
		if !out {
			files = append(files, file)
		}
	}
	gone := map[string]string{}
	symbols := payload.Symbols[:0:0]
	for _, symbol := range payload.Symbols {
		out, err := excluded(symbol.File)
		if err != nil {
			return facts.SemanticPayload{}, err
		}
		if out {
			gone[symbol.ID] = symbol.File
			continue
		}
		symbols = append(symbols, symbol)
	}
	unresolved := payload.Unresolved[:0:0]
	for _, row := range payload.Unresolved {
		out, err := excluded(row.File)
		if err != nil {
			return facts.SemanticPayload{}, err
		}
		if !out {
			unresolved = append(unresolved, row)
		}
	}
	references := payload.References[:0:0]
	for _, reference := range payload.References {
		out, err := excluded(reference.File)
		if err != nil {
			return facts.SemanticPayload{}, err
		}
		if out {
			continue
		}
		if target, removed := gone[reference.TargetID]; removed {
			unresolved = append(unresolved, facts.SemanticUnresolved{
				File: reference.File, SourceID: reference.SourceID, RequestedSymbol: reference.Text,
				Reason: "TARGET_NOT_INDEXED", Detail: target,
				StartLine: reference.StartLine, StartColumn: reference.StartColumn, Start: reference.Start,
			})
			continue
		}
		references = append(references, reference)
	}
	imports := payload.Imports[:0:0]
	for _, imported := range payload.Imports {
		out, err := excluded(imported.File)
		if err != nil {
			return facts.SemanticPayload{}, err
		}
		if out {
			continue
		}
		if _, removed := gone[imported.TargetID]; removed {
			imported.TargetID = ""
		}
		imports = append(imports, imported)
	}
	payload.Files, payload.Symbols, payload.References = files, symbols, references
	payload.Imports, payload.Unresolved = imports, unresolved
	return payload, nil
}
