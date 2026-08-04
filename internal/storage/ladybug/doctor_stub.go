//go:build !ladybug || !cgo

package ladybug

import "context"

// DiagnoseStorage reports filesystem facts and native unavailability.
func DiagnoseStorage(ctx context.Context, path string) (StorageDiagnosis, error) {
	if err := ctx.Err(); err != nil {
		return StorageDiagnosis{}, err
	}
	diagnosis, regular, err := newStorageDiagnosis(path)
	if err != nil {
		return StorageDiagnosis{}, err
	}
	if !regular {
		diagnosis.skipNativeChecks("database file is unavailable")
		return diagnosis, nil
	}
	diagnosis.addCheck("lock", DiagnosticSkip, "native support is unavailable")
	diagnosis.addCheck("open", DiagnosticFail, ErrUnavailable.Error())
	for _, name := range []string{"version", "schema", "transactions", "counts", "integrity"} {
		diagnosis.addCheck(name, DiagnosticSkip, "native support is unavailable")
	}
	diagnosis.finalize()
	return diagnosis, nil
}
