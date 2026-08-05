package goloader

import (
	"context"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"
)

// SelectionKind classifies a use that came through a selector expression.
type SelectionKind string

const (
	// SelectionNone marks a use that is not a selector.
	SelectionNone SelectionKind = ""
	// SelectionField is x.f where f is a struct field.
	SelectionField SelectionKind = "field"
	// SelectionMethodValue is x.M, a method bound to a receiver value.
	SelectionMethodValue SelectionKind = "method_value"
	// SelectionMethodExpression is T.M, a method used as a function value.
	SelectionMethodExpression SelectionKind = "method_expression"
)

// Use is one resolved occurrence of a symbol.
//
// A use records what the type checker resolved, not what the source spells:
// the target is the object itself, so a homonym in another package can never
// be mistaken for it.
type Use struct {
	Repository  string
	ModulePath  string
	PackagePath string
	FileName    string
	Name        string

	// SourceQualifiedName is the enclosing declaration, empty at file level.
	SourceQualifiedName string
	// SourceKind is the kind of the enclosing declaration.
	SourceKind DefinitionKind

	// TargetModulePath is empty for the standard library.
	TargetModulePath    string
	TargetPackagePath   string
	TargetQualifiedName string
	TargetKind          DefinitionKind
	// TargetIsLocalPackage reports a target defined in a root package of the
	// same load.
	TargetIsLocalPackage bool

	// Selection describes the selector this use came from, when any.
	Selection SelectionKind
	// ReceiverType is the receiver of a selection, package qualified.
	ReceiverType string
	// IndirectReceiver reports a selection that dereferences a pointer or
	// goes through an embedded field.
	IndirectReceiver bool

	Offset      int
	EndOffset   int
	StartLine   int
	StartColumn int

	object types.Object
}

// Object returns the go/types object the use resolved to.
func (use Use) Object() types.Object {
	return use.object
}

// UseOptions tunes extraction.
type UseOptions struct {
	// Repository is recorded on every use.
	Repository string
}

// ExtractUses collects every resolved occurrence of an indexable symbol.
//
// Only occurrences inside the root packages are reported; their targets may
// belong to any loaded package, which is what makes a cross-module reference
// exact. Local variables, parameters and package names are not targets: they
// are not symbols of the graph.
func ExtractUses(ctx context.Context, result Result, options UseOptions) ([]Use, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	modules := modulesByPackagePath(result.Packages)
	local := make(map[string]struct{}, len(result.Packages))
	for _, loaded := range result.Packages {
		local[loaded.PkgPath] = struct{}{}
	}

	uses := make([]Use, 0)
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
			uses = append(uses, extractFileUses(result.Fset, loaded, file, modulePath, modules, local, options)...)
		}
	}
	sort.Slice(uses, func(left, right int) bool {
		if uses[left].PackagePath != uses[right].PackagePath {
			return uses[left].PackagePath < uses[right].PackagePath
		}
		if uses[left].FileName != uses[right].FileName {
			return uses[left].FileName < uses[right].FileName
		}
		return uses[left].Offset < uses[right].Offset
	})
	return uses, nil
}

