package scip

import (
	"fmt"
	"strings"

	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
)

// symbolIdentity is a parsed SCIP symbol string.
//
// The grammar is fixed by the format, not by a producer:
//
//	<scheme> <manager> <package-name> <version> <descriptors>
//
// The first four are space separated and may be escaped; the rest of the
// string is the descriptor path. `local <id>` is the one shape that does not
// follow it, and it names something with no address outside its own file.
type symbolIdentity struct {
	scheme      string
	manager     string
	pkg         string
	version     string
	descriptors string
	local       bool
}

// addressable reports whether a SCIP symbol names something the graph can hold
// a node for.
//
// Two shapes are excluded. `local 0` is a parameter or a local variable: it has
// no identity outside its file, and Go and Rust already exclude the same class.
// The other is a bare namespace path -- `com/`, `com/example/`, `System/` --
// which producers emit once per segment of a package or using directive.
// Naming a namespace is not using a symbol.
//
// The namespace test is on the descriptors, and it has to be: the first
// version tested `package == "." && version == "."`, which is what scip-java
// writes on those qualifier occurrences. It is also what scip-dotnet writes on
// **every symbol the project declares**, so that rule dropped an entire
// language. A descriptor path says what a symbol is; the package coordinates
// say where it came from, and only one of those is the question here.
func addressable(symbol string) bool {
	identity, err := parseSymbol(symbol)
	if err != nil || identity.local {
		return false
	}
	descriptors := strings.TrimSpace(identity.descriptors)
	if descriptors == "" {
		return false
	}
	segments := splitDescriptors(descriptors)
	// A parameter is not a node. scip-java writes one as `local N`, which the
	// check above already drops; scip-dotnet writes
	// `Coverage/Catalog#Add().(shape)`, a fully qualified symbol. Without this
	// the same concept would be a graph node in one language and absent in
	// another for no reason but the producer, and a C# repository would carry
	// a node per parameter -- six of thirty-seven symbols in the fixture.
	if _, suffix, _ := descriptorParts(segments[len(segments)-1]); suffix == descriptorParameter {
		return false
	}
	for _, segment := range segments {
		if _, suffix, _ := descriptorParts(segment); suffix != descriptorNamespace {
			return true
		}
	}
	return false
}

func parseSymbol(symbol string) (symbolIdentity, error) {
	trimmed := strings.TrimSpace(symbol)
	if trimmed == "" {
		return symbolIdentity{}, fmt.Errorf("scip: empty symbol")
	}
	if strings.HasPrefix(trimmed, "local ") {
		return symbolIdentity{local: true, descriptors: strings.TrimPrefix(trimmed, "local ")}, nil
	}
	fields, rest := splitEscaped(trimmed, 4)
	if len(fields) < 4 {
		return symbolIdentity{}, fmt.Errorf("scip: symbol %q has no descriptors", symbol)
	}
	return symbolIdentity{
		scheme:      fields[0],
		manager:     fields[1],
		pkg:         fields[2],
		version:     fields[3],
		descriptors: rest,
	}, nil
}

// splitEscaped takes the first n space-separated fields and returns the
// remainder untouched. A field may contain a space when it is backtick
// escaped, which is how the format allows a package name to carry one.
func splitEscaped(value string, n int) ([]string, string) {
	fields := make([]string, 0, n)
	index := 0
	for len(fields) < n && index < len(value) {
		if value[index] == '`' {
			closing := strings.IndexByte(value[index+1:], '`')
			if closing < 0 {
				break
			}
			fields = append(fields, value[index+1:index+1+closing])
			index += closing + 2
			if index < len(value) && value[index] == ' ' {
				index++
			}
			continue
		}
		space := strings.IndexByte(value[index:], ' ')
		if space < 0 {
			fields = append(fields, value[index:])
			index = len(value)
			break
		}
		fields = append(fields, value[index:index+space])
		index += space + 1
	}
	return fields, value[min(index, len(value)):]
}

