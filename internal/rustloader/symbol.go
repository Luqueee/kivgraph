package rustloader

import (
	"fmt"
	"strings"
)

// DescriptorSuffix is the grammar element that says what a descriptor names.
// The suffix is part of the symbol string itself, so consumer and provider
// always agree on it even when they classify the symbol differently.
type DescriptorSuffix string

const (
	SuffixNamespace     DescriptorSuffix = "namespace"
	SuffixType          DescriptorSuffix = "type"
	SuffixTerm          DescriptorSuffix = "term"
	SuffixMethod        DescriptorSuffix = "method"
	SuffixTypeParameter DescriptorSuffix = "type_parameter"
	SuffixParameter     DescriptorSuffix = "parameter"
	SuffixMeta          DescriptorSuffix = "meta"
	SuffixMacro         DescriptorSuffix = "macro"
)

// Descriptor is one step of a SCIP symbol path.
type Descriptor struct {
	Name          string
	Suffix        DescriptorSuffix
	Disambiguator string
}

// CrateRef is the crate a symbol belongs to, as the analyzer named it.
type CrateRef struct {
	Name string
	// Version is `.` when the analyzer did not know one, and a URL for the
	// crates that ship with the toolchain.
	Version string
}

// Known reports whether the crate carries a version that identifies code.
func (crate CrateRef) Known() bool {
	version := strings.TrimSpace(crate.Version)
	return crate.Name != "" && version != "" && version != unknownCrateVersion
}

// SymbolIdentity is a parsed SCIP symbol string.
type SymbolIdentity struct {
	// Raw is the string the analyzer emitted. It is what a consumer and a
	// provider compare when they are indexed separately.
	Raw string
	// Local marks a symbol scoped to one document. A local has no durable
	// identity and never becomes a node of the graph.
	Local       bool
	Scheme      string
	Manager     string
	Crate       CrateRef
	Descriptors []Descriptor
}

// QualifiedName renders the descriptor path the way Rust spells a path.
func (identity SymbolIdentity) QualifiedName() string {
	parts := make([]string, 0, len(identity.Descriptors))
	for _, descriptor := range identity.Descriptors {
		name := descriptor.Name
		if descriptor.Disambiguator != "" {
			name += "(" + descriptor.Disambiguator + ")"
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, "::")
}

// Kind is the suffix of the last descriptor: the part of the identity that
// distinguishes a type from a term of the same name.
func (identity SymbolIdentity) Kind() DescriptorSuffix {
	if len(identity.Descriptors) == 0 {
		return ""
	}
	return identity.Descriptors[len(identity.Descriptors)-1].Suffix
}

// Name is the last descriptor name, which is what the declaration is called.
func (identity SymbolIdentity) Name() string {
	if len(identity.Descriptors) == 0 {
		return ""
	}
	return identity.Descriptors[len(identity.Descriptors)-1].Name
}

// Addressable reports whether the symbol can be a node of the graph: it needs
// a crate, a descriptor path and no document-local identity.
func (identity SymbolIdentity) Addressable() bool {
	return !identity.Local && identity.Crate.Name != "" && len(identity.Descriptors) != 0
}

// ParseSymbol reads a SCIP symbol string.
//
// The grammar is the one in the pinned schema: scheme, package manager,
// package name and version separated by spaces, then a descriptor path whose
// suffix characters say what each step names. A document-local symbol is
// spelled `local <n>` and is reported as such rather than rejected: the
// analyzer emits one for every binding inside a function body.
func ParseSymbol(raw string) (SymbolIdentity, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return SymbolIdentity{}, fmt.Errorf("symbol is empty")
	}
	if strings.HasPrefix(value, "local ") {
		return SymbolIdentity{Raw: value, Local: true}, nil
	}
	fields := strings.SplitN(value, " ", 5)
	if len(fields) != 5 {
		return SymbolIdentity{}, fmt.Errorf("symbol %q has %d space separated parts, want scheme, manager, name, version and descriptors", raw, len(fields))
	}
	descriptors, err := parseDescriptors(fields[4])
	if err != nil {
		return SymbolIdentity{}, fmt.Errorf("symbol %q: %w", raw, err)
	}
	return SymbolIdentity{
		Raw:         value,
		Scheme:      fields[0],
		Manager:     fields[1],
		Crate:       CrateRef{Name: fields[2], Version: fields[3]},
		Descriptors: descriptors,
	}, nil
}

