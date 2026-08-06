package ladybug

import (
	"sort"
	"strings"
)

// CanonicalSchemaVersion is the version of the definitive graph schema. It is
// stored in the database so a rebuild can detect an incompatible layout.
const CanonicalSchemaVersion = 2

// CanonicalSchemaFile is the versioned DDL generated from this metadata.
const CanonicalSchemaFile = "schemas/ladybug/002-canonical.cypher"

// SchemaProperty is one column of a node or relationship table.
type SchemaProperty struct {
	Name string
	Type string
	// Comment documents why the property exists when it is not obvious.
	Comment string
}

// SchemaNodeTable is one node table of the canonical schema.
type SchemaNodeTable struct {
	Name string
	// PrimaryKey is always a durable key produced by Ladygraph, never a display
	// name and never a database generated identifier.
	PrimaryKey SchemaProperty
	Properties []SchemaProperty
	Comment    string
}

// Multiplicity is the cardinality LadybugDB enforces on a relationship.
type Multiplicity string

const (
	// OneToMany is a containment relation: a child has a single parent.
	OneToMany Multiplicity = "ONE_MANY"
	// ManyToOne points many rows at a single owner.
	ManyToOne Multiplicity = "MANY_ONE"
	// ManyToMany is the default for semantic relations.
	ManyToMany Multiplicity = "MANY_MANY"
)

// SchemaRelationshipTable is one relationship table of the canonical schema.
type SchemaRelationshipTable struct {
	Name         string
	From         string
	To           string
	Multiplicity Multiplicity
	Properties   []SchemaProperty
	Comment      string
}

// edgeProperties are carried by every semantic relation, as the plan requires.
func edgeProperties() []SchemaProperty {
	return []SchemaProperty{
		{Name: "confidence", Type: "STRING", Comment: "EXACT_TYPECHECKED … UNRESOLVED"},
		{Name: "provenance", Type: "STRING", Comment: "mechanism that produced the fact"},
		{Name: "evidence_key", Type: "STRING", Comment: "observation supporting the edge"},
		{Name: "source_snapshot", Type: "INT64", Comment: "snapshot that wrote the edge"},
		{Name: "resolver_version", Type: "STRING", Comment: "resolver that produced it"},
	}
}

// containmentProperties document a structural relation.
func containmentProperties() []SchemaProperty {
	return []SchemaProperty{
		{Name: "confidence", Type: "STRING"},
		{Name: "provenance", Type: "STRING"},
	}
}

