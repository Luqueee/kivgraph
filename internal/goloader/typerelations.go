package goloader

import (
	"context"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"
)

// TypeRelationKind classifies one structural relation the type checker
// decides on its own, with no corresponding source occurrence to anchor it:
// interface satisfaction, embedding, or a promoted method hidden by one
// declared directly on the outer type.
type TypeRelationKind string

const (
	// RelationImplements reports that a concrete named type satisfies a
	// named interface, decided by types.Implements alone. IMPLEMENTS never
	// targets the empty interface: it is satisfied by everything and would
	// report nothing about the type.
	RelationImplements TypeRelationKind = "IMPLEMENTS"
	// RelationEmbeds reports an anonymous struct field, or an interface
	// embedding another named interface.
	RelationEmbeds TypeRelationKind = "EMBEDS"
	// RelationOverrides reports a method declared directly on a struct that
	// hides a promoted method of the same name reachable through one of its
	// embedded fields. Go has no virtual dispatch: this is shadowing of a
	// promoted method, never a runtime override.
	RelationOverrides TypeRelationKind = "OVERRIDES"
)

// TypeRelation is one structural relation between two named types or
// methods, resolved entirely by the type checker.
//
// It embeds Use so both ends carry the same raw material ExtractDefinitions
// and AssignStableKeys consume: module path, package path, qualified name,
// kind, and the go/types object itself. A target that lives in another
// repository resolves exactly like a reference does, through
// ResolveCrossRepository over this same Use.
type TypeRelation struct {
	Use
	Kind TypeRelationKind

	// Pointer qualifies IMPLEMENTS and EMBEDS. For IMPLEMENTS it reports
	// that only the pointer type satisfies the interface, the value type
	// does not. For EMBEDS it reports an embedded field of pointer type.
	// OVERRIDES leaves it false; pointerness is not what OVERRIDES decides.
	Pointer bool

	// PromotionDepth is the length of the embedding path OVERRIDES walked,
	// starting at the outer type's own embedded field, to reach the method
	// it hides. Unused by IMPLEMENTS and EMBEDS.
	PromotionDepth int
}

// TypeRelationOptions tunes resolution.
type TypeRelationOptions struct {
	// Repository is recorded on every relation.
	Repository string
}

// ResolveTypeRelations computes every IMPLEMENTS, EMBEDS and OVERRIDES
// relation reachable from the root packages of one load.
//
// Interfaces are gathered from every loaded package, including dependencies:
// a type declared in a root package can implement an interface it never
// imports by name. Concrete types, embedding declarations and overriding
// methods are read only from the root packages, matching ExtractDefinitions:
// a symbol this pass cannot key locally is not a fact this pass can
// originate from.
func ResolveTypeRelations(ctx context.Context, result Result, options TypeRelationOptions) ([]TypeRelation, error) {
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
	interfaces, err := visibleInterfaces(ctx, result)
	if err != nil {
		return nil, err
	}

	relations := make([]TypeRelation, 0)
	for _, loaded := range result.Packages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if loaded.Types == nil {
			continue
		}
		modulePath := ""
		if loaded.Module != nil {
			modulePath = loaded.Module.Path
		}
		builder := relationBuilder{
			result: result, loaded: loaded, modulePath: modulePath,
			modules: modules, local: local, options: options,
		}
		scope := loaded.Types.Scope()
		for _, name := range scope.Names() {
			named, ok := namedTypeAt(scope, name)
			if !ok {
				continue
			}
			relations = append(relations, builder.implements(named, interfaces)...)
			relations = append(relations, builder.embeds(named)...)
			relations = append(relations, builder.overrides(named)...)
		}
	}
	sort.SliceStable(relations, func(left, right int) bool {
		if relations[left].Kind != relations[right].Kind {
			return relations[left].Kind < relations[right].Kind
		}
		if relations[left].SourceQualifiedName != relations[right].SourceQualifiedName {
			return relations[left].SourceQualifiedName < relations[right].SourceQualifiedName
		}
		return relations[left].TargetQualifiedName < relations[right].TargetQualifiedName
	})
	return relations, nil
}

