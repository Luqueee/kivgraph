package main

import (
	"strings"
	"testing"
)

func TestRenderKeyValueTableAlignsHumanOutput(t *testing.T) {
	got := renderKeyValueTable("Daemon supervisor", []keyValueRow{
		{Key: "State", Value: "absent"},
		{Key: "Unit", Value: "/tmp/unit.plist"},
	})
	want := "Daemon supervisor\n  State: absent\n   Unit: /tmp/unit.plist\n"
	if got != want {
		t.Fatalf("renderKeyValueTable() = %q, want %q", got, want)
	}
}

func TestRenderKeyValueTableKeepsEmptyValuesVisible(t *testing.T) {
	got := renderKeyValueTable("Status", []keyValueRow{{Key: "Endpoint", Value: "not published"}})
	if !strings.Contains(got, "Endpoint: not published\n") {
		t.Fatalf("table omitted an empty-state value: %q", got)
	}
}
