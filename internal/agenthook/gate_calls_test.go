package agenthook

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/mcp/tools"
)

// A refusal tells the agent what to call instead, and it will: a call the tool
// rejects is worse than no advice. One of them was -- get_blast_radius has no
// `name` -- so every call a refusal prints is checked against the arguments
// the tool declares.
func TestEveryCallARefusalRecommendsIsOneTheToolAccepts(t *testing.T) {
	inputs := map[string]reflect.Type{
		"find_references":  reflect.TypeOf(tools.FindReferencesInput{}),
		"find_by_intent":   reflect.TypeOf(tools.FindByIntentInput{}),
		"get_blast_radius": reflect.TypeOf(tools.GetBlastRadiusInput{}),
		"get_file_outline": reflect.TypeOf(tools.GetFileOutlineInput{}),
	}
	accepted := func(tool reflect.Type) map[string]bool {
		out := map[string]bool{}
		for index := range tool.NumField() {
			name, _, _ := strings.Cut(tool.Field(index).Tag.Get("json"), ",")
			out[name] = true
		}
		return out
	}
	ambiguous := Facts{Declarations: 4, Repositories: 2, References: 60, Sample: []string{"repo:a.go:1"}}
	gate := Gate{Graph: &fakeGraph{symbol: ambiguous, intent: ambiguous}, Repository: "repo", Indexed: IndexedExtensions([]string{".go"})}
	reasons := []string{
		gate.Decide(context.Background(), Question{Kind: KindSymbol, Pattern: "Command"}).Reason,
		gate.Decide(context.Background(), Question{Kind: KindIntent, Pattern: "http|route|handler|serve"}).Reason,
		gate.Decide(context.Background(), Question{Kind: KindFiles, Pattern: "*.go"}).Reason,
		gate.denyResearchAgent(Question{Pattern: "where the routes are"}).Reason,
	}
	call := regexp.MustCompile(`(?m)^\s+([a-z_]+)\(([^)]*)\)`)
	argument := regexp.MustCompile(`([a-z_]+)=`)
	checked := 0
	for index, reason := range reasons {
		if reason == "" {
			t.Fatalf("question %d of this test was not refused, so its advice was not checked", index)
		}
		for _, match := range call.FindAllStringSubmatch(reason, -1) {
			tool, known := inputs[match[1]]
			if !known {
				t.Errorf("refusal recommends %q, which this test does not know: add its input type\n%s", match[1], reason)
				continue
			}
			arguments := accepted(tool)
			for _, name := range argument.FindAllStringSubmatch(match[2], -1) {
				checked++
				if !arguments[name[1]] {
					t.Errorf("refusal recommends %s(%s=…), and %s takes no %q\n%s", match[1], name[1], match[1], name[1], reason)
				}
			}
		}
	}
	if checked < 5 {
		t.Fatalf("only %d recommended arguments were found: the refusals changed shape and this test reads none", checked)
	}
}
