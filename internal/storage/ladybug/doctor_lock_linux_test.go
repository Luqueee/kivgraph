//go:build ladybug && cgo && linux

package ladybug

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestParseExternalLockPIDs(t *testing.T) {
	locks := strings.Join([]string{
		"1: POSIX ADVISORY WRITE 123 00:29:432602 0 EOF",
		"2: POSIX ADVISORY WRITE 456 00:29:432602 0 EOF",
		"3: POSIX ADVISORY WRITE 123 00:29:432602 0 EOF",
		"4: POSIX ADVISORY WRITE 999 00:29:7 0 EOF",
	}, "\n")
	got := parseExternalLockPIDs(locks, "00:29:432602", 456)
	if len(got) != 1 || got[0] != 123 {
		t.Fatalf("parseExternalLockPIDs() = %v, want [123]", got)
	}
}

func TestDiagnoseStorageDetectsExternalLock(t *testing.T) {
	path := newCanonicalDoctorDatabase(t)
	_, pid := startExternalDoctorLock(t, path)
	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if diagnosis.Healthy {
		t.Fatal("Healthy = true with an external writer")
	}
	check := requireDiagnosticCheck(t, diagnosis, "lock")
	if check.Status != DiagnosticFail || !strings.Contains(check.Detail, fmt.Sprint(pid)) {
		t.Fatalf("lock check = %#v, want external pid %d", check, pid)
	}
}
