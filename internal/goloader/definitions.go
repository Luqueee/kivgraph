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
		if kind == KindField && fieldOfUnnamedLiteral(stack) {
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
		if signature := typed.Signature(); signature != nil && signature.Recv() != nil {
			if !addressableReceiver(signature.Recv().Type()) {
				return "", false
			}
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
	if kind == KindField {
		if owner := fieldOwner(stack); owner != "" {
			return owner
		}
		return localContainer(stack)
	}
	for index := len(stack) - 1; index >= 0; index-- {
		if stack[index].owner != "" {
			return stack[index].owner
		}
	}
	return ""
}

// fieldOwner names the path from a named type down to a field, through the
// anonymous structs in between: the `Release` of `bundleManifest.Tools
// .RustAnalyzer` is not the `Release` of `bundleManifest`.
//
// Taking only the nearest named type made both of them `bundleManifest.Release`,
// one identity for two declarations, which the DEFINES multiplicity constraint
// rejects at publish time. The path is built from names alone, so moving a
// declaration inside its struct does not change it, and a nested named type
// restarts the path because it is addressable on its own.
//
// The path must be *rooted* at a named type, and an unrooted one is no answer at
// all. `var env struct{ Errors []struct{ Message string } }` inside a function
// has intermediate field names but no type declaration above them, so this used
// to return `Errors` -- non-empty, so the caller took it and never asked
// localContainer for the function and variable that actually separate it. Every
// file in a package that unmarshals into that shape then declared one
// `Errors.Message`, and indexing failed at publish time: measured on a real
// corpus, two test files of one package were enough.
func fieldOwner(stack []declarationContext) string {
	parts := make([]string, 0, 4)
	rooted := false
	innermost := -1
	for index, entry := range stack {
		if _, isField := entry.node.(*ast.Field); isField {
			innermost = index
		}
	}
	for index, entry := range stack {
		switch node := entry.node.(type) {
		case *ast.TypeSpec:
			parts = append(parts[:0], node.Name.Name)
			rooted = true
		case *ast.Field:
			if index != innermost && len(node.Names) != 0 {
				parts = append(parts, node.Names[0].Name)
			}
		}
	}
	if !rooted {
		return ""
	}
	return strings.Join(parts, ".")
}

// localContainer names the container of a field declared in an anonymous
// struct, which go/types cannot address from the package scope: the enclosing
// function (receiver qualified) followed by the named holders that reach the
// struct, for example `ParseTicketsCreate.raw`.
//
// Without it every `struct { GuildID string }` written inside any function of
// a package shares one identity, and the canonical graph ends up claiming one
// symbol is declared by several files. Names, not positions: inserting a
// statement above the declaration must not change the identity.
func localContainer(stack []declarationContext) string {
	parts := make([]string, 0, 4)
	for _, entry := range stack {
		switch node := entry.node.(type) {
		case *ast.FuncDecl:
			name := node.Name.Name
			if node.Recv != nil && len(node.Recv.List) != 0 {
				if receiver := receiverTypeExpr(node.Recv.List[0].Type); receiver != "" {
					name = receiver + "." + name
				}
			}
			parts = append(parts, name)
		case *ast.FuncLit:
			parts = append(parts, "func")
		case *ast.ValueSpec:
			if len(node.Names) != 0 {
				parts = append(parts, node.Names[0].Name)
			}
		case *ast.Field:
			if len(node.Names) != 0 {
				parts = append(parts, node.Names[0].Name)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	// The field's own name is the last Field on the stack; it is added by
	// qualifiedName, not by the container.
	return strings.Join(parts[:len(parts)-1], ".")
}

// fieldOfUnnamedLiteral reports whether a field belongs to an anonymous struct
// written inline inside a function, with no name on the way down to it: the
// `struct{ Files []group }` a method marshals itself into, or the
// `struct{ path, root string }` appended straight to a slice.
//
// Such a field is not addressable and, worse, not distinguishable: its identity
// falls back to the names of its containers, and there are none between the
// function and the field. Two sibling literals in one function then derive one
// key -- as do an embedded field, which contributes no name at all -- and one
// Symbol with two declaring Files is what the DEFINES multiplicity constraint
// forbids, failing at publish time as a node offset far from the declarations.
// They are the same class as the methods of an unnamed interface: not
// addressable, so never in the graph.
//
// A local struct bound to a name keeps its field: `var raw struct{ GuildID
// string }` inside `ParseFirst` is `ParseFirst.raw.GuildID`, which separates it
// from every other one.
func fieldOfUnnamedLiteral(stack []declarationContext) bool {
	function := -1
	for index, entry := range stack {
		switch entry.node.(type) {
		case *ast.TypeSpec:
			// A named type restarts the path, so the field is addressable
			// through it however deep the anonymous structs go.
			function = -1
		case *ast.FuncDecl, *ast.FuncLit:
			function = index
		}
	}
	if function == -1 {
		return false
	}
	innermost := -1
	for index, entry := range stack {
		if _, isField := entry.node.(*ast.Field); isField {
			innermost = index
		}
	}
	for index := function + 1; index < len(stack); index++ {
		switch node := stack[index].node.(type) {
		case *ast.ValueSpec:
			if len(node.Names) != 0 {
				return false
			}
		case *ast.Field:
			if index != innermost && len(node.Names) != 0 {
				return false
			}
		}
	}
	return true
}

// receiverTypeExpr names the receiver type of a method declaration, ignoring
// pointers and type parameters.
func receiverTypeExpr(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.StarExpr:
		return receiverTypeExpr(typed.X)
	case *ast.IndexExpr:
		return receiverTypeExpr(typed.X)
	case *ast.IndexListExpr:
		return receiverTypeExpr(typed.X)
	case *ast.Ident:
		return typed.Name
	default:
		return ""
	}
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

// addressableReceiver reports whether a method's receiver has a name a
// consumer could write.
//
// A method of an unnamed interface -- the assertion `x.(interface{ M() })`,
// or the anonymous interface of a `var _ interface{ ... } = ...` compliance
// check -- is unreachable: no path leads to it from the package scope, so it
// gets no object path, and its identity falls back to the qualified name,
// which for an unnamed owner is the bare method name. Two such declarations
// in one package then derive one key while sitting in two files, and a
// Symbol with two declaring Files is what the DEFINES multiplicity
// constraint forbids. They are Go's counterpart to Rust's `local N`: not
// addressable, so never in the graph.
func addressableReceiver(receiver types.Type) bool {
	return receiverTypeName(receiver) != ""
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
