package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/testsupport"
)

func TestGetSourceServesSeveralBodiesInOneCall(t *testing.T) {
	store, _ := sourceSnapshot(t, 91)
	_, response, err := getSource(context.Background(), nil, GetSourceInput{
		Symbols: []SourceRequest{
			{StableKey: "sym-merge"},
			{Repository: "alpha", Path: "set.go", QualifiedName: "pkg.Helper"},
		},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 2 || response.Returned != 2 || response.Coverage.Exact != 2 {
		t.Fatalf("response metadata = %#v", response)
	}
	merge := response.Results.Bodies[0]
	if !merge.Fresh || merge.Shifted != 0 || merge.Unavailable != "" {
		t.Fatalf("merge body = %#v, want a fresh unshifted body", merge)
	}
	if merge.Code != "func Merge() {\n\treturn\n}" {
		t.Fatalf("merge code = %q", merge.Code)
	}
	// The bytes arrive without a line number on every line, which is the whole
	// difference against a host range read.
	if strings.Contains(merge.Code, "1:") || strings.Contains(merge.Code, "#") {
		t.Fatalf("merge code carries a read envelope = %q", merge.Code)
	}
	helper := response.Results.Bodies[1]
	if helper.QualifiedName != "pkg.Helper" || helper.Code == "" || !helper.Fresh {
		t.Fatalf("helper body = %#v", helper)
	}
	if merge.StableKey != "" {
		t.Fatalf("concise body carries the stable key = %#v", merge)
	}
}

func TestGetSourceIncludesSurroundingLinesOnRequest(t *testing.T) {
	store, _ := sourceSnapshot(t, 92)
	_, response, err := getSource(context.Background(), nil, GetSourceInput{
		Symbols:      []SourceRequest{{StableKey: "sym-merge"}},
		ContextLines: 2,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	code := response.Results.Bodies[0].Code
	if !strings.Contains(code, "// Merge merges.") || !strings.Contains(code, "func Helper() int {") {
		t.Fatalf("code with two lines of context = %q, want the doc comment above and the line below", code)
	}
	if response.Results.ContextLines != 2 {
		t.Fatalf("context_lines = %d, want it echoed", response.Results.ContextLines)
	}
	for _, contextLines := range []int{-1, MaximumSourceContextLines + 1} {
		if _, _, err := getSource(context.Background(), nil, GetSourceInput{
			Symbols: []SourceRequest{{StableKey: "sym-merge"}}, ContextLines: contextLines,
		}, store); err == nil {
			t.Fatalf("context_lines = %d, want a rejection", contextLines)
		}
	}
}

// TestGetSourceReanchorsAShiftedDeclaration is the contract of ADR 0040: when
// the file no longer hashes to what the generation recorded, the file is the
// authority, the declaration is found again by name, and the shift is declared.
func TestGetSourceReanchorsAShiftedDeclaration(t *testing.T) {
	store, root := sourceSnapshot(t, 93)
	path := filepath.Join(root, "set.go")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Three lines above the declaration: the range the generation recorded now
	// points at the wrong place.
	shifted := "// added\n// added\n// added\n" + string(original)
	if err := os.WriteFile(path, []byte(shifted), 0o600); err != nil {
		t.Fatal(err)
	}

	_, response, err := getSource(context.Background(), nil, GetSourceInput{
		Symbols: []SourceRequest{{StableKey: "sym-merge"}},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	body := response.Results.Bodies[0]
	if body.Fresh {
		t.Fatalf("body = %#v, want it declared stale", body)
	}
	if body.Shifted != 3 {
		t.Fatalf("shifted = %d, want 3", body.Shifted)
	}
	if body.Code != "func Merge() {\n\treturn\n}" {
		t.Fatalf("re-anchored code = %q, want the declaration itself", body.Code)
	}
	if body.StartLine != 7 || body.EndLine != 9 {
		t.Fatalf("re-anchored range = %d-%d, want 7-9", body.StartLine, body.EndLine)
	}
}

// TestGetSourceDeclaresOneMissingBodyAndServesTheRest is the other half of the
// same contract: a file edited past recognition must not cost the caller the
// other answers in the same call.
func TestGetSourceDeclaresOneMissingBodyAndServesTheRest(t *testing.T) {
	store, root := sourceSnapshot(t, 94)
	if err := os.WriteFile(filepath.Join(root, "other.go"), []byte("package other\n\n// gone\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, response, err := getSource(context.Background(), nil, GetSourceInput{
		Symbols: []SourceRequest{{StableKey: "sym-other"}, {StableKey: "sym-merge"}},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if response.Returned != 2 || response.Coverage.Exact != 1 {
		t.Fatalf("response = %#v, want both rows with one of them unavailable", response)
	}
	gone := response.Results.Bodies[0]
	if gone.Code != "" || !strings.Contains(gone.Unavailable, "no declaration of \"Other\" remains") {
		t.Fatalf("removed declaration = %#v, want a stated reason and no bytes", gone)
	}
	if survived := response.Results.Bodies[1]; survived.Code == "" {
		t.Fatalf("second body = %#v, want it served despite the first failing", survived)
	}
}

// TestGetSourceRefusesToLeaveTheRepository covers the two path rejections ADR
// 0040 requires. Neither can be reached through the tool's own arguments -- the
// path comes from the graph -- so they are exercised on the loader directly,
// which is where the policy lives.
func TestGetSourceRefusesToLeaveTheRepository(t *testing.T) {
	root := testsupport.TempDir(t)
	outside := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(outside, "secret.go"), []byte("package secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	escaping := loadSourceFile(root, "../"+filepath.Base(outside)+"/secret.go", [sha256.Size]byte{})
	if escaping.err == nil || !strings.Contains(escaping.err.Error(), "escapes its repository") {
		t.Fatalf("escaping path error = %v, want a refusal", escaping.err)
	}
	linked := loadSourceFile(root, "linked/secret.go", [sha256.Size]byte{})
	if linked.err == nil || !strings.Contains(linked.err.Error(), "symlink component") {
		t.Fatalf("symlinked path error = %v, want a refusal", linked.err)
	}
}

func TestGetSourceRejectsEmptyAndOversizedRequests(t *testing.T) {
	store, _ := sourceSnapshot(t, 95)
	if _, _, err := getSource(context.Background(), nil, GetSourceInput{}, store); err == nil {
		t.Fatal("empty symbols error = nil, want a rejection")
	}
	many := make([]SourceRequest, MaximumSourceSymbols+1)
	for index := range many {
		many[index] = SourceRequest{StableKey: "sym-merge"}
	}
	if _, _, err := getSource(context.Background(), nil, GetSourceInput{Symbols: many}, store); err == nil {
		t.Fatalf("more than %d symbols error = nil, want a rejection", MaximumSourceSymbols)
	}
}

// TestReanchorDeclarationRefusesEquallyCloseHomonyms keeps the re-anchoring
// honest: two candidates the same distance away is an ambiguity, and choosing
// one would be the nominal coincidence the graph forbids.
func TestReanchorDeclarationRefusesEquallyCloseHomonyms(t *testing.T) {
	// Both declarations sit two lines from where the generation recorded one, so
	// nothing distinguishes them.
	lines := []string{"func Twin() {", "}", "", "", "func Twin() {"}
	if _, _, err := reanchorDeclaration(lines, "Twin", 3, 3); err == nil {
		t.Fatal("reanchorDeclaration(homonyms) error = nil, want an ambiguity")
	}
	// A comment mentioning the name is not a declaration of it.
	commented := []string{"// Twin does nothing", "func Twin() {", "\treturn", "}"}
	line, shifted, err := reanchorDeclaration(commented, "Twin", 1, 3)
	if err != nil {
		t.Fatalf("reanchorDeclaration(commented) error = %v", err)
	}
	if line != 2 || shifted != 1 {
		t.Fatalf("re-anchored at %d (shift %d), want line 2", line, shifted)
	}
}

// sourceSnapshot writes two real files and publishes a generation that records
// their digests, so freshness is a fact about the bytes rather than a flag.
func sourceSnapshot(t *testing.T, id uint64) (*hotsnapshot.SnapshotStore, string) {
	t.Helper()
	root := testsupport.TempDir(t)
	setSource := "package pkg\n\n// Merge merges.\nfunc Merge() {\n\treturn\n}\n\nfunc Helper() int {\n\treturn 1\n}\n"
	otherSource := "package other\n\nfunc Other() {\n}\n"
	for name, content := range map[string]string{"set.go": setSource, "other.go": otherSource} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-alpha", Name: "alpha", Path: root, Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "package-alpha", RepositoryKey: "repo-alpha", Language: "go", Name: "example.com/alpha/pkg", ModulePath: "example.com/alpha"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-set", RepositoryKey: "repo-alpha", PackageKey: "package-alpha", Path: "set.go", Language: "go", ContentHash: digestOf(setSource)},
			{Key: "file-other", RepositoryKey: "repo-alpha", PackageKey: "package-alpha", Path: "other.go", Language: "go", ContentHash: digestOf(otherSource)},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-merge", CanonicalIdentity: "go:pkg.Merge", FileKey: "file-set", Language: "go", Name: "Merge", QualifiedName: "pkg.Merge", Kind: "func", StartLine: 4, EndLine: 6},
			{StableKey: "sym-helper", CanonicalIdentity: "go:pkg.Helper", FileKey: "file-set", Language: "go", Name: "Helper", QualifiedName: "pkg.Helper", Kind: "func", StartLine: 8, EndLine: 10},
			{StableKey: "sym-other", CanonicalIdentity: "go:other.Other", FileKey: "file-other", Language: "go", Name: "Other", QualifiedName: "other.Other", Kind: "func", StartLine: 3, EndLine: 4},
		},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1_700_000_000+int64(id), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot), root
}

func digestOf(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

// TestGetSourceAnswersInProseNotJSON is the measurement that shaped this tool:
// a body inside a JSON string pays for every newline twice, which is the whole
// saving it exists to deliver. The text must therefore carry the code unescaped.
func TestGetSourceAnswersInProseNotJSON(t *testing.T) {
	store, _ := sourceSnapshot(t, 96)
	result, _, err := getSource(context.Background(), nil, GetSourceInput{
		Symbols: []SourceRequest{{StableKey: "sym-merge"}, {StableKey: "sym-other"}},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || len(result.Content) != 1 {
		t.Fatalf("result = %#v, want exactly one content block", result)
	}
	text, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content block is %T, want text", result.Content[0])
	}
	if strings.Contains(text.Text, `\n`) || strings.Contains(text.Text, `\t`) {
		t.Fatalf("rendered source carries escaped whitespace: %q", text.Text)
	}
	if !strings.Contains(text.Text, "@ alpha set.go:4-6 func pkg.Merge") {
		t.Fatalf("rendered source = %q, want a header naming the range and the declaration", text.Text)
	}
	if !strings.Contains(text.Text, "func Merge() {\n\treturn\n}") {
		t.Fatalf("rendered source = %q, want the body verbatim", text.Text)
	}
}