// namedTypeAt reports the non-alias, non-generic named type declared as name
// in scope. Aliases contribute no structural identity of their own, and a
// type with free type parameters is not a type types.Implements accepts.
func namedTypeAt(scope *types.Scope, name string) (*types.Named, bool) {
	typeName, isType := scope.Lookup(name).(*types.TypeName)
	if !isType || typeName.IsAlias() {
		return nil, false
	}
	named, isNamed := typeName.Type().(*types.Named)
	if !isNamed || (named.TypeParams() != nil && named.TypeParams().Len() > 0) {
		return nil, false
	}
	return named, true
}

// relationBuilder carries the load-wide context every relation constructor
// needs, so each one only takes what varies: the type under inspection and,
// for IMPLEMENTS, the interface catalogue.
type relationBuilder struct {
	result     Result
	loaded     *packages.Package
	modulePath string
	modules    map[string]string
	local      map[string]struct{}
	options    TypeRelationOptions
}

// implements reports every named interface named satisfies, by value or by
// pointer. named itself must not be an interface: satisfaction between two
// interfaces is embedding, reported by embeds instead.
func (builder relationBuilder) implements(named *types.Named, interfaces []interfaceCandidate) []TypeRelation {
	if _, isInterface := named.Underlying().(*types.Interface); isInterface {
		return nil
	}
	object := named.Obj()
	pointer := types.NewPointer(named)
	relations := make([]TypeRelation, 0)
	for _, candidate := range interfaces {
		interfaceType := candidate.named.Underlying().(*types.Interface)
		byValue := types.Implements(named, interfaceType)
		if !byValue && !types.Implements(pointer, interfaceType) {
			continue
		}
		relations = append(relations, builder.build(
			RelationImplements, object, candidate.object,
			object.Pos(), len(object.Name()), !byValue, 0,
		))
	}
	return relations
}

// embeds reports the anonymous struct fields of named, and the interfaces a
// named interface embeds.
func (builder relationBuilder) embeds(named *types.Named) []TypeRelation {
	object := named.Obj()
	relations := make([]TypeRelation, 0)
	switch underlying := named.Underlying().(type) {
	case *types.Struct:
		for i := range underlying.NumFields() {
			field := underlying.Field(i)
			if !field.Anonymous() {
				continue
			}
			fieldType := field.Type()
			pointer := false
			if pointerType, isPointer := fieldType.(*types.Pointer); isPointer {
				pointer = true
				fieldType = pointerType.Elem()
			}
			embedded := namedObject(fieldType)
			if embedded == nil {
				continue
			}
			relations = append(relations, builder.build(
				RelationEmbeds, object, embedded,
				field.Pos(), len(field.Name()), pointer, 0,
			))
		}
	case *types.Interface:
		for i := range underlying.NumEmbeddeds() {
			embedded := namedObject(underlying.EmbeddedType(i))
			if embedded == nil {
				continue // a type set element (union, approximation), not a plain embedded interface.
			}
			relations = append(relations, builder.build(
				RelationEmbeds, object, embedded,
				object.Pos(), len(object.Name()), false, 0,
			))
		}
	}
	return relations
}

// overrides reports every method named declares directly that hides a
// method of the same name promotable from one of its embedded fields.
func (builder relationBuilder) overrides(named *types.Named) []TypeRelation {
	structType, isStruct := named.Underlying().(*types.Struct)
	if !isStruct {
		return nil
	}
	relations := make([]TypeRelation, 0)
	for i := range named.NumMethods() {
		method := named.Method(i)
		for f := range structType.NumFields() {
			field := structType.Field(f)
			if !field.Anonymous() {
				continue
			}
			shadowed, depth := promotedMethod(field.Type(), builder.loaded.Types, method.Name())
			if shadowed == nil {
				continue
			}
			relations = append(relations, builder.build(
				RelationOverrides, method, shadowed,
				method.Pos(), len(method.Name()), false, depth,
			))
		}
	}
	return relations
}

