package scip

import (
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
)

// offsetTable converts the line/character positions a SCIP index carries into
// byte offsets into the file.
//
// The graph stores byte offsets because that is what slices a file. SCIP
// stores a character column whose unit is declared per document, and the three
// units disagree the moment a line holds a character outside ASCII: for `é`,
// the next column is 1 in UTF-16 and UTF-32 and 2 in UTF-8. Getting this wrong
// is invisible on an ASCII corpus and shifts every position in a file with one
// accent, which is why it is converted here rather than assumed anywhere.
type offsetTable struct {
	contents []byte
	// lineStarts[i] is the byte offset where line i begins.
	lineStarts []int
	encoding   scipwire.PositionEncoding
}

func newOffsetTable(contents []byte, encoding scipwire.PositionEncoding) *offsetTable {
	table := &offsetTable{contents: contents, encoding: encoding}
	if len(contents) == 0 {
		return table
	}
	table.lineStarts = append(table.lineStarts, 0)
	for index, character := range contents {
		if character == '\n' {
			table.lineStarts = append(table.lineStarts, index+1)
		}
	}
	return table
}

func (table *offsetTable) size() int { return len(table.contents) }

func (table *offsetTable) lastLine() int {
	if len(table.lineStarts) == 0 {
		return 0
	}
	return len(table.lineStarts) - 1
}

// position is the byte offset of a line and character. It is zero when the
// file was not read, which every consumer already tolerates: a payload from a
// producer that reports no offsets is still a payload.
func (table *offsetTable) position(line, character int32) int {
	if len(table.lineStarts) == 0 || line < 0 {
		return 0
	}
	if int(line) >= len(table.lineStarts) {
		return len(table.contents)
	}
	start := table.lineStarts[line]
	end := len(table.contents)
	if int(line)+1 < len(table.lineStarts) {
		end = table.lineStarts[line+1]
	}
	return start + table.columnBytes(table.contents[start:end], character)
}

// columnBytes walks one line and answers how many bytes the requested column
// is from its start, in the unit the document declared.
func (table *offsetTable) columnBytes(line []byte, character int32) int {
	if character <= 0 {
		return 0
	}
	switch table.encoding {
	case scipwire.PositionUTF8:
		if int(character) > len(line) {
			return len(line)
		}
		return int(character)
	default:
		// UTF-16 is the SCIP default and what an LSP-derived producer emits;
		// UTF-32 counts whole code points. They differ only above the basic
		// plane, where a rune costs two UTF-16 units and one UTF-32 unit.
		//
		// Unspecified lands here, and that is measured rather than assumed:
		// scip-java 0.12.3 sets PositionEncoding to 0 on every document, and
		// on the line `return "olá " + model.name();` it reports `model` at
		// character 24. Counting UTF-8 bytes would put it at 25, because `á`
		// is two. TestUnspecifiedEncodingCountsCodeUnits holds that down.
		remaining := int(character)
		offset := 0
		for offset < len(line) && remaining > 0 {
			value, width := utf8.DecodeRune(line[offset:])
			cost := 1
			if table.encoding != scipwire.PositionUTF32 && value > 0xFFFF {
				cost = len(utf16.Encode([]rune{value}))
			}
			if cost > remaining {
				break
			}
			remaining -= cost
			offset += width
		}
		return offset
	}
}
