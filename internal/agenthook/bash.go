package agenthook

import (
	"encoding/json"
	"slices"
	"strings"
)

// bashInput is a shell call.
type bashInput struct {
	Command string `json:"command"`
}

// searchPrograms are the text searches worth intercepting.
var searchPrograms = map[string]bool{
	"grep": true, "egrep": true, "fgrep": true, "rg": true, "ag": true, "ack": true,
}

// valueFlags take their argument as the next token, so the token after one of
// them is never the pattern.
//
// The list matters more than it looks: without `-e`, `grep -e NewServer .`
// reads `-e` as the pattern and the real one as a path, and the gate refuses
// the wrong call for the wrong reason.
var valueFlags = map[string]bool{
	"-e": true, "--regexp": true, "-f": true, "--file": true,
	"-m": true, "--max-count": true, "-A": true, "-B": true, "-C": true,
	"--include": true, "--exclude": true, "--exclude-dir": true,
	"-g": true, "--glob": true, "-t": true, "--type": true,
}

// classifyBash finds the search inside a shell command line.
//
// A command line is not one call. `cd x && grep -rn Foo . | head` is four, and
// the gate has an opinion about exactly one of them, so the line is split and
// each segment asked separately. The first segment that asks a gateable
// question is the answer: refusing the line refuses all of it anyway, and
// naming the first reason is the one a reader can act on.
func classifyBash(raw json.RawMessage) Question {
	var input bashInput
	if err := json.Unmarshal(raw, &input); err != nil || input.Command == "" {
		return Question{}
	}
	return classifyCommandLine(input.Command)
}

// classifyCommandLine reads one command line. A script a shell is handed is read
// the same way, and always from a strictly shorter string, so it ends.
func classifyCommandLine(command string) Question {
	command = strings.TrimSpace(command)
	// `(rg Name)` and `( cd x && rg Name )` run the same search in a subshell.
	if strings.HasPrefix(command, "(") && strings.HasSuffix(command, ")") {
		command = strings.TrimSpace(command[1 : len(command)-1])
	}
	for _, segment := range splitSegments(command) {
		tokens := tokenize(segment)
		script := ""
		for {
			var escaped bool
			tokens, escaped = stripAssignments(tokens)
			if escaped {
				// The caller already said it knows: the escape is on the
				// very command being gated.
				return Question{}
			}
			var unwrapped bool
			tokens, script, unwrapped = unwrapCommandWrapper(tokens)
			if script != "" || !unwrapped {
				break
			}
		}
		if script != "" {
			if question := classifyCommandLine(script); question.Kind != KindNone {
				return question
			}
			continue
		}
		if len(tokens) == 0 {
			continue
		}
		program := baseName(tokens[0])
		arguments := tokens[1:]
		// `git grep` is the same question wearing a subcommand.
		if program == "git" && len(arguments) > 0 && arguments[0] == "grep" {
			program, arguments = "grep", arguments[1:]
		}
		var question Question
		switch {
		case searchPrograms[program]:
			question = classifySearchProgram(program, arguments)
		case program == "find":
			question = classifyFind(arguments)
		}
		if question.Kind != KindNone {
			return question
		}
	}
	return Question{}
}

// unwrapCommandWrapper exposes the command a wrapper runs, and reports whether
// it took one off.
//
// A wrapper that only changes how the command runs does not change what is
// being asked: `timeout 20 rg Name` and `time rg Name` are an rg search, and a
// gate that read them as unknown programs let every ambiguous name through
// behind the first word. RTK compresses command output for agents, so `rtk rg
// ...` is still an rg search; `proxy` only asks RTK to leave that output raw.
// RTK's own commands remain ordinary unknown programs and are allowed.
//
// `sh -c "..."` carries a whole command line, which comes back as a script for
// the caller to read as one. A wrapper with nothing to run, or one of a form
// that does not run it (`command -v rg` asks where a program is), yields no
// tokens, which is not a search.
func unwrapCommandWrapper(tokens []string) (rest []string, script string, unwrapped bool) {
	if len(tokens) == 0 {
		return tokens, "", false
	}
	switch program := baseName(tokens[0]); program {
	case "rtk":
		if len(tokens) < 2 {
			return tokens, "", false
		}
		rest = tokens[1:]
		if rest[0] == "proxy" {
			rest = rest[1:]
		}
		return rest, "", true
	case "time", "nohup", "exec", "command":
		return skipFlags(tokens[1:], "-f", "-o", "-a"), "", true
	case "nice":
		return skipFlags(tokens[1:], "-n"), "", true
	case "env":
		return skipFlags(tokens[1:], "-u", "-C", "-S"), "", true
	case "timeout":
		rest = skipFlags(tokens[1:], "-k", "--kill-after", "-s", "--signal")
		if len(rest) == 0 {
			return nil, "", true
		}
		// The duration is the first word that is not a flag.
		return rest[1:], "", true
	case "sh", "bash", "zsh", "dash", "ksh":
		for index := 1; index < len(tokens); index++ {
			flag := tokens[index]
			if !strings.HasPrefix(flag, "-") || strings.HasPrefix(flag, "--") {
				return nil, "", true
			}
			if strings.Contains(flag, "c") {
				if index+1 < len(tokens) {
					return nil, tokens[index+1], true
				}
				return nil, "", true
			}
		}
		return nil, "", true
	}
	return tokens, "", false
}

