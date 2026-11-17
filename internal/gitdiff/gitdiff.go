// Package gitdiff answers which lines of a checkout differ from a ref.
//
// It exists for `kivgraph impact`, which maps changed lines onto the symbols of
// the graph. It runs git rather than reimplementing it, because the question
// "what changed since this ref" has rules -- renames, binary files, untracked
// files -- that git already knows, and it reads the answer in the one form that
// does not depend on them: the line ranges of the new side of each hunk.
package gitdiff

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// ErrUnknownRef is returned when the ref names no commit.
var ErrUnknownRef = errors.New("unknown ref")

// Range is a run of lines of the working tree, 1-based and inclusive.
//
// Between marks a change that removed lines and added none: it sits between
// Start and End -- two neighbouring lines of the new file -- and belongs to a
// symbol only when that symbol holds both of them. A deletion between two
// functions touches neither.
type Range struct {
	Start, End int
	Between    bool
}

// Overlaps reports whether the lines first..last of a symbol are touched by the
// range.
func (changed Range) Overlaps(first, last int) bool {
	if changed.Between {
		return first <= changed.Start && last >= changed.End
	}
	return first <= changed.End && last >= changed.Start
}

// Change is one file that differs from the ref.
type Change struct {
	// Path is slash-separated and relative to the checkout root.
	Path string
	// Deleted says the file no longer exists: its lines are gone and there is
	// nothing in the working tree to map them onto.
	Deleted bool
	// Ranges are the lines that differ. A file that is new, or untracked, is one
	// range over every line.
	Ranges []Range
	// Untracked says git has never been told about the file. Whatever the
	// graph does not hold of those is not a change anyone made: a build
	// directory, a tool's own state.
	Untracked bool
}

// Diff is the answer for one ref.
type Diff struct {
	// BaseCommit is the commit the ref names, spelled in full.
	BaseCommit string
	Changes    []Change
}

// WholeFile is the end of a range that covers a file whatever its length.
const WholeFile = int(^uint(0) >> 1)

// Root answers the top of the checkout that contains directory.
func Root(ctx context.Context, directory string) (string, error) {
	output, err := git(ctx, directory, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%s is not inside a git checkout: %w", directory, err)
	}
	return strings.TrimSpace(output), nil
}

// Compare lists what differs between ref and the working tree of the checkout at
// root: tracked files as they are on disk, staged or not, and untracked files
// that are not ignored.
func Compare(ctx context.Context, root, ref string) (Diff, error) {
	if strings.TrimSpace(ref) == "" || strings.HasPrefix(ref, "-") {
		return Diff{}, fmt.Errorf("%w %q", ErrUnknownRef, ref)
	}
	commit, err := git(ctx, root, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return Diff{}, fmt.Errorf("%w %q: it names no commit of %s", ErrUnknownRef, ref, root)
	}
	commit = strings.TrimSpace(commit)

	// -U0 so a hunk is exactly the lines that changed. --no-renames so a moved
	// file is a deletion and an addition, each of which the graph can place.
	// quotepath off so a path is not wrapped in quotes for being non-ASCII.
	patch, err := git(ctx, root, "-c", "core.quotepath=off", "diff", "--no-color", "--no-ext-diff",
		"--no-textconv", "--no-renames", "--unified=0", commit, "--")
	if err != nil {
		return Diff{}, fmt.Errorf("git diff %s: %w", ref, err)
	}
	changes, err := parsePatch(patch)
	if err != nil {
		return Diff{}, err
	}

	untracked, err := git(ctx, root, "-c", "core.quotepath=off", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Diff{}, fmt.Errorf("git ls-files: %w", err)
	}
	for _, path := range strings.Split(untracked, "\x00") {
		if path != "" {
			changes = append(changes, Change{Path: path, Untracked: true, Ranges: []Range{{Start: 1, End: WholeFile}}})
		}
	}
	return Diff{BaseCommit: commit, Changes: changes}, nil
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// parsePatch reads the output of `git diff --unified=0`.
func parsePatch(patch string) ([]Change, error) {
	var changes []Change
	var current *Change
	var oldPath string
	// A removed line that starts with "-- " reads exactly like a "--- " file
	// header, and an added "++ " like a "+++ ", so the headers are only read
	// before the first hunk of a file.
	inHunk := false
	scanner := bufio.NewScanner(strings.NewReader(patch))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			current, oldPath, inHunk = nil, "", false
		case inHunk && !strings.HasPrefix(line, "@@ "):
			continue
		case strings.HasPrefix(line, "--- "):
			oldPath = diffPath(strings.TrimPrefix(line, "--- "), "a/")
		case strings.HasPrefix(line, "+++ "):
			newPath := diffPath(strings.TrimPrefix(line, "+++ "), "b/")
			change := Change{Path: newPath}
			if newPath == "" {
				change = Change{Path: oldPath, Deleted: true}
			}
			changes = append(changes, change)
			current = &changes[len(changes)-1]
		case strings.HasPrefix(line, "@@ ") && current != nil && !current.Deleted:
			inHunk = true
			match := hunkHeader.FindStringSubmatch(line)
			if match == nil {
				return nil, fmt.Errorf("unreadable hunk header %q", line)
			}
			start, _ := strconv.Atoi(match[1])
			count := 1
			if match[2] != "" {
				count, _ = strconv.Atoi(match[2])
			}
			if count == 0 {
				// "+N,0" is the line the removal follows. A removal at the very
				// top of a file follows line 0, which does not exist.
				current.Ranges = append(current.Ranges, Range{Start: max(start, 1), End: max(start, 1) + 1, Between: true})
				break
			}
			current.Ranges = append(current.Ranges, Range{Start: start, End: start + count - 1})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read patch: %w", err)
	}
	return changes, nil
}

// diffPath strips the "a/" or "b/" a diff puts in front of a path, and answers ""
// for /dev/null, which is how a diff says a file does not exist on that side.
func diffPath(field, prefix string) string {
	field = strings.TrimSuffix(field, "\t")
	if unquoted, err := strconv.Unquote(field); err == nil && strings.HasPrefix(field, `"`) {
		field = unquoted
	}
	if field == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(field, prefix)
}

func git(ctx context.Context, directory string, arguments ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", directory}, arguments...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", err, message)
	}
	return stdout.String(), nil
}
