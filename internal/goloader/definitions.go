package goloader

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// DefinitionKind classifies one Go declaration.
type DefinitionKind string

const (
	// KindFunc is a package-level function.
	KindFunc DefinitionKind = "func"
	// KindMethod is a method of a named type or an interface.
	KindMethod DefinitionKind = "method"
	// KindType is a defined type.
	KindType DefinitionKind = "type"
	// KindAlias is a type alias.
	KindAlias DefinitionKind = "alias"
	// KindConst is a package-level constant.
	KindConst DefinitionKind = "const"
	// KindVar is a package-level variable.
	KindVar DefinitionKind = "var"
	// KindField is a struct field.
	KindField DefinitionKind = "field"
)

// Definition is one declaration backed by a go/types object.
type Definition struct {
	Repository  string
	ModulePath  string
	PackagePath string
	PackageName string
	FileName    string
	Name        string
	// QualifiedName is Owner.Name for methods and fields, Name otherwise.
	QualifiedName string
	Kind          DefinitionKind
	// Owner is the named type a method or field belongs to.
	Owner    string
	Exported bool
	// Signature is the type of the object with package-qualified names.
	Signature string
	// Receiver is the receiver type of a method, package qualified.
	Receiver string
	// Offsets and positions of the declared name, one based for lines.
	NameOffset  int
	StartLine   int
	StartColumn int
	// Declaration span of the whole syntax node that introduces the name.
	DeclarationStartOffset int
	DeclarationEndOffset   int
	EndLine                int

	object types.Object
}

// Object returns the go/types object of this definition.
//
// The object belongs to the type universe of the load that produced it and
// must not be compared across loads.
func (definition Definition) Object() types.Object {
	return definition.object
}

// DefinitionOptions tunes extraction.
type DefinitionOptions struct {
	// Repository is recorded on every definition. It is metadata, not a
	// resolution input.
	Repository string
	// IncludeUnexported keeps unexported declarations. Default is to keep
	// them: they are real symbols of the repository.
	ExcludeUnexported bool
}

// ExtractDefinitions collects declarations from the roots of one load.
//
// Only the root packages are traversed: their dependencies are loaded to give
// types their identity, not to be indexed here. Function-local variables,
// parameters and labels are deliberately omitted; they are not addressable
// symbols of the graph.
func ExtractDefinitions(ctx context.Context, result Result, options DefinitionOptions) ([]Definition, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	definitions := make([]Definition, 0)
	for _, loaded := range result.Packages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if loaded.TypesInfo == nil || loaded.Types == nil {
			continue
		}
		modulePath := ""
		if loaded.Module != nil {
			modulePath = loaded.Module.Path
		}
		for _, file := range loaded.Syntax {
			definitions = append(definitions, extractFile(result.Fset, loaded, file, modulePath, options)...)
		}
	}
	sort.Slice(definitions, func(left, right int) bool {
		if definitions[left].PackagePath != definitions[right].PackagePath {
			return definitions[left].PackagePath < definitions[right].PackagePath
		}
		if definitions[left].FileName != definitions[right].FileName {
			return definitions[left].FileName < definitions[right].FileName
		}
		return definitions[left].NameOffset < definitions[right].NameOffset
	})
	return definitions, nil
}

type declarationContext struct {
	owner string
	node  ast.Node
}

func extractFile(
	fset *token.FileSet,
	loaded *packages.Package,
	file *ast.File,
	modulePath string,
	options DefinitionOptions,
) []Definition {
	definitions := make([]Definition, 0)
	qualifier := packageQualifier(loaded.Types)
	stack := make([]declarationContext, 0, 8)

	visit := func(node ast.Node) bool {
		if node == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		current := declarationContext{node: node}
		if len(stack) > 0 {
			current.owner = stack[len(stack)-1].owner
		}
		if specification, isType := node.(*ast.TypeSpec); isType {
			current.owner = specification.Name.Name
		}
		if _, isFunction := node.(*ast.FuncDecl); isFunction {
			current.owner = ""
		}
		stack = append(stack, current)

		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier {
			return true
		}
		object := loaded.TypesInfo.Defs[identifier]
		if object == nil || identifier.Name == "_" {
			return true
		}
		kind, keep := classifyObject(object)
		if !keep {
			return true
		}
		if options.ExcludeUnexported && !object.Exported() {
			return true
		}
		owner := ownerFor(kind, object, stack)
		definition := Definition{
			Repository:    options.Repository,
			ModulePath:    modulePath,
			PackagePath:   loaded.PkgPath,
			PackageName:   loaded.Name,
			FileName:      fset.Position(identifier.Pos()).Filename,
			Name:          object.Name(),
			QualifiedName: qualifiedName(owner, object.Name()),
			Kind:          kind,
			Owner:         owner,
			Exported:      object.Exported(),
			Signature:     types.TypeString(object.Type(), qualifier),
			object:        object,
		}
		if function, isFunction := object.(*types.Func); isFunction {
			if receiver := function.Signature().Recv(); receiver != nil {
				definition.Receiver = types.TypeString(receiver.Type(), qualifier)
			}
		}
		namePosition := fset.Position(identifier.Pos())
		definition.NameOffset = namePosition.Offset
		definition.StartLine = namePosition.Line
		definition.StartColumn = namePosition.Column
		declaration := declarationNode(stack)
		start := fset.Position(declaration.Pos())
		end := fset.Position(declaration.End())
		definition.DeclarationStartOffset = start.Offset
		definition.DeclarationEndOffset = end.Offset
		definition.EndLine = end.Line
		definitions = append(definitions, definition)
		return true
	}

	ast.Inspect(file, visit)
	return definitions
}

