package agenthook

import (
	"encoding/json"
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
	for _, segment := range splitSegments(input.Command) {
		tokens := tokenize(segment)
		tokens, escaped := stripAssignments(tokens)
		if escaped {
			// The caller already said it knows: the escape is on the
			// very command being gated.
			return Question{}
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

// classifySearchProgram reads a grep-shaped argument list.
func classifySearchProgram(program string, arguments []string) Question {
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
			}
		case strings.HasPrefix(argument, "--") && strings.Contains(argument, "="):
			if name, value, _ := strings.Cut(argument, "="); name == "--regexp" {
				pattern = value
			}
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

// tokenize splits one segment into words, removing one layer of quoting.
func tokenize(segment string) []string {
	tokens, current := []string{}, strings.Builder{}
	quoted := false
	var quote rune
	flush := func() {
		if current.Len() > 0 || quoted {
			tokens = append(tokens, current.String())
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
		case character == ' ' || character == '\t':
			flush()
		default:
			current.WriteRune(character)
		}
	}
	flush()
	return tokens
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
