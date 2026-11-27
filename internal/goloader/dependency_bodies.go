package goloader

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// bodyStripper parses the files of the packages a load depends on without the
// bodies of their functions.
//
// A load type-checks every dependency from source and keeps its syntax and its
// type information for as long as the roots live, which is most of the memory of
// an index. Nothing reads a dependency's function bodies: what a root needs from
// a dependency is the types of its declarations, and a body cannot change them.
// Dropping the body at parse time means the checker has nothing to record for
// it, and the tree is garbage as soon as the package is checked.
//
// A file is a dependency's when it is outside the load directory. The files
// cgo writes into the build cache are kept whole: they are the compiled form of
// a root's own files as often as of a dependency's, and nothing in their name
// says which.
type bodyStripper struct {
	directories []string
	stripped    sync.Map
}

func newBodyStripper(directory string) *bodyStripper {
	directories := []string{filepath.Clean(directory)}
	if resolved, err := filepath.EvalSymlinks(directory); err == nil && filepath.Clean(resolved) != directories[0] {
		directories = append(directories, filepath.Clean(resolved))
	}
	return &bodyStripper{directories: directories}
}

// parse is packages.Config.ParseFile.
func (stripper *bodyStripper) parse(fileSet *token.FileSet, filename string, source []byte) (*ast.File, error) {
	if stripper.keeps(filename) {
		// What go/packages does when it is not given a parser: it promises
		// object resolution to anyone who reads the syntax.
		return parser.ParseFile(fileSet, filename, source, parser.AllErrors|parser.ParseComments)
	}
	file, err := parser.ParseFile(fileSet, filename, source, parser.AllErrors|parser.ParseComments|parser.SkipObjectResolution)
	if file == nil {
		return file, err
	}
	for _, declaration := range file.Decls {
		if function, isFunction := declaration.(*ast.FuncDecl); isFunction && strippable(function) {
			function.Body = nil
			stripper.stripped.Store(filename, struct{}{})
		}
	}
	return file, err
}

func (stripper *bodyStripper) keeps(filename string) bool {
	// What cgo writes is kept whole. go list names those files by hash, with no
	// extension, in the build cache; older toolchains name them `.cgo1.go`.
	if filepath.Ext(filename) != ".go" || strings.HasSuffix(filename, ".cgo1.go") || strings.HasSuffix(filename, "_cgo_gotypes.go") {
		return true
	}
	for _, directory := range stripper.directories {
		if relative, err := filepath.Rel(directory, filename); err == nil && !strings.HasPrefix(relative, "..") {
			return true
		}
	}
	return false
}

// strippable says whether the checker accepts the function without a body. It
// asks for one when the function is generic, and for `init`, whose body is
// part of the declaration.
func strippable(function *ast.FuncDecl) bool {
	if function.Body == nil || function.Type.TypeParams != nil && len(function.Type.TypeParams.List) > 0 {
		return false
	}
	if function.Recv == nil {
		return function.Name.Name != "init"
	}
	for _, receiver := range function.Recv.List {
		expression := receiver.Type
		if star, isStar := expression.(*ast.StarExpr); isStar {
			expression = star.X
		}
		switch expression.(type) {
		case *ast.IndexExpr, *ast.IndexListExpr:
			return false
		}
	}
	return true
}

// unusedImport is what the checker says about an import that only the bodies
// used. Go 1.20 and earlier word it "but not used".
var unusedImport = regexp.MustCompile(` imported (as \S+ )?(and|but) not used$`)

// touches reports whether any root was parsed without a body, which means a
// directory the roots live in was not recognised and the load has to be redone
// whole.
func (stripper *bodyStripper) touches(roots []*packages.Package) bool {
	for _, root := range roots {
		for _, filename := range root.CompiledGoFiles {
			if _, stripped := stripper.stripped.Load(filename); stripped {
				return true
			}
		}
	}
	return false
}

// forgive removes, from the dependencies, the unused-import errors the missing
// bodies cause. Any other error of a dependency stays: only an error in a file
// that lost its bodies, saying exactly this, is the stripping's doing.
//
// Which file an error is in is read from the checker's own position, without
// `//line` directives. A generated parser reports under the .y file its
// directives name, and a directive can just as well name another real file of
// the package: the name in the message proves nothing about who lost bodies.
func (stripper *bodyStripper) forgive(roots []*packages.Package) {
	isRoot := make(map[*packages.Package]struct{}, len(roots))
	for _, root := range roots {
		isRoot[root] = struct{}{}
	}
	packages.Visit(roots, nil, func(loaded *packages.Package) {
		if _, root := isRoot[loaded]; root || len(loaded.Errors) == 0 {
			return
		}
		kept := loaded.Errors[:0]
		for _, failure := range loaded.Errors {
			if failure.Kind == packages.TypeError && unusedImport.MatchString(failure.Msg) && stripper.lostBodies(loaded, failure) {
				continue
			}
			kept = append(kept, failure)
		}
		// A newer go command makes go/packages add its advisory to the first
		// package that reports anything. It explains the errors, so it goes
		// with them when they were all ours.
		if len(kept) < len(loaded.Errors) && allAdvisories(kept) {
			kept = kept[:0]
		}
		loaded.Errors = kept
	})
}

// lostBodies says whether the unused import was reported in a file whose bodies
// were dropped. The physical file comes from the checker's error that the
// failure was made from; a failure with no such error, which only a hand-made
// package is, is judged by the position it prints.
func (stripper *bodyStripper) lostBodies(loaded *packages.Package, failure packages.Error) bool {
	for _, reported := range loaded.TypeErrors {
		if reported.Msg != failure.Msg || reported.Fset == nil || reported.Fset.Position(reported.Pos).String() != failure.Pos {
			continue
		}
		_, stripped := stripper.stripped.Load(reported.Fset.PositionFor(reported.Pos, false).Filename)
		return stripped
	}
	position := failure.Pos
	for range 2 {
		index := strings.LastIndexByte(position, ':')
		if index < 0 {
			return false
		}
		position = position[:index]
	}
	_, stripped := stripper.stripped.Load(position)
	return stripped
}

// allAdvisories says whether every error is the toolchain-skew advisory, which
// reports no failure of the code (see PackageError.toolchainSkew).
func allAdvisories(failures []packages.Error) bool {
	for _, failure := range failures {
		if !(PackageError{Kind: classify(failure.Kind), Position: failure.Pos, Message: failure.Msg}).toolchainSkew() {
			return false
		}
	}
	return true
}