// CanonicalNodeTables returns the node tables in creation order.
func CanonicalNodeTables() []SchemaNodeTable {
	return []SchemaNodeTable{
		{
			Name:       "GraphMetadata",
			Comment:    "schema version and resolver identity of the stored graph",
			PrimaryKey: SchemaProperty{Name: "key", Type: "STRING"},
			Properties: []SchemaProperty{{Name: "value", Type: "STRING"}},
		},
		{
			Name:       "Repository",
			Comment:    "one indexed repository",
			PrimaryKey: SchemaProperty{Name: "stable_key", Type: "STRING"},
			Properties: []SchemaProperty{
				{Name: "name", Type: "STRING"},
				{Name: "root_path", Type: "STRING"},
				{Name: "commit", Type: "STRING"},
				{Name: "branch", Type: "STRING"},
				{Name: "dirty", Type: "BOOL"},
				{Name: "languages", Type: "STRING", Comment: "comma separated, sorted"},
			},
		},
		{
			Name:       "Package",
			Comment:    "npm package or Go package; container holds the Go module",
			PrimaryKey: SchemaProperty{Name: "stable_key", Type: "STRING"},
			Properties: []SchemaProperty{
				{Name: "repository_key", Type: "STRING"},
				{Name: "language", Type: "STRING"},
				{Name: "name", Type: "STRING"},
				{Name: "version", Type: "STRING"},
				{Name: "root_path", Type: "STRING"},
				{Name: "manifest_path", Type: "STRING"},
				{Name: "container", Type: "STRING"},
			},
		},
		{
			Name:       "File",
			Comment:    "path is repository relative so a key never embeds a machine",
			PrimaryKey: SchemaProperty{Name: "stable_key", Type: "STRING"},
			Properties: []SchemaProperty{
				{Name: "repository_key", Type: "STRING"},
				{Name: "package_key", Type: "STRING"},
				{Name: "path", Type: "STRING"},
				{Name: "language", Type: "STRING"},
				{Name: "content_hash", Type: "STRING"},
				{Name: "generated", Type: "BOOL"},
			},
		},
		{
			Name:       "Symbol",
			Comment:    "canonical_identity is the auditable text the key derives from",
			PrimaryKey: SchemaProperty{Name: "stable_key", Type: "STRING"},
			Properties: []SchemaProperty{
				{Name: "canonical_identity", Type: "STRING"},
				{Name: "repository_key", Type: "STRING"},
				{Name: "package_key", Type: "STRING"},
				{Name: "file_key", Type: "STRING"},
				{Name: "language", Type: "STRING"},
				{Name: "name", Type: "STRING"},
				{Name: "qualified_name", Type: "STRING"},
				{Name: "kind", Type: "STRING"},
				{Name: "exported", Type: "BOOL"},
				{Name: "signature", Type: "STRING"},
				{Name: "start_line", Type: "INT64"},
				{Name: "start_column", Type: "INT64"},
				{Name: "start_offset", Type: "INT64"},
				{Name: "end_line", Type: "INT64"},
				{Name: "end_offset", Type: "INT64"},
			},
		},
		{
			Name:       "Evidence",
			Comment:    "observation backing an edge; text is a short excerpt",
			PrimaryKey: SchemaProperty{Name: "stable_key", Type: "STRING"},
			Properties: []SchemaProperty{
				{Name: "repository_key", Type: "STRING"},
				{Name: "file_key", Type: "STRING"},
				{Name: "start_line", Type: "INT64"},
				{Name: "start_column", Type: "INT64"},
				{Name: "start_offset", Type: "INT64"},
				{Name: "end_offset", Type: "INT64"},
				{Name: "text", Type: "STRING"},
			},
		},
		{
			Name:       "UnresolvedReference",
			Comment:    "a fact that could not become an exact edge, with its reason",
			PrimaryKey: SchemaProperty{Name: "stable_key", Type: "STRING"},
			Properties: []SchemaProperty{
				{Name: "repository_key", Type: "STRING"},
				{Name: "file_key", Type: "STRING"},
				{Name: "language", Type: "STRING"},
				{Name: "source_symbol_key", Type: "STRING"},
				{Name: "requested_package", Type: "STRING"},
				{Name: "requested_symbol", Type: "STRING"},
				{Name: "reason", Type: "STRING"},
				{Name: "detail", Type: "STRING"},
				{Name: "start_line", Type: "INT64"},
				{Name: "start_column", Type: "INT64"},
				{Name: "start_offset", Type: "INT64"},
			},
		},
	}
}

// symbolRelation builds a semantic Symbol to Symbol relationship table.
func symbolRelation(name, comment string) SchemaRelationshipTable {
	return SchemaRelationshipTable{
		Name:         name,
		From:         "Symbol",
		To:           "Symbol",
		Multiplicity: ManyToMany,
		Properties:   edgeProperties(),
		Comment:      comment,
	}
}

// CanonicalRelationshipTables returns the relationship tables in creation
// order. Every semantic edge kind of the canonical model has exactly one table.
func CanonicalRelationshipTables() []SchemaRelationshipTable {
	return []SchemaRelationshipTable{
		{
			Name: "CONTAINS_PACKAGE", From: "Repository", To: "Package",
			Multiplicity: OneToMany, Properties: containmentProperties(),
			Comment: "a package belongs to exactly one repository",
		},
		{
			Name: "CONTAINS_FILE", From: "Package", To: "File",
			Multiplicity: OneToMany, Properties: containmentProperties(),
			Comment: "a file belongs to exactly one package",
		},
		{
			Name: "DEFINES", From: "File", To: "Symbol",
			Multiplicity: OneToMany, Properties: containmentProperties(),
			Comment: "a symbol is declared in exactly one file",
		},
		{
			Name: "OBSERVED_IN", From: "Evidence", To: "File",
			Multiplicity: ManyToOne, Properties: nil,
			Comment: "an observation belongs to one file",
		},
		{
			Name: "REPORTS_UNRESOLVED", From: "Repository", To: "UnresolvedReference",
			Multiplicity: OneToMany, Properties: nil,
			Comment: "module level failures have no file, so the owner is the repository",
		},
		{
			Name: "PACKAGE_DEPENDS_ON", From: "Package", To: "Package",
			Multiplicity: ManyToMany, Properties: edgeProperties(),
			Comment: "a real dependency between packages, never a nominal string",
		},
		{
			Name: "MODULE_DEPENDS_ON", From: "Package", To: "Package",
			Multiplicity: ManyToMany, Properties: edgeProperties(),
			Comment: "module level dependency for Go",
		},
		symbolRelation("IMPORTS_SYMBOL", "consumer binding to the provider declaration"),
		symbolRelation("EXPORTS", "a module exposes a symbol under a public name"),
		symbolRelation("REEXPORTS", "a module forwards a symbol of another module"),
		symbolRelation("REFERENCES", "a plain use of a symbol"),
		symbolRelation("CALLS_DIRECT", "the callee of a call expression"),
		symbolRelation("PASSES_AS_CALLBACK", "a function handed over as a value"),
		symbolRelation("ASSIGNS_FUNCTION", "a function stored in a variable"),
		symbolRelation("RETURNS_FUNCTION", "a function returned as a value"),
		symbolRelation("TYPE_USES", "a type used in a type position"),
		symbolRelation("IMPLEMENTS", "a type satisfies an interface"),
		symbolRelation("EXTENDS", "a type extends another type"),
		symbolRelation("EMBEDS", "a type embeds another type"),
		symbolRelation("OVERRIDES", "a member overrides an inherited one"),
	}
}

