package tools

import (
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/indexing"
)

func approval() sdkmcp.InputResponseMap {
	return sdkmcp.InputResponseMap{
		indexConsentRequestID: &sdkmcp.ElicitResult{Action: "accept", Content: map[string]any{"confirmed": true}},
	}
}

// Consent is an answer to a question about one batch. Anything that is not that
// answer is no answer, and the question is asked again: an approval kept from
// another call, one echoed with no state, and one to a question this server
// never asked.
func TestConsentAnswerIsOnlyAnAnswerToThisBatch(t *testing.T) {
	batch := []indexing.Project{{Name: "demo", Path: "/tmp/demo", Languages: []string{"go"}}}
	other := []indexing.Project{{Name: "demo", Path: "/tmp/other", Languages: []string{"go"}}}
	digest := batchDigest(batch)

	for name, test := range map[string]struct {
		responses sdkmcp.InputResponseMap
		state     string
		answered  bool
		approved  bool
	}{
		"no response":                     {nil, digest, false, false},
		"an empty response":               {sdkmcp.InputResponseMap{}, digest, false, false},
		"approval with no state":          {approval(), "", false, false},
		"approval given for another set":  {approval(), batchDigest(other), false, false},
		"an answer to another question":   {sdkmcp.InputResponseMap{"other": &sdkmcp.ElicitResult{Action: "accept"}}, digest, false, false},
		"a response that is nothing":      {sdkmcp.InputResponseMap{indexConsentRequestID: nil}, digest, true, false},
		"a nil elicit result":             {sdkmcp.InputResponseMap{indexConsentRequestID: (*sdkmcp.ElicitResult)(nil)}, digest, true, false},
		"declined":                        {sdkmcp.InputResponseMap{indexConsentRequestID: &sdkmcp.ElicitResult{Action: "decline"}}, digest, true, false},
		"cancelled":                       {sdkmcp.InputResponseMap{indexConsentRequestID: &sdkmcp.ElicitResult{Action: "cancel"}}, digest, true, false},
		"declined with confirmed content": {sdkmcp.InputResponseMap{indexConsentRequestID: &sdkmcp.ElicitResult{Action: "decline", Content: map[string]any{"confirmed": true}}}, digest, true, false},
		"accepted, not confirmed":         {sdkmcp.InputResponseMap{indexConsentRequestID: &sdkmcp.ElicitResult{Action: "accept", Content: map[string]any{"confirmed": false}}}, digest, true, false},
		"accepted with no content":        {sdkmcp.InputResponseMap{indexConsentRequestID: &sdkmcp.ElicitResult{Action: "accept"}}, digest, true, false},
		"accepted and confirmed":          {approval(), digest, true, true},
	} {
		approved, answered := consentAnswer(test.responses, test.state, digest)
		if answered != test.answered || approved != test.approved {
			t.Errorf("%s: consentAnswer = (approved %t, answered %t), want (%t, %t)",
				name, approved, answered, test.approved, test.answered)
		}
	}
}

// The digest names what an approval covers, so every part of a project is in it
// and no two batches that differ in one are the same: it is compared with a
// brute-force pairing over generated batches.
func TestBatchDigestSeparatesEveryBatchThatDiffers(t *testing.T) {
	names := []string{"a", "b"}
	paths := []string{"/p", "/q"}
	languages := [][]string{{"go"}, {"go", "ts"}, {"ts"}, nil}
	var batches [][]indexing.Project
	for _, name := range names {
		for _, path := range paths {
			for _, language := range languages {
				batches = append(batches, []indexing.Project{{Name: name, Path: path, Languages: language}})
			}
		}
	}
	// Order matters to an approval that names projects, and a longer batch is
	// another batch.
	one := indexing.Project{Name: "a", Path: "/p", Languages: []string{"go"}}
	two := indexing.Project{Name: "b", Path: "/q", Languages: []string{"go"}}
	batches = append(batches, []indexing.Project{one, two}, []indexing.Project{two, one}, nil)

	seen := map[string]int{}
	for index, batch := range batches {
		digest := batchDigest(batch)
		if previous, clash := seen[digest]; clash {
			t.Fatalf("batches %d (%v) and %d (%v) share the digest %s", previous, batches[previous], index, batch, digest)
		}
		seen[digest] = index
	}
	if batchDigest(batches[0]) != batchDigest(append([]indexing.Project(nil), batches[0]...)) {
		t.Error("a copy of a batch must have the digest of the batch")
	}
	// A field that runs into the next must not read as the other one.
	if batchDigest([]indexing.Project{{Name: "ab", Path: "c"}}) == batchDigest([]indexing.Project{{Name: "a", Path: "bc"}}) {
		t.Error("moving a character from one field to the next kept the digest")
	}
}

// Without a client that can answer, the explicit flag is the only consent, and
// it is never assumed.
func TestRequireIndexConsentWithoutAFormClientNeedsTheFlag(t *testing.T) {
	batch := []indexing.Project{{Name: "demo", Path: "/tmp/demo", Languages: []string{"go"}}}
	yes, no := true, false
	for name, test := range map[string]struct {
		confirmed *bool
		wantCode  string
	}{
		"absent":    {nil, CodePermissionRequired},
		"false":     {&no, CodePermissionRequired},
		"confirmed": {&yes, ""},
	} {
		pending, err := requireIndexConsent(nil, batch, test.confirmed)
		if pending != nil {
			t.Errorf("%s: a client that cannot answer was asked a question", name)
		}
		if got := ErrorCode(err); got != test.wantCode {
			t.Errorf("%s: error code = %q, want %q (error %v)", name, got, test.wantCode, err)
		}
	}
}