// skipFlags drops the leading flags of a wrapper, and the value of the ones that
// take one as the next word. `--name=value` carries its own.
func skipFlags(tokens []string, valueFlags ...string) []string {
	for len(tokens) > 0 && strings.HasPrefix(tokens[0], "-") && tokens[0] != "-" {
		flag := tokens[0]
		tokens = tokens[1:]
		if slices.Contains(valueFlags, flag) && len(tokens) > 0 {
			tokens = tokens[1:]
		}
	}
	return tokens
}

// classifySearchProgram reads a grep-shaped argument list.
func classifySearchProgram(program string, arguments []string) Question {
	if program == "rg" && hasArgument(arguments, "--files") {
		return classifyRGFiles(arguments)
	}
	pattern, paths := "", []string{}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case valueFlags[argument]:
			if index+1 < len(arguments) {
				index++
				if argument == "-e" || argument == "--regexp" {
					pattern = arguments[index]
				}
				paths = appendFileFilter(paths, argument, arguments[index])
			}
		case strings.HasPrefix(argument, "--") && strings.Contains(argument, "="):
			name, value, _ := strings.Cut(argument, "=")
			if name == "--regexp" {
				pattern = value
			}
			paths = appendFileFilter(paths, name, value)
		case strings.HasPrefix(argument, "-") && argument != "-":
			// A bundle of short switches, or a long one without a value.
		case pattern == "":
			pattern = argument
		default:
			paths = append(paths, argument)
		}
	}
	question := patternQuestion(pattern)
	if question.Kind == KindNone {
		return question
	}
	question.Paths, question.Tool = paths, program
	return question
}

// appendFileFilter reads the flag that says which files a search looks at as
// the native tool's `glob` and `type` are read: it is where the search looks.
//
// Without it `grep -rn version --include=*.json .` named no file type at all
// -- its one path is a directory -- and was refused as a search over code.
// An exclusion says where it does not look, which proves nothing.
func appendFileFilter(paths []string, flag, value string) []string {
	switch flag {
	case "--include", "-g", "--glob":
		if value != "" && !strings.HasPrefix(value, "!") {
			return append(paths, value)
		}
	case "-t", "--type":
		if value != "" {
			return append(paths, "*."+value)
		}
	}
	return paths
}

// classifyRGFiles reads ripgrep's file-listing mode. Its positional arguments
// are directory scopes, not search patterns; treating the first as a symbol
// turns `rg --files internal` into a false question about a symbol named
// "internal". A source glob is the only part the graph can usefully gate.
func classifyRGFiles(arguments []string) Question {
	pattern, paths := "", []string{}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--":
			paths = append(paths, arguments[index+1:]...)
			index = len(arguments)
		case argument == "--files":
		case argument == "-g" || argument == "--glob":
			if index+1 < len(arguments) {
				index++
				if pattern == "" && !strings.HasPrefix(arguments[index], "!") {
					pattern = arguments[index]
				}
			}
		case strings.HasPrefix(argument, "--glob="):
			glob := strings.TrimPrefix(argument, "--glob=")
			if pattern == "" && !strings.HasPrefix(glob, "!") {
				pattern = glob
			}
		case valueFlags[argument]:
			if index+1 < len(arguments) {
				index++
			}
		case strings.HasPrefix(argument, "-"):
		default:
			paths = append(paths, argument)
		}
	}
	if pattern == "" {
		return Question{}
	}
	return Question{Kind: KindFiles, Pattern: pattern, Paths: append(paths, pattern), Tool: "rg"}
}

