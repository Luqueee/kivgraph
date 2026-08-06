package indexer

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"

	"github.com/Luqueee/ladygraph/internal/facts"
)

// GoChange contains the signals produced by Go loading and package/module
// discovery for one transition. The signals are intentionally explicit: this
// package does not infer semantic identity from names or text matches.
type GoChange struct {
	RepositoryKey string
	PackageKey    string
	FileKey       string
	Path          string

	BodyChanged      bool
	SignatureChanged bool
	ImportsChanged   bool
	GoModChanged     bool
	ReplaceChanged   bool
	FileDeleted      bool
	PackageDeleted   bool
}

// ClassifyGoChange maps authoritative Go loader signals to invalidation scope.
// More disruptive signals take precedence over narrower source changes.
func ClassifyGoChange(change GoChange) InvalidationPlan {
	plan := newPlan(facts.LanguageGo, change.RepositoryKey, change.PackageKey, change.FileKey, change.Path)
	switch {
	case change.PackageDeleted:
		plan.Class = ChangePackageDeleted
		addActions(&plan, ActionRemovePackage, ActionRemoveFile, ActionInvalidateConsumers, ActionResolveReferences)
	case change.ReplaceChanged:
		plan.Class = ChangeReplaceChanged
		addActions(&plan, ActionRebuildRegistry, ActionInvalidateModuleResolution, ActionReindexProject)
	case change.GoModChanged:
		plan.Class = ChangeGoModChanged
		addActions(&plan, ActionRebuildRegistry, ActionInvalidateModuleResolution, ActionReindexProject)
	case change.FileDeleted:
		plan.Class = ChangeFileDeleted
		addActions(&plan, ActionRemoveFile, ActionInvalidateConsumers, ActionResolveReferences)
	case change.ImportsChanged:
		plan.Class = ChangeImportsChanged
		addActions(&plan, ActionReindexPackage, ActionInvalidateModuleResolution, ActionResolveReferences)
	case change.SignatureChanged:
		plan.Class = ChangeSignatureChanged
		addActions(&plan, ActionReindexProvider, ActionInvalidateConsumers, ActionResolveReferences)
	case change.BodyChanged:
		plan.Class = ChangeBodyOnly
		addActions(&plan, ActionReindexFile)
	default:
		plan.Class = ChangeUnknown
		addActions(&plan, ActionReindexProject)
	}
	return plan
}

// GoSourceChange supplies the bytes needed to classify a Go source transition
// when the loader has not already emitted explicit change signals.
type GoSourceChange struct {
	GoChange
	Previous []byte
	Current  []byte
}

// ClassifyGoSourceChange compares imports and declaration signatures using the
// Go parser. A parse failure is returned rather than silently downgraded to a
// body change; the returned plan is conservative and requests a project scan.
func ClassifyGoSourceChange(change GoSourceChange) (InvalidationPlan, error) {
	if change.PackageDeleted || change.FileDeleted || change.ReplaceChanged || change.GoModChanged || change.ImportsChanged || change.SignatureChanged || change.BodyChanged {
		return ClassifyGoChange(change.GoChange), nil
	}
	previous, previousSet, err := parseGoSource(change.Previous)
	if err != nil {
		plan := ClassifyGoChange(GoChange{
			RepositoryKey: change.RepositoryKey,
			PackageKey:    change.PackageKey,
			FileKey:       change.FileKey,
			Path:          change.Path,
		})
		return plan, fmt.Errorf("parse previous Go source: %w", err)
	}
	current, currentSet, err := parseGoSource(change.Current)
	if err != nil {
		plan := ClassifyGoChange(GoChange{
			RepositoryKey: change.RepositoryKey,
			PackageKey:    change.PackageKey,
			FileKey:       change.FileKey,
			Path:          change.Path,
		})
		return plan, fmt.Errorf("parse current Go source: %w", err)
	}

	change.ImportsChanged = !equalStrings(goImports(previous), goImports(current))
	previousDeclarations, err := goDeclarations(previousSet, previous)
	if err != nil {
		return ClassifyGoChange(change.GoChange), fmt.Errorf("fingerprint previous Go declarations: %w", err)
	}
	currentDeclarations, err := goDeclarations(currentSet, current)
	if err != nil {
		return ClassifyGoChange(change.GoChange), fmt.Errorf("fingerprint current Go declarations: %w", err)
	}
	change.SignatureChanged = !equalStringMaps(previousDeclarations, currentDeclarations)
	change.BodyChanged = !bytes.Equal(change.Previous, change.Current)
	return ClassifyGoChange(change.GoChange), nil
}

