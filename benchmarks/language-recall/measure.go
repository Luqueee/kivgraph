package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type symbolResult struct {
	Repository string   `json:"repository"`
	Path       string   `json:"path"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Line       int      `json:"line"`
	Truth      []string `json:"truth_files"`
	Reported   []string `json:"reported_files"`
	Missed     []string `json:"missed_files,omitempty"`
	Extra      []string `json:"extra_files,omitempty"`
	Verdict    string   `json:"verdict"`
}

// ratio is a numerator and a denominator that can be summed across symbols.
type ratio struct{ num, den int }

func measureLanguage(ctx context.Context, session *sdkmcp.ClientSession, oracles map[string]*oracle, members []corpusRepository, language string, cfg config) (languageResult, error) {
	allowed := map[string]bool{}
	for _, kind := range kinds[language] {
		allowed[kind] = true
	}
	result := languageResult{Language: language, Skipped: map[string]int{}}
	var candidates []candidate
	for _, repository := range members {
		framed, err := buildFrame(ctx, session, repository, allowed)
		if err != nil {
			return result, err
		}
		result.Listed += framed.Listed
		for reason, n := range framed.Skipped {
			result.Skipped[reason] += n
		}
		candidates = append(candidates, framed.Candidates...)
	}
	result.Eligible = len(candidates)
	if len(candidates) == 0 {
		return result, fmt.Errorf("no declaration could be sampled")
	}
	repositories := map[string]corpusRepository{}
	for _, repository := range members {
		repositories[repository.Name] = repository
	}

	picked := draw(candidates, cfg.Count, cfg.Seed)
	var all, nonTest, testOnly, precision []ratio
	var fullRecall, zeroTruth ratio
	byKind := map[string][]ratio{}
	verdicts := map[string]int{}
	for index, pick := range picked {
		repository := repositories[pick.Repository]
		content, err := os.ReadFile(filepath.Join(repository.Path, pick.Path))
		if err != nil {
			return result, err
		}
		truth, err := oracles[pick.Repository].dependents(ctx, pick.Path, pick.Line, pick.Name, rename(content, pick.offset, pick.Name))
		if err != nil {
			result.OracleFailures++
			continue
		}
		reported, verdict, err := referencingFiles(ctx, session, pick.Repository, pick.StableKey)
		if err != nil {
			result.KivgraphFailures++
			continue
		}
		// A file the oracle does not check has no truth to be compared with.
		for file := range reported {
			if !inProject(repository, file) {
				delete(reported, file)
			}
		}
		if verdict == "" {
			verdict = "none"
		}
		verdicts[verdict]++
		symbol := symbolResult{
			Repository: pick.Repository, Path: pick.Path, Name: pick.Name, Kind: pick.Kind, Line: pick.Line,
			Verdict: verdict,
		}
		var hit, hitNonTest, truthNonTest, hitTest, truthTest int
		for file := range truth {
			symbol.Truth = append(symbol.Truth, file)
			isTest := isTestFile(language, file)
			if isTest {
				truthTest++
			} else {
				truthNonTest++
			}
			if reported[file] {
				hit++
				if isTest {
					hitTest++
				} else {
					hitNonTest++
				}
			} else {
				symbol.Missed = append(symbol.Missed, file)
			}
		}
		for file := range reported {
			symbol.Reported = append(symbol.Reported, file)
			if _, ok := truth[file]; !ok {
				symbol.Extra = append(symbol.Extra, file)
			}
		}
		sort.Strings(symbol.Truth)
		sort.Strings(symbol.Reported)
		sort.Strings(symbol.Missed)
		sort.Strings(symbol.Extra)
		result.Symbols = append(result.Symbols, symbol)
		if verdict == "COMPLETE" && hitNonTest < truthNonTest {
			result.CompleteWithMiss++
		}
		if len(truth) > 0 {
			result.WithDependents++
			all = append(all, ratio{hit, len(truth)})
			byKind[pick.Kind] = append(byKind[pick.Kind], ratio{hit, len(truth)})
			if truthNonTest > 0 {
				nonTest = append(nonTest, ratio{hitNonTest, truthNonTest})
			}
			if truthTest > 0 {
				testOnly = append(testOnly, ratio{hitTest, truthTest})
			}
			fullRecall.den++
			if hit == len(truth) {
				fullRecall.num++
			}
		} else {
			zeroTruth.den++
			if len(reported) > 0 {
				zeroTruth.num++
			}
		}
		if len(reported) > 0 {
			precision = append(precision, ratio{hit, len(reported)})
		}
		if (index+1)%10 == 0 {
			fmt.Fprintf(os.Stderr, "%s: %d of %d declarations\n", language, index+1, len(picked))
		}
	}
	result.Sampled = len(result.Symbols)
	result.Verdicts = verdicts
	random := rand.New(rand.NewPCG(uint64(cfg.Seed), 1))
	result.Recall = bootstrap(all, random)
	result.RecallNonTest = bootstrap(nonTest, random)
	result.RecallTest = bootstrap(testOnly, random)
	result.Precision = bootstrap(precision, random)
	// ES private names (`#x`) get their own row: they are a different kind of
	// declaration to resolve, and one class with many of them can dominate a
	// kind's figure.
	var private, nonPrivate []ratio
	for _, symbol := range result.Symbols {
		if len(symbol.Truth) == 0 {
			continue
		}
		part := ratio{len(symbol.Truth) - len(symbol.Missed), len(symbol.Truth)}
		if strings.HasPrefix(symbol.Name, "#") {
			private = append(private, part)
		} else {
			nonPrivate = append(nonPrivate, part)
		}
	}
	result.RecallPrivate = bootstrap(private, random)
	result.RecallNonPrivate = bootstrap(nonPrivate, random)
	result.FullRecallSymbols = proportion(fullRecall)
	result.WithoutDependentsReported = proportion(zeroTruth)
	var names []string
	for kind := range byKind {
		names = append(names, kind)
	}
	sort.Strings(names)
	for _, kind := range names {
		m := bootstrap(byKind[kind], random)
		result.ByKind = append(result.ByKind, kindResult{Kind: kind, Symbols: len(byKind[kind]), Recall: m})
	}
	return result, nil
}

