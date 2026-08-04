package hotsnapshot

import (
	"strconv"
	"strings"
	"testing"
)

var (
	benchmarkStringTable StringTable
	benchmarkStrings     []string
)

func BenchmarkStringInterner(b *testing.B) {
	values := benchmarkValues()
	b.ReportAllocs()
	for b.Loop() {
		interner := NewStringInterner()
		for _, value := range values {
			if _, err := interner.Intern(value); err != nil {
				b.Fatal(err)
			}
		}
		benchmarkStringTable = interner.Freeze()
	}
}

func BenchmarkDuplicatedStrings(b *testing.B) {
	values := benchmarkValues()
	b.ReportAllocs()
	for b.Loop() {
		copied := make([]string, 0, len(values))
		for _, value := range values {
			copied = append(copied, strings.Clone(value))
		}
		benchmarkStrings = copied
	}
}

func benchmarkValues() []string {
	const (
		distinct = 1_000
		repeats  = 100
	)
	values := make([]string, 0, distinct*repeats)
	for index := 0; index < distinct; index++ {
		value := "packages/repository-" + strconv.Itoa(index) + "/src/index.ts"
		for repeat := 0; repeat < repeats; repeat++ {
			values = append(values, value)
		}
	}
	return values
}
