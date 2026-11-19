package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// oracle type-checks one repository and reports the errors it raises. The
// baseline is the errors of the untouched source: a repository that does not
// check cleanly still works as long as only the errors a rename adds are read.
type oracle struct {
	repository corpusRepository
	baseline   map[string]bool
	lines      map[string][]string
}

// oracleError is one diagnostic, keyed without its column so a rename that
// lengthens a line does not make an old error look new.
type oracleError struct {
	File    string
	Line    int
	Code    string
	Message string
}

func (e oracleError) key() string {
	return fmt.Sprintf("%s:%d:%s:%s", e.File, e.Line, e.Code, e.Message)
}

func newOracle(ctx context.Context, repository corpusRepository) (*oracle, error) {
	o := &oracle{repository: repository, baseline: map[string]bool{}, lines: map[string][]string{}}
	errs, err := o.check(ctx, "", nil)
	if err != nil {
		return nil, err
	}
	for _, e := range errs {
		o.baseline[e.key()] = true
	}
	return o, nil
}

// dependents renames the declaration called name in file (a path relative to the
// repository) by replacing the file's content, type-checks, and returns the
// files that gained an error because of the rename, with how many each gained.
// The source on disk is restored before returning.
//
// An error counts only when the line it is reported on mentions the old name.
// Renaming a type can change what the checker infers elsewhere, and those
// cascading errors are on lines that never name the declaration: counting
// them would make files that do not use it look like dependents.
//
// Errors on the declaration's own line are left out too. They are about the
// declaration no longer matching something -- an `override` with nothing to
// override, a method a structural interface expected -- which is a relation,
// not a use.
func (o *oracle) dependents(ctx context.Context, file string, line int, name string, renamed []byte) (map[string]int, error) {
	var errs []oracleError
	var err error
	switch o.repository.Language {
	case "go":
		errs, err = o.check(ctx, file, renamed)
	default:
		errs, err = o.checkOnDisk(ctx, file, renamed)
	}
	if err != nil {
		return nil, err
	}
	mention := regexp.MustCompile(`(?:^|[^\w$#])` + regexp.QuoteMeta(name) + `(?:$|[^\w$])`)
	files := map[string]int{}
	for _, e := range errs {
		if o.baseline[e.key()] || isRelationError(o.repository.Language, e) {
			continue
		}
		if e.File == file && e.Line == line {
			continue
		}
		if !mention.MatchString(o.line(e.File, e.Line)) {
			continue
		}
		files[e.File]++
	}
	return files, nil
}

// line returns a line of the original source. The disk holds the original
// again by the time errors are read, and a rename only lengthens a line.
func (o *oracle) line(file string, number int) string {
	lines, ok := o.lines[file]
	if !ok {
		content, err := os.ReadFile(filepath.Join(o.repository.Path, file))
		if err == nil {
			lines = strings.Split(string(content), "\n")
		}
		o.lines[file] = lines
	}
	if number < 1 || number > len(lines) {
		return ""
	}
	return lines[number-1]
}

// isRelationError reports whether an error is about a type no longer satisfying
// an interface or a base class, not about the renamed name being used. Those
// files depend on the declaration through an implementation relation, which
// `find_references` is not the tool for, so they are not part of the truth.
func isRelationError(language string, e oracleError) bool {
	switch language {
	case "go":
		return strings.Contains(e.Message, "does not implement") || strings.Contains(e.Message, "missing method") ||
			strings.Contains(e.Message, "wrong type for method")
	case "rust":
		switch e.Code {
		case "E0046", "E0053", "E0185", "E0186", "E0195", "E0407", "E0437", "E0438":
			return true
		}
		return false
	case "dart":
		switch e.Code {
		case "NON_ABSTRACT_CLASS_INHERITS_ABSTRACT_MEMBER", "INVALID_OVERRIDE", "CONCRETE_CLASS_WITH_ABSTRACT_MEMBER",
			"INVALID_IMPLEMENTATION_OVERRIDE", "NON_ABSTRACT_CLASS_INHERITS_ABSTRACT_MEMBER_ONE",
			"NON_ABSTRACT_CLASS_INHERITS_ABSTRACT_MEMBER_TWO", "NON_ABSTRACT_CLASS_INHERITS_ABSTRACT_MEMBER_THREE",
			"NON_ABSTRACT_CLASS_INHERITS_ABSTRACT_MEMBER_FOUR", "NON_ABSTRACT_CLASS_INHERITS_ABSTRACT_MEMBER_FIVE_PLUS",
			// A constructor is written with its class's name, so renaming the
			// class breaks the constructors' own declarations; an `implements`
			// clause is an implementation relation. Neither is a use.
			"INVALID_CONSTRUCTOR_NAME", "INVALID_FACTORY_NAME_NOT_A_CLASS", "REDIRECT_TO_INVALID_RETURN_TYPE", "IMPLEMENTS_NON_CLASS",
			"CONST_METHOD", "FIELD_INITIALIZER_OUTSIDE_CONSTRUCTOR":
			return true
		}
		return false
	default:
		switch e.Code {
		case "TS2415", "TS2416", "TS2420", "TS2515", "TS2559":
			return true
		}
		return false
	}
}