// bootstrap is the pooled ratio over symbols, with a percentile interval from
// resampling the symbols. Files within a symbol are not independent, so the
// symbol is the unit that is resampled.
func bootstrap(parts []ratio, random *rand.Rand) metric {
	var num, den int
	for _, part := range parts {
		num += part.num
		den += part.den
	}
	m := metric{Num: num, Den: den, Symbols: len(parts)}
	if den == 0 {
		return m
	}
	m.Value = float64(num) / float64(den)
	const rounds = 2000
	samples := make([]float64, 0, rounds)
	for r := 0; r < rounds; r++ {
		var n, d int
		for range parts {
			part := parts[random.IntN(len(parts))]
			n += part.num
			d += part.den
		}
		if d > 0 {
			samples = append(samples, float64(n)/float64(d))
		}
	}
	sort.Float64s(samples)
	if len(samples) > 0 {
		m.Low = samples[int(0.025*float64(len(samples)))]
		m.High = samples[int(0.975*float64(len(samples)-1))]
	}
	return m
}

// proportion is a share of symbols with a Wilson interval, which stays inside
// [0, 1] and behaves at the extremes where the normal approximation does not.
func proportion(r ratio) metric {
	m := metric{Num: r.num, Den: r.den, Symbols: r.den}
	if r.den == 0 {
		return m
	}
	const z = 1.959964
	n, p := float64(r.den), float64(r.num)/float64(r.den)
	denominator := 1 + z*z/n
	centre := (p + z*z/(2*n)) / denominator
	margin := z * sqrt(p*(1-p)/n+z*z/(4*n*n)) / denominator
	m.Value, m.Low, m.High = p, centre-margin, centre+margin
	return m
}