// parseDescriptors reads the descriptor path of a symbol.
func parseDescriptors(path string) ([]Descriptor, error) {
	descriptors := make([]Descriptor, 0, 4)
	for offset := 0; offset < len(path); {
		switch path[offset] {
		case '[':
			name, next, err := readEnclosed(path, offset, '[', ']')
			if err != nil {
				return nil, err
			}
			descriptors = append(descriptors, Descriptor{Name: name, Suffix: SuffixTypeParameter})
			offset = next
			continue
		case '(':
			name, next, err := readEnclosed(path, offset, '(', ')')
			if err != nil {
				return nil, err
			}
			descriptors = append(descriptors, Descriptor{Name: name, Suffix: SuffixParameter})
			offset = next
			continue
		}
		name, next, err := readName(path, offset)
		if err != nil {
			return nil, err
		}
		if next >= len(path) {
			return nil, fmt.Errorf("descriptor %q has no suffix", name)
		}
		switch path[next] {
		case '/':
			descriptors = append(descriptors, Descriptor{Name: name, Suffix: SuffixNamespace})
			offset = next + 1
		case '#':
			descriptors = append(descriptors, Descriptor{Name: name, Suffix: SuffixType})
			offset = next + 1
		case '.':
			descriptors = append(descriptors, Descriptor{Name: name, Suffix: SuffixTerm})
			offset = next + 1
		case ':':
			descriptors = append(descriptors, Descriptor{Name: name, Suffix: SuffixMeta})
			offset = next + 1
		case '!':
			descriptors = append(descriptors, Descriptor{Name: name, Suffix: SuffixMacro})
			offset = next + 1
		case '(':
			disambiguator, afterParen, err := readEnclosed(path, next, '(', ')')
			if err != nil {
				return nil, err
			}
			if afterParen >= len(path) || path[afterParen] != '.' {
				return nil, fmt.Errorf("method descriptor %q is not terminated by a dot", name)
			}
			descriptors = append(descriptors, Descriptor{
				Name: name, Suffix: SuffixMethod, Disambiguator: disambiguator,
			})
			offset = afterParen + 1
		default:
			return nil, fmt.Errorf("descriptor %q has an unknown suffix %q", name, string(path[next]))
		}
	}
	if len(descriptors) == 0 {
		return nil, fmt.Errorf("symbol has no descriptors")
	}
	return descriptors, nil
}

// readName reads one descriptor name, which is either plain text up to its
// suffix character or a backtick quoted run in which a backtick is doubled.
func readName(path string, offset int) (string, int, error) {
	if path[offset] == '`' {
		var name strings.Builder
		for index := offset + 1; index < len(path); index++ {
			if path[index] != '`' {
				name.WriteByte(path[index])
				continue
			}
			if index+1 < len(path) && path[index+1] == '`' {
				name.WriteByte('`')
				index++
				continue
			}
			return name.String(), index + 1, nil
		}
		return "", 0, fmt.Errorf("unterminated escaped descriptor name")
	}
	for index := offset; index < len(path); index++ {
		switch path[index] {
		case '/', '#', '.', ':', '!', '(', ')', '[', ']':
			if index == offset {
				return "", 0, fmt.Errorf("descriptor name is empty before %q", string(path[index]))
			}
			return path[offset:index], index, nil
		}
	}
	return path[offset:], len(path), nil
}

// readEnclosed reads a bracketed run, honouring backtick escaping inside it.
func readEnclosed(path string, offset int, open, close byte) (string, int, error) {
	if path[offset] != open {
		return "", 0, fmt.Errorf("expected %q", string(open))
	}
	var value strings.Builder
	for index := offset + 1; index < len(path); index++ {
		switch path[index] {
		case '`':
			name, next, err := readName(path, index)
			if err != nil {
				return "", 0, err
			}
			value.WriteString(name)
			index = next - 1
		case close:
			return value.String(), index + 1, nil
		default:
			value.WriteByte(path[index])
		}
	}
	return "", 0, fmt.Errorf("unterminated %q group", string(open))
}
