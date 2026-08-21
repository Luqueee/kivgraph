package main

// The question set and its ground truth.
//
// Five tools that all call themselves code graphs do not answer the same
// question. Asking only "who calls this" would put two of them at zero for
// being outside their purpose rather than for being wrong: graphify is a BFS
// over an extracted graph and code-review-graph is built around blast radius.
// So the set has three families, each one the question a tool's own
// documentation says it answers, and every tool is asked in its own vocabulary.
//
// What is compared is the answer, never the spelling: a claimed file is
// canonicalised to `repository:path` before scoring, and an outline is compared
// as a set of declaration names. A tool that returns the right facts in an
// unusual shape scores the same as one that returns them in ours.

// family is what a question asks for. It decides which truth applies and which
// call a tool is asked to make.
const (
	// familyReferences is "which files hold a call or reference to this
	// declaration". Truth is a set of files.
	familyReferences = "references"
	// familyImpact is "which files hold something that reaches this
	// declaration transitively". Truth is a set of files, at a declared depth.
	familyImpact = "impact"
	// familyOutline is "what is declared in this file". Truth is a set of
	// declaration names, because that is the answer's unit here.
	familyOutline = "outline"
)

// subject is one declaration, in the addressing every tool here accepts.
type subject struct {
	// Repo is the registered repository name, which is the directory's base.
	Repo string `json:"repo"`
	// Dir is where that repository lives relative to the corpus root: what
	// turns a corpus-relative path into a repository-relative one.
	Dir string `json:"dir"`
	// Path is repository-relative.
	Path string `json:"path"`
	// Name is the qualified name; for a free function, the bare name.
	Name string `json:"name"`
	// Symbol is the bare name a caller-tracing tool takes.
	Symbol string `json:"symbol"`
}

// corpusPath is the subject's path relative to the corpus root.
func (s subject) corpusPath() string { return s.Dir + "/" + s.Path }

type question struct {
	ID       string  `json:"id"`
	Family   string  `json:"family"`
	Ask      string  `json:"question"`
	Language string  `json:"language"`
	Subject  subject `json:"subject"`
	// Truth is what a correct answer holds: corpus-relative files for
	// references and impact, declaration names for an outline. The declaring
	// file is never in it -- every tool knows where the subject lives, because
	// the question said so, and counting it would inflate all five equally.
	Truth []string `json:"ground_truth"`
	// Depth is the hop count an impact question is scored at. Zero elsewhere.
	Depth int `json:"depth,omitempty"`
	// Declarations is every file declaring the bare name anywhere in the
	// corpus. The native arm reads all of them: that is the minimum a reader
	// needs to tell homonyms apart, and it is why `grep` alone cannot answer.
	Declarations []string `json:"declaration_sites,omitempty"`
}

// The reference truth is the manual classification recorded in
// benchmarks/codebase-memory-comparison and re-verified in
// benchmarks/graft-comparison: every `grep` occurrence read and attributed to
// the declaration it resolves against. The newest commit among the repositories
// these questions touch is 2026-08-12, so the same truth still applies.
var withRetryDeclarations = []string{
	"libraries/library-env/src/retry.ts",
	"libraries/library-shared/src/utils/retry.ts",
	"modules/sdk-module-ts/src/sdk/managers/CommandManager.ts",
	"modules/sdk-module-ts/src/sdk/types/ModuleResult.ts",
	"services/api-db-go/internal/infrastructure/postgres/retry.go",
	"services/api-db-go/internal/shared/infisical/infisical.go",
	"services/api-music/internal/shared/infisical/infisical.go",
}

var nowMsDeclarations = []string{
	"services/api-music-nodo/src/providers/chipbot.rs",
	"services/api-music-nodo/src/providers/deezer.rs",
	"services/api-music-nodo/src/system/chipbot_files.rs",
	"services/kenalink-rs/src/util.rs",
}

