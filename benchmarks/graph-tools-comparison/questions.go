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
	// familyConsumers is "which files outside the repository that declares this
	// use it". Truth is a set of files, and it excludes the declaring
	// repository entirely: a use next door is not the question.
	familyConsumers = "consumers"
	// familyDependencies is the outward direction of impact: "which files
	// declare something this reaches", at a declared depth. Third-party and
	// standard library are out of scope -- the question is about this corpus.
	familyDependencies = "dependencies"
	// familyLocate is "where is this declared". Truth is a set of files, and
	// the axis is precision: a name is mentioned in far more files than declare
	// it, so the work is telling a declaration from a use.
	familyLocate = "locate"
	// familyBodies is "hand me the code of these declarations". Truth is the
	// set of addresses whose **complete** declaration came back: a body that
	// stops early is the failure this family exists to catch.
	familyBodies = "bodies"
	// familyFacts is "what is this symbol", asked where the name is ambiguous.
	// Truth is one string, `kind@start-end`, because that is the whole answer.
	familyFacts = "facts"
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
	// First and Last are the declaration's opening and closing source lines,
	// copied from the file. They exist for the bodies family, which cannot
	// check a body is complete without knowing where it ends.
	First string `json:"first_line,omitempty"`
	Last  string `json:"last_line,omitempty"`
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
	// Reached is what the subject's own source names outward, for the
	// dependencies family. It exists to price the native arm honestly: a reader
	// tracing outward reads the subject, then has to find where each name it
	// saw is declared, and that search is part of what the answer cost.
	Reached []string `json:"reached_names,omitempty"`
	// Also carries the extra subjects of a question that asks about several at
	// once. It exists because "one call across files and repositories" is a
	// claim about batching, and a question with one subject cannot test it.
	Also []subject `json:"also,omitempty"`
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

// The hard set, selected blind by the rules in `harder.md` and with every
// occurrence read and attributed before any tool was run against it. Each
// question names a dimension the first ten never touched: a method homonym
// across receivers, a call through an interface, a type rather than a function,
// a renamed re-export, a trait object, an absence in each language, and a Rust
// outline.
// impactQuestions are the impact family, which had exactly one question in the
// other two sets while being the question this tool is sold on. Both ask about
// the same subject and differ only in depth, so what they isolate is the depth
// itself rather than two unrelated subjects.
//
// The truth is the same at two hops and at three, and that is the point: the
// frontier at hop two is `main`, two `Test*` functions and a file-local test
// helper, and nothing calls any of them. An answer that grows there is inventing
// reach. `main` alone is declared eight times in this repository and named in
// fifty places, nearly all of them prose.
var impactQuestions = []question{
	{
		ID:       "I2_go_depth2",
		Family:   familyImpact,
		Ask:      "Which files hold something that reaches the AutomationScheduledGuildIDs of GuildsHandler within two hops?",
		Language: "go",
		Depth:    2,
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/application/handlers/guilds_handler.go",
			Name: "GuildsHandler.AutomationScheduledGuildIDs", Symbol: "AutomationScheduledGuildIDs",
		},
		Truth: []string{
			"services/api-db-go/cmd/server/main.go",
			"services/api-db-go/internal/application/handlers/guilds_mock_test.go",
			"services/api-db-go/internal/application/routers/guilds_router.go",
			"services/api-db-go/internal/application/routers/routers_test.go",
		},
		Declarations: []string{"services/api-db-go/internal/application/handlers/guilds_handler.go"},
	},
	{
		ID:       "I3_go_depth3",
		Family:   familyImpact,
		Ask:      "Which files hold something that reaches the AutomationScheduledGuildIDs of GuildsHandler within three hops?",
		Language: "go, the frontier is entry points",
		Depth:    3,
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/application/handlers/guilds_handler.go",
			Name: "GuildsHandler.AutomationScheduledGuildIDs", Symbol: "AutomationScheduledGuildIDs",
		},
		Truth: []string{
			"services/api-db-go/cmd/server/main.go",
			"services/api-db-go/internal/application/handlers/guilds_mock_test.go",
			"services/api-db-go/internal/application/routers/guilds_router.go",
			"services/api-db-go/internal/application/routers/routers_test.go",
		},
		Declarations: []string{"services/api-db-go/internal/application/handlers/guilds_handler.go"},
	},
}

