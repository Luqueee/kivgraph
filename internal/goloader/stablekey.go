package goloader

import (
	"context"
	"errors"
	"fmt"
	"go/types"
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
	resolved, err := ObjectIdentity{
		Repository:    definition.Repository,
		ModulePath:    definition.ModulePath,
		PackagePath:   definition.PackagePath,
		QualifiedName: definition.QualifiedName,
		Kind:          definition.Kind,
		Signature:     definition.Signature,
		Object:        definition.Object(),
	}.Resolve(encoders)
	if err != nil {
		return KeyedDefinition{}, err
	}
	return KeyedDefinition{
		Definition:        definition,
		ObjectPath:        resolved.ObjectPath,
		CanonicalIdentity: resolved.CanonicalIdentity,
		StableKey:         resolved.StableKey,
	}, nil
}

// ObjectIdentity is everything needed to derive the durable key of one Go
// object, whether it was extracted as a definition or reached as the target of
// a use in another repository.
type ObjectIdentity struct {
	Repository    string
	ModulePath    string
	PackagePath   string
	QualifiedName string
	Kind          DefinitionKind
	Signature     string
	Object        types.Object
}

// ResolvedIdentity is the durable identity of one object.
type ResolvedIdentity struct {
	ObjectPath        string
	CanonicalIdentity string
	StableKey         hotsnapshot.StableKey
}

// Resolve derives the object path, canonical identity and stable key.
//
// Encoders are cached per package path; pass the same map across calls to
// avoid rebuilding the object path index of a package.
func (identity ObjectIdentity) Resolve(
	encoders map[string]*objectpath.Encoder,
) (ResolvedIdentity, error) {
	if strings.TrimSpace(identity.Repository) == "" {
		return ResolvedIdentity{}, fmt.Errorf("%w: %s", ErrMissingRepository, identity.QualifiedName)
	}
	if strings.TrimSpace(identity.ModulePath) == "" {
		return ResolvedIdentity{}, fmt.Errorf("%w: %s", ErrMissingModulePath, identity.QualifiedName)
	}
	if strings.TrimSpace(identity.PackagePath) == "" {
		return ResolvedIdentity{}, fmt.Errorf("object %q has no package path", identity.QualifiedName)
	}

	path := objectPathFor(identity.PackagePath, identity.Object, encoders)
	stable := hotsnapshot.StableKeyIdentity{
		FormatVersion: hotsnapshot.StableKeyFormatVersion,
		Language:      GoLanguage,
		Repository:    identity.Repository,
		Package:       identity.ModulePath + " " + identity.PackagePath,
		QualifiedName: qualifiedIdentity(identity.QualifiedName, path),
		Kind:          string(identity.Kind),
		Discriminator: discriminator(identity.Signature),
	}
	canonical, err := stable.Canonical()
	if err != nil {
		return ResolvedIdentity{}, fmt.Errorf("object %q identity: %w", identity.QualifiedName, err)
	}
	key, err := stable.Key()
	if err != nil {
		return ResolvedIdentity{}, fmt.Errorf("object %q key: %w", identity.QualifiedName, err)
	}
	return ResolvedIdentity{
		ObjectPath:        path,
		CanonicalIdentity: canonical,
		StableKey:         key,
	}, nil
}

// objectPathFor returns the go/types object path, or an empty string when the
// object is not reachable from the package scope.
//
// Members of an instantiated generic have no path of their own: the encoder
// indexes the declaration, not the instance. Falling back to the generic
// origin is what keeps `Box[int].Unwrap` addressable as the declared
// `Box.Unwrap` instead of losing the edge.
func objectPathFor(
	packagePath string,
	object types.Object,
	encoders map[string]*objectpath.Encoder,
) string {
	if object == nil || object.Pkg() == nil {
		return ""
	}
	encoder := encoders[packagePath]
	if encoder == nil {
		encoder = new(objectpath.Encoder)
		encoders[packagePath] = encoder
	}
	if path, err := encoder.For(object); err == nil {
		return string(path)
	}
	origin := genericOrigin(object)
	if origin == nil || origin == object {
		return ""
	}
	path, err := encoder.For(origin)
	if err != nil {
		return ""
	}
	return string(path)
}

// genericOrigin returns the declared object an instantiated one comes from.
func genericOrigin(object types.Object) types.Object {
	switch typed := object.(type) {
	case *types.Func:
		return typed.Origin()
	case *types.Var:
		return typed.Origin()
	default:
		return nil
	}
}

// qualifiedIdentity embeds the object path only when it is name based, which
// is the case for package-level objects: `Compute`, `Shape`.
//
// Members are addressed by index — a method is `Shape.M0` and a field
// `Shape.UF1` — so inserting a method or a field would rotate the identity of
// every later member. Identity must survive reordering, so those objects use
// the syntactic qualified name instead. The index-based path is still kept in
// `KeyedDefinition.ObjectPath` for cross-repository resolution.
func qualifiedIdentity(qualifiedName, path string) string {
	if path != "" && path == qualifiedName {
		return "objectpath:" + path
	}
	return "syntax:" + qualifiedName
}

// discriminator separates symbols that share every other identity field, such
// as a method and a field with the same qualified name.
func discriminator(signature string) string {
	trimmed := strings.TrimSpace(signature)
	if trimmed == "" {
		return "none"
	}
	return trimmed
}
