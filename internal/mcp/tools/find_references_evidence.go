package tools

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

const (
	// MaximumReferenceEvidence is how many exact rows of one page get a
	// preview. The rows past it are counted in `evidence_trimmed`, so a page
	// never reads as fully previewed when it was not.
	MaximumReferenceEvidence = 5
	// ReferenceEvidenceContextLines is how far a preview reaches on either side
	// of the row's `start_line`, without leaving the callable that holds it.
	ReferenceEvidenceContextLines = 4
	// Keep whole lines and the anchor, even when earlier context is oversized.
	MaximumReferenceEvidenceBytes = 1536
)

// ReferencePreview shows verified code around a reference row's declaration.
//
// It shows the declaration the row names, never where the subject is used: the
// snapshot records which symbol contains a reference and not where inside it,
// so the window hangs from the row's `start_line` and nothing here says the
// subject occurs in it. The enclosing callable comes from the same position
// resolver as get_source's `line`, which refuses a file that changed, a line no
// indexed function contains and two functions that tie.
type ReferencePreview struct {
	Profiles          ProfileNames `json:"profile,omitempty"`
	Repository        string       `json:"repository"`
	Path              string       `json:"path"`
	QualifiedName     string       `json:"qualified_name,omitempty"`
	StartLine         uint32       `json:"start_line"`
	ShownStartLine    uint32       `json:"shown_start_line,omitempty"`
	ShownEndLine      uint32       `json:"shown_end_line,omitempty"`
	CallableStartLine uint32       `json:"callable_start_line,omitempty"`
	CallableEndLine   uint32       `json:"callable_end_line,omitempty"`
	Code              string       `json:"code,omitempty"`
	// Unavailable says why this row carries no code. One stale file does not
	// invalidate the previews of the other rows of the same page.
	Unavailable string `json:"unavailable,omitempty"`
}

// evidenceSource is one generation the previews of a page are read from: the
// snapshot, the files already verified against it and the callable ranges
// already indexed, each paid for once per page.
type evidenceSource struct {
	snapshot *hotsnapshot.GraphSnapshot
	// Previews stay bound to this snapshot even if the store publishes another.
	publication string
	positions   positionFiles
	cache       map[string]*sourceFile
}

func newEvidenceSource(snapshot *hotsnapshot.GraphSnapshot) *evidenceSource {
	return &evidenceSource{snapshot: snapshot, publication: publicationToken(snapshot.Metadata()), cache: map[string]*sourceFile{}}
}

// A subject is already resolved. Read its own range, not an enclosing callable.
func (source *evidenceSource) definition(ctx context.Context, id hotsnapshot.SymbolID, subject ReferenceSubject) *ReferencePreview {
	if ctx.Err() != nil {
		return nil
	}
	symbol, ok := source.snapshot.Symbol(id)
	if !ok || symbol.StartLine == 0 || symbol.EndLine < symbol.StartLine || symbol.EndLine-symbol.StartLine > MaximumReferenceEvidenceBytes {
		return nil
	}
	location, err := resolveSymbolLocation(source.snapshot, symbol)
	if err != nil {
		return nil
	}
	key := location.RepositoryPath + "\x00" + location.FilePath
	file := source.cache[key]
	if file == nil {
		record, ok := source.snapshot.File(symbol.File)
		if !ok {
			return nil
		}
		file = loadSourceFile(location.RepositoryPath, location.FilePath, record.ContentDigest)
		source.cache[key] = file
	}
	if ctx.Err() != nil || !file.fresh || file.err != nil || uint64(symbol.EndLine) > uint64(file.physicalLines()) {
		return nil
	}
	lines := file.lines[symbol.StartLine-1 : symbol.EndLine]
	if !fitsReferencePreview(lines) {
		return nil
	}
	return &ReferencePreview{
		Repository: subject.Repository, Path: subject.FilePath, QualifiedName: subject.QualifiedName,
		StartLine: symbol.StartLine, ShownStartLine: symbol.StartLine, ShownEndLine: symbol.EndLine, Code: strings.Join(lines, "\n"),
	}
}

// referenceEvidence previews the first MaximumReferenceEvidence distinct exact
// rows of a page, in page order, and counts the ones it left without.
//
// Only an exact row has a proven holder to show: previewing a candidate or an
// unresolved one would pass a guess off as a fact. A holder that reaches the
// subject through several edge kinds is one row of code, not several.
//
// `sources` is keyed by profile name, and by "" when the page names none.
func referenceEvidence(ctx context.Context, rows []ReferenceSummary, sources map[string]*evidenceSource) ([]ReferencePreview, int) {
	var previews []ReferencePreview
	trimmed := 0
	seen := make(map[string]struct{})
	for _, row := range rows {
		if !facts.Confidence(row.Confidence).Exact() {
			continue
		}
		key := string(row.Profiles) + "\x00" + row.Repository + "\x00" + row.FilePath + "\x00" + strconv.FormatUint(uint64(row.StartLine), 10)
		if _, repeated := seen[key]; repeated {
			continue
		}
		seen[key] = struct{}{}
		if len(previews) == MaximumReferenceEvidence {
			trimmed++
			continue
		}
		previews = append(previews, previewReference(ctx, row, sources))
	}
	return previews, trimmed
}