// qualifiedName is the descriptor path in the spelling a reader expects:
// `com/example/basic/Service#greet().` becomes
// `com.example.basic.Service.greet`.
//
// The suffixes are dropped because they are grammar, not name -- `#` for a
// type, `.` for a term, `().` for a method -- but the disambiguator of an
// overload is kept: `greet(+1).` and `greet().` are two declarations, and a
// qualified name that folded them would give one stable key to both.
func (identity symbolIdentity) qualifiedName() string {
	descriptors := strings.TrimSpace(identity.descriptors)
	if descriptors == "" {
		return ""
	}
	var parts []string
	for _, segment := range splitDescriptors(descriptors) {
		name, suffix, disambiguator := descriptorParts(segment)
		if name == "" {
			continue
		}
		if suffix == descriptorMethod && disambiguator != "" {
			name += "(" + disambiguator + ")"
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, ".")
}

// name is the last descriptor, for a producer that did not send a display
// name.
func (identity symbolIdentity) name() string {
	segments := splitDescriptors(strings.TrimSpace(identity.descriptors))
	if len(segments) == 0 {
		return ""
	}
	name, _, _ := descriptorParts(segments[len(segments)-1])
	return name
}

type descriptorSuffix uint8

const (
	descriptorUnknown descriptorSuffix = iota
	descriptorNamespace
	descriptorType
	descriptorTerm
	descriptorMethod
	descriptorTypeParameter
	descriptorParameter
	descriptorMeta
)

// splitDescriptors cuts a descriptor path into its segments. It cannot be a
// Split on a separator: every suffix character is also legal inside a backtick
// escaped name, and `/` ends a namespace while `#` ends a type.
func splitDescriptors(value string) []string {
	var segments []string
	start := 0
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '`':
			closing := strings.IndexByte(value[index+1:], '`')
			if closing < 0 {
				index = len(value)
				continue
			}
			index += closing + 1
		case '/', '#':
			segments = append(segments, value[start:index+1])
			start = index + 1
		case '.':
			// A method ends `().`; a term ends `.`. Either way the segment
			// stops here, and `()` was consumed by the parenthesis case.
			segments = append(segments, value[start:index+1])
			start = index + 1
		case '(':
			closing := strings.IndexByte(value[index:], ')')
			if closing < 0 {
				index = len(value)
				continue
			}
			index += closing
		case ')':
			// A type parameter `[T]` and a parameter `(x)` close their own
			// segment.
			segments = append(segments, value[start:index+1])
			start = index + 1
		case ']':
			segments = append(segments, value[start:index+1])
			start = index + 1
		}
	}
	if start < len(value) {
		segments = append(segments, value[start:])
	}
	return segments
}

// descriptorParts splits one segment into its name, what the suffix says it
// is, and the disambiguator an overload carries.
func descriptorParts(segment string) (string, descriptorSuffix, string) {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return "", descriptorUnknown, ""
	}
	suffix := descriptorUnknown
	body := segment
	switch {
	case strings.HasSuffix(segment, "/"):
		suffix, body = descriptorNamespace, strings.TrimSuffix(segment, "/")
	case strings.HasSuffix(segment, "#"):
		suffix, body = descriptorType, strings.TrimSuffix(segment, "#")
	case strings.HasSuffix(segment, ")."):
		suffix, body = descriptorMethod, strings.TrimSuffix(segment, ".")
	case strings.HasSuffix(segment, "."):
		suffix, body = descriptorTerm, strings.TrimSuffix(segment, ".")
	case strings.HasSuffix(segment, "]"):
		suffix, body = descriptorTypeParameter, strings.Trim(segment, "[]")
	case strings.HasSuffix(segment, ")"):
		suffix, body = descriptorParameter, strings.Trim(segment, "()")
	case strings.HasSuffix(segment, ":"):
		suffix, body = descriptorMeta, strings.TrimSuffix(segment, ":")
	}
	disambiguator := ""
	if suffix == descriptorMethod {
		if open := strings.LastIndexByte(body, '('); open >= 0 {
			disambiguator = strings.TrimSuffix(body[open+1:], ")")
			body = body[:open]
		}
	}
	return unescape(body), suffix, disambiguator
}

