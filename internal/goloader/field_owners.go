package goloader

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// fieldOwnerIndex names the owner of a field the way the declaration of that
// field does, whichever way a use reached it.
//
// A use sees a field through the type of the expression it wrote: a type alias,
// a struct that embeds the type that declares the field, or an unnamed struct
// whose path from its named type is only in the declaration. None of those
// spells the name the provider's facts carry (`Envelope.Data.Errors`), and an
// edge to a name that is nowhere fails the whole pass at validation. The
// declaration is the one place that does spell it, so the owner is read there,
// by position, with the same rules the definitions are named with.
//
// Only the packages of one load are known. A field declared elsewhere has no
// owner here, and the caller falls back to what the selection says.
type fieldOwnerIndex struct {
	packages map[string]*packages.Package
	owners   map[string]map[token.Pos]string
}

func newFieldOwnerIndex(roots []*packages.Package) *fieldOwnerIndex {
	index := &fieldOwnerIndex{
		packages: make(map[string]*packages.Package),
		owners:   make(map[string]map[token.Pos]string),
	}
	packages.Visit(roots, nil, func(loaded *packages.Package) {
		if loaded.Types != nil && loaded.TypesInfo != nil && len(loaded.Syntax) != 0 {
			index.packages[loaded.PkgPath] = loaded
		}
	})
	return index
}

// ownerOf returns the owner of a field as its declaration names it.
func (index *fieldOwnerIndex) ownerOf(field *types.Var) (string, bool) {
	if index == nil || field.Pkg() == nil {
		return "", false
	}
	loaded := index.packages[field.Pkg().Path()]
	if loaded == nil {
		return "", false
	}
	owners, built := index.owners[loaded.PkgPath]
	if !built {
		owners = declaredFieldOwners(loaded)
		index.owners[loaded.PkgPath] = owners
	}
	owner, declared := owners[field.Origin().Pos()]
	return owner, declared
}

// declaredFieldOwners walks a package once and names every field a definition
// would publish, by the position of its name.
func declaredFieldOwners(loaded *packages.Package) map[token.Pos]string {
	owners := make(map[token.Pos]string)
	for _, file := range loaded.Syntax {
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
			field, isVar := loaded.TypesInfo.Defs[identifier].(*types.Var)
			if !isVar || !field.IsField() || fieldOfUnnamedLiteral(stack) {
				return true
			}
			owners[identifier.Pos()] = ownerFor(KindField, field, stack)
			return true
		})
	}
	return owners
}
