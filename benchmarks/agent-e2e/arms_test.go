package main

import (
	"reflect"
	"testing"
)

// The scorer is the one piece of arithmetic in this harness, and a silent error
// in it would not fail a run -- it would publish a wrong verdict. These cases
// are the ones that decide a report: a perfect run, a partial one, an arm that
// edited nothing, and an arm that edited the wrong files.
func TestScoreComparesWrittenFilesAgainstTheAuthorsChange(t *testing.T) {
	subject := task{Truth: []string{"src/a.ts", "src/b.ts", "tests/a.test.ts"}}

	for _, testCase := range []struct {
		name  string
		wrote []string
		want  runResult
	}{
		{
			name:  "every file and nothing else",
			wrote: []string{"src/a.ts", "src/b.ts", "tests/a.test.ts"},
			want:  runResult{Precision: 1, Recall: 1, Exact: true},
		},
		{
			name:  "the test was skipped",
			wrote: []string{"src/a.ts", "src/b.ts"},
			want:  runResult{Precision: 1, Recall: 2.0 / 3.0, Missing: []string{"tests/a.test.ts"}},
		},
		{
			name:  "one right file and one unrelated file",
			wrote: []string{"src/a.ts", "src/unrelated.ts"},
			want: runResult{
				Precision: 0.5, Recall: 1.0 / 3.0,
				Missing:  []string{"src/b.ts", "tests/a.test.ts"},
				Spurious: []string{"src/unrelated.ts"},
			},
		},
		{
			name:  "nothing was written at all",
			wrote: nil,
			want: runResult{
				Precision: 0, Recall: 0,
				Missing: []string{"src/a.ts", "src/b.ts", "tests/a.test.ts"},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := runResult{}
			result.score(subject, testCase.wrote, nil)

			if result.Precision != testCase.want.Precision || result.Recall != testCase.want.Recall {
				t.Errorf("P=%.3f R=%.3f, want P=%.3f R=%.3f",
					result.Precision, result.Recall, testCase.want.Precision, testCase.want.Recall)
			}
			if result.Exact != testCase.want.Exact {
				t.Errorf("Exact = %v, want %v", result.Exact, testCase.want.Exact)
			}
			if !reflect.DeepEqual(result.Missing, testCase.want.Missing) {
				t.Errorf("Missing = %q, want %q", result.Missing, testCase.want.Missing)
			}
			if !reflect.DeepEqual(result.Spurious, testCase.want.Spurious) {
				t.Errorf("Spurious = %q, want %q", result.Spurious, testCase.want.Spurious)
			}
		})
	}
}

// A prompt that names a file answers the question the benchmark is asking, so the
// frozen task set is checked for it here rather than trusted to have stayed clean.
func TestFrozenPromptsNameNoSourceFile(t *testing.T) {
	set, err := loadTasks("tasks.json")
	if err != nil {
		t.Fatalf("loadTasks() error = %v", err)
	}
	for _, subject := range set.Tasks {
		for _, file := range subject.Truth {
			if contains(subject.Intent, file) {
				t.Errorf("%s: the prompt names %s, which is the answer", subject.ID, file)
			}
		}
		if subject.NTruth != len(subject.Truth) {
			t.Errorf("%s: n_truth = %d, but %d files are listed", subject.ID, subject.NTruth, len(subject.Truth))
		}
	}
}

func contains(haystack, needle string) bool {
	return needle != "" && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return index
		}
	}
	return -1
}
