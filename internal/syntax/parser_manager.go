// Package syntax parses source with the pinned tree-sitter grammars and
// reports syntactic candidates, which are never semantic evidence on their
// own.
package syntax

import (
	"context"
	"errors"
	"fmt"
	"sync"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tree_sitter_javascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tree_sitter_python "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tree_sitter_rust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tree_sitter_typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Language identifies one of the grammars pinned in grammars/manifest.json.
type Language string

const (
	LanguageTypeScript Language = "typescript"
	LanguageTSX        Language = "tsx"
	LanguageJavaScript Language = "javascript"
	LanguageGo         Language = "go"
	LanguageRust       Language = "rust"
	LanguagePython     Language = "python"
)

// ParserErrorKind classifies manager and parser failures. Syntax errors remain
// in SyntaxTree and are intentionally not returned as ParserError values.
type ParserErrorKind string

const (
	ParserErrorCanceled            ParserErrorKind = "CANCELED"
	ParserErrorManagerClosed       ParserErrorKind = "MANAGER_CLOSED"
	ParserErrorUnsupportedLanguage ParserErrorKind = "UNSUPPORTED_LANGUAGE"
	ParserErrorInitialization      ParserErrorKind = "PARSER_INITIALIZATION"
	ParserErrorParse               ParserErrorKind = "PARSE"
)

var (
	// ErrParserManagerClosed indicates that no new parse can start.
	ErrParserManagerClosed = errors.New("parser manager is closed")
	// ErrUnsupportedLanguage indicates a language without a pinned grammar.
	ErrUnsupportedLanguage = errors.New("unsupported language")
	// ErrSyntaxTreeClosed indicates an operation on a released tree.
	ErrSyntaxTreeClosed = errors.New("syntax tree is closed")
)

// ParserError describes a classified parser-manager failure.
type ParserError struct {
	Kind     ParserErrorKind
	Language Language
	Err      error
}

func (err *ParserError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Language == "" {
		return fmt.Sprintf("parser error %s: %v", err.Kind, err.Err)
	}
	return fmt.Sprintf("parser error %s for %s: %v", err.Kind, err.Language, err.Err)
}

func (err *ParserError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func newParserError(kind ParserErrorKind, language Language, cause error) error {
	return &ParserError{Kind: kind, Language: language, Err: cause}
}

// InputPoint is a zero-based row and byte-column position in a source file.
type InputPoint struct {
	Row    uint
	Column uint
}

// SyntaxTree owns one Tree-sitter tree. Call Close when the tree is no longer
// needed; a closed tree cannot be used to build an inventory.
type SyntaxTree struct {
	language Language
	tree     *tree_sitter.Tree
}

// Language returns the grammar used to build the tree.
func (tree *SyntaxTree) Language() Language {
	if tree == nil {
		return ""
	}
	return tree.language
}

// Close releases the native tree. Close is idempotent.
func (tree *SyntaxTree) Close() {
	if tree == nil || tree.tree == nil {
		return
	}
	tree.tree.Close()
	tree.tree = nil
}

// RootNode returns the root node while the tree remains open.
func (tree *SyntaxTree) RootNode() (*tree_sitter.Node, error) {
	if tree == nil || tree.tree == nil {
		return nil, ErrSyntaxTreeClosed
	}
	return tree.tree.RootNode(), nil
}

// HasError reports whether Tree-sitter found syntax errors in the tree.
func (tree *SyntaxTree) HasError() (bool, error) {
	root, err := tree.RootNode()
	if err != nil {
		return false, err
	}
	return root.HasError(), nil
}

// SExpression returns the native S-expression representation of the tree.
func (tree *SyntaxTree) SExpression() (string, error) {
	if tree == nil || tree.tree == nil {
		return "", ErrSyntaxTreeClosed
	}
	return tree.tree.RootNode().ToSexp(), nil
}

// ParserManager owns reusable parsers and bounds native parser concurrency.
type ParserManager struct {
	languages     map[Language]*tree_sitter.Language
	maxConcurrent int
	slots         chan struct{}
	mu            sync.Mutex
	closed        bool
	active        int
	all           map[*tree_sitter.Parser]struct{}
	idle          map[Language][]*tree_sitter.Parser
	closedCond    *sync.Cond
}

// ParserManagerStats is a point-in-time view of native parser ownership.
type ParserManagerStats struct {
	ActiveParsers int
	IdleParsers   int
	TotalParsers  int
	MaxConcurrent int
}

// NewParserManager creates a manager with at most maxConcurrent parse calls in
// flight. Parsers are created lazily and reused by language.
func NewParserManager(maxConcurrent int) (*ParserManager, error) {
	if maxConcurrent <= 0 {
		return nil, errors.New("max concurrent parsers must be greater than zero")
	}
	manager := &ParserManager{
		languages: map[Language]*tree_sitter.Language{
			LanguageTypeScript: tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTypescript()),
			LanguageTSX:        tree_sitter.NewLanguage(tree_sitter_typescript.LanguageTSX()),
			LanguageJavaScript: tree_sitter.NewLanguage(tree_sitter_javascript.Language()),
			LanguageGo:         tree_sitter.NewLanguage(tree_sitter_go.Language()),
			LanguageRust:       tree_sitter.NewLanguage(tree_sitter_rust.Language()),
			LanguagePython:     tree_sitter.NewLanguage(tree_sitter_python.Language()),
		},
		maxConcurrent: maxConcurrent,
		slots:         make(chan struct{}, maxConcurrent),
		all:           make(map[*tree_sitter.Parser]struct{}),
		idle:          make(map[Language][]*tree_sitter.Parser),
	}
	manager.closedCond = sync.NewCond(&manager.mu)
	return manager, nil
}

// Parse parses source and returns a tree even when the source has syntax
// errors. Syntax errors are observable through SyntaxTree.HasError.
func (manager *ParserManager) Parse(ctx context.Context, language Language, source []byte) (*SyntaxTree, error) {
	ctx = normalizeContext(ctx)
	lease, err := manager.acquire(ctx, language)
	if err != nil {
		return nil, err
	}
	defer lease.release()

	nativeTree := parseWithContext(ctx, lease.parser, source)
	if err := ctx.Err(); err != nil {
		if nativeTree != nil {
			nativeTree.Close()
		}
		return nil, newParserError(ParserErrorCanceled, language, err)
	}
	if nativeTree == nil {
		return nil, newParserError(ParserErrorParse, language, errors.New("tree-sitter returned no tree"))
	}
	return &SyntaxTree{language: language, tree: nativeTree}, nil
}

// Stats reports parser reuse and concurrency state.
func (manager *ParserManager) Stats() ParserManagerStats {
	if manager == nil {
		return ParserManagerStats{}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	idle := 0
	for _, parsers := range manager.idle {
		idle += len(parsers)
	}
	return ParserManagerStats{
		ActiveParsers: manager.active,
		IdleParsers:   idle,
		TotalParsers:  len(manager.all),
		MaxConcurrent: manager.maxConcurrent,
	}
}

// Close waits for in-flight parses, closes every native parser, and prevents
// new work. It is safe to call more than once.
func (manager *ParserManager) Close() error {
	if manager == nil {
		return nil
	}
	manager.mu.Lock()
	if !manager.closed {
		manager.closed = true
	}
	for manager.active != 0 {
		manager.closedCond.Wait()
	}
	parsers := make([]*tree_sitter.Parser, 0, len(manager.all))
	for parser := range manager.all {
		parsers = append(parsers, parser)
	}
	manager.all = make(map[*tree_sitter.Parser]struct{})
	manager.idle = make(map[Language][]*tree_sitter.Parser)
	manager.mu.Unlock()

	for _, parser := range parsers {
		parser.Close()
	}
	return nil
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func parseWithContext(ctx context.Context, parser *tree_sitter.Parser, source []byte) *tree_sitter.Tree {
	length := len(source)
	return parser.ParseWithOptions(func(offset int, _ tree_sitter.Point) []byte {
		if offset < length {
			return source[offset:]
		}
		return []byte{}
	}, nil, &tree_sitter.ParseOptions{
		ProgressCallback: func(tree_sitter.ParseState) bool {
			return ctx.Err() != nil
		},
	})
}

type parserLease struct {
	manager  *ParserManager
	language Language
	parser   *tree_sitter.Parser
	released bool
}

func (lease *parserLease) release() {
	if lease == nil || lease.released {
		return
	}
	lease.released = true
	lease.manager.release(lease.language, lease.parser)
}

func (manager *ParserManager) acquire(ctx context.Context, language Language) (*parserLease, error) {
	if manager == nil {
		return nil, newParserError(ParserErrorManagerClosed, language, ErrParserManagerClosed)
	}
	if _, ok := manager.languages[language]; !ok {
		return nil, newParserError(ParserErrorUnsupportedLanguage, language, ErrUnsupportedLanguage)
	}
	if err := ctx.Err(); err != nil {
		return nil, newParserError(ParserErrorCanceled, language, err)
	}

	manager.mu.Lock()
	closed := manager.closed
	manager.mu.Unlock()
	if closed {
		return nil, newParserError(ParserErrorManagerClosed, language, ErrParserManagerClosed)
	}
	select {
	case manager.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, newParserError(ParserErrorCanceled, language, ctx.Err())
	}

	manager.mu.Lock()
	if manager.closed {
		manager.mu.Unlock()
		<-manager.slots
		return nil, newParserError(ParserErrorManagerClosed, language, ErrParserManagerClosed)
	}
	manager.active++
	var parser *tree_sitter.Parser
	idle := manager.idle[language]
	if len(idle) != 0 {
		parser = idle[len(idle)-1]
		manager.idle[language] = idle[:len(idle)-1]
	}
	manager.mu.Unlock()

	if parser == nil {
		parser = tree_sitter.NewParser()
		if err := parser.SetLanguage(manager.languages[language]); err != nil {
			manager.finish(language, parser, false)
			return nil, newParserError(ParserErrorInitialization, language, err)
		}
		manager.mu.Lock()
		if manager.closed {
			manager.mu.Unlock()
			manager.finish(language, parser, false)
			return nil, newParserError(ParserErrorManagerClosed, language, ErrParserManagerClosed)
		}
		manager.all[parser] = struct{}{}
		manager.mu.Unlock()
	}
	return &parserLease{manager: manager, language: language, parser: parser}, nil
}

func (manager *ParserManager) release(language Language, parser *tree_sitter.Parser) {
	manager.finish(language, parser, true)
}

func (manager *ParserManager) finish(language Language, parser *tree_sitter.Parser, reusable bool) {
	closeParser := false
	manager.mu.Lock()
	if parser != nil && reusable && !manager.closed {
		manager.idle[language] = append(manager.idle[language], parser)
	} else if parser != nil {
		delete(manager.all, parser)
		closeParser = true
	}
	manager.active--
	if manager.active == 0 {
		manager.closedCond.Broadcast()
	}
	manager.mu.Unlock()
	if closeParser {
		parser.Close()
	}
	<-manager.slots
}
