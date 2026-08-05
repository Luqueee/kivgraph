package goloader

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMeasurePrecisionEmitsTheGoSemanticGate(t *testing.T) {
	report, err := MeasurePrecision(context.Background(), filepath.Join("..", "..", "testdata", "go"))
	if err != nil {
		t.Fatalf("MeasurePrecision() error = %v", err)
	}

	for _, entry := range report.Cases {
		if len(entry.MissingEdges) != 0 || len(entry.UnexpectedEdges) != 0 {
			t.Fatalf("case %q edges differ:\nmissing: %v\nunexpected: %v",
				entry.Name, entry.MissingEdges, entry.UnexpectedEdges)
		}
		if len(entry.MissingUnresolved) != 0 || len(entry.UnexpectedUnresolved) != 0 {
			t.Fatalf("case %q unresolved differ:\nmissing: %v\nunexpected: %v",
				entry.Name, entry.MissingUnresolved, entry.UnexpectedUnresolved)
		}
	}

	totals := report.Totals
	if totals.ExpectedEdges != 16 || totals.TruePositives != 16 {
		t.Fatalf("totals = %+v, want the sixteen fixture edges", totals)
	}
	if totals.FalseExactEdges != 0 || totals.FalsePositives != 0 || totals.FalseNegatives != 0 {
		t.Fatalf("totals = %+v, want no false edge", totals)
	}
	if totals.Precision != 1 || totals.Recall != 1 {
		t.Fatalf("precision=%v recall=%v", totals.Precision, totals.Recall)
	}
	if totals.ExpectedUnresolved != 2 || totals.UnresolvedCorrectlyClassified != 2 ||
		totals.UnresolvedMisclassified != 0 {
		t.Fatalf("unresolved totals = %+v", totals)
	}
	if report.Gate != PrecisionGate {
		t.Fatalf("gate = %q, want %q", report.Gate, PrecisionGate)
	}
}

func TestMeasurePrecisionIsDeterministic(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "go")
	first, err := MeasurePrecision(context.Background(), root)
	if err != nil {
		t.Fatalf("MeasurePrecision() error = %v", err)
	}
	second, err := MeasurePrecision(context.Background(), root)
	if err != nil {
		t.Fatalf("MeasurePrecision() error = %v", err)
	}
	if first.Totals != second.Totals || first.Gate != second.Gate {
		t.Fatalf("measurement is not deterministic:\n%+v\n%+v", first, second)
	}
	for index := range first.Cases {
		if first.Cases[index].Metrics != second.Cases[index].Metrics {
			t.Fatalf("case %q differs between runs", first.Cases[index].Name)
		}
	}
}

func TestMeasurePrecisionRejectsAMissingFixture(t *testing.T) {
	if _, err := MeasurePrecision(context.Background(), t.TempDir()); err == nil {
		t.Fatalf("MeasurePrecision() must fail without the fixtures")
	}
}
