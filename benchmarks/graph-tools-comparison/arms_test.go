package main

import (
	"reflect"
	"testing"
)

// TestScoreAgainstScoresAnAbsence defends the convention an absence question
// depends on. Both ratios are undefined against an empty truth, and the
// question is only worth asking if answering it correctly -- claiming nothing
// -- is the score that beats claiming something.
func TestScoreAgainstScoresAnAbsence(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		claimed []string
		truth   []string
		want    score
	}{
		{
			name: "nothing claimed against nothing true is exact",
			want: score{Claimed: []string{}, Missing: []string{}, Spurious: []string{}, Precision: 1, Recall: 1},
		},
		{
			name:    "anything claimed against nothing true has nothing to miss",
			claimed: []string{"repo:b.go", "repo:a.go"},
			want: score{
				Claimed: []string{"repo:a.go", "repo:b.go"}, Missing: []string{},
				Spurious: []string{"repo:a.go", "repo:b.go"}, Precision: 0, Recall: 1,
			},
		},
		{
			name:    "nothing claimed against a real truth is not an absence",
			truth:   []string{"repo:a.go"},
			want:    score{Claimed: []string{}, Missing: []string{"repo:a.go"}, Spurious: []string{}},
			claimed: nil,
		},
		{
			name:    "one hit and one miss out of two claimed",
			claimed: []string{"repo:a.go", "repo:c.go"},
			truth:   []string{"repo:a.go", "repo:b.go"},
			want: score{
				Claimed: []string{"repo:a.go", "repo:c.go"}, Missing: []string{"repo:b.go"},
				Spurious: []string{"repo:c.go"}, Precision: 0.5, Recall: 0.5,
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := scoreAgainst(testCase.claimed, testCase.truth)
			if !reflect.DeepEqual(testCase.want, *got) {
				t.Fatalf("scoreAgainst() = %#v, want %#v", *got, testCase.want)
			}
		})
	}
}