// questions is the measured set: four reference questions, one impact question
// and two outlines. Q1 has no reachable answer on this corpus and is kept for
// exactly that reason -- kena's repositories consume each other as published
// packages, never as source, so the honest result is an absence and dropping
// the question would hide it.
var questions = []question{
	{
		ID:       "R1_ts_xrepo",
		Family:   familyReferences,
		Ask:      "Which call sites use the withRetry declared in libraries/library-shared/src/utils/retry.ts?",
		Language: "typescript, cross-package",
		Subject: subject{
			Repo: "library-shared", Dir: "libraries/library-shared",
			Path: "src/utils/retry.ts", Name: "withRetry", Symbol: "withRetry",
		},
		Truth: []string{
			"modules/sdk-module-ts/src/sdk/client/KenaModule.ts",
			"packages/core/src/cluster/master/index.ts",
			"packages/core/src/cluster/worker/BotWorker.ts",
			"packages/core/src/shared/utils/sharding.ts",
			"packages/gateway/src/grpc/server.ts",
		},
		Declarations: withRetryDeclarations,
	},
	{
		ID:       "R2_go",
		Family:   familyReferences,
		Ask:      "Which call sites use withRetry in services/api-db-go/internal/infrastructure/postgres/retry.go?",
		Language: "go",
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/infrastructure/postgres/retry.go", Name: "withRetry", Symbol: "withRetry",
		},
		Truth: []string{
			"services/api-db-go/internal/infrastructure/postgres/client.go",
			"services/api-db-go/internal/infrastructure/postgres/retry_test.go",
		},
		Declarations: withRetryDeclarations,
	},
	{
		ID:       "R3_ts_intra",
		Family:   familyReferences,
		Ask:      "Which files call getRequiredField from packages/core ipcCase.ts?",
		Language: "typescript, intra-repository",
		Subject: subject{
			Repo: "core", Dir: "packages/core",
			Path: "src/cluster/worker/ipc/utils/ipcCase.ts", Name: "getRequiredField", Symbol: "getRequiredField",
		},
		Truth: []string{
			"packages/core/src/cluster/worker/ipc/channel.ipc.ts",
			"packages/core/src/cluster/worker/ipc/client.ipc.ts",
			"packages/core/src/cluster/worker/ipc/guild.ipc.ts",
			"packages/core/src/cluster/worker/ipc/member.ipc.ts",
			"packages/core/src/cluster/worker/ipc/role.ipc.ts",
			"packages/core/src/cluster/worker/ipc/thread.ipc.ts",
			"packages/core/src/cluster/worker/ipc/user.ipc.ts",
			"packages/core/src/cluster/worker/ipc/voice.ipc.ts",
			"packages/core/tests/cluster/worker/ipc/utils/ipcCase.test.ts",
		},
		Declarations: []string{"packages/core/src/cluster/worker/ipc/utils/ipcCase.ts"},
	},
	{
		ID:       "R4_rust",
		Family:   familyReferences,
		Ask:      "Which files call now_ms() from services/kenalink-rs/src/util.rs?",
		Language: "rust",
		Subject: subject{
			Repo: "kenalink-rs", Dir: "services/kenalink-rs",
			Path: "src/util.rs", Name: "util::now_ms", Symbol: "now_ms",
		},
		Truth: []string{
			"services/kenalink-rs/src/api_rest/error.rs",
			"services/kenalink-rs/src/api_rest/routes_players.rs",
			"services/kenalink-rs/src/api_rest/routes_sessions.rs",
			"services/kenalink-rs/src/api_ws/mod.rs",
			"services/kenalink-rs/src/audio/songbird_engine.rs",
			"services/kenalink-rs/src/main.rs",
		},
		Declarations: nowMsDeclarations,
	},
	{
		// The subject is unexported Go, so its reachable set is closed by the
		// language: nothing outside `internal/infrastructure/postgres` can name
		// it. That is what makes a transitive truth checkable by hand instead of
		// asserted -- `expBackoffJitter` is called from retry.go's own withRetry
		// and from retry_test.go, and withRetry is called from client.go, so two
		// hops reach exactly one file the first hop did not.
		ID:       "I1_go_depth2",
		Family:   familyImpact,
		Ask:      "Which files hold something that reaches expBackoffJitter within two hops?",
		Language: "go",
		Depth:    2,
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/infrastructure/postgres/retry.go",
			Name: "expBackoffJitter", Symbol: "expBackoffJitter",
		},
		Truth: []string{
			"services/api-db-go/internal/infrastructure/postgres/client.go",
			"services/api-db-go/internal/infrastructure/postgres/retry_test.go",
		},
		Declarations: []string{"services/api-db-go/internal/infrastructure/postgres/retry.go"},
	},
	{
		// 467 lines and 17 top-level declarations: big enough that reading it to
		// answer costs real tokens.
		ID:       "O1_ts_large",
		Family:   familyOutline,
		Ask:      "What is declared at the top level of packages/core ipc/channel.ipc.ts?",
		Language: "typescript",
		Subject: subject{
			Repo: "core", Dir: "packages/core",
			Path: "src/cluster/worker/ipc/channel.ipc.ts",
		},
		Truth: []string{
			"createErrorResponse", "createSuccessResponse", "handleAddFollower",
			"handleBulkDeleteMessages", "handleChannelIPC", "handleCreate",
			"handleCreateThread", "handleDelete", "handleEdit",
			"handleEditPermissions", "handleFetch", "handleList",
			"handleSetVoiceStatus", "handleTriggerTyping", "inFlightChannelFetches",
			"resolveChannel", "sendResponse",
		},
	},
	{
		// The counter-case, kept on purpose. Three declarations in 78 lines is
		// where `AGENTS.md` already says an index costs more than reading the
		// file, and a benchmark that only asked the flattering size would be
		// measuring its own question selection.
		ID:       "O2_go_small",
		Family:   familyOutline,
		Ask:      "What is declared at the top level of api-db-go postgres/retry.go?",
		Language: "go",
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/infrastructure/postgres/retry.go",
		},
		Truth: []string{"expBackoffJitter", "retryInfo", "withRetry"},
	},
}
