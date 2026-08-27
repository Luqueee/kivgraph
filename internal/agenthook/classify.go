package agenthook

import (
	"encoding/json"
	"strings"
)

// Kind is the shape of the question a call asks, and therefore which tool
// answers it.
//
// The shapes are separate because the answers are. A bare identifier is a
// question about a symbol and `find_references` answers it; a sentence is a
// question about intent and only `find_by_intent` answers it; and routing
// either one to the other tool would give a worse answer than the `grep` the
// gate refused. `KindNone` is every call that is not a search, which is most of
// them, and it never reaches the graph.
type Kind uint8

const (
	// KindNone is a call the gate has no opinion about.
	KindNone Kind = iota
	// KindSymbol is a bare identifier: something has that name.
	KindSymbol
	// KindIntent is prose, several words, or a pattern too loose to be a
	// name: the caller does not know what the thing is called.
	KindIntent
	// KindFiles is a question about which files exist, not what is in them.
	KindFiles
	// KindResearchAgent is a subagent asked to go read the codebase.
	KindResearchAgent
)

// Question is what a call was really asking, once the tool and its flags are
// stripped away.
type Question struct {
	Kind Kind
	// Pattern is the text the caller searched for, with any quoting removed.
	Pattern string
	// Paths are the places it searched, as written. Empty means the working
	// directory, which is what every one of these tools defaults to.
	Paths []string
	// Tool is the spelling the agent used, kept for the message: telling a
	// caller its `rg` was refused reads better than telling it its `Bash` was.
	Tool string
}

// Classify answers what a payload asks, without touching the filesystem or the
// graph.
//
// It is deliberately total and deliberately shy: anything it does not
// positively recognise as a search is `KindNone`. The asymmetry is the whole
// design -- a missed `grep` costs the tokens the gate exists to save, and a
// wrongly refused `git log` costs the user their trust in it.
func Classify(payload Payload) Question {
	switch classifyTool(payload.ToolName) {
	case toolBash:
		return classifyBash(payload.ToolInput)
	case toolGrep:
		return classifyGrep(payload.ToolInput)
	case toolGlob:
		return classifyGlob(payload.ToolInput)
	case toolAgent:
		return classifyAgent(payload.ToolInput)
	default:
		return Question{}
	}
}

// grepInput is the shape all three agents give a native text search.
type grepInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Glob    string `json:"glob"`
	Type    string `json:"type"`
}

func classifyGrep(raw json.RawMessage) Question {
	var input grepInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return Question{}
	}
	question := patternQuestion(input.Pattern)
	if question.Kind == KindNone {
		return question
	}
	question.Tool = "grep"
	if input.Path != "" {
		question.Paths = []string{input.Path}
	}
	// A glob or a type restricts the search to a language's files, which is
	// the caller saying it is looking at code even when the path does not.
	if input.Glob != "" {
		question.Paths = append(question.Paths, input.Glob)
	}
	if input.Type != "" {
		question.Paths = append(question.Paths, "*."+input.Type)
	}
	return question
}

// globInput is a question about filenames.
type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
}

func classifyGlob(raw json.RawMessage) Question {
	var input globInput
	if err := json.Unmarshal(raw, &input); err != nil || input.Pattern == "" {
		return Question{}
	}
	paths := []string{input.Pattern}
	if input.Path != "" {
		paths = append(paths, input.Path)
	}
	return Question{Kind: KindFiles, Pattern: input.Pattern, Paths: paths, Tool: "glob"}
}

// agentInput is a subagent dispatch.
type agentInput struct {
	SubagentType string `json:"subagent_type"`
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
}

// researchAgents are the subagent types whose whole job is reading code.
//
// A subagent that deploys, writes or reviews is not gated: it will read code on
// the way, but refusing it would refuse the task rather than redirect the
// question, and the gate has no better answer to offer it.
var researchAgents = map[string]bool{
	"explore": true, "general-purpose": true, "search": true, "plan": true,
}

func classifyAgent(raw json.RawMessage) Question {
	var input agentInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return Question{}
	}
	if !researchAgents[strings.ToLower(strings.TrimSpace(input.SubagentType))] {
		return Question{}
	}
	intent := input.Description
	if intent == "" {
		intent = firstLine(input.Prompt)
	}
	return Question{Kind: KindResearchAgent, Pattern: intent, Tool: "agent"}
}

// firstLine is the opening sentence of a prompt, bounded so a refusal that
// quotes it back stays one line.
func firstLine(prompt string) string {
	line := strings.TrimSpace(prompt)
	if index := strings.IndexAny(line, ".\n"); index > 0 {
		line = line[:index]
	}
	if len(line) > 120 {
		line = strings.TrimSpace(line[:120]) + "…"
	}
	return line
}