// previewReference reads one row from the first of its profiles whose
// generation verifies it. A row several profiles share is identical in each,
// and the preview names the one that vouched for it.
func previewReference(ctx context.Context, row ReferenceSummary, sources map[string]*evidenceSource) ReferencePreview {
	unavailable := ReferencePreview{Profiles: row.Profiles, Repository: row.Repository, Path: row.FilePath, StartLine: row.StartLine}
	names := row.Profiles.ProfileNames()
	if len(names) == 0 {
		names = []string{""}
	}
	for _, name := range names {
		source := sources[name]
		if source == nil {
			unavailable.Unavailable = "no snapshot to read this profile from"
			continue
		}
		preview, err := source.preview(ctx, row)
		if err != nil {
			unavailable.Unavailable = evidenceReason(err)
			continue
		}
		if name != "" {
			preview.Profiles = profileNames(name)
		}
		return preview
	}
	return unavailable
}

func (source *evidenceSource) preview(ctx context.Context, row ReferenceSummary) (ReferencePreview, error) {
	line := row.StartLine
	request := SourceRequest{Repository: row.Repository, Line: &line, Publication: source.publication}
	holder, err := source.positions.resolve(ctx, source.snapshot, request, row.FilePath, source.cache)
	if err != nil {
		return ReferencePreview{}, err
	}
	symbol, found := source.snapshot.Symbol(holder)
	if !found {
		return ReferencePreview{}, errors.New("the generation has no record for the enclosing callable")
	}
	qualifiedName, named := source.snapshot.Strings().String(symbol.QualifiedName)
	location, err := resolveSymbolLocation(source.snapshot, symbol)
	if err != nil || !named {
		return ReferencePreview{}, errors.New("the generation records no name or location for the enclosing callable")
	}
	file := source.cache[location.RepositoryPath+"\x00"+location.FilePath]
	if file == nil || file.err != nil || !file.fresh {
		return ReferencePreview{}, errors.New("position source is not verified")
	}

	last := file.physicalLines()
	start, end := int(symbol.StartLine), int(symbol.EndLine)
	if int(line) > last || len(file.lines[line-1]) > MaximumReferenceEvidenceBytes {
		return ReferencePreview{}, fmt.Errorf("line %d is unavailable within the %d byte preview limit", line, MaximumReferenceEvidenceBytes)
	}
	lo, hi := start, end
	var partialStart, partialEnd uint32
	if end > last || !fitsReferencePreview(file.lines[start-1:end]) {
		partialStart, partialEnd = symbol.StartLine, symbol.EndLine
		from := max(start, int(line)-ReferenceEvidenceContextLines)
		to := min(end, int(line)+ReferenceEvidenceContextLines, last)
		size := len(file.lines[line-1])
		lo, hi = int(line), int(line)
		for lo > from && size+1+len(file.lines[lo-2]) <= MaximumReferenceEvidenceBytes {
			lo--
			size += 1 + len(file.lines[lo-1])
		}
		for hi < to && size+1+len(file.lines[hi]) <= MaximumReferenceEvidenceBytes {
			size += 1 + len(file.lines[hi])
			hi++
		}
	}
	return ReferencePreview{
		Repository:        row.Repository,
		Path:              row.FilePath,
		QualifiedName:     qualifiedName,
		StartLine:         line,
		ShownStartLine:    uint32(lo),
		ShownEndLine:      uint32(hi),
		CallableStartLine: partialStart,
		CallableEndLine:   partialEnd,
		Code:              strings.Join(file.lines[lo-1:hi], "\n"),
	}, nil
}

// Count the bytes strings.Join will emit, without allocating a discarded body.
func fitsReferencePreview(lines []string) bool {
	remaining := MaximumReferenceEvidenceBytes
	for i, text := range lines {
		if i != 0 {
			remaining--
		}
		if len(text) > remaining {
			return false
		}
		remaining -= len(text)
	}
	return true
}

// evidenceReason is the sentence a preview carries instead of code. A tool
// error keeps its message and drops its code: the code classifies a failed
// call, and here only one row failed.
func evidenceReason(err error) string {
	var toolError *ToolError
	if errors.As(err, &toolError) && toolError.Message != "" {
		return toolError.Message
	}
	return err.Error()
}
