package indexing

import (
	"math"
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

// memoryLimit reads the soft memory limit without changing it.
func memoryLimit() int64 { return debug.SetMemoryLimit(-1) }

func TestCollectSoonerKeepsTheHeapUnderTheContainersLimit(t *testing.T) {
	container := int64(3 << 30)
	found := func() (int64, bool) { return container, true }
	absent := func() (int64, bool) { return 0, false }

	t.Run("a pass runs under four fifths of the limit and restores it", func(t *testing.T) {
		t.Setenv("GOMEMLIMIT", "")
		os.Unsetenv("GOMEMLIMIT")
		before := debug.SetMemoryLimit(math.MaxInt64)
		defer debug.SetMemoryLimit(before)

		first := collectSoonerWithin(found)
		if got, want := memoryLimit(), int64(float64(container)*cgroupHeadroom); got != want {
			t.Fatalf("during a pass the limit is %d, want %d", got, want)
		}
		second := collectSoonerWithin(found)
		first()
		if got := memoryLimit(); got == math.MaxInt64 {
			t.Fatal("with a pass still running the first to finish put the limit back")
		}
		second()
		if got := memoryLimit(); got != math.MaxInt64 {
			t.Fatalf("after the last pass the limit is %d, want none", got)
		}
	})

	for _, test := range []struct {
		name  string
		setup func(t *testing.T)
		limit func() (int64, bool)
		want  int64
	}{
		{"GOMEMLIMIT in the environment", func(t *testing.T) { t.Setenv("GOMEMLIMIT", "1GiB") }, found, math.MaxInt64},
		{"a limit already set from code", func(t *testing.T) { os.Unsetenv("GOMEMLIMIT"); debug.SetMemoryLimit(1 << 30) }, found, 1 << 30},
		{"no limit on the container", func(t *testing.T) { os.Unsetenv("GOMEMLIMIT") }, absent, math.MaxInt64},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := debug.SetMemoryLimit(math.MaxInt64)
			defer debug.SetMemoryLimit(before)
			test.setup(t)
			end := collectSoonerWithin(test.limit)
			if got := memoryLimit(); got != test.want {
				t.Fatalf("during a pass the limit is %d, want %d", got, test.want)
			}
			end()
			if got := memoryLimit(); got != test.want {
				t.Fatalf("after the pass the limit is %d, want %d", got, test.want)
			}
		})
	}
}
