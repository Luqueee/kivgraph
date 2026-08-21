package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
)

// Completion is computed here, in Go, and not written into a shell script.
//
// A static script would be a third place the command line is spelled, after the
// table and the flag sets, and it would drift exactly the way the help table
// already had. It also could not answer the questions worth asking: which
// generations are on disk, which clients this machine has, which tools this
// installation has actually been called with. Those change between two
// invocations of the same binary.
//
// So each shell gets a fixed stub that forwards the words typed so far to
// `kivgraph __complete`, and every candidate comes from the table.

// completeFileMarker asks the shell to use its own file completion. Quoting,
// symlinks, `~` and the current directory are the shell's job, and a list of
// paths produced here would get all four subtly wrong.
const completeFileMarker = ":file"

// runComplete answers the candidates for a partially typed command line.
//
// It is given the words after the program name, with the word being typed last
// -- possibly empty, which is what a trailing space looks like. It always exits
// 0: a shell that gets a non-zero status from its completion function shows the
// user a bell, and "no candidates" is a normal answer, not a failure.
func runComplete(args []string, stdout, _ io.Writer) int {
	for _, candidate := range completionCandidates(args) {
		fmt.Fprintln(stdout, candidate)
	}
	return 0
}

// completionCandidates is the whole engine, kept separate from the writer so
// the table can be exercised directly.
func completionCandidates(words []string) []string {
	// A shell hands over the words before the cursor plus the partial word.
	// With nothing typed at all the partial word is empty.
	partial := ""
	if len(words) > 0 {
		partial = words[len(words)-1]
		words = words[:len(words)-1]
	}

	spec, consumed, found := findCommand(words)
	if !found {
		// Still naming the command. Offer the next word of every
		// invocation that the words so far are a prefix of, so `doctor `
		// offers `storage` and `graph` without offering `logs`.
		return matching(commandWordsAfter(words), partial)
	}

	rest := words[consumed:]
	// A flag expecting a value, with the value being typed now.
	if len(rest) > 0 {
		if hint, expects := expectedValue(spec, rest[len(rest)-1]); expects {
			if hint.paths {
				return []string{completeFileMarker}
			}
			if hint.values == nil {
				return nil
			}
			return matching(hint.values(), partial)
		}
	}

	// bash's COMP_WORDBREAKS contains '=', so it hands `--kind=s` over as
	// three words rather than one. zsh and fish do not. Recognising both
	// shapes here keeps the stubs identical and free of shell-specific
	// reassembly.
	if len(rest) >= 2 && rest[len(rest)-1] == "=" && strings.HasPrefix(rest[len(rest)-2], "--") {
		hint, known := spec.hints[strings.TrimPrefix(rest[len(rest)-2], "--")]
		switch {
		case !known:
			return nil
		case hint.paths:
			return []string{completeFileMarker}
		case hint.values == nil:
			return nil
		default:
			return matching(hint.values(), partial)
		}
	}

	// `--flag=value` completes the value after the equals sign, which is the
	// form a shell hands over as a single word.
	if name, value, split := strings.Cut(partial, "="); split && strings.HasPrefix(name, "--") {
		hint, known := spec.hints[strings.TrimPrefix(name, "--")]
		if !known {
			return nil
		}
		if hint.paths {
			return []string{completeFileMarker}
		}
		if hint.values == nil {
			return nil
		}
		prefixed := make([]string, 0, 8)
		for _, candidate := range matching(hint.values(), value) {
			prefixed = append(prefixed, name+"="+candidate)
		}
		return prefixed
	}

	candidates := unusedFlags(spec, rest)
	// A command with sub-forms offers them alongside its own flags, so
	// `mcp ` offers install, status and remove.
	candidates = append(candidates, commandWordsAfter(words)...)
	return matching(candidates, partial)
}

// commandWordsAfter answers the next invocation word of every command the given
// words are a strict prefix of.
func commandWordsAfter(words []string) []string {
	seen := make(map[string]bool)
	next := make([]string, 0, 16)
	for _, spec := range allCommands() {
		if spec.hidden || len(spec.words) <= len(words) {
			continue
		}
		if spec.absence != nil && spec.absence() != "" {
			// A command this build cannot run is not a candidate. The
			// help says why; completion would only offer a dead end.
			continue
		}
		prefix := true
		for index, word := range words {
			if spec.words[index] != word {
				prefix = false
				break
			}
		}
		if !prefix {
			continue
		}
		candidate := spec.words[len(words)]
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		next = append(next, candidate)
	}
	if len(words) == 0 {
		next = append(next, "help")
	}
	return next
}

// expectedValue reports whether the previous word is a flag still waiting for
// its value.
//
// A boolean flag never waits: `--json` is complete on its own, and offering it a
// value would make the next word impossible to type.
func expectedValue(spec commandSpec, previous string) (flagHint, bool) {
	name := strings.TrimPrefix(strings.TrimPrefix(previous, "--"), "-")
	if name == previous || name == "" {
		return flagHint{}, false
	}
	if strings.Contains(name, "=") {
		return flagHint{}, false
	}
	entry := lookupFlag(spec, name)
	if entry == nil || isBoolFlag(entry) {
		return flagHint{}, false
	}
	return spec.hints[name], true
}