// ClassifyGoModChange classifies a go.mod transition and distinguishes a
// replace change from ordinary module metadata changes.
func ClassifyGoModChange(change GoChange, previous, current []byte) (InvalidationPlan, error) {
	if bytes.Equal(previous, current) {
		return ClassifyGoChange(change), nil
	}
	before, err := modfile.Parse("go.mod", previous, nil)
	if err != nil {
		return ClassifyGoChange(GoChange{
			RepositoryKey: change.RepositoryKey,
			PackageKey:    change.PackageKey,
			FileKey:       change.FileKey,
			Path:          change.Path,
			GoModChanged:  true,
		}), fmt.Errorf("parse previous go.mod: %w", err)
	}
	after, err := modfile.Parse("go.mod", current, nil)
	if err != nil {
		return ClassifyGoChange(GoChange{
			RepositoryKey: change.RepositoryKey,
			PackageKey:    change.PackageKey,
			FileKey:       change.FileKey,
			GoModChanged:  true,
		}), fmt.Errorf("parse current go.mod: %w", err)
	}
	change.GoModChanged = true
	change.ReplaceChanged = !equalStrings(moduleReplacements(before), moduleReplacements(after))
	return ClassifyGoChange(change), nil
}

func parseGoSource(source []byte) (*ast.File, *token.FileSet, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "source.go", source, parser.ParseComments)
	if err != nil {
		return nil, nil, err
	}
	return file, fileSet, nil
}

func goImports(file *ast.File) []string {
	imports := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		path := spec.Path.Value
		if unquoted, err := strconv.Unquote(path); err == nil {
			path = unquoted
		}
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports = append(imports, name+"\x00"+path)
	}
	sort.Strings(imports)
	return imports
}

func goDeclarations(fileSet *token.FileSet, file *ast.File) (map[string]string, error) {
	declarations := make(map[string]string)
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			receiver, err := formatGoNode(fileSet, declaration.Recv)
			if err != nil {
				return nil, err
			}
			signature, err := formatGoNode(fileSet, declaration.Type)
			if err != nil {
				return nil, err
			}
			key := "func\x00" + receiver + "\x00" + declaration.Name.Name
			declarations[key] = signature
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					typeText, err := formatGoNode(fileSet, spec.Type)
					if err != nil {
						return nil, err
					}
					key := declaration.Tok.String() + "\x00" + spec.Name.Name
					declarations[key] = typeText
				case *ast.ValueSpec:
					typeText, err := formatGoNode(fileSet, spec.Type)
					if err != nil {
						return nil, err
					}
					for _, name := range spec.Names {
						key := declaration.Tok.String() + "\x00" + name.Name
						declarations[key] = typeText
					}
				}
			}
		}
	}
	return declarations, nil
}

func formatGoNode(fileSet *token.FileSet, node ast.Node) (string, error) {
	if node == nil {
		return "", nil
	}
	if fields, ok := node.(*ast.FieldList); ok {
		node = &ast.FuncType{Params: fields}
	}
	var output bytes.Buffer
	if err := format.Node(&output, fileSet, node); err != nil {
		return "", err
	}
	return strings.Join(strings.Fields(output.String()), " "), nil
}

func moduleReplacements(file *modfile.File) []string {
	replacements := make([]string, 0, len(file.Replace))
	for _, replacement := range file.Replace {
		replacements = append(replacements, strings.Join([]string{
			replacement.Old.Path,
			replacement.Old.Version,
			replacement.New.Path,
			replacement.New.Version,
		}, "\x00"))
	}
	sort.Strings(replacements)
	return replacements
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalStringMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
