package rustloader

import "testing"

// TestParseSymbolReadsTheShapesRustAnalyzerEmits uses the exact strings the
// recorded index contains, including the sysroot crate whose version is a URL
// and the impl path whose type parameters are escaped.
func TestParseSymbolReadsTheShapesRustAnalyzerEmits(t *testing.T) {
	tests := map[string]struct {
		raw           string
		crate         CrateRef
		qualifiedName string
		kind          DescriptorSuffix
		name          string
		addressable   bool
	}{
		"crate root": {
			raw:           "rust-analyzer cargo engine 1.4.0 crate/",
			crate:         CrateRef{Name: "engine", Version: "1.4.0"},
			qualifiedName: "crate", kind: SuffixNamespace, name: "crate", addressable: true,
		},
		"function": {
			raw:           "rust-analyzer cargo engine 1.4.0 run().",
			crate:         CrateRef{Name: "engine", Version: "1.4.0"},
			qualifiedName: "run", kind: SuffixMethod, name: "run", addressable: true,
		},
		"type": {
			raw:           "rust-analyzer cargo support 1.4.0 Value#",
			crate:         CrateRef{Name: "support", Version: "1.4.0"},
			qualifiedName: "Value", kind: SuffixType, name: "Value", addressable: true,
		},
		"field": {
			raw:           "rust-analyzer cargo support 1.4.0 Value#inner.",
			crate:         CrateRef{Name: "support", Version: "1.4.0"},
			qualifiedName: "Value::inner", kind: SuffixTerm, name: "inner", addressable: true,
		},
		"sysroot impl method": {
			raw:           "rust-analyzer cargo core https://github.com/rust-lang/rust/library/core ops/arith/impl#[i32][`Mul<Self>`]mul().",
			crate:         CrateRef{Name: "core", Version: "https://github.com/rust-lang/rust/library/core"},
			qualifiedName: "ops::arith::impl::i32::Mul<Self>::mul", kind: SuffixMethod, name: "mul", addressable: true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			identity, err := ParseSymbol(test.raw)
			if err != nil {
				t.Fatalf("ParseSymbol() error = %v", err)
			}
			if identity.Crate != test.crate {
				t.Fatalf("crate = %#v, want %#v", identity.Crate, test.crate)
			}
			if got := identity.QualifiedName(); got != test.qualifiedName {
				t.Fatalf("qualified name = %q, want %q", got, test.qualifiedName)
			}
			if identity.Kind() != test.kind || identity.Name() != test.name {
				t.Fatalf("kind/name = %q/%q, want %q/%q", identity.Kind(), identity.Name(), test.kind, test.name)
			}
			if identity.Addressable() != test.addressable {
				t.Fatalf("addressable = %t", identity.Addressable())
			}
		})
	}
}

// TestParseSymbolMarksLocalsAsUnaddressable is the rule that keeps a document
// scoped counter out of the graph: `local 0` names a different binding in
// every file.
func TestParseSymbolMarksLocalsAsUnaddressable(t *testing.T) {
	identity, err := ParseSymbol("local 0")
	if err != nil {
		t.Fatalf("ParseSymbol() error = %v", err)
	}
	if !identity.Local || identity.Addressable() {
		t.Fatalf("identity = %#v", identity)
	}
}

func TestParseSymbolRejectsWhatItCannotIdentify(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":              "",
		"missing version":    "rust-analyzer cargo engine run().",
		"no descriptors":     "rust-analyzer cargo engine 1.4.0 ",
		"unknown suffix":     "rust-analyzer cargo engine 1.4.0 run$",
		"unterminated group": "rust-analyzer cargo engine 1.4.0 impl#[i32",
		"unterminated quote": "rust-analyzer cargo engine 1.4.0 `run",
		"method without dot": "rust-analyzer cargo engine 1.4.0 run()",
	} {
		t.Run(name, func(t *testing.T) {
			if identity, err := ParseSymbol(raw); err == nil {
				t.Fatalf("ParseSymbol(%q) = %#v, want an error", raw, identity)
			}
		})
	}
}

// TestKnownRejectsTheVersionTheAnalyzerDidNotKnow keeps a cross-repository
// edge from resting on a version that identifies nothing.
func TestKnownRejectsTheVersionTheAnalyzerDidNotKnow(t *testing.T) {
	if (CrateRef{Name: "engine", Version: "."}).Known() {
		t.Fatal("a dot version must not identify a crate")
	}
	if (CrateRef{Name: "engine"}).Known() {
		t.Fatal("an empty version must not identify a crate")
	}
	if !(CrateRef{Name: "engine", Version: "1.4.0"}).Known() {
		t.Fatal("a real version must identify a crate")
	}
}
