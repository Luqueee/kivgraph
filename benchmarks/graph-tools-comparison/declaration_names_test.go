package main

import (
	"reflect"
	"testing"
)

func TestDeclarationNamesTopLevelOnly(t *testing.T) {
	rust := []string{
		"audio::range::RangeOutcome", "audio::range::build_response",
		"audio::range::impl::RangeOutcome::span", "audio::range::tests",
		"audio::range::tests::parses_send_file_matrix", "audio::range",
		"audio::range::parse_range",
	}
	want := []string{"RangeOutcome", "build_response", "tests", "parse_range"}
	if got := declarationNames(rust); !reflect.DeepEqual(want, got) {
		t.Fatalf("rust = %#v, want %#v", got, want)
	}
	bare := []string{"withRetry", "retryInfo", "handleCreate.mentionable"}
	if got := declarationNames(bare); !reflect.DeepEqual([]string{"withRetry", "retryInfo"}, got) {
		t.Fatalf("bare = %#v", got)
	}
}
