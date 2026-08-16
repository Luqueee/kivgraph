// Package scipwire decodes the subset of the SCIP index format Kivgraph
// reads from `rust-analyzer scip`.
//
// The schema is pinned: scip.proto of github.com/scip-code/scip v0.9.0,
// sha256 04cb20f2b8be73f6c0376b5b3e84c3ae20ebaff0ad3d23ba2d16f866b395ed7d.
// Only the messages and fields listed in this file are read; everything else
// is skipped by wire type, so a newer indexer that adds fields still decodes.
//
// The decoder is written here rather than taken from the upstream Go bindings
// because Kivgraph reads six messages out of the schema and the bindings
// arrive with a formatter, a validator and their dependency trees. The wire
// format is fixed by protobuf itself, and every field number in this file is
// taken from the pinned schema.
package scipwire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// ErrMalformedIndex reports an index this decoder cannot read.
var ErrMalformedIndex = errors.New("malformed SCIP index")

// SymbolRole values, from the SymbolRole enum of the pinned schema. Only the
// roles rust-analyzer emits are named; the field keeps every bit it received.
const (
	// RoleDefinition marks the occurrence that defines its symbol.
	RoleDefinition int32 = 0x1
	// RoleImport, RoleWriteAccess and RoleReadAccess are part of the schema
	// and are never set by rust-analyzer. They are named so a reader can
	// tell an absent role from an unknown one.
	RoleImport      int32 = 0x2
	RoleWriteAccess int32 = 0x4
	RoleReadAccess  int32 = 0x8
)

// TextEncoding is the Metadata.text_document_encoding enum.
type TextEncoding int32

const (
	EncodingUnspecified TextEncoding = 0
	EncodingUTF8        TextEncoding = 1
	EncodingUTF16       TextEncoding = 2
)

// PositionEncoding is the Document.position_encoding enum.
type PositionEncoding int32

const (
	PositionUnspecified PositionEncoding = 0
	PositionUTF8        PositionEncoding = 1
	PositionUTF16       PositionEncoding = 2
	PositionUTF32       PositionEncoding = 3
)

// Index is one decoded SCIP document set.
type Index struct {
	ToolName        string
	ToolVersion     string
	ProjectRoot     string
	TextEncoding    TextEncoding
	Documents       []Document
	ExternalSymbols []SymbolInformation
}

// Document is one indexed source file, with paths relative to the project
// root the indexer was pointed at.
type Document struct {
	RelativePath     string
	Language         string
	PositionEncoding PositionEncoding
	Occurrences      []Occurrence
	Symbols          []SymbolInformation
}

// Occurrence is one span that mentions a symbol. Lines and characters are zero
// based; a character is a UTF-8 byte offset from the start of its line when
// the document says so.
type Occurrence struct {
	Symbol string
	Roles  int32
	Range  Range
	// EnclosingRange is the body of the definition this occurrence's symbol
	// has, when the indexer knew one in this same document.
	EnclosingRange Range
}

// Definition reports whether this occurrence defines its symbol.
func (occurrence Occurrence) Definition() bool {
	return occurrence.Roles&RoleDefinition != 0
}

// Range is a half-open span. Present distinguishes an absent range from one
// that starts at the beginning of a file.
type Range struct {
	StartLine      int32
	StartCharacter int32
	EndLine        int32
	EndCharacter   int32
	Present        bool
}

// Contains reports whether other falls inside this range.
func (span Range) Contains(other Range) bool {
	if !span.Present || !other.Present {
		return false
	}
	if other.StartLine < span.StartLine || other.EndLine > span.EndLine {
		return false
	}
	if other.StartLine == span.StartLine && other.StartCharacter < span.StartCharacter {
		return false
	}
	if other.EndLine == span.EndLine && other.EndCharacter > span.EndCharacter {
		return false
	}
	return true
}

// SymbolInformation is what the indexer knows about one symbol.
type SymbolInformation struct {
	Symbol string
	// Kind is the SymbolInformation.Kind enum value. It is kept as the
	// number the indexer sent: the vocabulary is large and grows, and the
	// reader that needs a name owns the mapping.
	Kind            int32
	DisplayName     string
	Signature       string
	EnclosingSymbol string
	Documentation   []string
}

// Decode reads an index. Unknown fields are skipped, malformed ones are
// refused: a partially read index would be a graph with holes nobody declared.
func Decode(data []byte) (Index, error) {
	var index Index
	err := eachField(data, func(field int32, value fieldValue) error {
		switch field {
		case 1:
			bytes, err := value.bytes()
			if err != nil {
				return err
			}
			return decodeMetadata(bytes, &index)
		case 2:
			bytes, err := value.bytes()
			if err != nil {
				return err
			}
			document, err := decodeDocument(bytes)
			if err != nil {
				return err
			}
			index.Documents = append(index.Documents, document)
			return nil
		case 3:
			bytes, err := value.bytes()
			if err != nil {
				return err
			}
			symbol, err := decodeSymbolInformation(bytes)
			if err != nil {
				return err
			}
			index.ExternalSymbols = append(index.ExternalSymbols, symbol)
			return nil
		}
		return nil
	})
	if err != nil {
		return Index{}, err
	}
	return index, nil
}

