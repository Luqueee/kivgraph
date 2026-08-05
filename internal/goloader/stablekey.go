package goloader

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/Luqueee/luque/internal/hotsnapshot"
)

// GoLanguage is the language recorded in every Go stable key identity.
const GoLanguage = "go"

// ErrMissingRepository reports a definition without the repository it belongs
// to: identity cannot be global without it.
var ErrMissingRepository = errors.New("definition has no repository")

// ErrMissingModulePath reports a definition outside any module.
var ErrMissingModulePath = errors.New("definition has no module path")

// KeyedDefinition is a definition with its durable identity.
type KeyedDefinition struct {
	Definition
	// ObjectPath is the go/types object path when the object is reachable
	// from the package scope. Unexported and local objects have none.
	ObjectPath string
	// CanonicalIdentity is the auditable text the key is derived from.
	CanonicalIdentity string
	StableKey         hotsnapshot.StableKey
}

// AssignStableKeys derives the durable identity of every definition.
//
// The identity uses module path, package path, object path, kind and
// repository, as the plan requires, and never source positions: moving a
// declaration must not change its key.
func AssignStableKeys(ctx context.Context, definitions []Definition) ([]KeyedDefinition, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	keyed := make([]KeyedDefinition, 0, len(definitions))
	encoders := make(map[string]*objectpath.Encoder)
	for _, definition := range definitions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry, err := keyDefinition(definition, encoders)
		if err != nil {
			return nil, err
		}
		keyed = append(keyed, entry)
	}
	return keyed, nil
}

func keyDefinition(
	definition Definition,
	encoders map[string]*objectpath.Encoder,
) (KeyedDefinition, error) {
	if strings.TrimSpace(definition.Repository) == "" {
		return KeyedDefinition{}, fmt.Errorf("%w: %s", ErrMissingRepository, definition.QualifiedName)
	}
	if strings.TrimSpace(definition.ModulePath) == "" {
		return KeyedDefinition{}, fmt.Errorf("%w: %s", ErrMissingModulePath, definition.QualifiedName)
	}
	if strings.TrimSpace(definition.PackagePath) == "" {
		return KeyedDefinition{}, fmt.Errorf("definition %q has no package path", definition.QualifiedName)
	}

	path := objectPathFor(definition, encoders)
	identity := hotsnapshot.StableKeyIdentity{
		FormatVersion: hotsnapshot.StableKeyFormatVersion,
		Language:      GoLanguage,
		Repository:    definition.Repository,
		Package:       definition.ModulePath + " " + definition.PackagePath,
		QualifiedName: qualifiedIdentity(definition, path),
		Kind:          string(definition.Kind),
		Discriminator: discriminator(definition),
	}
	canonical, err := identity.Canonical()
	if err != nil {
		return KeyedDefinition{}, fmt.Errorf("definition %q identity: %w", definition.QualifiedName, err)
	}
	key, err := identity.Key()
	if err != nil {
		return KeyedDefinition{}, fmt.Errorf("definition %q key: %w", definition.QualifiedName, err)
	}
	return KeyedDefinition{
		Definition:        definition,
		ObjectPath:        path,
		CanonicalIdentity: canonical,
		StableKey:         key,
	}, nil
}

// objectPathFor returns the go/types object path, or an empty string when the
// object is not reachable from the package scope.
func objectPathFor(definition Definition, encoders map[string]*objectpath.Encoder) string {
	object := definition.Object()
	if object == nil || object.Pkg() == nil {
		return ""
	}
	encoder := encoders[definition.PackagePath]
	if encoder == nil {
		encoder = new(objectpath.Encoder)
		encoders[definition.PackagePath] = encoder
	}
	path, err := encoder.For(object)
	if err != nil {
		return ""
	}
	return string(path)
}

// qualifiedIdentity embeds the object path only when it is name based, which
// is the case for package-level objects: `Compute`, `Shape`.
//
// Members are addressed by index — a method is `Shape.M0` and a field
// `Shape.UF1` — so inserting a method or a field would rotate the identity of
// every later member. Identity must survive reordering, so those objects use
// the syntactic qualified name instead. The index-based path is still kept in
// `KeyedDefinition.ObjectPath` for cross-repository resolution.
func qualifiedIdentity(definition Definition, path string) string {
	if path != "" && path == definition.QualifiedName {
		return "objectpath:" + path
	}
	return "syntax:" + definition.QualifiedName
}

// discriminator separates symbols that share every other identity field, such
// as a method and a field with the same qualified name.
func discriminator(definition Definition) string {
	signature := strings.TrimSpace(definition.Signature)
	if signature == "" {
		return "none"
	}
	return signature
}
