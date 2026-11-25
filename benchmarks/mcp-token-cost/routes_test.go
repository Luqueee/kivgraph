package main

import "testing"

func TestRouteComparisonPreservesPaginationAndCoverage(t *testing.T) {
	page := `{"coverage":{"exact":2},"next_cursor":"abc"}`
	for _, other := range []string{`{"coverage":{"exact":1},"next_cursor":"def"}`, `{"coverage":{"exact":2}}`} {
		if samePage(page, other) {
			t.Errorf("accepted different page: %s", other)
		}
	}
	if !samePage(page, `{"coverage":{"exact":2},"next_cursor":"def"}`) {
		t.Fatal("opaque cursor contents are not reference facts")
	}
}
