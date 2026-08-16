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

func resolveSymbolLocation(snapshot *hotsnapshot.GraphSnapshot, symbol hotsnapshot.SymbolRecord) (symbolLocation, error) {
	file, fileOK := snapshot.File(symbol.File)
	if !fileOK {
		return symbolLocation{}, fmt.Errorf("symbol %q references missing file %d", symbol.StableKey, symbol.File)
	}
	return resolveFileLocation(snapshot, file, string(symbol.StableKey))
}

// resolveFileLocation reads the repository and package a file belongs to.
// owner names whatever asked, so a corrupt snapshot reports which row led
// here instead of only which table was short.
func resolveFileLocation(
	snapshot *hotsnapshot.GraphSnapshot,
	file hotsnapshot.FileRecord,
	owner string,
) (symbolLocation, error) {
	pkg, packageOK := snapshot.Package(file.Package)
	if !packageOK {
		return symbolLocation{}, fmt.Errorf("%q references missing package %d", owner, file.Package)
	}
	repository, repositoryOK := snapshot.Repository(file.Repository)
	if !repositoryOK {
		return symbolLocation{}, fmt.Errorf("%q references missing repository %d", owner, file.Repository)
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
			"%q references invalid location strings (repository_key_ok=%t repository_name_ok=%t repository_path_ok=%t package_name_ok=%t module_path_ok=%t file_path_ok=%t)",
			owner, repositoryKeyOK, repositoryNameOK, repositoryPathOK, packageNameOK, modulePathOK, filePathOK,
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
