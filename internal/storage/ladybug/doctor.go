package ladybug

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const GoBindingVersion = "v0.13.1"

type DiagnosticStatus string

const (
	DiagnosticPass DiagnosticStatus = "PASS"
	DiagnosticFail DiagnosticStatus = "FAIL"
	DiagnosticSkip DiagnosticStatus = "SKIP"
)

// SchemaKind names which on-disk table layout a StorageDiagnosis was
// validated against. A PASS or FAIL on "schema", "counts" or "integrity"
// is not interpretable without knowing which of the two schemas ladygraph
// doctor storage measured them against, so every StorageDiagnosis
// declares one.
type SchemaKind string

const (
	// SchemaCanonical is the schema CanonicalNodeTables and
	// CanonicalRelationshipTables describe: the one LoadCanonical and
	// `ladygraph rebuild` write, identified by its GraphMetadata node table.
	SchemaCanonical SchemaKind = "canonical"
	// SchemaSynthetic is the frozen, hand written 001 schema the ladybug
	// benchmarks still build and validate against.
	SchemaSynthetic SchemaKind = "synthetic"
	// SchemaUnknown means the database was never opened far enough to
	// tell: native support is unavailable, or the file itself could not
	// be inspected. Never guessed as one of the other two.
	SchemaUnknown SchemaKind = "unknown"
)

// DiagnosticCheck is one independently reportable storage invariant.
type DiagnosticCheck struct {
	Name   string           `json:"name"`
	Status DiagnosticStatus `json:"status"`
	Detail string           `json:"detail"`
}

// StorageDiagnosis is the complete result of `ladygraph doctor storage`.
type StorageDiagnosis struct {
	Path             string `json:"path"`
	SizeBytes        int64  `json:"size_bytes"`
	Mode             string `json:"mode"`
	Readable         bool   `json:"readable"`
	Writable         bool   `json:"writable"`
	GoBindingVersion string `json:"go_binding_version"`
	EngineVersion    string `json:"engine_version,omitempty"`
	StorageVersion   uint64 `json:"storage_version,omitempty"`
	// Schema names which on-disk table layout the checks below actually
	// validated: SchemaCanonical, SchemaSynthetic, or SchemaUnknown when
	// the database was never opened. Set as soon as that is known, before
	// the schema check itself runs, so a FAIL still says what it FAILed
	// against.
	Schema SchemaKind `json:"schema"`
	// SchemaVersion is the schema_version GraphMetadata.value declares.
	// Only meaningful when Schema is SchemaCanonical: the synthetic
	// schema carries no version marker of its own, so this stays zero.
	SchemaVersion int               `json:"schema_version,omitempty"`
	Tables        map[string]string `json:"tables,omitempty"`
	Counts        map[string]int64  `json:"counts,omitempty"`
	Checks        []DiagnosticCheck `json:"checks"`
	Healthy       bool              `json:"healthy"`
}

// Check returns one named diagnostic result.
func (diagnosis StorageDiagnosis) Check(name string) (DiagnosticCheck, bool) {
	for _, check := range diagnosis.Checks {
		if check.Name == name {
			return check, true
		}
	}
	return DiagnosticCheck{}, false
}

func newStorageDiagnosis(path string) (StorageDiagnosis, bool, error) {
	if strings.TrimSpace(path) == "" {
		return StorageDiagnosis{}, false, ErrInvalidPath
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return StorageDiagnosis{}, false, err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(absolute); resolveErr == nil {
		absolute = resolved
	}
	diagnosis := StorageDiagnosis{
		Path: absolute, GoBindingVersion: GoBindingVersion, Schema: SchemaUnknown,
		Tables: make(map[string]string), Counts: make(map[string]int64),
	}
	diagnosis.addCheck("location", DiagnosticPass, absolute)
	info, err := os.Stat(absolute)
	if err != nil {
		diagnosis.addCheck("size", DiagnosticFail, err.Error())
		diagnosis.addCheck("permissions", DiagnosticFail, err.Error())
		return diagnosis, false, nil
	}
	diagnosis.SizeBytes = info.Size()
	diagnosis.Mode = info.Mode().String()
	if !info.Mode().IsRegular() {
		detail := fmt.Sprintf("mode=%s; database path is not a regular file", diagnosis.Mode)
		diagnosis.addCheck("size", DiagnosticFail, detail)
		diagnosis.addCheck("permissions", DiagnosticFail, detail)
		return diagnosis, false, nil
	}
	diagnosis.addCheck("size", DiagnosticPass, fmt.Sprintf("%d bytes", diagnosis.SizeBytes))
	if file, openErr := os.Open(absolute); openErr == nil {
		diagnosis.Readable = true
		_ = file.Close()
	}
	if file, openErr := os.OpenFile(absolute, os.O_WRONLY, 0); openErr == nil {
		diagnosis.Writable = true
		_ = file.Close()
	}
	permissionStatus := DiagnosticPass
	if !diagnosis.Readable || !diagnosis.Writable {
		permissionStatus = DiagnosticFail
	}
	diagnosis.addCheck("permissions", permissionStatus, fmt.Sprintf("mode=%s readable=%t writable=%t", diagnosis.Mode, diagnosis.Readable, diagnosis.Writable))
	return diagnosis, true, nil
}

func (diagnosis *StorageDiagnosis) addCheck(name string, status DiagnosticStatus, detail string) {
	diagnosis.Checks = append(diagnosis.Checks, DiagnosticCheck{Name: name, Status: status, Detail: detail})
}

func (diagnosis *StorageDiagnosis) skipNativeChecks(reason string) {
	for _, name := range []string{"lock", "open", "version", "schema", "transactions", "counts", "integrity"} {
		diagnosis.addCheck(name, DiagnosticSkip, reason)
	}
	diagnosis.Healthy = false
}

func (diagnosis *StorageDiagnosis) finalize() {
	diagnosis.Healthy = len(diagnosis.Checks) != 0
	for _, check := range diagnosis.Checks {
		if check.Status != DiagnosticPass {
			diagnosis.Healthy = false
			return
		}
	}
}