func (o *oracle) checkOnDisk(ctx context.Context, file string, renamed []byte) ([]oracleError, error) {
	path := filepath.Join(o.repository.Path, file)
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if renamed != nil {
		if err := os.WriteFile(path, renamed, 0o644); err != nil {
			return nil, err
		}
		defer func() { _ = os.WriteFile(path, original, 0o644) }()
	}
	return o.check(ctx, "", nil)
}

// check runs the type checker. For Go the rename travels as an overlay and the
// disk is never written; for TypeScript the caller has already written it.
func (o *oracle) check(ctx context.Context, file string, content []byte) ([]oracleError, error) {
	switch o.repository.Language {
	case "go":
		return o.checkGo(ctx, file, content)
	case "rust":
		return o.checkRust(ctx)
	case "dart":
		return o.checkDart(ctx)
	}
	return o.checkTypeScript(ctx)
}

// checkGo loads every package of the module with its tests, type-checking from
// source all the way down. Loading from source matters: a package whose
// dependency no longer compiles would otherwise not be checked at all, and the
// uses of the renamed name in it would be missed.
func (o *oracle) checkGo(ctx context.Context, file string, content []byte) ([]oracleError, error) {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     o.repository.Path,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
		Tests: true,
		Env:   toolEnvironment(),
	}
	if content != nil {
		cfg.Overlay = map[string][]byte{filepath.Join(o.repository.Path, file): content}
	}
	loaded, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var errs []oracleError
	packages.Visit(loaded, nil, func(pkg *packages.Package) {
		for _, packageError := range pkg.Errors {
			e, ok := parseGoError(o.repository.Path, packageError)
			if !ok || seen[e.key()] {
				continue
			}
			seen[e.key()] = true
			errs = append(errs, e)
		}
	})
	sort.Slice(errs, func(i, j int) bool { return errs[i].key() < errs[j].key() })
	return errs, nil
}

var goPosition = regexp.MustCompile(`^(.*):(\d+):(\d+)$`)

func parseGoError(root string, e packages.Error) (oracleError, bool) {
	match := goPosition.FindStringSubmatch(e.Pos)
	if match == nil {
		return oracleError{}, false
	}
	relative, err := filepath.Rel(root, match[1])
	if err != nil || strings.HasPrefix(relative, "..") {
		return oracleError{}, false
	}
	line, _ := strconv.Atoi(match[2])
	return oracleError{File: filepath.ToSlash(relative), Line: line, Message: e.Msg}, true
}

var tscError = regexp.MustCompile(`^(.+?)\((\d+),\d+\): error (TS\d+): (.*)$`)

// checkTypeScript runs the repository's own compiler on its own project, so the
// files it reads are the files `tsc` is configured to read.
func (o *oracle) checkTypeScript(ctx context.Context) ([]oracleError, error) {
	process := exec.CommandContext(ctx, "npx", "--no-install", "tsc", "--noEmit", "-p", o.repository.Project, "--pretty", "false")
	process.Dir = o.repository.Path
	process.Env = toolEnvironment()
	var out bytes.Buffer
	process.Stdout, process.Stderr = &out, &out
	// A type error is a nonzero exit and the whole point. Only a failure that
	// printed no diagnostic is an error here.
	runErr := process.Run()
	var errs []oracleError
	for _, line := range strings.Split(out.String(), "\n") {
		match := tscError.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if match == nil {
			continue
		}
		number, _ := strconv.Atoi(match[2])
		errs = append(errs, oracleError{File: filepath.ToSlash(match[1]), Line: number, Code: match[3], Message: match[4]})
	}
	if runErr != nil && len(errs) == 0 {
		return nil, fmt.Errorf("tsc: %w: %s", runErr, strings.TrimSpace(out.String()))
	}
	return errs, nil
}