var hardQuestions = []question{
	{
		ID:       "H1_go_method",
		Family:   familyReferences,
		Ask:      "Which files reference the GetAll declared on BotsHandler in services/api-db-go?",
		Language: "go, method homonym",
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/application/handlers/bots_handler.go",
			Name: "BotsHandler.GetAll", Symbol: "GetAll",
		},
		Truth: []string{"services/api-db-go/internal/application/routers/bots_router.go"},
		Declarations: []string{
			"services/api-db-go/internal/application/handlers/bots_handler.go",
			"services/api-db-go/internal/application/handlers/command_handler.go",
			"services/api-db-go/internal/application/handlers/premium_handler.go",
		},
	},
	{
		ID:       "H2_go_iface",
		Family:   familyReferences,
		Ask:      "Which files call the FindPendingGuilds implemented by NotifierSubRepository in services/api-db-go?",
		Language: "go, called through an interface",
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/infrastructure/postgres/notifier_sub_repository.go",
			Name: "NotifierSubRepository.FindPendingGuilds", Symbol: "FindPendingGuilds",
		},
		Truth: []string{"services/api-db-go/internal/application/handlers/guilds_handler.go"},
		Declarations: []string{
			"services/api-db-go/internal/infrastructure/pgrepo/repos.go",
			"services/api-db-go/internal/infrastructure/postgres/notifier_sub_repository.go",
		},
	},
	{
		ID:       "H3_ts_type",
		Family:   familyReferences,
		Ask:      "Which files use the ApiRuntimeState declared in libraries/library-shared/src/types/gateway-registry.ts?",
		Language: "typescript, a type not a function",
		Subject: subject{
			Repo: "library-shared", Dir: "libraries/library-shared",
			Path: "src/types/gateway-registry.ts",
			Name: "ApiRuntimeState", Symbol: "ApiRuntimeState",
		},
		Truth: []string{
			"libraries/library-shared/src/redis/cache/gateway/registry/api-registry-cache.ts",
			"packages/gateway/src/grpc/manager/RegistryGrpcManager.ts",
		},
		Declarations: []string{"libraries/library-shared/src/types/gateway-registry.ts"},
	},
	{
		ID:       "H4_ts_alias",
		Family:   familyReferences,
		Ask:      "Which files use the CommandManager declared in modules/sdk-module-ts/src/sdk/managers/CommandManager.ts?",
		Language: "typescript, re-exported under another name",
		Subject: subject{
			Repo: "sdk-module-ts", Dir: "modules/sdk-module-ts",
			Path: "src/sdk/managers/CommandManager.ts",
			Name: "CommandManager", Symbol: "CommandManager",
		},
		Truth:        []string{"modules/sdk-module-ts/src/sdk/client/ModuleActions.ts"},
		Declarations: []string{"modules/sdk-module-ts/src/sdk/managers/CommandManager.ts"},
	},
	{
		ID:       "H5_rs_trait",
		Family:   familyReferences,
		Ask:      "Which files call the delete_player implemented by MemoryStateStore in services/kenalink-rs?",
		Language: "rust, called through a trait object",
		Subject: subject{
			Repo: "kenalink-rs", Dir: "services/kenalink-rs",
			Path: "src/state/memory.rs",
			Name: "state::memory::impl::MemoryStateStore::StateStore::delete_player", Symbol: "delete_player",
		},
		Truth: []string{
			"services/kenalink-rs/src/api_rest/routes_players.rs",
			"services/kenalink-rs/src/api_ws/mod.rs",
			"services/kenalink-rs/src/main.rs",
		},
		Declarations: []string{
			"services/kenalink-rs/src/api_rest/routes_players.rs",
			"services/kenalink-rs/src/state/memory.rs",
			"services/kenalink-rs/src/state/mod.rs",
		},
	},
	{
		ID:       "A1_go_absent",
		Family:   familyReferences,
		Ask:      "Which files reference the BenchmarkDeserializeValueDate declared in services/api-db-go?",
		Language: "go, absence",
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/infrastructure/redis/serialization_bench_test.go",
			Name: "BenchmarkDeserializeValueDate", Symbol: "BenchmarkDeserializeValueDate",
		},
		Truth:        []string{},
		Declarations: []string{"services/api-db-go/internal/infrastructure/redis/serialization_bench_test.go"},
	},
	{
		ID:       "A2_ts_absent",
		Family:   familyReferences,
		Ask:      "Which files use the addMockedSongsToQueue declared in modules/music-module/src/mocks/music-mocks.ts?",
		Language: "typescript, absence",
		Subject: subject{
			Repo: "music-module", Dir: "modules/music-module",
			Path: "src/mocks/music-mocks.ts",
			Name: "addMockedSongsToQueue", Symbol: "addMockedSongsToQueue",
		},
		Truth:        []string{},
		Declarations: []string{"modules/music-module/src/mocks/music-mocks.ts"},
	},
	{
		ID:       "A3_rs_absent",
		Family:   familyReferences,
		Ask:      "Which files call the build_all_image_sizes declared in services/api-music-nodo?",
		Language: "rust, absence",
		Subject: subject{
			Repo: "api-music-nodo", Dir: "services/api-music-nodo",
			Path: "src/providers/spotify.rs",
			Name: "providers::spotify::build_all_image_sizes", Symbol: "build_all_image_sizes",
		},
		Truth:        []string{},
		Declarations: []string{"services/api-music-nodo/src/providers/spotify.rs"},
	},
	{
		ID:       "O3_rs_outline",
		Family:   familyOutline,
		Ask:      "What is declared at the top level of api-music-nodo audio/range.rs?",
		Language: "rust",
		Subject: subject{
			Repo: "api-music-nodo", Dir: "services/api-music-nodo",
			Path: "src/audio/range.rs",
		},
		Truth: []string{
			"RangeOutcome", "build_response", "file_response", "insert_header",
			"parse_decimal", "parse_range", "tests",
		},
	},
}

