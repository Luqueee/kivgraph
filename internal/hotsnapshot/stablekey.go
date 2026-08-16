package hotsnapshot

import (
	"encoding/base32"
	"errors"
	"github.com/zeebo/blake3"
	"strconv"
	"strings"
)

const StableKeyFormatVersion uint16 = 1
const stableKeyNamespace = "luque-stable-key" // Persistent namespace; retained across the Kivgraph rename.

var (
	ErrUnsupportedStableKeyFormat = errors.New("unsupported stable key format")
	ErrInvalidStableKeyIdentity   = errors.New("invalid stable key identity")
)

// StableKey is a persistent, opaque identifier for a symbol. It encodes the
// 256-bit BLAKE3 digest as unpadded base32 and is independent of snapshot IDs
// and source locations.
type StableKey string

// StableKeyIdentity identifies a symbol independently of source location.
// Package is a TypeScript package name or Go module/package path, as
// appropriate for Language. Discriminator distinguishes otherwise equivalent
// symbols, for example an overload signature or an anonymous declaration.
//
// Module is the declaring module of the symbol, repository relative, and is
// only part of the identity for languages whose declarations are not globally
// addressable by package and qualified name alone. TypeScript sets it: two
// files of one package may each declare a local `s`, and both are distinct
// symbols. Go leaves it empty; its object path already carries the container,
// and an empty Module is omitted from the canonical identity so Go keys are
// byte identical to the ones format version 1 has always produced.
type StableKeyIdentity struct {
	FormatVersion uint16
	Language      string
	Repository    string
	Package       string
	Module        string
	QualifiedName string
	Kind          string
	Discriminator string
}

// stableKeyField is one length-prefixed component of a canonical identity.
type stableKeyField struct {
	name   string
	value  string
	length string
}

// Canonical returns the auditable identity passed to BLAKE3. Length-prefixing
// makes all field boundaries unambiguous even when values contain separators or
// newlines.
func (identity StableKeyIdentity) Canonical() (string, error) {
	if identity.FormatVersion != StableKeyFormatVersion {
		return "", ErrUnsupportedStableKeyFormat
	}

	fields := make([]stableKeyField, 0, 7)
	fields = append(fields,
		stableKeyField{name: "language", value: identity.Language},
		stableKeyField{name: "repository", value: identity.Repository},
		stableKeyField{name: "package", value: identity.Package},
	)
	if identity.Module != "" {
		fields = append(fields, stableKeyField{name: "module", value: identity.Module})
	}
	fields = append(fields,
		stableKeyField{name: "qualified_name", value: identity.QualifiedName},
		stableKeyField{name: "kind", value: identity.Kind},
		stableKeyField{name: "discriminator", value: identity.Discriminator},
	)

	version := strconv.FormatUint(uint64(identity.FormatVersion), 10)
	size := len(stableKeyNamespace+"\x00version=") + len(version)
	for index := range fields {
		if fields[index].value == "" {
			return "", ErrInvalidStableKeyIdentity
		}
		fields[index].length = strconv.Itoa(len(fields[index].value))
		size += 3 + len(fields[index].name) + len(fields[index].length) + len(fields[index].value)
	}

	var canonical strings.Builder
	canonical.Grow(size)
	canonical.WriteString(stableKeyNamespace + "\x00version=")
	canonical.WriteString(version)
	for _, field := range fields {
		canonical.WriteByte('\x00')
		canonical.WriteString(field.name)
		canonical.WriteByte('=')
		canonical.WriteString(field.length)
		canonical.WriteByte(':')
		canonical.WriteString(field.value)
	}
	return canonical.String(), nil
}

// Key returns the BLAKE3 digest of the canonical identity in a printable,
// fixed-width encoding suitable for APIs and durable storage.
func (identity StableKeyIdentity) Key() (StableKey, error) {
	canonical, err := identity.Canonical()
	if err != nil {
		return "", err
	}
	hasher := blake3.New()
	_, _ = hasher.WriteString(canonical)
	var sum [32]byte
	_, _ = hasher.Digest().Read(sum[:])
	return StableKey(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])), nil
}