// cargoMessage is the part of a `cargo check --message-format=json` line the
// oracle reads.
type cargoMessage struct {
	Reason  string `json:"reason"`
	Message struct {
		Level   string `json:"level"`
		Message string `json:"message"`
		Code    *struct {
			Code string `json:"code"`
		} `json:"code"`
		Spans []struct {
			FileName  string `json:"file_name"`
			LineStart int    `json:"line_start"`
			IsPrimary bool   `json:"is_primary"`
		} `json:"spans"`
	} `json:"message"`
}

// checkRust runs `cargo check` over every target of the workspace, so the
// files it reads are the files the package itself builds, tests included.
// rustc stops after name resolution when that fails, so an error that needs
// type inference can be hidden behind an unresolved name elsewhere: the truth
// can only be too small, never contain a file that does not use the name.
func (o *oracle) checkRust(ctx context.Context) ([]oracleError, error) {
	process := exec.CommandContext(ctx, "cargo", "check", "--workspace", "--all-targets", "--locked", "--keep-going", "--message-format=json", "--color=never")
	process.Dir = o.repository.Path
	process.Env = toolEnvironment()
	var stdout, stderr bytes.Buffer
	process.Stdout, process.Stderr = &stdout, &stderr
	runErr := process.Run()
	var errs []oracleError
	for _, line := range strings.Split(stdout.String(), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var message cargoMessage
		if json.Unmarshal([]byte(line), &message) != nil || message.Reason != "compiler-message" || message.Message.Level != "error" {
			continue
		}
		for _, span := range message.Message.Spans {
			if !span.IsPrimary || filepath.IsAbs(span.FileName) || strings.HasPrefix(span.FileName, "<") {
				continue
			}
			code := ""
			if message.Message.Code != nil {
				code = message.Message.Code.Code
			}
			errs = append(errs, oracleError{File: filepath.ToSlash(span.FileName), Line: span.LineStart, Code: code, Message: message.Message.Message})
			break
		}
	}
	if runErr != nil && len(errs) == 0 {
		return nil, fmt.Errorf("cargo check: %w: %s", runErr, strings.TrimSpace(stderr.String()))
	}
	return errs, nil
}

// checkDart runs `dart analyze` over the package, tests included, and keeps
// the errors, and the two warnings a rename turns an `export ... show` or
// `hide` clause into: an export that names a declaration is a dependency the
// analyzer only warns about. The machine format is
// `SEVERITY|TYPE|CODE|file|line|column|length|message`.
func (o *oracle) checkDart(ctx context.Context) ([]oracleError, error) {
	process := exec.CommandContext(ctx, "dart", "analyze", "--format=machine")
	process.Dir = o.repository.Path
	process.Env = toolEnvironment()
	var stdout, stderr bytes.Buffer
	process.Stdout, process.Stderr = &stdout, &stderr
	runErr := process.Run()
	root, err := filepath.EvalSymlinks(o.repository.Path)
	if err != nil {
		return nil, err
	}
	var errs []oracleError
	for _, line := range strings.Split(stdout.String()+stderr.String(), "\n") {
		fields := strings.SplitN(strings.TrimRight(line, "\r"), "|", 8)
		if len(fields) != 8 || (fields[0] != "ERROR" && fields[2] != "UNDEFINED_SHOWN_NAME" && fields[2] != "UNDEFINED_HIDDEN_NAME") {
			continue
		}
		number, err := strconv.Atoi(fields[4])
		if err != nil {
			continue
		}
		file, err := filepath.Rel(root, fields[3])
		if err != nil || strings.HasPrefix(file, "..") {
			continue
		}
		errs = append(errs, oracleError{File: filepath.ToSlash(file), Line: number, Code: fields[2], Message: fields[7]})
	}
	if runErr != nil && len(errs) == 0 && strings.Contains(stderr.String(), "Could not") {
		return nil, fmt.Errorf("dart analyze: %w: %s", runErr, strings.TrimSpace(stderr.String()))
	}
	return errs, nil
}
