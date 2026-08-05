package goloader

import (
	"context"
	"go/ast"
	"go/token"
	"sort"

	"golang.org/x/tools/go/packages"
)

// ReferenceKind classifies a resolved use as a graph edge.
type ReferenceKind string

const (
	// ReferenceRead is a plain use of a symbol.
	ReferenceRead ReferenceKind = "REFERENCES"
	// ReferenceCallsDirect is the callee of a call expression.
	ReferenceCallsDirect ReferenceKind = "CALLS_DIRECT"
	// ReferenceTypeUses is a use of a type in a type position or conversion.
	ReferenceTypeUses ReferenceKind = "TYPE_USES"
	// ReferencePassesAsCallback is a function or method handed over as a
	// value in a call argument, never invoked at that site.
	ReferencePassesAsCallback ReferenceKind = "PASSES_AS_CALLBACK"
	// ReferenceAssignsFunction is a function or method stored in a variable
	// or constant, never invoked at that site.
	ReferenceAssignsFunction ReferenceKind = "ASSIGNS_FUNCTION"
	// ReferenceReturnsFunction is a function or method returned as a value.
	ReferenceReturnsFunction ReferenceKind = "RETURNS_FUNCTION"
)

// Reference is one use classified as a graph edge.
type Reference struct {
	Use
	Kind ReferenceKind
}

// ClassifyReferences turns resolved uses into classified edges.
//
// The classification comes from the syntax the checker already resolved: the
// callee of a call expression is a direct call, a type target is a type use,
// and everything else is a plain reference. Calling a variable that happens to
// hold a function is not a direct call to a function symbol, so it stays a
// reference.
func ClassifyReferences(ctx context.Context, result Result, uses []Use) ([]Reference, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	roles, err := collectRoles(ctx, result)
	if err != nil {
		return nil, err
	}

	references := make([]Reference, 0, len(uses))
	for _, use := range uses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		references = append(references, Reference{
			Use:  use,
			Kind: classifyUse(use, roles[position{file: use.FileName, offset: use.Offset}]),
		})
	}
	sort.SliceStable(references, func(left, right int) bool {
		if references[left].FileName != references[right].FileName {
			return references[left].FileName < references[right].FileName
		}
		return references[left].Offset < references[right].Offset
	})
	return references, nil
}

// role is the syntactic position a use occupies.
type role uint8

// Roles are ordered by strength, matching the TypeScript worker: a callee is
// never downgraded to a return, an assignment or an argument.
const (
	roleNone role = iota
	roleArgument
	roleAssignment
	roleReturn
	roleCallee
)

type position struct {
	file   string
	offset int
}

func classifyUse(use Use, syntacticRole role) ReferenceKind {
	callable := isCallable(use.TargetKind)
	switch {
	case syntacticRole == roleCallee && callable:
		return ReferenceCallsDirect
	case syntacticRole == roleReturn && callable:
		return ReferenceReturnsFunction
	case syntacticRole == roleAssignment && callable:
		return ReferenceAssignsFunction
	case syntacticRole == roleArgument && callable:
		return ReferencePassesAsCallback
	case use.TargetKind == KindType || use.TargetKind == KindAlias:
		return ReferenceTypeUses
	default:
		return ReferenceRead
	}
}

func isCallable(kind DefinitionKind) bool {
	return kind == KindFunc || kind == KindMethod
}

// collectRoles records the syntactic role of every identifier of the roots.
func collectRoles(ctx context.Context, result Result) (map[position]role, error) {
	roles := make(map[position]role)
	for _, loaded := range result.Packages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, file := range loaded.Syntax {
			collectFileRoles(result.Fset, loaded, file, roles)
		}
	}
	return roles, nil
}

func collectFileRoles(
	fset *token.FileSet,
	loaded *packages.Package,
	file *ast.File,
	roles map[position]role,
) {
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CallExpr:
			// Arguments first: a callee of the same call wins the role.
			for _, argument := range typed.Args {
				if identifier := valueIdentifier(argument); identifier != nil {
					setRole(roles, identifierPosition(fset, identifier), roleArgument)
				}
			}
			if identifier := calleeIdentifier(typed.Fun); identifier != nil {
				setRole(roles, identifierPosition(fset, identifier), roleCallee)
			}
		case *ast.AssignStmt:
			for _, value := range typed.Rhs {
				if identifier := valueIdentifier(value); identifier != nil {
					setRole(roles, identifierPosition(fset, identifier), roleAssignment)
				}
			}
		case *ast.ValueSpec:
			for _, value := range typed.Values {
				if identifier := valueIdentifier(value); identifier != nil {
					setRole(roles, identifierPosition(fset, identifier), roleAssignment)
				}
			}
		case *ast.ReturnStmt:
			for _, value := range typed.Results {
				if identifier := valueIdentifier(value); identifier != nil {
					setRole(roles, identifierPosition(fset, identifier), roleReturn)
				}
			}
		}
		return true
	})
}

// calleeIdentifier unwraps the callee of a call to the identifier that names
// the called object, or nil when the callee is not a named object.
func calleeIdentifier(expression ast.Expr) *ast.Ident {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed
	case *ast.ParenExpr:
		return calleeIdentifier(typed.X)
	case *ast.SelectorExpr:
		return typed.Sel
	case *ast.IndexExpr:
		return calleeIdentifier(typed.X)
	case *ast.IndexListExpr:
		return calleeIdentifier(typed.X)
	default:
		return nil
	}
}

// valueIdentifier names the object an argument denotes, or nil when the
// argument is not a plain named value. A nested call is not an argument
// identifier: its own callee is classified separately.
func valueIdentifier(expression ast.Expr) *ast.Ident {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed
	case *ast.ParenExpr:
		return valueIdentifier(typed.X)
	case *ast.SelectorExpr:
		return typed.Sel
	case *ast.IndexExpr:
		return valueIdentifier(typed.X)
	case *ast.IndexListExpr:
		return valueIdentifier(typed.X)
	default:
		return nil
	}
}

// setRole keeps the strongest role: a callee is never downgraded.
func setRole(roles map[position]role, place position, candidate role) {
	if existing, exists := roles[place]; exists && existing >= candidate {
		return
	}
	roles[place] = candidate
}

func identifierPosition(fset *token.FileSet, identifier *ast.Ident) position {
	place := fset.Position(identifier.Pos())
	return position{file: place.Filename, offset: place.Offset}
}
