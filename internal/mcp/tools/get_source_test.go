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

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestGetSourceValidatesPositionAfterTrim(t *testing.T) {
	store, root := sourceSnapshot(t, 91)
	long := "package pkg\n\n\nfunc Merge() {\n//" + strings.Repeat("x", MaximumSourceBytes) + "\n}\n"
	if err := os.WriteFile(filepath.Join(root, "set.go"), []byte(long), 0o600); err != nil {
		t.Fatal(err)
	}
	req := GetSourceInput{Symbols: []SourceRequest{
		{StableKey: "sym-other"},
		{StableKey: "sym-merge"},
		{Repository: "alpha", Path: "other.go", Line: new(uint32(3)), Publication: publicationOf(store)},
	}}
	_, r, err := getSource(t.Context(), nil, req, store)
	if err != nil || r.Returned != 1 || r.Results.Trimmed != 2 {
		t.Fatalf("valid trimmed batch: returned %d, trimmed %d, err %v", r.Returned, r.Results.Trimmed, err)
	}
	stale, _ := sourceSnapshot(t, 90)
	req.Symbols[2].Publication = publicationOf(stale)
	if _, _, err = getSource(t.Context(), nil, req, store); ErrorCode(err) != CodeSnapshotUnavailable {
		t.Fatalf("trimmed stale position: %v", err)
	}
}

// publicationOf is the token a caller copies from a response served by store.
func publicationOf(store *hotsnapshot.SnapshotStore) string {
	return publicationToken(store.Load().Metadata())
}

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