// reachQuestions are the two families nothing measured before: which repository
// other than the declaring one consumes a symbol, and what a symbol reaches
// outward. They are the two calls the routing table in AGENTS.md recommends and
// that no question in the other three sets ever made.
//
// The truths were built by reading, not by pattern matching, and one of them was
// wrong the first time in both directions. For every file the corpus mentions the
// name in, the import that binds it was resolved -- multiline import clauses
// included, which a single-line grep misses and which cost this set a true
// consumer. A name a file declares itself, or imports from a repository-local
// copy, is not a consumer of the subject.
//
// And a consumer can name nothing at all: `modules/sdk-module-ts/src/index.ts`
// re-exports the subject through `export * from "@kena/shared"`, so the symbol
// crosses a repository boundary in a file whose text never spells it. No text
// search can reach that row. It was found by the graph, then verified
// independently by enumerating every star re-export of the package in the
// corpus -- there is exactly one, and no named re-export.
//
// Two of the four have an empty or single-file answer on purpose. Proving that
// nothing reaches across a boundary is what the routing table sells and what
// `grep` structurally cannot do: kena holds two independent Go modules that do
// not import each other, and the same file duplicated in both.
var reachQuestions = []question{
	{
		ID:       "X1_ts_shared_enum",
		Family:   familyConsumers,
		Ask:      "Which files outside library-shared consume the HttpStatus it declares?",
		Language: "typescript, five rival declarations of the same name",
		Subject: subject{
			Repo: "library-shared", Dir: "libraries/library-shared",
			Path: "src/result/custom-error.ts",
			Name: "HttpStatus", Symbol: "HttpStatus",
		},
		// Twenty-four files outside library-shared name HttpStatus. Five declare
		// their own enum, seventeen import a repository-local one by relative
		// path, and two import it from "@kena/shared". api-gateway does both: one
		// of its files imports the shared enum while nine import its own, so a
		// tool that decides per repository cannot be right here. The third file
		// names nothing: it star re-exports the package.
		Truth: []string{
			"modules/sdk-module-ts/src/index.ts",
			"services/api-cdn/src/application/middlewares/session-middleware.ts",
			"services/api-gateway/src/application/controllers/cluster-controller.ts",
		},
		Declarations: []string{
			"libraries/library-shared/src/result/custom-error.ts",
			"libraries/library-web/src/shared/CustomError.ts",
			"services/api-gateway/src/domain/result/custom-error.ts",
			"services/api-metrics/src/domain/result/custom-error.ts",
			"services/api-premium/src/domain/result/custom-error.ts",
			"services/api-translations/src/domain/result/custom-error.ts",
		},
	},
	{
		ID:       "X2_go_absent_consumers",
		Family:   familyConsumers,
		Ask:      "Which files outside api-db-go consume the LoadSecrets it declares?",
		Language: "go, the answer is nothing and the corpus looks otherwise",
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/shared/infisical/infisical.go",
			Name: "LoadSecrets", Symbol: "LoadSecrets",
		},
		// Nothing. kena holds two Go modules, kena.bot/api-db-go and
		// kena.bot/api-music, and neither imports the other -- verified by
		// grepping both module paths across every .go file. api-music carries its
		// own copy of infisical.go, so three of its files name LoadSecrets and
		// mean their own. A name-based answer claims those three.
		Truth: []string{},
		Declarations: []string{
			"services/api-db-go/internal/shared/infisical/infisical.go",
			"services/api-music/internal/shared/infisical/infisical.go",
		},
	},
	{
		ID:       "X3_go_reach_depth1",
		Family:   familyDependencies,
		Ask:      "Which files declare something RegisterGuilds reaches outward in one hop?",
		Language: "go, nine reached declarations in one file",
		Depth:    1,
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/application/routers/guilds_router.go",
			Name: "RegisterGuilds", Symbol: "RegisterGuilds",
		},
		// It reaches the GuildsHandler type and the eight methods it routes to.
		// All nine are declared in one file, checked one by one against its
		// receiver. Everything else it touches is fiber, which is not this
		// project's code.
		Truth: []string{
			"services/api-db-go/internal/application/handlers/guilds_handler.go",
		},
		Declarations: []string{
			"services/api-db-go/internal/application/routers/guilds_router.go",
		},
		Reached: []string{
			"GuildsHandler", "ModmailWebAccessList", "ModmailEnabledGuildIDs",
			"ModmailEnabledList", "AutomationScheduledGuildIDs",
			"NotifierPendingGuildIDs", "ModmailWebAccessCheck", "GetPermissions", "Get",
		},
	},
	{
		ID:       "X4_ts_reach_depth1",
		Family:   familyDependencies,
		Ask:      "Which files declare something RecommendationsCache reaches outward in one hop?",
		Language: "typescript, one edge is inheritance and one is a type-only import",
		Depth:    1,
		Subject: subject{
			Repo: "library-shared", Dir: "libraries/library-shared",
			Path: "src/redis/cache/music/recommendations-cache.ts",
			Name: "RecommendationsCache", Symbol: "RecommendationsCache",
		},
		// Two files, and the two edges do not look alike: the class extends
		// BaseCache, and its two methods name ChipbotRecommendationsResponse
		// through an `import type` that erases at runtime.
		//
		// The unit is what the declaration's own source names, its methods
		// included: the class spans lines 6-18 and the type appears inside that
		// span, so a reader asking what this class depends on is told both. That
		// definition is the question, not a tool's model of it -- and the two
		// disagree here. Asking about the method reaches the type; asking about
		// the class does not, at depth one or at any depth, because containment
		// is not a dependency edge in this graph. A traversal that will not
		// descend into a class understates what the class reaches, and says
		// nothing about having stopped.
		Truth: []string{
			"libraries/library-shared/src/redis/cache/base-cache.ts",
			"libraries/library-shared/src/redis/cache/music/types.ts",
		},
		Declarations: []string{
			"libraries/library-shared/src/redis/cache/music/recommendations-cache.ts",
		},
		Reached: []string{"BaseCache", "ChipbotRecommendationsResponse"},
	},
}

