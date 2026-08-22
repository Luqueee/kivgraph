package tools

import (
	"fmt"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// Response formats. A row carries what an agent can act on -- a name, a path,
// a line. The identifiers it can only hand back to another tool are opt-in:
// they are most of the bytes and none of the meaning.
const (
	ResponseFormatConcise  = "concise"
	ResponseFormatDetailed = "detailed"
)

// normalizeResponseFormat defaults to concise. Detailed restores the derived
// identifiers: the canonical identity, and the `*_key` fields whose value is
// already spelled out by the path standing next to them.
func normalizeResponseFormat(value string) (string, error) {
	switch value {
	case "":
		return ResponseFormatConcise, nil
	case ResponseFormatConcise, ResponseFormatDetailed:
		return value, nil
	default:
		return "", NewToolError(CodeInvalidArgument, fmt.Sprintf(
			"response_format %q is unsupported, use %q or %q",
			value, ResponseFormatConcise, ResponseFormatDetailed,
		))
	}
}

// symbolLocation is where something lives: the repository that holds it and
// the repository-relative path of its file. A result without it forces a
// second call before the agent can open anything, which is the whole cost the
// result was meant to save.
type symbolLocation struct {
	RepositoryKey  string
	RepositoryName string
	RepositoryPath string
	PackageName    string
	ModulePath     string
	FilePath       string
}

// symbolStableKey resolves the key a symbol record names.
//
// A record carries its key as a dense index into the snapshot's key table, and
// an index that does not resolve can only come from an inconsistent snapshot.
// Every caller here wants the key for a row or a message rather than a second
// error to thread, so a miss reads as no key at all.
func symbolStableKey(snapshot *hotsnapshot.GraphSnapshot, symbol hotsnapshot.SymbolRecord) string {
	key, _ := snapshot.StableKey(symbol.StableKey)
	return string(key)
}

func resolveSymbolLocation(snapshot *hotsnapshot.GraphSnapshot, symbol hotsnapshot.SymbolRecord) (symbolLocation, error) {
	file, fileOK := snapshot.File(symbol.File)
	if !fileOK {
		return symbolLocation{}, fmt.Errorf("symbol %q references missing file %d", symbolStableKey(snapshot, symbol), symbol.File)
	}
	location, err := resolveFileLocation(snapshot, file)
	if err != nil {
		return symbolLocation{}, fmt.Errorf("symbol %q: %w", symbolStableKey(snapshot, symbol), err)
	}
	return location, nil
}

// resolveFileLocation reads the repository and package a file belongs to.
//
// Which row led here is named by the caller instead of passed in: resolving a
// symbol's key copies it out of the snapshot's key arena, and this runs once
// per row of every page while the name is only ever read out of a message a
// consistent snapshot never produces.
func resolveFileLocation(
	snapshot *hotsnapshot.GraphSnapshot,
	file hotsnapshot.FileRecord,
) (symbolLocation, error) {
	pkg, packageOK := snapshot.Package(file.Package)
	if !packageOK {
		return symbolLocation{}, fmt.Errorf("file references missing package %d", file.Package)
	}
	repository, repositoryOK := snapshot.Repository(file.Repository)
	if !repositoryOK {
		return symbolLocation{}, fmt.Errorf("file references missing repository %d", file.Repository)
	}

	table := snapshot.Strings()
	repositoryKey, repositoryKeyOK := table.String(repository.Key)
	repositoryName, repositoryNameOK := table.String(repository.Name)
	repositoryPath, repositoryPathOK := table.String(repository.Path)
	packageName, packageNameOK := table.String(pkg.Name)
	modulePath, modulePathOK := table.String(pkg.ModulePath)
	filePath, filePathOK := table.String(file.Path)
	if !repositoryKeyOK || !repositoryNameOK || !repositoryPathOK || !packageNameOK || !modulePathOK || !filePathOK {
		return symbolLocation{}, fmt.Errorf(
			"file references invalid location strings (repository_key_ok=%t repository_name_ok=%t repository_path_ok=%t package_name_ok=%t module_path_ok=%t file_path_ok=%t)",
			repositoryKeyOK, repositoryNameOK, repositoryPathOK, packageNameOK, modulePathOK, filePathOK,
		)
	}

	return symbolLocation{
		RepositoryKey:  repositoryKey,
		RepositoryName: repositoryName,
		RepositoryPath: repositoryPath,
		PackageName:    packageName,
		ModulePath:     modulePath,
		FilePath:       filePath,
	}, nil
}