// Two repositories routinely hold a file at the same relative path. Each body
// must come from its own repository: the second symbol sits past the end of the
// first repository's file, so sharing one read would slice out of range.
func TestGetSourceKeepsSamePathInTwoRepositoriesApart(t *testing.T) {
	alphaRoot, betaRoot := testsupport.TempDir(t), testsupport.TempDir(t)
	alphaSource := "package main\n\nfunc Alpha() {}\n"
	betaSource := "package main\n\n\n\n\n\n\nfunc Beta() {\n\treturn\n}\n"
	for root, content := range map[string]string{alphaRoot: alphaSource, betaRoot: betaSource} {
		if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-alpha", Name: "alpha", Path: alphaRoot, Languages: "go"},
			{Key: "repo-beta", Name: "beta", Path: betaRoot, Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "package-alpha", RepositoryKey: "repo-alpha", Language: "go", Name: "example.com/alpha", ModulePath: "example.com/alpha"},
			{Key: "package-beta", RepositoryKey: "repo-beta", Language: "go", Name: "example.com/beta", ModulePath: "example.com/beta"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-alpha", RepositoryKey: "repo-alpha", PackageKey: "package-alpha", Path: "main.go", Language: "go", ContentHash: digestOf(alphaSource)},
			{Key: "file-beta", RepositoryKey: "repo-beta", PackageKey: "package-beta", Path: "main.go", Language: "go", ContentHash: digestOf(betaSource)},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-alpha", CanonicalIdentity: "go:main.Alpha", FileKey: "file-alpha", Language: "go", Name: "Alpha", QualifiedName: "main.Alpha", Kind: "func", StartLine: 3, EndLine: 3},
			{StableKey: "sym-beta", CanonicalIdentity: "go:main.Beta", FileKey: "file-beta", Language: "go", Name: "Beta", QualifiedName: "main.Beta", Kind: "func", StartLine: 8, EndLine: 10},
		},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, 97, time.Unix(1_700_000_097, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	_, response, err := getSource(context.Background(), nil, GetSourceInput{
		Symbols: []SourceRequest{{StableKey: "sym-alpha"}, {StableKey: "sym-beta"}},
	}, hotsnapshot.NewSnapshotStore(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	bodies := response.Results.Bodies
	if len(bodies) != 2 {
		t.Fatalf("bodies = %#v, want two", bodies)
	}
	if bodies[0].Code != "func Alpha() {}" || !bodies[0].Fresh {
		t.Fatalf("alpha body = %#v", bodies[0])
	}
	if bodies[1].Repository != "beta" || bodies[1].Code != "func Beta() {\n\treturn\n}" || !bodies[1].Fresh {
		t.Fatalf("beta body = %#v, want beta's own lines", bodies[1])
	}
}

func TestGetSourceIncludesSurroundingLinesOnRequest(t *testing.T) {
	store, _ := sourceSnapshot(t, 92)
	result, response, err := getSource(context.Background(), nil, GetSourceInput{
		Symbols:      []SourceRequest{{StableKey: "sym-merge"}},
		ContextLines: 2,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{result.Content[0].(*sdkmcp.TextContent).Text, renderSourceProfiles(nil, response.Results)} {
		if !strings.Contains(text, "set.go:4-6 func pkg.Merge [shown 2-8]") {
			t.Fatalf("source header does not distinguish declaration and excerpt:\n%s", text)
		}
	}
	code := response.Results.Bodies[0].Code
	if !strings.Contains(code, "// Merge merges.") || !strings.Contains(code, "func Helper() int {") {
		t.Fatalf("code with two lines of context = %q, want the doc comment above and the line below", code)
	}
	if response.Results.ContextLines != 2 {
		t.Fatalf("context_lines = %d, want it echoed", response.Results.ContextLines)
	}
	for _, tc := range []struct {
		context int
		header  string
	}{
		{0, "set.go:4-6 func pkg.Merge\nfunc Merge()"},
		{100, "set.go:4-6 func pkg.Merge [shown 1-10]\npackage pkg"},
	} {
		result, _, err := getSource(t.Context(), nil, GetSourceInput{
			Symbols: []SourceRequest{{StableKey: "sym-merge"}}, ContextLines: tc.context,
		}, store)
		if err != nil {
			t.Fatalf("context_lines=%d: %v", tc.context, err)
		}
		if text := result.Content[0].(*sdkmcp.TextContent).Text; !strings.Contains(text, tc.header) {
			t.Fatalf("context %d: expected %q in %q", tc.context, tc.header, text)
		}
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
func sourceSnapshot(t testing.TB, id uint64) (*hotsnapshot.SnapshotStore, string) {
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

// rangeSnapshot publishes one fresh file whose single symbol records the given
// line range, which may lie past the file's end.
func rangeSnapshot(t testing.TB, content string, start, end uint32) *hotsnapshot.SnapshotStore {
	t.Helper()
	root := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "eof.go"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo-alpha", Name: "alpha", Path: root, Languages: "go"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "package-alpha", RepositoryKey: "repo-alpha", Language: "go", Name: "example.com/alpha/pkg", ModulePath: "example.com/alpha"}},
		Files:        []hotsnapshot.FileRow{{Key: "file-eof", RepositoryKey: "repo-alpha", PackageKey: "package-alpha", Path: "eof.go", Language: "go", ContentHash: digestOf(content)}},
		Symbols:      []hotsnapshot.SymbolRow{{StableKey: "sym-eof", CanonicalIdentity: "go:pkg.Eof", FileKey: "file-eof", Language: "go", Name: "Eof", QualifiedName: "pkg.Eof", Kind: "func", StartLine: start, EndLine: end}},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, 93, time.Unix(1_700_000_093, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// TestSourceBodyRefusesDeclarationStartingPastEndOfFile: a start beyond the last
// physical line must be declared unavailable whatever the context, and the
// empty element Split leaves after a terminal newline is not a line.
func TestSourceBodyRefusesDeclarationStartingPastEndOfFile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		start   uint32
	}{
		{"terminal newline, start on sentinel", "a\nb\n", 3},
		{"terminal newline, start beyond sentinel", "a\nb\n", 4},
		{"no terminal newline", "a\nb", 3},
		{"empty file", "", 1},
	} {
		store := rangeSnapshot(t, tc.content, tc.start, tc.start)
		for _, contextLines := range []int{0, 2, 100} {
			_, response, err := getSource(t.Context(), nil, GetSourceInput{
				Symbols: []SourceRequest{{StableKey: "sym-eof"}}, ContextLines: contextLines,
			}, store)
			if err != nil {
				t.Fatalf("%s, context %d: %v", tc.name, contextLines, err)
			}
			body := response.Results.Bodies[0]
			if body.Unavailable == "" || body.Code != "" {
				t.Fatalf("%s, context %d: unavailable=%q code=%q, want unavailable and no code", tc.name, contextLines, body.Unavailable, body.Code)
			}
		}
	}
}

func TestSourceBodyKeepsLastLineBehavior(t *testing.T) {
	for _, content := range []string{"a\nb\n", "a\nb"} {
		store := rangeSnapshot(t, content, 2, 2)
		for _, tc := range []struct {
			context int
			want    string
		}{{0, "b"}, {5, "a\nb"}} {
			_, response, err := getSource(t.Context(), nil, GetSourceInput{
				Symbols: []SourceRequest{{StableKey: "sym-eof"}}, ContextLines: tc.context,
			}, store)
			if err != nil {
				t.Fatal(err)
			}
			body := response.Results.Bodies[0]
			if body.Unavailable != "" || body.Code != tc.want {
				t.Fatalf("content %q context %d: unavailable=%q code=%q, want %q", content, tc.context, body.Unavailable, body.Code, tc.want)
			}
		}
	}
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
