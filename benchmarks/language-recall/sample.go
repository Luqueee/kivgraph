package main

import (
	"bytes"
	"context"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const renameSuffix = "Kvg"

// kinds are the declaration kinds each language samples. The names are the ones
// Kivgraph stores, and they are the sampling strata: a measurement that only
// sampled functions would say nothing about fields.
var kinds = map[string][]string{
	"go":         {"func", "method", "type", "alias", "const", "var", "field"},
	"typescript": {"function", "class", "interface", "enum", "type", "method", "property", "variable"},
	"rust":       {"function", "method", "static_method", "trait_method", "struct", "enum", "enum_member", "field", "trait", "constant", "static", "type_alias"},
	"dart":       {"class", "mixin", "enum", "enum_member", "extension", "type_alias", "getter", "setter", "function", "method", "field", "variable"},
	"java":       {"class", "interface", "enum", "method", "field", "term"},
	"csharp":     {"class", "interface", "enum", "struct", "method", "property", "field"},
	"python":     {"class", "function", "method", "variable", "constant"},
}

var skippedNames = map[string]bool{"_": true, "main": true, "init": true}

// candidate is a declaration whose name can be found, as a whole word and in
// its declaring position, on its first line. Anything else is not sampled:
// renaming the wrong occurrence would measure the rename and not the tool.
type candidate struct {
	declaration
	offset int // byte offset of the name in the file
}

// sourceFiles lists the files declarations are sampled from: tracked source,
// without tests or declaration files. References from tests are still part of
// the truth; they are only not where the sample is drawn.
func sourceFiles(ctx context.Context, repository corpusRepository) ([]string, error) {
	output, err := exec.CommandContext(ctx, "git", "-C", repository.Path, "ls-files").Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, path := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if isSourceFile(repository.Language, path) && inProject(repository, path) {
			files = append(files, path)
		}
	}
	sort.Strings(files)
	return files, nil
}