// CanonicalSchemaStatements renders the DDL of the canonical schema.
//
// The statements are generated from the metadata above, so the documented
// schema and the created schema cannot drift.
func CanonicalSchemaStatements() []string {
	nodes := CanonicalNodeTables()
	relationships := CanonicalRelationshipTables()
	statements := make([]string, 0, len(nodes)+len(relationships))
	for _, table := range nodes {
		statements = append(statements, nodeStatement(table))
	}
	for _, table := range relationships {
		statements = append(statements, relationshipStatement(table))
	}
	return statements
}

func nodeStatement(table SchemaNodeTable) string {
	var builder strings.Builder
	builder.WriteString("CREATE NODE TABLE IF NOT EXISTS ")
	builder.WriteString(table.Name)
	builder.WriteString("(\n    ")
	builder.WriteString(table.PrimaryKey.Name)
	builder.WriteString(" ")
	builder.WriteString(table.PrimaryKey.Type)
	builder.WriteString(" PRIMARY KEY")
	for _, property := range table.Properties {
		builder.WriteString(",\n    ")
		builder.WriteString(property.Name)
		builder.WriteString(" ")
		builder.WriteString(property.Type)
	}
	builder.WriteString("\n)")
	return builder.String()
}

func relationshipStatement(table SchemaRelationshipTable) string {
	var builder strings.Builder
	builder.WriteString("CREATE REL TABLE IF NOT EXISTS ")
	builder.WriteString(table.Name)
	builder.WriteString("(\n    FROM ")
	builder.WriteString(table.From)
	builder.WriteString(" TO ")
	builder.WriteString(table.To)
	for _, property := range table.Properties {
		builder.WriteString(",\n    ")
		builder.WriteString(property.Name)
		builder.WriteString(" ")
		builder.WriteString(property.Type)
	}
	builder.WriteString(",\n    ")
	builder.WriteString(string(table.Multiplicity))
	builder.WriteString("\n)")
	return builder.String()
}

// CanonicalSchemaDocument renders the complete versioned DDL file.
func CanonicalSchemaDocument() string {
	var builder strings.Builder
	builder.WriteString("// Ladygraph canonical graph schema, version ")
	builder.WriteString(schemaVersionText())
	builder.WriteString(".\n")
	builder.WriteString("// Generated from internal/storage/ladybug.CanonicalSchemaStatements.\n")
	builder.WriteString("// Every primary key is a durable Ladygraph key; no key is inferred from a\n")
	builder.WriteString("// display name and none is generated by the database.\n")
	for _, statement := range CanonicalSchemaStatements() {
		builder.WriteString("\n")
		builder.WriteString(statement)
		builder.WriteString(";\n")
	}
	return builder.String()
}

