package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A cursor is bound to its selector. Its presence matters; its opaque value does not.
func samePage(a, b string) bool {
	decode := func(text string) map[string]json.RawMessage {
		var page map[string]json.RawMessage
		if json.Unmarshal([]byte(text), &page) != nil {
			return nil
		}
		if _, ok := page["next_cursor"]; ok {
			page["next_cursor"] = json.RawMessage(`true`)
		}
		return page
	}
	left, right := decode(a), decode(b)
	return left != nil && maps.EqualFunc(left, right, slices.Equal[json.RawMessage])
}

func sourceBatches(ctx context.Context, session *sdkmcp.ClientSession, subject map[string]any, rows []compactRow) ([]string, int, error) {
	requests := make([]map[string]any, 1, len(rows)+1)
	requests[0] = subject
	for _, r := range rows {
		requests = append(requests, selector(r.Repository, r.FilePath, r.QualifiedName))
	}
	var texts []string
	structured := 0
	for batch := range slices.Chunk(requests, maximumSourceSymbols) {
		text, n, err := call(ctx, session, "get_source", map[string]any{"symbols": batch})
		if err != nil {
			return nil, 0, err
		}
		texts = append(texts, text)
		structured += n
	}
	return texts, structured, nil
}

type routeSample struct {
	lookup, page string
	bodies       []string
	answer, full time.Duration
}

func sampleRoute(ctx context.Context, s *sdkmcp.ClientSession, q question, direct bool) (v routeSample, err error) {
	start := time.Now()
	var subject map[string]any
	args := map[string]any{"name": q.Symbol}
	if !direct {
		var row symbolRow
		row, v.lookup, _, err = resolveSubject(ctx, s, q)
		if err != nil {
			return v, err
		}
		subject = selector(row.Repository, row.FilePath, row.QualifiedName)
		args = maps.Clone(subject)
	} else if q.Path != "" && q.Repository != "" {
		args = selector(q.Repository, q.Path, q.QualifiedName())
	}
	args["direction"] = "incoming"
	v.page, _, err = call(ctx, s, "find_references", args)
	if err != nil {
		return v, err
	}
	v.answer = time.Since(start)
	if direct {
		var p struct{ Results struct{ Subject, Qn string } }
		if err = json.Unmarshal([]byte(v.page), &p); err != nil {
			return v, err
		}
		repo, path, _, e := parseSymbolLocation("", p.Results.Subject)
		if e != nil {
			return v, e
		}
		subject = selector(repo, path, p.Results.Qn)
	}
	rows, err := compactRows("find_references", v.page)
	if err != nil {
		return v, err
	}
	v.bodies, _, err = sourceBatches(ctx, s, subject, rows)
	v.full = time.Since(start)
	return v, err
}

var routeArms = [2]string{"lookup-first", "direct"}

// routeRow is one measured route sample, kept in the order it ran.
type routeRow struct {
	Question  string `json:"question"`
	Pair      int    `json:"pair"`
	Arm       string `json:"arm"`
	Position  int    `json:"position"`
	AnswerNS  int64  `json:"answer_ns"`
	SessionNS int64  `json:"session_ns"`
}

// writeRouteRows records every sample of one pair before the samples are pooled
// into percentiles, so the percentiles can be recomputed from the same rows.
func writeRouteRows(enc *json.Encoder, question string, pair int, order [2]int, samples [2]routeSample) error {
	if enc == nil {
		return nil
	}
	for pos := range 2 {
		k := (order[0] - 1 + pos) % 2
		v := samples[k]
		row := routeRow{Question: question, Pair: pair, Arm: routeArms[k], Position: order[k], AnswerNS: v.answer.Nanoseconds(), SessionNS: v.full.Nanoseconds()}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return nil
}

func compareRoutes(ctx context.Context, s *sdkmcp.ClientSession, t *counter, questions []question, baseline results, rawPath string) error {
	const pairs = 20
	var answers, full [2][]time.Duration
	var answerTokens, sessionTokens [2]int
	var raw *json.Encoder
	if rawPath != "" {
		f, err := os.OpenFile(rawPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		raw = json.NewEncoder(f)
	}
	for _, q := range questions {
		for i := -1; i < pairs; i++ {
			var samples [2]routeSample
			var order [2]int
			for j := range 2 {
				k := (i + 1 + j) % 2
				v, err := sampleRoute(ctx, s, q, k == 1)
				if err != nil {
					return fmt.Errorf("%s: %w", q.Symbol, err)
				}
				samples[k] = v
				order[k] = j + 1
			}
			if !samePage(samples[0].page, samples[1].page) || !slices.Equal(samples[0].bodies, samples[1].bodies) {
				return fmt.Errorf("%s: reference pages or source batches differ", q.Symbol)
			}
			if i < 0 {
				continue
			}
			if err := writeRouteRows(raw, q.Symbol, i, order, samples); err != nil {
				return err
			}
			for k, v := range samples {
				answers[k] = append(answers[k], v.answer)
				full[k] = append(full[k], v.full)
				if i == 0 {
					n := t.count(v.lookup) + t.count(v.page)
					answerTokens[k] += n
					sessionTokens[k] += n
					for _, body := range v.bodies {
						sessionTokens[k] += t.count(body)
					}
				}
			}
		}
	}
	fmt.Printf("native: %d answer tokens, %d session tokens\n", baseline.Totals.NativeAnswer, baseline.Totals.Native)
	for k, name := range routeArms {
		fmt.Printf("%s: %d answer tokens, %d served session tokens; %d samples", name, answerTokens[k], sessionTokens[k], len(full[k]))
		for i, series := range [][]time.Duration{answers[k], full[k]} {
			slices.Sort(series)
			fmt.Printf("; %s p50/p95 %.3f/%.3f ms", [2]string{"answer", "session"}[i], float64(series[(len(series)-1)/2])/1e6, float64(series[(95*len(series)+99)/100-1])/1e6)
		}
		fmt.Println()
	}
	return nil
}