func decodeMetadata(data []byte, index *Index) error {
	return eachField(data, func(field int32, value fieldValue) error {
		switch field {
		case 2:
			bytes, err := value.bytes()
			if err != nil {
				return err
			}
			return eachField(bytes, func(toolField int32, toolValue fieldValue) error {
				switch toolField {
				case 1:
					name, err := toolValue.text()
					if err != nil {
						return err
					}
					index.ToolName = name
				case 2:
					version, err := toolValue.text()
					if err != nil {
						return err
					}
					index.ToolVersion = version
				}
				return nil
			})
		case 3:
			root, err := value.text()
			if err != nil {
				return err
			}
			index.ProjectRoot = root
		case 4:
			encoding, err := value.number()
			if err != nil {
				return err
			}
			index.TextEncoding = TextEncoding(encoding)
		}
		return nil
	})
}

func decodeDocument(data []byte) (Document, error) {
	var document Document
	err := eachField(data, func(field int32, value fieldValue) error {
		switch field {
		case 1:
			path, err := value.text()
			if err != nil {
				return err
			}
			document.RelativePath = path
		case 2:
			bytes, err := value.bytes()
			if err != nil {
				return err
			}
			occurrence, err := decodeOccurrence(bytes)
			if err != nil {
				return err
			}
			document.Occurrences = append(document.Occurrences, occurrence)
		case 3:
			bytes, err := value.bytes()
			if err != nil {
				return err
			}
			symbol, err := decodeSymbolInformation(bytes)
			if err != nil {
				return err
			}
			document.Symbols = append(document.Symbols, symbol)
		case 4:
			language, err := value.text()
			if err != nil {
				return err
			}
			document.Language = language
		case 6:
			encoding, err := value.number()
			if err != nil {
				return err
			}
			document.PositionEncoding = PositionEncoding(encoding)
		}
		return nil
	})
	if err != nil {
		return Document{}, err
	}
	return document, nil
}

func decodeOccurrence(data []byte) (Occurrence, error) {
	var occurrence Occurrence
	err := eachField(data, func(field int32, value fieldValue) error {
		switch field {
		case 1:
			span, err := packedRange(value)
			if err != nil {
				return err
			}
			occurrence.Range = span
		case 2:
			symbol, err := value.text()
			if err != nil {
				return err
			}
			occurrence.Symbol = symbol
		case 3:
			roles, err := value.number()
			if err != nil {
				return err
			}
			occurrence.Roles = int32(roles)
		case 7:
			span, err := packedRange(value)
			if err != nil {
				return err
			}
			occurrence.EnclosingRange = span
		case 8, 9, 10, 11:
			bytes, err := value.bytes()
			if err != nil {
				return err
			}
			span, err := typedRange(bytes, field == 8 || field == 10)
			if err != nil {
				return err
			}
			if field == 8 || field == 9 {
				occurrence.Range = span
			} else {
				occurrence.EnclosingRange = span
			}
		}
		return nil
	})
	if err != nil {
		return Occurrence{}, err
	}
	return occurrence, nil
}

func decodeSymbolInformation(data []byte) (SymbolInformation, error) {
	var symbol SymbolInformation
	err := eachField(data, func(field int32, value fieldValue) error {
		switch field {
		case 1:
			name, err := value.text()
			if err != nil {
				return err
			}
			symbol.Symbol = name
		case 3:
			documentation, err := value.text()
			if err != nil {
				return err
			}
			symbol.Documentation = append(symbol.Documentation, documentation)
		case 5:
			kind, err := value.number()
			if err != nil {
				return err
			}
			symbol.Kind = int32(kind)
		case 6:
			name, err := value.text()
			if err != nil {
				return err
			}
			symbol.DisplayName = name
		case 7:
			// Signature reuses the field numbers of Document for wire
			// compatibility, so the text is field 5 of the nested message.
			bytes, err := value.bytes()
			if err != nil {
				return err
			}
			return eachField(bytes, func(signatureField int32, signatureValue fieldValue) error {
				if signatureField != 5 {
					return nil
				}
				text, err := signatureValue.text()
				if err != nil {
					return err
				}
				symbol.Signature = text
				return nil
			})
		case 8:
			enclosing, err := value.text()
			if err != nil {
				return err
			}
			symbol.EnclosingSymbol = enclosing
		}
		return nil
	})
	if err != nil {
		return SymbolInformation{}, err
	}
	return symbol, nil
}