func hasArgument(arguments []string, wanted string) bool {
	for _, argument := range arguments {
		if argument == "--" {
			return false
		}
		if argument == wanted {
			return true
		}
	}
	return false
}

// nameFlags are the `find` predicates that ask about a filename.
var nameFlags = map[string]bool{"-name": true, "-iname": true, "-path": true, "-ipath": true}

// classifyFind reads a `find` argument list.
//
// Only the filename predicates are a question the graph answers. `find . -mtime
// -1` and `find . -delete` are not searches for code and never reach here.
func classifyFind(arguments []string) Question {
	pattern, paths := "", []string{}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case nameFlags[argument]:
			if index+1 < len(arguments) {
				index++
				if pattern == "" {
					pattern = unquote(arguments[index])
				}
			}
		case strings.HasPrefix(argument, "-"):
			// Another predicate, and some take a value we do not read.
			// Reading it wrong only ever costs us a path, never a
			// wrong refusal: the pattern is already taken by then.
		default:
			paths = append(paths, argument)
		}
	}
	if pattern == "" {
		return Question{}
	}
	return Question{Kind: KindFiles, Pattern: pattern, Paths: append(paths, pattern), Tool: "find"}
}

// splitSegments cuts a command line at the operators that separate commands,
// respecting quotes so a `;` inside a pattern does not start a new one.
func splitSegments(command string) []string {
	segments, current := []string{}, strings.Builder{}
	var quote rune
	runes := []rune(command)
	for index := 0; index < len(runes); index++ {
		character := runes[index]
		switch {
		case quote != 0:
			if character == quote {
				quote = 0
			}
			current.WriteRune(character)
		case character == '\'' || character == '"':
			quote = character
			current.WriteRune(character)
		case character == ';' || character == '|' || character == '&' || character == '\n':
			segments = append(segments, current.String())
			current.Reset()
		default:
			current.WriteRune(character)
		}
	}
	return append(segments, current.String())
}

// tokenize splits one segment into words, removing one layer of quoting and the
// redirections.
//
// `> out.txt`, `2>err.log` and `< in.txt` say where the output goes and where
// the input comes from, and neither is somewhere a search looks: kept as words
// they became paths, and a `.txt` among them made a search over code read as a
// search over files the graph does not cover. A `>` inside quotes is a character
// of a pattern or of a path, and stays.
func tokenize(segment string) []string {
	tokens, current := []string{}, strings.Builder{}
	quoted, discard := false, false
	var quote rune
	flush := func() {
		if current.Len() > 0 || quoted {
			if discard {
				discard = false
			} else {
				tokens = append(tokens, current.String())
			}
			current.Reset()
			quoted = false
		}
	}
	for _, character := range segment {
		switch {
		case quote != 0:
			if character == quote {
				quote = 0
			} else {
				current.WriteRune(character)
			}
		case character == '\'' || character == '"':
			quote, quoted = character, true
		case character == '>' || character == '<':
			// A file descriptor in front of it belongs to the redirection.
			if !isDescriptor(current.String()) {
				flush()
			}
			current.Reset()
			quoted, discard = false, true
		case character == ' ' || character == '\t':
			flush()
		default:
			current.WriteRune(character)
		}
	}
	flush()
	return tokens
}

// isDescriptor reports whether a word is the number a redirection starts with.
func isDescriptor(word string) bool {
	if word == "" {
		return false
	}
	for _, character := range word {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// DisableVariable turns the gate off for one call.
//
// It is read in two places and both are needed. The gate itself reads it from
// its own environment, which is how a user turns the gate off for a session;
// and stripAssignments reads it off the front of the very command being gated,
// which is how the refusal's own advice works -- an agent that prefixes the
// retry has changed nothing about its environment, only about that one line.
const DisableVariable = "KIVGRAPH_DISABLE_HOOK"

// stripAssignments removes the `VAR=value` prefix of a command and reports
// whether it turns the gate off.
func stripAssignments(tokens []string) ([]string, bool) {
	escaped := false
	for len(tokens) > 0 {
		name, value, found := strings.Cut(tokens[0], "=")
		if !found || name == "" || strings.ContainsAny(name, "/. -") {
			break
		}
		if name == DisableVariable && value != "" && value != "0" {
			escaped = true
		}
		tokens = tokens[1:]
	}
	return tokens, escaped
}

// baseName is the program a path names.
func baseName(token string) string {
	if index := strings.LastIndexByte(token, '/'); index >= 0 {
		return token[index+1:]
	}
	return token
}