func extractFileUses(
	fset *token.FileSet,
	loaded *packages.Package,
	file *ast.File,
	modulePath string,
	modules map[string]string,
	localPackages map[string]struct{},
	options UseOptions,
) []Use {
	qualifier := packageQualifier(loaded.Types)
	selections := make(map[*ast.Ident]*types.Selection)
	ast.Inspect(file, func(node ast.Node) bool {
		selector, isSelector := node.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		if selection := loaded.TypesInfo.Selections[selector]; selection != nil {
			selections[selector.Sel] = selection
		}
		return true
	})

	uses := make([]Use, 0)
	stack := make([]declarationContext, 0, 8)
	ast.Inspect(file, func(node ast.Node) bool {
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
		object := loaded.TypesInfo.Uses[identifier]
		if object == nil {
			return true
		}
		kind, indexable := classifyObject(object)
		if !indexable {
			return true
		}
		// Universe objects such as int or error belong to no repository.
		if object.Pkg() == nil {
			return true
		}
		targetPackage := object.Pkg().Path()
		selection := selections[identifier]
		position := fset.Position(identifier.Pos())
		source, sourceKind := enclosingDeclaration(loaded, stack)
		use := Use{
			Repository:          options.Repository,
			ModulePath:          modulePath,
			PackagePath:         loaded.PkgPath,
			FileName:            position.Filename,
			Name:                object.Name(),
			SourceQualifiedName: source,
			SourceKind:          sourceKind,
			TargetModulePath:    modules[targetPackage],
			TargetPackagePath:   targetPackage,
			TargetQualifiedName: useTargetQualifiedName(object, selection),
			TargetKind:          kind,
			Offset:              position.Offset,
			EndOffset:           fset.Position(identifier.End()).Offset,
			StartLine:           position.Line,
			StartColumn:         position.Column,
			object:              object,
		}
		if _, isLocal := localPackages[targetPackage]; isLocal {
			use.TargetIsLocalPackage = true
		}
		if selection != nil {
			use.Selection = selectionKind(selection.Kind())
			use.ReceiverType = types.TypeString(selection.Recv(), qualifier)
			use.IndirectReceiver = selection.Indirect()
		}
		uses = append(uses, use)
		return true
	})
	return uses
}

func selectionKind(kind types.SelectionKind) SelectionKind {
	switch kind {
	case types.FieldVal:
		return SelectionField
	case types.MethodVal:
		return SelectionMethodValue
	case types.MethodExpr:
		return SelectionMethodExpression
	default:
		return SelectionNone
	}
}

// useTargetQualifiedName names the target as Owner.Name for members.
//
// A field object does not know the type that declares it, so the owner comes
// from the selection the checker resolved, never from the spelling.
func useTargetQualifiedName(object types.Object, selection *types.Selection) string {
	switch typed := object.(type) {
	case *types.Func:
		if receiver := typed.Signature().Recv(); receiver != nil {
			if owner := receiverTypeName(receiver.Type()); owner != "" {
				return owner + "." + typed.Name()
			}
		}
	case *types.Var:
		if typed.IsField() && selection != nil {
			if owner := receiverTypeName(selection.Recv()); owner != "" {
				return owner + "." + typed.Name()
			}
		}
	}
	return object.Name()
}

// enclosingDeclaration returns the declaration that contains a use.
func enclosingDeclaration(loaded *packages.Package, stack []declarationContext) (string, DefinitionKind) {
	for index := len(stack) - 1; index >= 0; index-- {
		switch declaration := stack[index].node.(type) {
		case *ast.FuncDecl:
			object := loaded.TypesInfo.Defs[declaration.Name]
			if object == nil {
				return declaration.Name.Name, KindFunc
			}
			kind, _ := classifyObject(object)
			owner := ""
			if function, isFunction := object.(*types.Func); isFunction {
				if receiver := function.Signature().Recv(); receiver != nil {
					owner = receiverTypeName(receiver.Type())
				}
			}
			return qualifiedName(owner, declaration.Name.Name), kind
		case *ast.TypeSpec:
			return declaration.Name.Name, KindType
		case *ast.ValueSpec:
			if len(declaration.Names) > 0 {
				object := loaded.TypesInfo.Defs[declaration.Names[0]]
				kind := KindVar
				if object != nil {
					if classified, indexable := classifyObject(object); indexable {
						kind = classified
					}
				}
				return declaration.Names[0].Name, kind
			}
		}
	}
	return "", ""
}

// modulesByPackagePath maps every loaded package to the module that owns it,
// including dependencies, so a cross-module target keeps its module identity.
func modulesByPackagePath(roots []*packages.Package) map[string]string {
	modules := make(map[string]string)
	packages.Visit(roots, nil, func(loaded *packages.Package) {
		if loaded.Module == nil {
			return
		}
		path := loaded.Module.Path
		if loaded.Module.Replace != nil && loaded.Module.Replace.Path != "" {
			path = loaded.Module.Replace.Path
		}
		modules[loaded.PkgPath] = path
	})
	return modules
}
