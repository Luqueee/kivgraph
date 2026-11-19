package indexing

import (
	"os"
	"runtime/debug"
	"testing"
)

// percent reads the GC percent without changing it.
func percent() int {
	current := debug.SetGCPercent(100)
	debug.SetGCPercent(current)
	return current
}

func TestCollectSoonerLowersTheGCPercentForTheLengthOfThePasses(t *testing.T) {
	if _, set := os.LookupEnv("GOGC"); set {
		t.Skip("GOGC is set: the policy leaves it alone, which the next test covers")
	}
	debug.SetGCPercent(100)
	defer debug.SetGCPercent(100)

	first := collectSooner()
	if got := percent(); got != indexingGCPercent {
		t.Fatalf("during a pass the percent is %d, want %d", got, indexingGCPercent)
	}
	second := collectSooner()
	first()
	if got := percent(); got != indexingGCPercent {
		t.Fatalf("with a pass still running the percent is %d, want %d: the first to finish must not restore it", got, indexingGCPercent)
	}
	second()
	if got := percent(); got != 100 {
		t.Fatalf("after the last pass the percent is %d, want the 100 it had", got)
	}
}

func TestCollectSoonerLeavesAChoiceOfTheOperatorAlone(t *testing.T) {
	for _, test := range []struct {
		name string
		set  func(t *testing.T)
		want int
	}{
		{"GOGC in the environment", func(t *testing.T) { t.Setenv("GOGC", "200") }, 100},
		{"a percent already lower", func(t *testing.T) { debug.SetGCPercent(20) }, 20},
		{"a collector that is off", func(t *testing.T) { debug.SetGCPercent(-1) }, -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			debug.SetGCPercent(100)
			defer debug.SetGCPercent(100)
			test.set(t)
			end := collectSooner()
			if got := percent(); got != test.want {
				t.Fatalf("during a pass the percent is %d, want %d", got, test.want)
			}
			end()
			if got := percent(); got != test.want {
				t.Fatalf("after the pass the percent is %d, want %d", got, test.want)
			}
		})
	}
}
