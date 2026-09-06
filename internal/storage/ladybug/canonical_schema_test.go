package ladybug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// TestCanonicalSchemaFileMatchesTheMetadata keeps the versioned DDL and the Go
// metadata from drifting: the file is generated, never hand edited.
func TestCanonicalSchemaFileMatchesTheMetadata(t *testing.T) {
	path := filepath.Join("..", "..", "..", "schemas", "ladybug", "005-canonical.cypher")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read canonical schema %q: %v", path, err)
	}
	if string(contents) != CanonicalSchemaDocument() {
		t.Fatalf("%s is out of date; regenerate it from CanonicalSchemaDocument", path)
	}
}

// TestCanonicalSchemaDocumentationMatchesTheMetadata keeps the reference
// document from describing a schema the database does not have.
func TestCanonicalSchemaDocumentationMatchesTheMetadata(t *testing.T) {
	path := filepath.Join("..", "..", "..", "docs", "storage", "canonical-schema.md")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read canonical schema documentation: %v", err)
	}
	if string(contents) != CanonicalSchemaDocumentation() {
		t.Fatalf("%s is out of date; regenerate it from CanonicalSchemaDocumentation", path)
	}
	for _, table := range CanonicalNodeTables() {
		if !strings.Contains(string(contents), "### "+table.Name) {
			t.Fatalf("documentation does not describe node table %q", table.Name)
		}
	}
	for _, table := range CanonicalRelationshipTables() {
		if !strings.Contains(string(contents), "| `"+table.Name+"` |") {
			t.Fatalf("documentation does not describe relationship %q", table.Name)
		}
	}
}

// TestCanonicalSchemaCoversEveryEdgeKind is the parity invariant with the
// canonical model: an edge Kivgraph can produce and cannot store is a fact lost
// at write time.
func TestCanonicalSchemaCoversEveryEdgeKind(t *testing.T) {
	tables := make(map[string]SchemaRelationshipTable, len(CanonicalRelationshipTables()))
	for _, table := range CanonicalRelationshipTables() {
		tables[table.Name] = table
	}

	for _, kind := range []facts.EdgeKind{
		facts.ContainsPackage, facts.ContainsFile, facts.Defines,
		facts.PackageDependsOn, facts.ModuleDependsOn,
		facts.ImportsSymbol, facts.Exports, facts.Reexports,
		facts.References, facts.CallsDirect, facts.PassesAsCallback,
		facts.AssignsFunction, facts.ReturnsFunction,
		facts.TypeUses, facts.Implements, facts.Extends, facts.Embeds, facts.Overrides, facts.PartOf,
	} {
		if !kind.Valid() {
			t.Fatalf("edge kind %q is not part of the canonical vocabulary", kind)
		}
		if _, exists := tables[string(kind)]; !exists {
			t.Fatalf("edge kind %q has no relationship table", kind)
		}
	}
}

func TestCanonicalSchemaKeysAndMultiplicities(t *testing.T) {
	for _, table := range CanonicalNodeTables() {
		if table.PrimaryKey.Name == "" || table.PrimaryKey.Type != "STRING" {
			t.Fatalf("node table %q has no durable string key: %#v", table.Name, table.PrimaryKey)
		}
		if table.Name != "GraphMetadata" && table.PrimaryKey.Name != "stable_key" {
			t.Fatalf("node table %q must key on stable_key", table.Name)
		}
		seen := map[string]struct{}{table.PrimaryKey.Name: {}}
		for _, property := range table.Properties {
			if _, duplicate := seen[property.Name]; duplicate {
				t.Fatalf("node table %q repeats property %q", table.Name, property.Name)
			}
			seen[property.Name] = struct{}{}
		}
	}

	containment := map[string]Multiplicity{
		"CONTAINS_PACKAGE":   OneToMany,
		"CONTAINS_FILE":      OneToMany,
		"DEFINES":            OneToMany,
		"OBSERVED_IN":        ManyToOne,
		"REPORTS_UNRESOLVED": OneToMany,
	}
	for _, table := range CanonicalRelationshipTables() {
		if table.From == "" || table.To == "" {
			t.Fatalf("relationship %q has no endpoints", table.Name)
		}
		want, structural := containment[table.Name]
		switch {
		case structural:
			if table.Multiplicity != want {
				t.Fatalf("relationship %q multiplicity = %q, want %q", table.Name, table.Multiplicity, want)
			}
		default:
			if table.Multiplicity != ManyToMany {
				t.Fatalf("semantic relationship %q must be MANY_MANY", table.Name)
			}
			if !hasProperty(table, "confidence") || !hasProperty(table, "provenance") ||
				!hasProperty(table, "evidence_key") {
				t.Fatalf("semantic relationship %q lacks the plan properties: %#v", table.Name, table.Properties)
			}
		}
	}
}

func TestCanonicalSchemaStatementsAreWellFormed(t *testing.T) {
	statements := CanonicalSchemaStatements()
	if len(statements) != len(CanonicalNodeTables())+len(CanonicalRelationshipTables()) {
		t.Fatalf("statements = %d", len(statements))
	}
	for _, statement := range statements {
		if strings.Contains(statement, ";") {
			t.Fatalf("statement carries its own terminator: %q", statement)
		}
		if !strings.HasPrefix(statement, "CREATE NODE TABLE IF NOT EXISTS ") &&
			!strings.HasPrefix(statement, "CREATE REL TABLE IF NOT EXISTS ") {
			t.Fatalf("unexpected statement: %q", statement)
		}
		if strings.Count(statement, "(") != 1 || strings.Count(statement, ")") != 1 {
			t.Fatalf("unbalanced statement: %q", statement)
		}
	}

	// Node tables are created before the relationships that reference them.
	document := CanonicalSchemaDocument()
	for _, table := range CanonicalRelationshipTables() {
		relationshipIndex := strings.Index(document, "CREATE REL TABLE IF NOT EXISTS "+table.Name+"(")
		for _, endpoint := range []string{table.From, table.To} {
			nodeIndex := strings.Index(document, "CREATE NODE TABLE IF NOT EXISTS "+endpoint+"(")
			if nodeIndex == -1 {
				t.Fatalf("relationship %q references unknown node table %q", table.Name, endpoint)
			}
			if nodeIndex > relationshipIndex {
				t.Fatalf("relationship %q is created before its endpoint %q", table.Name, endpoint)
			}
		}
	}

	names := CanonicalTableNames()
	if len(names) != len(statements) {
		t.Fatalf("table names = %d, statements = %d", len(names), len(statements))
	}
	for index := 1; index < len(names); index++ {
		if names[index-1] >= names[index] {
			t.Fatalf("table names are not sorted and unique: %v", names)
		}
	}
}

func hasProperty(table SchemaRelationshipTable, name string) bool {
	for _, property := range table.Properties {
		if property.Name == name {
			return true
		}
	}
	return false
}
