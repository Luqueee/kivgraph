package hotsnapshot

import (
	"errors"
	"strconv"
	"testing"
)

func TestStableKeyCanonicalIdentity(t *testing.T) {
	identity := stableKeyTestIdentity()

	canonical, err := identity.Canonical()
	if err != nil {
		t.Fatalf("Canonical() error = %v", err)
	}
	wantCanonical := "luque-stable-key\x00version=1" +
		"\x00language=10:typescript" +
		"\x00repository=15:github.com/acme" +
		"\x00package=10:@acme/core" +
		"\x00qualified_name=18:Parser.parseConfig" +
		"\x00kind=6:method" +
		"\x00discriminator=20:(input: Config): AST"
	if canonical != wantCanonical {
		t.Fatalf("Canonical() = %q, want %q", canonical, wantCanonical)
	}

	key, err := identity.Key()
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	const wantKey = "CWOZHP3LOBOMOJUWVQFJZ7QC2D7Y7H32QXMG4UWC7D2VCT5QJTCA"
	if key != wantKey {
		t.Fatalf("Key() = %q, want %q", key, wantKey)
	}
}

func TestStableKeyDeterministicAndLineIndependent(t *testing.T) {
	identity := stableKeyTestIdentity()
	first, err := identity.Key()
	if err != nil {
		t.Fatal(err)
	}
	second, err := identity.Key()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("repeated key = %q, want %q", second, first)
	}

	// Source position is deliberately absent from StableKeyIdentity: moving this
	// declaration cannot affect an otherwise unchanged identity.
	movedLineIdentity := stableKeyTestIdentity()
	moved, err := movedLineIdentity.Key()
	if err != nil {
		t.Fatal(err)
	}
	if moved != first {
		t.Fatalf("key after source-line move = %q, want %q", moved, first)
	}
}

func TestStableKeyChangesWithEachIdentityField(t *testing.T) {
	original := stableKeyTestIdentity()
	originalKey, err := original.Key()
	if err != nil {
		t.Fatal(err)
	}

	changes := []StableKeyIdentity{
		{FormatVersion: 1, Language: "go", Repository: original.Repository, Package: original.Package, QualifiedName: original.QualifiedName, Kind: original.Kind, Discriminator: original.Discriminator},
		{FormatVersion: 1, Language: original.Language, Repository: "github.com/other", Package: original.Package, QualifiedName: original.QualifiedName, Kind: original.Kind, Discriminator: original.Discriminator},
		{FormatVersion: 1, Language: original.Language, Repository: original.Repository, Package: "@acme/other", QualifiedName: original.QualifiedName, Kind: original.Kind, Discriminator: original.Discriminator},
		{FormatVersion: 1, Language: original.Language, Repository: original.Repository, Package: original.Package, QualifiedName: "Parser.parseFile", Kind: original.Kind, Discriminator: original.Discriminator},
		{FormatVersion: 1, Language: original.Language, Repository: original.Repository, Package: original.Package, QualifiedName: original.QualifiedName, Kind: "function", Discriminator: original.Discriminator},
		{FormatVersion: 1, Language: original.Language, Repository: original.Repository, Package: original.Package, QualifiedName: original.QualifiedName, Kind: original.Kind, Discriminator: "(input: File): AST"},
	}
	for index, changed := range changes {
		key, err := changed.Key()
		if err != nil {
			t.Fatalf("change %d Key() error = %v", index, err)
		}
		if key == originalKey {
			t.Fatalf("change %d did not change key %q", index, key)
		}
	}
}

func TestStableKeyCorpusHasNoCollisions(t *testing.T) {
	keys := make(map[StableKey]struct{}, 20_000)
	for index := 0; index < 20_000; index++ {
		identity := StableKeyIdentity{
			FormatVersion: StableKeyFormatVersion,
			Language:      "go",
			Repository:    "github.com/acme/repository-" + strconv.Itoa(index%97),
			Package:       "example.com/module/package-" + strconv.Itoa(index%29),
			QualifiedName: "Type" + strconv.Itoa(index) + ".Method",
			Kind:          "method",
			Discriminator: "func(string, int) error",
		}
		key, err := identity.Key()
		if err != nil {
			t.Fatalf("Key(%d) error = %v", index, err)
		}
		if _, exists := keys[key]; exists {
			t.Fatalf("collision for corpus entry %d: %q", index, key)
		}
		keys[key] = struct{}{}
	}
}

func TestStableKeyRejectsIncompleteOrUnknownFormat(t *testing.T) {
	identity := stableKeyTestIdentity()
	identity.QualifiedName = ""
	if _, err := identity.Canonical(); !errors.Is(err, ErrInvalidStableKeyIdentity) {
		t.Fatalf("Canonical() error = %v, want ErrInvalidStableKeyIdentity", err)
	}

	identity = stableKeyTestIdentity()
	identity.FormatVersion++
	if _, err := identity.Canonical(); !errors.Is(err, ErrUnsupportedStableKeyFormat) {
		t.Fatalf("Canonical() error = %v, want ErrUnsupportedStableKeyFormat", err)
	}
}

func stableKeyTestIdentity() StableKeyIdentity {
	return StableKeyIdentity{
		FormatVersion: StableKeyFormatVersion,
		Language:      "typescript",
		Repository:    "github.com/acme",
		Package:       "@acme/core",
		QualifiedName: "Parser.parseConfig",
		Kind:          "method",
		Discriminator: "(input: Config): AST",
	}
}