// inProject keeps a Python sample inside what the oracle type-checks. The other
// languages check everything their build names, and are not narrowed.
func inProject(repository corpusRepository, path string) bool {
	if repository.Language != "python" {
		return true
	}
	for _, root := range strings.Fields(repository.Project) {
		if strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

func isSourceFile(language, path string) bool {
	switch language {
	case "go":
		return strings.HasSuffix(path, ".go") && !isTestFile(language, path)
	case "rust":
		return strings.HasSuffix(path, ".rs") && !isTestFile(language, path)
	case "dart":
		return strings.HasSuffix(path, ".dart") && !isTestFile(language, path)
	case "java":
		return strings.HasPrefix(path, "src/main/java/") && strings.HasSuffix(path, ".java")
	case "csharp":
		return strings.HasSuffix(path, ".cs") && !isTestFile(language, path)
	case "python":
		return strings.HasSuffix(path, ".py") && !isTestFile(language, path)
	default:
		return (strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")) &&
			!strings.HasSuffix(path, ".d.ts") && !isTestFile(language, path)
	}
}

func isTestFile(language, path string) bool {
	if language == "go" {
		return strings.HasSuffix(path, "_test.go")
	}
	if language == "rust" {
		return strings.HasPrefix(path, "tests/") || strings.Contains(path, "/tests/") || strings.HasSuffix(path, "_test.rs") || strings.HasSuffix(path, "_tests.rs")
	}
	if language == "dart" {
		return strings.HasPrefix(path, "test/") || strings.Contains(path, "/test/") || strings.HasSuffix(path, "_test.dart")
	}
	if language == "java" {
		return strings.HasPrefix(path, "src/test/") || strings.Contains(path, "/src/test/")
	}
	if language == "csharp" {
		lower := strings.ToLower(path)
		return strings.HasPrefix(lower, "test/") || strings.HasPrefix(lower, "tests/") || strings.Contains(lower, "/test/") ||
			strings.Contains(lower, "/tests/") || strings.Contains(lower, "tests/") || strings.HasSuffix(lower, "tests.cs")
	}
	if language == "python" {
		base := filepath.Base(path)
		return strings.HasPrefix(path, "tests/") || strings.Contains(path, "/tests/") ||
			strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") || base == "conftest.py"
	}
	base := filepath.Base(path)
	return strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
		strings.HasPrefix(path, "test/") || strings.HasPrefix(path, "tests/") || strings.Contains(path, "/__tests__/")
}

func declarationPattern(language, kind, name string) *regexp.Regexp {
	quoted := regexp.QuoteMeta(name)
	var pattern string
	switch language + "/" + kind {
	case "go/func":
		pattern = `^func\s+(` + quoted + `)\b`
	case "go/method":
		pattern = `^func\s*\([^)]*\)\s*(` + quoted + `)\b`
	case "go/type", "go/alias":
		pattern = `^\s*(?:type\s+)?(` + quoted + `)\b`
	case "go/const", "go/var":
		pattern = `^\s*(?:(?:const|var)\s+)?(` + quoted + `)\b`
	case "go/field":
		pattern = `^\s*(` + quoted + `)\s+\S`
	case "typescript/function":
		pattern = `\bfunction\s*\*?\s*(` + quoted + `)\b`
	case "typescript/class":
		pattern = `\bclass\s+(` + quoted + `)\b`
	case "typescript/interface", "typescript/enum", "typescript/type":
		pattern = `\b(?:interface|enum|type)\s+(` + quoted + `)\b`
	case "typescript/variable":
		pattern = `^(?:export\s+)?(?:declare\s+)?(?:const|let|var)\s+(` + quoted + `)\b`
	case "typescript/method", "typescript/property":
		pattern = `^\s*(?:(?:public|private|protected|static|readonly|async|abstract|override|declare|get|set)\s+)*(` + quoted + `)\s*[?!]?\s*[(:=<]`
	case "rust/function", "rust/method", "rust/static_method", "rust/trait_method":
		pattern = `\bfn\s+(` + quoted + `)\b`
	case "rust/struct":
		pattern = `\bstruct\s+(` + quoted + `)\b`
	case "rust/enum":
		pattern = `\benum\s+(` + quoted + `)\b`
	case "rust/trait":
		pattern = `\btrait\s+(` + quoted + `)\b`
	case "rust/type_alias":
		pattern = `\btype\s+(` + quoted + `)\b`
	case "rust/constant":
		pattern = `\bconst\s+(` + quoted + `)\b`
	case "rust/static":
		pattern = `\bstatic\s+(?:mut\s+)?(` + quoted + `)\b`
	case "rust/field":
		pattern = `^\s*(?:pub(?:\([^)]*\))?\s+)?(` + quoted + `)\s*:[^:]`
	case "rust/enum_member":
		pattern = `^\s*(` + quoted + `)\s*(?:[,({=]|$)`
	case "dart/class":
		pattern = `\bclass\s+(` + quoted + `)\b`
	case "dart/mixin":
		pattern = `\bmixin\s+(` + quoted + `)\b`
	case "dart/enum":
		pattern = `\benum\s+(` + quoted + `)\b`
	case "dart/extension":
		pattern = `\bextension\s+(` + quoted + `)\b`
	case "dart/type_alias":
		pattern = `\btypedef\s+(` + quoted + `)\b`
	case "dart/getter":
		pattern = `\bget\s+(` + quoted + `)\b`
	case "dart/setter":
		pattern = `\bset\s+(` + quoted + `)\b`
	case "dart/function", "dart/method":
		pattern = `^\s*(?:@\w+(?:\([^)]*\))?\s+)*[^(=;{}]*?\b(` + quoted + `)\s*(?:<[^>(]*>)?\s*\(`
	case "dart/field", "dart/variable":
		pattern = `^\s*(?:@\w+(?:\([^)]*\))?\s+)*(?:(?:static|late|final|const|var|external|covariant)\s+)*(?:[\w<>?,.\[\]\s]*?\s)?(` + quoted + `)\s*(?:=[^>]|;|,|$)`
	case "dart/enum_member":
		pattern = `^\s*(?:@\w+(?:\([^)]*\))?\s+)*(` + quoted + `)\s*(?:[,;(<{]|$)`
	case "java/class", "java/interface", "java/enum":
		pattern = `\b(?:class|interface|enum|record)\s+(` + quoted + `)\b`
	case "java/method":
		pattern = `^\s*(?:(?:public|protected|private|static|final|abstract|synchronized|default|native|strictfp)\s+)*(?:<[^>]*>\s*)?[\w<>\[\],.? ]+?\s+(` + quoted + `)\s*\(`
	case "java/field":
		pattern = `^\s*(?:(?:public|protected|private|static|final|volatile|transient)\s+)*[\w<>\[\],.? ]+?\s+(` + quoted + `)\s*(?:=[^=]|;|,)`
	case "java/term":
		pattern = `^\s*(` + quoted + `)\s*(?:[,;({]|$)`
	case "csharp/class", "csharp/interface", "csharp/enum", "csharp/struct":
		pattern = `\b(?:class|interface|enum|struct|record)\s+(` + quoted + `)\b`
	case "csharp/method":
		pattern = `^\s*(?:(?:public|private|protected|internal|static|virtual|override|abstract|sealed|async|partial|unsafe|extern|new|readonly)\s+)*[\w<>\[\],.?() ]+?\s+(` + quoted + `)\s*(?:<[^>(]*>)?\s*\(`
	case "csharp/property":
		pattern = `^\s*(?:(?:public|private|protected|internal|static|virtual|override|abstract|sealed|new|readonly|required)\s+)*[\w<>\[\],.?() ]+?\s+(` + quoted + `)\s*(?:\{|=>|$)`
	case "csharp/field":
		pattern = `^\s*(?:(?:public|private|protected|internal|static|readonly|const|volatile|new)\s+)*[\w<>\[\],.?() ]+?\s+(` + quoted + `)\s*(?:=[^=>]|;|,)`
	case "python/class":
		pattern = `^\s*class\s+(` + quoted + `)\b`
	case "python/function", "python/method":
		pattern = `^\s*(?:async\s+)?def\s+(` + quoted + `)\b`
	case "python/variable", "python/constant":
		// A plain or annotated binding, at module level, in a class body or on
		// `self`. Unpacking, `for` and `with` targets are not renamed.
		pattern = `^\s*(?:self\.)?(` + quoted + `)\s*(?::[^=]*)?(?:=[^=]|$)`
	default:
		return nil
	}
	return regexp.MustCompile(pattern)
}

// locate finds the byte offset of a declaration's name, or reports that the
// declaration cannot be renamed safely.
func locate(language string, content []byte, found declaration) (int, bool) {
	if skippedNames[found.Name] || found.Line < 1 {
		return 0, false
	}
	pattern := declarationPattern(language, found.Kind, found.Name)
	if pattern == nil {
		return 0, false
	}
	start := 0
	for line := 1; line < found.Line; line++ {
		next := bytes.IndexByte(content[start:], '\n')
		if next < 0 {
			return 0, false
		}
		start += next + 1
	}
	end := bytes.IndexByte(content[start:], '\n')
	if end < 0 {
		end = len(content) - start
	}
	match := pattern.FindSubmatchIndex(content[start : start+end])
	if match == nil {
		return 0, false
	}
	return start + match[2], true
}

func rename(content []byte, offset int, name string) []byte {
	out := make([]byte, 0, len(content)+len(renameSuffix))
	out = append(out, content[:offset+len(name)]...)
	out = append(out, renameSuffix...)
	return append(out, content[offset+len(name):]...)
}

// frame is everything that could be sampled, with the reasons the rest was not.
type frame struct {
	Candidates []candidate
	Listed     int
	Skipped    map[string]int
}

func buildFrame(ctx context.Context, session *sdkmcp.ClientSession, repository corpusRepository, allowed map[string]bool) (frame, error) {
	files, err := sourceFiles(ctx, repository)
	if err != nil {
		return frame{}, err
	}
	out := frame{Skipped: map[string]int{}}
	for _, file := range files {
		content, err := os.ReadFile(filepath.Join(repository.Path, file))
		if err != nil {
			return frame{}, err
		}
		found, err := declarations(ctx, session, repository.Name, file)
		if err != nil {
			out.Skipped["outline refused"]++
			continue
		}
		for _, declared := range found {
			out.Listed++
			if !allowed[declared.Kind] {
				out.Skipped["kind not sampled"]++
				continue
			}
			offset, ok := locate(repository.Language, content, declared)
			if !ok {
				out.Skipped["name not found in declaring position"]++
				continue
			}
			out.Candidates = append(out.Candidates, candidate{declaration: declared, offset: offset})
		}
	}
	return out, nil
}

// draw takes count candidates, cycling through the kinds in a shuffled order so
// every kind is represented before any is repeated. Kinds with few candidates
// run out first and the rest fill the sample.
func draw(candidates []candidate, count int, seed int64) []candidate {
	random := rand.New(rand.NewPCG(uint64(seed), 0))
	byKind := map[string][]candidate{}
	for _, c := range candidates {
		byKind[c.Kind] = append(byKind[c.Kind], c)
	}
	var order []string
	for kind := range byKind {
		order = append(order, kind)
	}
	sort.Strings(order)
	for _, kind := range order {
		group := byKind[kind]
		sort.Slice(group, func(i, j int) bool { return group[i].StableKey < group[j].StableKey })
		random.Shuffle(len(group), func(i, j int) { group[i], group[j] = group[j], group[i] })
	}
	var picked []candidate
	for len(picked) < count {
		progressed := false
		for _, kind := range order {
			if len(byKind[kind]) == 0 || len(picked) == count {
				continue
			}
			picked = append(picked, byKind[kind][0])
			byKind[kind] = byKind[kind][1:]
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return picked
}