func unescape(value string) string {
	if !strings.HasPrefix(value, "`") || !strings.HasSuffix(value, "`") || len(value) < 2 {
		return value
	}
	return strings.ReplaceAll(value[1:len(value)-1], "``", "`")
}

// kindName is what the graph calls a declaration.
//
// SymbolInformation.Kind is the producer's own classification and it is rich
// -- scip-java tells a static method from an abstract one. The graph does not
// need that resolution and would have to agree on a number with every future
// producer, so the descriptor suffix decides, and the numeric kind only
// refines what the suffix cannot say.
func kindName(kind int32, identity symbolIdentity) string {
	segments := splitDescriptors(strings.TrimSpace(identity.descriptors))
	if len(segments) == 0 {
		return "symbol"
	}
	_, suffix, _ := descriptorParts(segments[len(segments)-1])
	switch suffix {
	case descriptorNamespace:
		return "namespace"
	case descriptorType:
		if name, ok := scipKindNames[kind]; ok {
			return name
		}
		return "type"
	case descriptorMethod:
		if name, ok := scipKindNames[kind]; ok {
			return name
		}
		return "method"
	case descriptorTerm:
		if name, ok := scipKindNames[kind]; ok {
			return name
		}
		return "term"
	case descriptorTypeParameter:
		return "type_parameter"
	case descriptorParameter:
		return "parameter"
	case descriptorMeta:
		return "meta"
	default:
		return "symbol"
	}
}

// scipKindNames maps the SymbolInformation.Kind values a producer sets onto
// the vocabulary the graph already uses for other languages. Only the values
// observed from a real index are named; an unnamed one falls back to what the
// descriptor suffix says, which is never wrong, only coarse.
//
// The numbers are read from scip-java 0.12.3 over testdata/java/coverage, not
// from the schema: the enum is long, most of it is other languages', and a
// mapping written from the proto would claim coverage of values no producer
// here emits. Two are deliberately absent. Kind 0 is unspecified, which
// scip-java uses for locals, enum members and -- the surprising one -- a
// record: `record Point` arrives as kind 0 with the signature
// `public static final Point`, so the graph calls it a `type`. Naming it a
// class would be this package's inference, not the producer's fact.
var scipKindNames = map[int32]string{
	7:  "class",
	9:  "constructor",
	11: "enum",
	15: "field",
	21: "interface",
	26: "method",
	58: "type_parameter",
	66: "method",
	79: "field",
	80: "method",
}

// exported is whether the declaration is visible outside its own file.
//
// SCIP has no visibility field. What it does have is the fact that a symbol
// with a package and a version is addressable from outside the document at
// all -- a producer emits `local N` for everything that is not. So this is
// true for every parsed declaration, and the honest reading is "addressable",
// not "public": a package-private Java class is exported by this measure.
// Narrowing it would need the signature, which is the producer's prose.
func exported(info scipwire.SymbolInformation, identity symbolIdentity) bool {
	signature := strings.TrimSpace(info.Signature)
	if signature == "" {
		return !identity.local
	}
	return !strings.HasPrefix(signature, "private ")
}

// signature is the discriminator of the stable key, so it must be stable
// across runs and must not carry a newline: a producer that renders an
// annotation puts one in, and the key would then depend on formatting.
func signature(info scipwire.SymbolInformation, identity symbolIdentity) string {
	value := strings.TrimSpace(info.Signature)
	if value == "" {
		// Never empty: facts.SemanticTargetKey refuses an identity whose
		// discriminator is blank, and two overloads would otherwise derive
		// one key.
		return identity.descriptors
	}
	return strings.Join(strings.Fields(value), " ")
}
