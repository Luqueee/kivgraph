package indexing

import "testing"

func TestParseCgroupLimit(t *testing.T) {
	for _, test := range []struct {
		content string
		want    int64
		found   bool
	}{
		{"3221225472\n", 3221225472, true},
		{"max\n", 0, false},
		{"9223372036854771712\n", 0, false}, // what a cgroup v1 reports for no limit
		{"0\n", 0, false},
		{"-1\n", 0, false},
		{"", 0, false},
		{"3 GB", 0, false},
	} {
		got, found := parseCgroupLimit(test.content)
		if got != test.want || found != test.found {
			t.Errorf("parseCgroupLimit(%q) = %d, %v; want %d, %v", test.content, got, found, test.want, test.found)
		}
	}
}
