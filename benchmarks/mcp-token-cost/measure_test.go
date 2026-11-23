package main

import (
	"reflect"
	"strings"
	"testing"
)

// A page the harness cannot read must fail the run. It used to price such a
// page at zero bodies, which flattered the arm that serves them.
func TestCompactRowsRefusesAnAnswerItCannotRead(t *testing.T) {
	_, err := compactRows("find_references", `{"total":3,"returned":3,"results":{"repository":"r","other":[]}}`)
	if err == nil || !strings.Contains(err.Error(), "returned 3 rows") {
		t.Fatalf("an answer with rows in an unknown shape: err = %v, want it to name the 3 rows", err)
	}
	if _, err := compactRows("find_references", `not json`); err == nil {
		t.Fatal("text that is not JSON was accepted")
	}
	rows, err := compactRows("find_references", `{"total":0,"returned":0,"results":{"repository":"r"}}`)
	if err != nil || len(rows) != 0 {
		t.Fatalf("an empty answer: rows = %v, err = %v, want none and no error", rows, err)
	}
}

func TestCompactRowsReadsBothShapesAnAnswerComesIn(t *testing.T) {
	want := []compactRow{
		{Repository: "r", FilePath: "a.go", QualifiedName: "First", StartLine: 3, EndLine: 9},
		{Repository: "r", FilePath: "b.go", QualifiedName: "Second", StartLine: 7, EndLine: 7},
		{Repository: "r", FilePath: "c.go", QualifiedName: "Command.Third", StartLine: 1, EndLine: 2},
	}
	for name, text := range map[string]string{
		"one kind, files at the top": `{"total":3,"returned":3,"results":{"repository":"r","files":[` +
			`{"file":"a.go","at":["First@3-9"]},{"file":"b.go","at":["Second@7"]},{"file":"c.go","at":["Command.Third@1-2"]}]}}`,
		"several kinds, files one level down": `{"total":3,"returned":3,"results":{"repository":"r","groups":[` +
			`{"kind":"func","files":[{"file":"a.go","at":["First@3-9"]},{"file":"b.go","at":["Second@7"]}]},` +
			`{"kind":"method","files":[{"file":"c.go","at":["Command.Third@1-2"]}]}]}}`,
	} {
		got, err := compactRows("find_references", text)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: rows = %#v, want %#v", name, got, want)
		}
	}
}