// chainQuestions are the three tools an agent reaches for **after** an answer:
// where is this declared, hand me its code, what is it. The routing table names
// all three and no question in any set had ever called one of them.
//
// Their truths are the cheapest to state and the easiest to get wrong by
// trusting a pattern. `withRetry` is named in 22 files and declared in 7, and
// the eighth candidate a regex offers -- `const withRetryMock = vi.fn()` -- is a
// different identifier that matched for want of a closing word boundary. The
// spans were read out of the files, line by line, not taken from any tool.
var httpStatusDeclarations = []string{
	"libraries/library-shared/src/result/custom-error.ts",
	"libraries/library-web/src/shared/CustomError.ts",
	"services/api-gateway/src/domain/result/custom-error.ts",
	"services/api-metrics/src/domain/result/custom-error.ts",
	"services/api-premium/src/domain/result/custom-error.ts",
	"services/api-translations/src/domain/result/custom-error.ts",
}

var chainQuestions = []question{
	{
		ID:       "X5_locate_homonym",
		Family:   familyLocate,
		Ask:      "Which files declare withRetry?",
		Language: "go and typescript, seven declarations of one name",
		Subject: subject{
			Repo: "library-shared", Dir: "libraries/library-shared",
			Path: "src/utils/retry.ts", Name: "withRetry", Symbol: "withRetry",
		},
		// The seven declarations of the function: two Go copies of one file, a
		// third Go one in another package, three exported TypeScript functions
		// and a private method on a class. 22 files name it.
		//
		// The graph also holds 15 symbols named withRetry that declare nothing:
		// every TypeScript barrel that re-publishes the name gets one, of kind
		// `export` or `import`. They are not in this truth, and the arm that
		// reports them says how many it set aside. That is the same call ADR
		// 0046 made for find_references, where forwarding edges are withheld
		// unless asked for.
		Truth:        withRetryDeclarations,
		Declarations: withRetryDeclarations,
	},
	{
		ID:       "X6_bodies_two_languages",
		Family:   familyBodies,
		Ask:      "Give me the complete source of these three declarations, in one call",
		Language: "go and typescript, two repositories",
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/shared/infisical/infisical.go",
			Name: "LoadSecrets", Symbol: "LoadSecrets",
			First: "func LoadSecrets() (configured bool, err error) {",
			Last:  "}",
		},
		Also: []subject{
			{
				Repo: "api-db-go", Dir: "services/api-db-go",
				Path: "internal/application/routers/guilds_router.go",
				Name: "RegisterGuilds", Symbol: "RegisterGuilds",
				First: "func RegisterGuilds(app *fiber.App, h *handlers.GuildsHandler) {",
				Last:  "}",
			},
			{
				Repo: "library-shared", Dir: "libraries/library-shared",
				Path: "src/redis/cache/music/recommendations-cache.ts",
				Name: "RecommendationsCache", Symbol: "RecommendationsCache",
				First: "export class RecommendationsCache extends BaseCache {",
				Last:  "}",
			},
		},
		// One address per declaration whose whole body came back. Spans read
		// from the files: 72-102, 34-48 and 6-18.
		Truth: []string{
			"api-db-go:internal/shared/infisical/infisical.go#LoadSecrets",
			"api-db-go:internal/application/routers/guilds_router.go#RegisterGuilds",
			"library-shared:src/redis/cache/music/recommendations-cache.ts#RecommendationsCache",
		},
		Declarations: []string{
			"services/api-db-go/internal/shared/infisical/infisical.go",
			"services/api-db-go/internal/application/routers/guilds_router.go",
			"libraries/library-shared/src/redis/cache/music/recommendations-cache.ts",
		},
	},
	{
		ID:       "X7_facts_homonym",
		Family:   familyFacts,
		Ask:      "What kind of declaration is the HttpStatus in library-shared, and where does it end?",
		Language: "typescript, six declarations share the name",
		Subject: subject{
			Repo: "library-shared", Dir: "libraries/library-shared",
			Path: "src/result/custom-error.ts",
			Name: "HttpStatus", Symbol: "HttpStatus",
		},
		// Read from the file: `export enum HttpStatus {` on 29, its closing
		// brace on 36. Five other files declare the same name, so an answer that
		// resolves by name alone can land on any of them.
		Truth:        []string{"enum@29-36"},
		Declarations: httpStatusDeclarations,
	},
}

// questionSet resolves the name a run was asked for. An unknown name is a
// failure rather than a fallback: silently measuring the wrong set would
// publish a number under the wrong label.
func questionSet(name string) []question {
	switch name {
	case "", "measured":
		return questions
	case "hard":
		return hardQuestions
	case "impact":
		return impactQuestions
	case "reach":
		return reachQuestions
	case "chain":
		return chainQuestions
	case "all":
		out := append([]question{}, questions...)
		out = append(out, hardQuestions...)
		out = append(out, impactQuestions...)
		out = append(out, reachQuestions...)
		return append(out, chainQuestions...)
	default:
		panic("unknown question set " + name +
			`: use "measured", "hard", "impact", "reach", "chain" or "all"`)
	}
}