// build assembles one TypeRelation, describing both ends the way
// ExtractDefinitions would, so a later pass can key them the same way.
func (builder relationBuilder) build(
	kind TypeRelationKind,
	source, target types.Object,
	anchor token.Pos,
	anchorLen int,
	pointer bool,
	promotionDepth int,
) TypeRelation {
	sourceKind, sourceQualified := describeSymbol(source)
	targetKind, targetQualified := describeSymbol(target)
	targetPackagePath := ""
	if target.Pkg() != nil {
		targetPackagePath = target.Pkg().Path()
	}
	_, isLocalTarget := builder.local[targetPackagePath]
	position := builder.result.Fset.Position(anchor)

	return TypeRelation{
		Use: Use{
			Repository:           builder.options.Repository,
			ModulePath:           builder.modulePath,
			PackagePath:          builder.loaded.PkgPath,
			FileName:             position.Filename,
			Name:                 target.Name(),
			SourceQualifiedName:  sourceQualified,
			SourceKind:           sourceKind,
			TargetModulePath:     builder.modules[targetPackagePath],
			TargetPackagePath:    targetPackagePath,
			TargetQualifiedName:  targetQualified,
			TargetKind:           targetKind,
			TargetIsLocalPackage: isLocalTarget,
			Offset:               position.Offset,
			EndOffset:            position.Offset + anchorLen,
			StartLine:            position.Line,
			StartColumn:          position.Column,
			object:               target,
		},
		Kind:           kind,
		Pointer:        pointer,
		PromotionDepth: promotionDepth,
	}
}

// describeSymbol names an object as ExtractDefinitions would: Owner.Name for
// a method, the bare name otherwise, alongside its DefinitionKind.
func describeSymbol(object types.Object) (DefinitionKind, string) {
	kind, _ := classifyObject(object)
	owner := ""
	if kind == KindMethod {
		if function, isFunction := object.(*types.Func); isFunction {
			if receiver := function.Signature().Recv(); receiver != nil {
				owner = receiverTypeName(receiver.Type())
			}
		}
	}
	return kind, qualifiedName(owner, object.Name())
}

// promotedMethod looks up name in fieldType's own method set: the method an
// embedded field would promote if the outer type did not declare one of the
// same name. The pointer method set is always used, regardless of how the
// field itself is embedded, so a pointer-receiver method is still found. The
// returned depth is the length of the selection index the checker walked
// inside fieldType to reach it: 1 when fieldType declares it directly, more
// when fieldType itself only promotes it from a deeper embedding.
func promotedMethod(fieldType types.Type, pkg *types.Package, name string) (*types.Func, int) {
	receiver := fieldType
	if _, isPointer := fieldType.(*types.Pointer); !isPointer {
		receiver = types.NewPointer(fieldType)
	}
	selection := types.NewMethodSet(receiver).Lookup(pkg, name)
	if selection == nil {
		return nil, 0
	}
	method, isFunc := selection.Obj().(*types.Func)
	if !isFunc {
		return nil, 0
	}
	return method, len(selection.Index())
}

// interfaceCandidate is one named interface visible to IMPLEMENTS, together
// with the package that declares it.
type interfaceCandidate struct {
	pkg    *packages.Package
	object *types.TypeName
	named  *types.Named
}

// visibleInterfaces gathers every named interface with at least one method,
// across every loaded package, including dependencies: a type declared in a
// root package can implement an interface it never imports by name. The
// empty interface is excluded everywhere, named or not: it is satisfied by
// everything and reports nothing about a type.
func visibleInterfaces(ctx context.Context, result Result) ([]interfaceCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	candidates := make([]interfaceCandidate, 0)
	seen := make(map[*types.TypeName]struct{})
	packages.Visit(result.Packages, nil, func(loaded *packages.Package) {
		if loaded.Types == nil {
			return
		}
		scope := loaded.Types.Scope()
		for _, name := range scope.Names() {
			named, ok := namedTypeAt(scope, name)
			if !ok {
				continue
			}
			interfaceType, isInterface := named.Underlying().(*types.Interface)
			if !isInterface || interfaceType.NumMethods() == 0 {
				continue
			}
			object := named.Obj()
			if _, exists := seen[object]; exists {
				continue
			}
			seen[object] = struct{}{}
			candidates = append(candidates, interfaceCandidate{pkg: loaded, object: object, named: named})
		}
	})
	sort.Slice(candidates, func(left, right int) bool {
		if candidates[left].pkg.PkgPath != candidates[right].pkg.PkgPath {
			return candidates[left].pkg.PkgPath < candidates[right].pkg.PkgPath
		}
		return candidates[left].object.Name() < candidates[right].object.Name()
	})
	return candidates, nil
}
