package rustloader

import "strings"

// SCIP SymbolInformation.Kind values, from the pinned schema. Only the ones
// rust-analyzer's symbol_kind() can produce are named: a number this build
// does not know falls back to the descriptor suffix rather than being renamed
// into something the analyzer never said.
const (
	scipKindAssociatedType int32 = 3
	scipKindAttribute      int32 = 4
	scipKindConstant       int32 = 8
	scipKindEnum           int32 = 11
	scipKindEnumMember     int32 = 12
	scipKindField          int32 = 15
	scipKindFunction       int32 = 17
	scipKindMacro          int32 = 25
	scipKindMethod         int32 = 26
	scipKindModule         int32 = 29
	scipKindParameter      int32 = 37
	scipKindSelfParameter  int32 = 44
	scipKindStruct         int32 = 49
	scipKindTrait          int32 = 53
	scipKindType           int32 = 54
	scipKindTypeAlias      int32 = 55
	scipKindTypeParameter  int32 = 58
	scipKindUnion          int32 = 59
	scipKindVariable       int32 = 61
	scipKindTraitMethod    int32 = 70
	scipKindStaticMethod   int32 = 80
	scipKindStaticVariable int32 = 82
)

var scipKindNames = map[int32]string{
	scipKindAssociatedType: "associated_type",
	scipKindAttribute:      "attribute",
	scipKindConstant:       "constant",
	scipKindEnum:           "enum",
	scipKindEnumMember:     "enum_member",
	scipKindField:          "field",
	scipKindFunction:       "function",
	scipKindMacro:          "macro",
	scipKindMethod:         "method",
	scipKindModule:         "module",
	scipKindParameter:      "parameter",
	scipKindSelfParameter:  "self_parameter",
	scipKindStruct:         "struct",
	scipKindTrait:          "trait",
	scipKindType:           "type",
	scipKindTypeAlias:      "type_alias",
	scipKindTypeParameter:  "type_parameter",
	scipKindUnion:          "union",
	scipKindVariable:       "variable",
	scipKindTraitMethod:    "trait_method",
	scipKindStaticMethod:   "static_method",
	scipKindStaticVariable: "static",
}

// PublishedKind answers the kind a reader sees.
//
// It is not the kind the stable key uses: the key takes the descriptor suffix,
// which travels inside the symbol string and therefore cannot disagree between
// a consumer and its provider. This one is finer -- a struct, an enum and a
// type alias all carry the `#` suffix -- and it exists because `type Value` is
// a useless answer to "what is this".
//
// It used to answer "implementation" for an `impl` block, and that branch could
// not run. Not publishing an impl block is a tested contract, older than this
// change: TestAnalyzeDeclaresTheImplementationBlockItCannotDefine asserts that
// `shapes::impl::Circle` lands in Unresolved with DEFINITION_NOT_INDEXED, and
// that no in-repository reference names a key the pass does not publish. So a
// kind for a symbol the loader never creates was unreachable by construction,
// not merely unobserved -- and nothing covered the branch itself. On kena, with
// 3063 Rust symbols across two workspaces, `find_symbol` with
// `kind=implementation` answers zero. LUQUE-2008.
//
// The consequence is declared rather than hidden: a reference whose target is an
// impl header stays UNRESOLVED, because there is no symbol to point it at. That
// is 56 of the 1969 unresolved Rust references on kena, the same shape as a
// reference into the sysroot -- the declaration lives outside what this graph
// publishes. Members of those blocks are indexed normally, which is why
// `error::impl::ApiError::with_context_header` is a row and `error::impl::ApiError`
// is not.
func PublishedKind(identity SymbolIdentity, scipKind int32) string {
	if name, known := scipKindNames[scipKind]; known {
		return name
	}
	return string(identity.Kind())
}

// PublishedName answers the name a reader sees.
func PublishedName(identity SymbolIdentity, displayName string) string {
	if trimmed := strings.TrimSpace(displayName); trimmed != "" {
		return trimmed
	}
	return identity.Name()
}

// implementedTrait answers the trait an implementation member belongs to, when
// the symbol says it does. `impl#[Circle][Named]name().` is a trait method;
// `impl#[Circle]new().` is an inherent one and belongs to no trait.
func implementedTrait(identity SymbolIdentity) string {
	parameters := make([]string, 0, 2)
	seenImpl := false
	for _, descriptor := range identity.Descriptors {
		switch {
		case descriptor.Suffix == SuffixType && descriptor.Name == "impl":
			seenImpl = true
		case seenImpl && descriptor.Suffix == SuffixTypeParameter:
			parameters = append(parameters, descriptor.Name)
		}
	}
	if !seenImpl || len(parameters) != 2 {
		return ""
	}
	return parameters[1]
}