// packedRange reads the deprecated `repeated int32` range encoding, which is
// what rust-analyzer writes: three numbers on one line, four across lines.
func packedRange(value fieldValue) (Range, error) {
	numbers, err := value.numbers()
	if err != nil {
		return Range{}, err
	}
	switch len(numbers) {
	case 0:
		return Range{}, nil
	case 3:
		return Range{
			StartLine: int32(numbers[0]), StartCharacter: int32(numbers[1]),
			EndLine: int32(numbers[0]), EndCharacter: int32(numbers[2]),
			Present: true,
		}, nil
	case 4:
		return Range{
			StartLine: int32(numbers[0]), StartCharacter: int32(numbers[1]),
			EndLine: int32(numbers[2]), EndCharacter: int32(numbers[3]),
			Present: true,
		}, nil
	default:
		return Range{}, fmt.Errorf("%w: range has %d values, want 3 or 4", ErrMalformedIndex, len(numbers))
	}
}

// typedRange reads the SingleLineRange and MultiLineRange messages a newer
// indexer may write instead of the packed form.
func typedRange(data []byte, singleLine bool) (Range, error) {
	span := Range{Present: true}
	err := eachField(data, func(field int32, value fieldValue) error {
		number, err := value.number()
		if err != nil {
			return err
		}
		switch {
		case singleLine && field == 1:
			span.StartLine, span.EndLine = int32(number), int32(number)
		case singleLine && field == 2:
			span.StartCharacter = int32(number)
		case singleLine && field == 3:
			span.EndCharacter = int32(number)
		case !singleLine && field == 1:
			span.StartLine = int32(number)
		case !singleLine && field == 2:
			span.StartCharacter = int32(number)
		case !singleLine && field == 3:
			span.EndLine = int32(number)
		case !singleLine && field == 4:
			span.EndCharacter = int32(number)
		}
		return nil
	})
	if err != nil {
		return Range{}, err
	}
	return span, nil
}

// fieldValue is one decoded protobuf field, still in its wire form.
type fieldValue struct {
	wire    int32
	varint  uint64
	payload []byte
}

func (value fieldValue) text() (string, error) {
	bytes, err := value.bytes()
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func (value fieldValue) bytes() ([]byte, error) {
	if value.wire != 2 {
		return nil, fmt.Errorf("%w: expected a length delimited field, got wire type %d", ErrMalformedIndex, value.wire)
	}
	return value.payload, nil
}

func (value fieldValue) number() (int64, error) {
	if value.wire != 0 {
		return 0, fmt.Errorf("%w: expected a varint field, got wire type %d", ErrMalformedIndex, value.wire)
	}
	return int64(value.varint), nil
}

// numbers reads a repeated int32 field in either encoding: packed inside one
// length delimited field, or one varint per occurrence.
func (value fieldValue) numbers() ([]int64, error) {
	if value.wire == 0 {
		return []int64{int64(value.varint)}, nil
	}
	data, err := value.bytes()
	if err != nil {
		return nil, err
	}
	numbers := make([]int64, 0, 4)
	for offset := 0; offset < len(data); {
		number, read := binary.Uvarint(data[offset:])
		if read <= 0 {
			return nil, fmt.Errorf("%w: truncated packed varint", ErrMalformedIndex)
		}
		numbers = append(numbers, int64(int32(number)))
		offset += read
	}
	return numbers, nil
}

// eachField walks one protobuf message, calling visit for every field and
// skipping the ones the caller ignores.
func eachField(data []byte, visit func(field int32, value fieldValue) error) error {
	for offset := 0; offset < len(data); {
		tag, read := binary.Uvarint(data[offset:])
		if read <= 0 {
			return fmt.Errorf("%w: truncated field tag", ErrMalformedIndex)
		}
		offset += read
		field := int32(tag >> 3)
		wire := int32(tag & 0x7)
		if field <= 0 {
			return fmt.Errorf("%w: invalid field number %d", ErrMalformedIndex, field)
		}
		value := fieldValue{wire: wire}
		switch wire {
		case 0:
			number, read := binary.Uvarint(data[offset:])
			if read <= 0 {
				return fmt.Errorf("%w: truncated varint in field %d", ErrMalformedIndex, field)
			}
			value.varint = number
			offset += read
		case 1:
			if len(data)-offset < 8 {
				return fmt.Errorf("%w: truncated 64 bit field %d", ErrMalformedIndex, field)
			}
			offset += 8
		case 2:
			length, read := binary.Uvarint(data[offset:])
			if read <= 0 {
				return fmt.Errorf("%w: truncated length in field %d", ErrMalformedIndex, field)
			}
			offset += read
			if length > math.MaxInt32 || int(length) > len(data)-offset {
				return fmt.Errorf("%w: field %d claims %d bytes, %d remain", ErrMalformedIndex, field, length, len(data)-offset)
			}
			value.payload = data[offset : offset+int(length)]
			offset += int(length)
		case 5:
			if len(data)-offset < 4 {
				return fmt.Errorf("%w: truncated 32 bit field %d", ErrMalformedIndex, field)
			}
			offset += 4
		default:
			return fmt.Errorf("%w: unsupported wire type %d in field %d", ErrMalformedIndex, wire, field)
		}
		if err := visit(field, value); err != nil {
			return err
		}
	}
	return nil
}
