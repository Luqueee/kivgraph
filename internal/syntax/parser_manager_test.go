package syntax

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNewParserManagerParsesAllPinnedLanguagesAndReusesParsers(t *testing.T) {
	manager, err := NewParserManager(2)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	defer manager.Close()

	sources := map[Language][]byte{
		LanguageTypeScript: []byte("const answer: number = 42;\n"),
		LanguageTSX:        []byte("const view = <main>answer</main>;\n"),
		LanguageJavaScript: []byte("const answer = 42;\n"),
		LanguageGo:         []byte("package example\n\nfunc Answer() int { return 42 }\n"),
		LanguagePython:     []byte("class Answer:\n    pass\n"),
	}
	for _, language := range []Language{LanguageTypeScript, LanguageTSX, LanguageJavaScript, LanguageGo, LanguagePython} {
		tree, err := manager.Parse(context.Background(), language, sources[language])
		if err != nil {
			t.Fatalf("Parse(%s) error = %v", language, err)
		}
		if tree.Language() != language {
			t.Fatalf("Parse(%s) language = %q", language, tree.Language())
		}
		hasError, err := tree.HasError()
		if err != nil {
			t.Fatalf("HasError(%s) error = %v", language, err)
		}
		if hasError {
			t.Fatalf("Parse(%s) reported a syntax error", language)
		}
		tree.Close()
	}

	for index := 0; index < 3; index++ {
		tree, err := manager.Parse(context.Background(), LanguageJavaScript, []byte("const value = 1;"))
		if err != nil {
			t.Fatalf("reuse Parse() error = %v", err)
		}
		tree.Close()
	}
	stats := manager.Stats()
	if stats.TotalParsers != 5 || stats.IdleParsers != 5 || stats.ActiveParsers != 0 {
		t.Fatalf("manager stats = %#v, want one reusable parser per language", stats)
	}
}

func TestParserManagerPreservesSyntaxErrorsInTree(t *testing.T) {
	manager, err := NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	defer manager.Close()

	tree, err := manager.Parse(context.Background(), LanguageJavaScript, []byte("const = ;"))
	if err != nil {
		t.Fatalf("Parse() returned an operational error for invalid syntax: %v", err)
	}
	defer tree.Close()
	hasError, err := tree.HasError()
	if err != nil {
		t.Fatalf("HasError() error = %v", err)
	}
	if !hasError {
		t.Fatal("invalid syntax did not produce an error node")
	}
}

func TestParserManagerClassifiesCancellationAndUnsupportedLanguage(t *testing.T) {
	manager, err := NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	defer manager.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = manager.Parse(ctx, LanguageGo, []byte("package example"))
	var parserErr *ParserError
	if !errors.As(err, &parserErr) || parserErr.Kind != ParserErrorCanceled || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled parse error = %v, want classified context cancellation", err)
	}

	_, err = manager.Parse(context.Background(), Language("dart"), []byte("void main() {}"))
	if !errors.As(err, &parserErr) || parserErr.Kind != ParserErrorUnsupportedLanguage || !errors.Is(err, ErrUnsupportedLanguage) {
		t.Fatalf("unsupported language error = %v", err)
	}
}

func TestParserManagerWaitCancellationDoesNotLeakParser(t *testing.T) {
	manager, err := NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	defer manager.Close()

	lease, err := manager.acquire(context.Background(), LanguageGo)
	if err != nil {
		t.Fatalf("acquire() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = manager.Parse(ctx, LanguageGo, []byte("package example"))
	lease.release()
	var parserErr *ParserError
	if !errors.As(err, &parserErr) || parserErr.Kind != ParserErrorCanceled {
		t.Fatalf("waiting canceled parse error = %v", err)
	}
	stats := manager.Stats()
	if stats.ActiveParsers != 0 || stats.TotalParsers != 1 || stats.IdleParsers != 1 {
		t.Fatalf("manager stats after canceled wait = %#v", stats)
	}
}

func TestParserManagerCloseRejectsNewWorkAndIsIdempotent(t *testing.T) {
	manager, err := NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	_, err = manager.Parse(context.Background(), LanguageGo, []byte("package example"))
	var parserErr *ParserError
	if !errors.As(err, &parserErr) || parserErr.Kind != ParserErrorManagerClosed || !errors.Is(err, ErrParserManagerClosed) {
		t.Fatalf("closed manager error = %v", err)
	}
}

func TestParserManagerIncrementalParseKeepsPreviousTreeUsable(t *testing.T) {
	manager, err := NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	defer manager.Close()

	oldSource := []byte("const value = 1;\n")
	previous, err := manager.Parse(context.Background(), LanguageJavaScript, oldSource)
	if err != nil {
		t.Fatalf("initial Parse() error = %v", err)
	}
	defer previous.Close()
	newSource := []byte("const value = 2;\n")
	updated, err := manager.ParseIncremental(context.Background(), LanguageJavaScript, newSource, previous, InputEdit{
		StartByte:   14,
		OldEndByte:  15,
		NewEndByte:  15,
		StartPoint:  InputPoint{Row: 0, Column: 14},
		OldEndPoint: InputPoint{Row: 0, Column: 15},
		NewEndPoint: InputPoint{Row: 0, Column: 15},
	})
	if err != nil {
		t.Fatalf("ParseIncremental() error = %v", err)
	}
	defer updated.Close()
	oldRoot, err := previous.RootNode()
	if err != nil {
		t.Fatalf("previous RootNode() error = %v", err)
	}
	newRoot, err := updated.RootNode()
	if err != nil {
		t.Fatalf("updated RootNode() error = %v", err)
	}
	oldText := oldRoot.Utf8Text(oldSource)
	newText := newRoot.Utf8Text(newSource)
	if oldText == newText || !strings.Contains(newText, "2") {
		t.Fatalf("incremental trees old=%q new=%q", oldText, newText)
	}
}
func TestParserManagerIncrementalParseReturnsChangedRanges(t *testing.T) {
	manager, err := NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	defer manager.Close()

	oldSource := []byte("const value = 1;\n")
	previous, err := manager.Parse(context.Background(), LanguageJavaScript, oldSource)
	if err != nil {
		t.Fatalf("initial Parse() error = %v", err)
	}
	defer previous.Close()
	newSource := []byte("const value = foo(1);\n")
	updated, ranges, err := manager.ParseIncrementalWithRanges(context.Background(), LanguageJavaScript, newSource, previous, InputEdit{
		StartByte:   14,
		OldEndByte:  15,
		NewEndByte:  20,
		StartPoint:  InputPoint{Row: 0, Column: 14},
		OldEndPoint: InputPoint{Row: 0, Column: 15},
		NewEndPoint: InputPoint{Row: 0, Column: 20},
	})
	if err != nil {
		t.Fatalf("ParseIncrementalWithRanges() error = %v", err)
	}
	defer updated.Close()
	if len(ranges) == 0 {
		t.Fatal("ParseIncrementalWithRanges() returned no changed ranges")
	}
	for _, changed := range ranges {
		if changed.EndByte <= changed.StartByte || changed.EndByte > uint(len(newSource)) {
			t.Fatalf("invalid changed range = %#v", changed)
		}
	}
}

func TestNewParserManagerRejectsInvalidConcurrency(t *testing.T) {
	if _, err := NewParserManager(0); err == nil {
		t.Fatal("NewParserManager(0) accepted invalid concurrency")
	}
}
