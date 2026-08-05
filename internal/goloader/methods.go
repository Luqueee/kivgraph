package goloader

import (
	"context"
	"go/ast"
	"go/types"
	"sort"

	"golang.org/x/tools/go/packages"
)

// MethodResolution is one method use resolved to the type that declares it.
//
// The receiver is what the expression has; the declaring type is where the
// method really lives. They differ when a method is promoted from an embedded
// field, and that difference is the fact the graph needs.
type MethodResolution struct {
	Use

	// Selection is the selector form: value, expression or none.
	Selection SelectionKind
	// ReceiverTypeName is the named receiver type, without pointer.
	ReceiverTypeName string
	// ReceiverPackagePath is the package that declares the receiver type.
	ReceiverPackagePath string
	// ReceiverIsPointer reports a pointer receiver expression.
	ReceiverIsPointer bool
	// ReceiverIsInterface reports a call through an interface value.
	ReceiverIsInterface bool

	// DeclaringTypeName is the type that declares the method.
	DeclaringTypeName string
	// DeclaringPackagePath is the package of the declaring type.
	DeclaringPackagePath string
	// DeclaringReceiverIsPointer reports a method declared on a pointer.
	DeclaringReceiverIsPointer bool

	// Promoted reports a method reached through embedded fields.
	Promoted bool
	// EmbeddedPath names the embedded fields traversed, outermost first.
	EmbeddedPath []string
}

// ResolveMethods resolves every method use to its receiver and declaring type.
//
// Only uses the checker already resolved are considered, and the receiver is
// read from the selection, so an embedded method is attributed to the type
// that declares it instead of the type that exposes it.
func ResolveMethods(ctx context.Context, result Result, uses []Use) ([]MethodResolution, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	selections, err := collectSelections(ctx, result)
	if err != nil {
		return nil, err
	}

	resolutions := make([]MethodResolution, 0)
	for _, use := range uses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if use.TargetKind != KindMethod {
			continue
		}
		resolution := MethodResolution{Use: use, Selection: use.Selection}
		describeDeclaringType(&resolution, use.Object())
		if selection := selections[position{file: use.FileName, offset: use.Offset}]; selection != nil {
			describeReceiver(&resolution, selection)
		}
		resolutions = append(resolutions, resolution)
	}
	sort.SliceStable(resolutions, func(left, right int) bool {
		if resolutions[left].FileName != resolutions[right].FileName {
			return resolutions[left].FileName < resolutions[right].FileName
		}
		return resolutions[left].Offset < resolutions[right].Offset
	})
	return resolutions, nil
}

func collectSelections(ctx context.Context, result Result) (map[position]*types.Selection, error) {
	selections := make(map[position]*types.Selection)
	for _, loaded := range result.Packages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if loaded.TypesInfo == nil {
			continue
		}
		for _, file := range loaded.Syntax {
			collectFileSelections(result, loaded, file, selections)
		}
	}
	return selections, nil
}

func collectFileSelections(
	result Result,
	loaded *packages.Package,
	file *ast.File,
	selections map[position]*types.Selection,
) {
	ast.Inspect(file, func(node ast.Node) bool {
		selector, isSelector := node.(*ast.SelectorExpr)
		if !isSelector {
			return true
		}
		selection := loaded.TypesInfo.Selections[selector]
		if selection == nil {
			return true
		}
		selections[identifierPosition(result.Fset, selector.Sel)] = selection
		return true
	})
}

// describeDeclaringType reads the receiver of the method declaration itself.
func describeDeclaringType(resolution *MethodResolution, object types.Object) {
	function, isFunction := object.(*types.Func)
	if !isFunction || function.Signature() == nil {
		return
	}
	receiver := function.Signature().Recv()
	if receiver == nil {
		return
	}
	declared := receiver.Type()
	if pointer, isPointer := declared.(*types.Pointer); isPointer {
		resolution.DeclaringReceiverIsPointer = true
		declared = pointer.Elem()
	}
	resolution.DeclaringTypeName = receiverTypeName(declared)
	if named := namedObject(declared); named != nil && named.Pkg() != nil {
		resolution.DeclaringPackagePath = named.Pkg().Path()
	}
}

// describeReceiver reads the receiver of the expression and the embedded path
// the checker walked to reach the method.
func describeReceiver(resolution *MethodResolution, selection *types.Selection) {
	receiver := selection.Recv()
	if pointer, isPointer := receiver.(*types.Pointer); isPointer {
		resolution.ReceiverIsPointer = true
		receiver = pointer.Elem()
	}
	resolution.ReceiverTypeName = receiverTypeName(receiver)
	if named := namedObject(receiver); named != nil && named.Pkg() != nil {
		resolution.ReceiverPackagePath = named.Pkg().Path()
	}
	if _, isInterface := receiver.Underlying().(*types.Interface); isInterface {
		resolution.ReceiverIsInterface = true
	}

	index := selection.Index()
	if len(index) <= 1 {
		return
	}
	resolution.Promoted = true
	resolution.EmbeddedPath = embeddedPath(receiver, index[:len(index)-1])
}

// embeddedPath names the embedded fields the selection traversed.
func embeddedPath(receiver types.Type, index []int) []string {
	path := make([]string, 0, len(index))
	current := receiver
	for _, step := range index {
		if pointer, isPointer := current.(*types.Pointer); isPointer {
			current = pointer.Elem()
		}
		structure, isStructure := current.Underlying().(*types.Struct)
		if !isStructure || step >= structure.NumFields() {
			return path
		}
		field := structure.Field(step)
		path = append(path, field.Name())
		current = field.Type()
	}
	return path
}

// namedObject returns the type name object of a named or aliased type.
func namedObject(typ types.Type) *types.TypeName {
	switch typed := typ.(type) {
	case *types.Named:
		return typed.Obj()
	case *types.Alias:
		return typed.Obj()
	default:
		return nil
	}
}
