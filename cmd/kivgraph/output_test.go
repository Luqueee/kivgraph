package main

import (
	"strings"
	"testing"
)

func TestRenderKeyValueTableAlignsHumanOutput(t *testing.T) {
	title := "Daemon supervisor"
	rows := []keyValueRow{
		{Key: "State", Value: "absent"},
		{Key: "Unit", Value: "/tmp/unit.plist"},
	}
	got := renderKeyValueTable(title, rows)
	want := "Daemon supervisor\n  State: absent\n   Unit: /tmp/unit.plist\n"
	if got != want {
		t.Fatalf("renderKeyValueTable(%q, %#v) = %q, want %q", title, rows, got, want)
	}
}

func TestRenderKeyValueTableKeepsEmptyValuesVisible(t *testing.T) {
	title := "Status"
	rows := []keyValueRow{{Key: "Endpoint", Value: ""}}
	got := renderKeyValueTable(title, rows)
	if !strings.Contains(got, "Endpoint: \n") {
		t.Fatalf("renderKeyValueTable(%q, %#v) omitted an empty-state value: %q", title, rows, got)
	}
}