// unusedFlags answers the command's flags that the line does not already carry,
// so a second `--json` is never suggested.
func unusedFlags(spec commandSpec, rest []string) []string {
	used := make(map[string]bool, len(rest))
	for _, word := range rest {
		if !strings.HasPrefix(word, "-") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(word, "--"), "-"), "=")
		used[name] = true
	}
	candidates := make([]string, 0, 8)
	forEachFlag(spec, func(entry *flag.Flag) {
		if used[entry.Name] {
			return
		}
		candidates = append(candidates, "--"+entry.Name)
	})
	if !used["help"] {
		candidates = append(candidates, "--help")
	}
	return candidates
}

func forEachFlag(spec commandSpec, visit func(*flag.Flag)) {
	if spec.flags == nil {
		return
	}
	spec.flags().VisitAll(visit)
}

func lookupFlag(spec commandSpec, name string) *flag.Flag {
	if spec.flags == nil {
		return nil
	}
	return spec.flags().Lookup(name)
}

// isBoolFlag asks the flag package, which is the only thing that knows: a
// boolean flag's value implements IsBoolFlag.
func isBoolFlag(entry *flag.Flag) bool {
	boolean, ok := entry.Value.(interface{ IsBoolFlag() bool })
	return ok && boolean.IsBoolFlag()
}

// matching keeps the candidates that extend what has been typed. Sorting is
// deliberate: a menu whose order changes between two presses of the key is
// harder to use than an alphabetical one.
func matching(candidates []string, partial string) []string {
	kept := make([]string, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if seen[candidate] || !strings.HasPrefix(candidate, partial) {
			continue
		}
		seen[candidate] = true
		kept = append(kept, candidate)
	}
	sort.Strings(kept)
	return kept
}

// publishedGenerationIDs answers the generations on disk, newest first.
//
// This is the candidate a static script could never produce, and the one most
// worth having: a generation is a zero-padded number nobody remembers, and
// `rollback --generation` with the wrong one is not a harmless mistake.
func publishedGenerationIDs() []string {
	configuration, err := config.LoadConfig("")
	if err != nil {
		return nil
	}
	root := generation.GenerationsDir(filepath.Dir(configuration.Storage.DatabasePath))
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, 8)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// A generation directory is the padded id and nothing else. A
		// build in flight carries a .tmp suffix and is not something to
		// roll back to.
		name := entry.Name()
		if strings.TrimLeft(name, "0123456789") != "" {
			continue
		}
		ids = append(ids, name)
	}
	// Newest first: the generation a reader wants is almost always the last
	// one, and an alphabetical list of zero-padded numbers buries it.
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids
}

// runCompletionScript prints the stub for one shell.
func runCompletionScript(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && helpRequested(args) {
		fmt.Fprintf(stdout, "Usage\n  kivgraph completion bash|zsh|fish\n\n%s\n",
			"Print the shell completion script for one shell")
		return 0
	}
	if len(args) != 1 {
		writeCommandError(stderr, "completion: name exactly one shell: %s",
			strings.Join(updateShellNames(), ", "))
		return 2
	}
	script, known := completionScripts[args[0]]
	if !known {
		writeCommandError(stderr, "completion: unsupported shell %q, want %s",
			args[0], strings.Join(updateShellNames(), ", "))
		return 2
	}
	fmt.Fprint(stdout, script)
	return 0
}

// The stubs are fixed. They carry no command name, no flag and no vocabulary,
// which is what keeps them from drifting: a shell script that has to be
// regenerated when a flag is added is a script that will be out of date.
var completionScripts = map[string]string{
	"bash": `# kivgraph completion for bash. Install with:
#   kivgraph completion bash > /usr/local/etc/bash_completion.d/kivgraph
# or, without root:
#   echo 'source <(kivgraph completion bash)' >> ~/.bashrc
#
# Written for bash 3.2, which is the bash macOS ships: no mapfile, no compopt.
_kivgraph_complete() {
    local current candidates
    current="${COMP_WORDS[COMP_CWORD]}"
    candidates="$(kivgraph __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)"
    if [ "$candidates" = ":file" ]; then
        COMPREPLY=()
        if type compopt >/dev/null 2>&1; then
            compopt -o default
        else
            COMPREPLY=( $(compgen -f -- "$current") )
        fi
        return 0
    fi
    COMPREPLY=( $(compgen -W "$candidates" -- "$current") )
}
complete -F _kivgraph_complete kivgraph
`,
	"zsh": `# kivgraph completion for zsh. Install with:
#   kivgraph completion zsh > "${fpath[1]}/_kivgraph"
#compdef kivgraph
_kivgraph() {
    local -a candidates
    local raw
    raw=$(kivgraph __complete "${words[@]:1}" 2>/dev/null)
    if [[ $raw == ":file" ]]; then
        _files
        return
    fi
    candidates=(${(f)raw})
    compadd -- $candidates
}
compdef _kivgraph kivgraph
`,
	"fish": `# kivgraph completion for fish. Install with:
#   kivgraph completion fish > ~/.config/fish/completions/kivgraph.fish
function __kivgraph_complete
    set -l words (commandline -opc) (commandline -ct)
    set -e words[1]
    set -l candidates (kivgraph __complete $words 2>/dev/null)
    if test "$candidates" = ":file"
        __fish_complete_path (commandline -ct)
        return
    end
    for candidate in $candidates
        echo $candidate
    end
end
complete -c kivgraph -f -a '(__kivgraph_complete)'
`,
}