// CanonicalSchemaDocumentation renders the reference document of the schema.
//
// It is generated from the same metadata as the DDL so the documentation
// cannot describe a schema the database does not have.
func CanonicalSchemaDocumentation() string {
	var builder strings.Builder
	builder.WriteString("# Esquema canónico de LadybugDB\n\n")
	builder.WriteString("Versión del esquema: `")
	builder.WriteString(schemaVersionText())
	builder.WriteString("`. DDL versionado: `")
	builder.WriteString(CanonicalSchemaFile)
	builder.WriteString("`.\n\n")
	builder.WriteString("Este documento se genera desde ")
	builder.WriteString("`internal/storage/ladybug.CanonicalSchemaDocumentation`; ")
	builder.WriteString("no se edita a mano.\n\n")

	builder.WriteString("## Reglas\n\n")
	builder.WriteString("- Toda clave primaria es una clave durable de Ladygraph. ")
	builder.WriteString("Ninguna se deriva de un nombre visible ni la genera la base.\n")
	builder.WriteString("- `GraphMetadata` guarda la versión del esquema y la del resolutor: ")
	builder.WriteString("una base con otra versión se reconstruye, no se migra en caliente.\n")
	builder.WriteString("- Las rutas de `File` son relativas al repositorio, de modo que una ")
	builder.WriteString("clave nunca incrusta la máquina que la produjo.\n")
	builder.WriteString("- Toda relación semántica transporta `confidence`, `provenance` y ")
	builder.WriteString("`evidence_key`; una arista sin procedencia no puede ser exacta.\n")
	builder.WriteString("- LadybugDB indexa la clave primaria de cada tabla. No se declaran ")
	builder.WriteString("índices secundarios: las búsquedas exactas por repositorio, archivo o ")
	builder.WriteString("nombre las sirve el HotSnapshot, no la base.\n\n")

	builder.WriteString("## Nodos\n\n")
	for _, table := range CanonicalNodeTables() {
		builder.WriteString("### ")
		builder.WriteString(table.Name)
		builder.WriteString("\n\n")
		if table.Comment != "" {
			builder.WriteString(table.Comment)
			builder.WriteString(".\n\n")
		}
		builder.WriteString("| Propiedad | Tipo | Nota |\n| --- | --- | --- |\n")
		builder.WriteString(propertyRow(table.PrimaryKey, "clave primaria"))
		for _, property := range table.Properties {
			builder.WriteString(propertyRow(property, property.Comment))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("## Relaciones\n\n")
	builder.WriteString("| Relación | Origen | Destino | Multiplicidad | Propiedades |\n")
	builder.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, table := range CanonicalRelationshipTables() {
		names := make([]string, 0, len(table.Properties))
		for _, property := range table.Properties {
			names = append(names, "`"+property.Name+"`")
		}
		properties := "—"
		if len(names) > 0 {
			properties = strings.Join(names, ", ")
		}
		builder.WriteString("| `" + table.Name + "` | " + table.From + " | " + table.To +
			" | `" + string(table.Multiplicity) + "` | " + properties + " |\n")
	}
	builder.WriteString("\n## Restricciones\n\n")
	builder.WriteString("- `CONTAINS_PACKAGE`, `CONTAINS_FILE` y `DEFINES` son `ONE_MANY`: ")
	builder.WriteString("un paquete pertenece a un repositorio, un archivo a un paquete y un ")
	builder.WriteString("símbolo se declara en un archivo.\n")
	builder.WriteString("- `OBSERVED_IN` es `MANY_ONE`: muchas evidencias por archivo.\n")
	builder.WriteString("- `REPORTS_UNRESOLVED` cuelga del repositorio porque un fallo de ")
	builder.WriteString("módulo no tiene archivo.\n")
	builder.WriteString("- Las relaciones semánticas son `MANY_MANY`: un símbolo puede llamar ")
	builder.WriteString("al mismo destino desde varios sitios, y cada ocurrencia lleva su ")
	builder.WriteString("evidencia.\n")
	return builder.String()
}

func propertyRow(property SchemaProperty, note string) string {
	if note == "" {
		note = "—"
	}
	return "| `" + property.Name + "` | `" + property.Type + "` | " + note + " |\n"
}

func schemaVersionText() string {
	digits := []byte("00")
	version := CanonicalSchemaVersion
	digits[1] = byte('0' + version%10)
	digits[0] = byte('0' + (version/10)%10)
	return "0" + string(digits)
}

// CanonicalTableNames lists every table of the schema, sorted, for diagnostics.
func CanonicalTableNames() []string {
	names := make([]string, 0)
	for _, table := range CanonicalNodeTables() {
		names = append(names, table.Name)
	}
	for _, table := range CanonicalRelationshipTables() {
		names = append(names, table.Name)
	}
	sort.Strings(names)
	return names
}