// classifyObject reports the kind of an object and whether it is an
// addressable symbol of the graph.
func classifyObject(object types.Object) (DefinitionKind, bool) {
	switch typed := object.(type) {
	case *types.Func:
		if typed.Signature() != nil && typed.Signature().Recv() != nil {
			return KindMethod, true
		}
		if typed.Parent() != nil && typed.Parent() != typed.Pkg().Scope() {
			return "", false
		}
		return KindFunc, true
	case *types.TypeName:
		if typed.Parent() != nil && typed.Parent() != typed.Pkg().Scope() {
			return "", false
		}
		if typed.IsAlias() {
			return KindAlias, true
		}
		return KindType, true
	case *types.Const:
		if typed.Parent() != typed.Pkg().Scope() {
			return "", false
		}
		return KindConst, true
	case *types.Var:
		if typed.IsField() {
			return KindField, true
		}
		if typed.Parent() != typed.Pkg().Scope() {
			return "", false
		}
		return KindVar, true
	default:
		return "", false
	}
}

func ownerFor(kind DefinitionKind, object types.Object, stack []declarationContext) string {
	if kind == KindMethod {
		if function, isFunction := object.(*types.Func); isFunction {
			if receiver := function.Signature().Recv(); receiver != nil {
				if name := receiverTypeName(receiver.Type()); name != "" {
					return name
				}
			}
		}
	}
	if kind != KindMethod && kind != KindField {
		return ""
	}
	for index := len(stack) - 1; index >= 0; index-- {
		if stack[index].owner != "" {
			return stack[index].owner
		}
	}
	return ""
}

func receiverTypeName(typ types.Type) string {
	switch typed := typ.(type) {
	case *types.Pointer:
		return receiverTypeName(typed.Elem())
	case *types.Named:
		return typed.Obj().Name()
	case *types.Alias:
		return typed.Obj().Name()
	default:
		return ""
	}
}

func qualifiedName(owner, name string) string {
	if owner == "" {
		return name
	}
	return owner + "." + name
}

// packageQualifier prints every named type by its package path, including the
// current package.
//
// The signature feeds the stable key discriminator, so it must not depend on
// who is looking: the provider printing `Shape` and a consumer printing
// `example.com/provider/api.Shape` for the same object would produce two keys
// for one symbol and leave every cross-repository edge dangling.
func packageQualifier(_ *types.Package) types.Qualifier {
	return func(other *types.Package) string {
		if other == nil {
			return ""
		}
		return other.Path()
	}
}

// PackageDefinitions groups definitions by package path, preserving order.
func PackageDefinitions(definitions []Definition) map[string][]Definition {
	grouped := make(map[string][]Definition)
	for _, definition := range definitions {
		grouped[definition.PackagePath] = append(grouped[definition.PackagePath], definition)
	}
	return grouped
}

// QualifiedNames returns the qualified names of definitions, for diagnostics.
func QualifiedNames(definitions []Definition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, strings.TrimSpace(definition.QualifiedName))
	}
	return names
}

func declarationNode(stack []declarationContext) ast.Node {
	if len(stack) == 0 {
		return nil
	}
	for index := len(stack) - 1; index >= 0; index-- {
		switch stack[index].node.(type) {
		case *ast.FuncDecl, *ast.TypeSpec, *ast.ValueSpec, *ast.Field:
			return stack[index].node
		}
	}
	return stack[len(stack)-1].node
}
