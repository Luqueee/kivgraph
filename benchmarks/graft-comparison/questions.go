package main

// The question set and its ground truth.
//
// Every subject is addressed the way both surfaces accept one -- a declaration
// identified by repository, repository-relative path and name -- because a bare
// name does not identify a symbol in this corpus: `withRetry` is seven
// declarations in three languages and `now_ms` is four in one. A benchmark that
// asked by name alone would compare two answers to two different questions.
//
// The ground truth is the manual classification recorded in
// benchmarks/codebase-memory-comparison: every `grep` occurrence read and
// attributed to the declaration it resolves against. The corpus has not moved
// since -- the newest commit among the repositories the questions touch is
// 2026-08-12 -- so the same truth applies, and this run re-measures both
// surfaces against it rather than trusting either one's own answer.

// subject is one declaration, in the addressing both surfaces share.
type subject struct {
	// Repo is the name kivgraph registered, which is the directory's base name.
	Repo string `json:"repo"`
	// Dir is where that repository lives relative to the corpus root. It is
	// what turns a corpus-relative path into a repository-relative one.
	Dir string `json:"dir"`
	// Path is repository-relative: what kivgraph answers with.
	Path string `json:"path"`
	// Name is the qualified name, which for a free function is the bare name.
	Name string `json:"name"`
	// Symbol is the bare name a caller-tracing tool takes.
	Symbol string `json:"symbol"`
}

// corpusPath is the subject's path relative to the corpus root, which is what
// graft answers with.
func (s subject) corpusPath() string { return s.Dir + "/" + s.Path }

// question is one "who calls this declaration" measurement.
type question struct {
	ID       string  `json:"id"`
	Ask      string  `json:"question"`
	Language string  `json:"language"`
	Subject  subject `json:"subject"`
	// Callers is the truth: every file holding a call or reference to this
	// declaration, corpus-relative, excluding the declaring file.
	Callers []string `json:"ground_truth_callers"`
	// Declarations is every file declaring the bare name anywhere in the
	// corpus. The native arm reads all of them: that is the minimum a reader
	// needs to tell homonyms apart, and it is why `grep` alone cannot answer.
	Declarations []string `json:"declaration_sites"`
}

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

// questions is the measured set. Q1 has no reachable answer on this corpus and
// is kept for exactly that reason: kena's repositories consume each other as
// published packages, never as source, so the honest result is an absence and a
// benchmark that dropped the question would hide it.
var questions = []question{
	{
		ID:       "Q1_ts_xrepo",
		Ask:      "Which call sites use the withRetry declared in libraries/library-shared/src/utils/retry.ts?",
		Language: "typescript, cross-package",
		Subject: subject{
			Repo: "library-shared", Dir: "libraries/library-shared",
			Path: "src/utils/retry.ts", Name: "withRetry", Symbol: "withRetry",
		},
		Callers: []string{
			"modules/sdk-module-ts/src/sdk/client/KenaModule.ts",
			"packages/core/src/cluster/master/index.ts",
			"packages/core/src/cluster/worker/BotWorker.ts",
			"packages/core/src/shared/utils/sharding.ts",
			"packages/gateway/src/grpc/server.ts",
		},
		Declarations: withRetryDeclarations,
	},
	{
		ID:       "Q2_go",
		Ask:      "Which call sites use withRetry in services/api-db-go/internal/infrastructure/postgres/retry.go?",
		Language: "go",
		Subject: subject{
			Repo: "api-db-go", Dir: "services/api-db-go",
			Path: "internal/infrastructure/postgres/retry.go", Name: "withRetry", Symbol: "withRetry",
		},
		Callers: []string{
			"services/api-db-go/internal/infrastructure/postgres/client.go",
			"services/api-db-go/internal/infrastructure/postgres/retry_test.go",
		},
		Declarations: withRetryDeclarations,
	},
	{
		ID:       "Q3_ts_intra",
		Ask:      "Which files call getRequiredField from packages/core ipcCase.ts?",
		Language: "typescript, intra-repository",
		Subject: subject{
			Repo: "core", Dir: "packages/core",
			Path: "src/cluster/worker/ipc/utils/ipcCase.ts", Name: "getRequiredField", Symbol: "getRequiredField",
		},
		Callers: []string{
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
		ID:       "Q4_rust",
		Ask:      "Which files call now_ms() from services/kenalink-rs/src/util.rs?",
		Language: "rust",
		Subject: subject{
			Repo: "kenalink-rs", Dir: "services/kenalink-rs",
			Path: "src/util.rs", Name: "util::now_ms", Symbol: "now_ms",
		},
		Callers: []string{
			"services/kenalink-rs/src/api_rest/error.rs",
			"services/kenalink-rs/src/api_rest/routes_players.rs",
			"services/kenalink-rs/src/api_rest/routes_sessions.rs",
			"services/kenalink-rs/src/api_ws/mod.rs",
			"services/kenalink-rs/src/audio/songbird_engine.rs",
			"services/kenalink-rs/src/main.rs",
		},
		Declarations: nowMsDeclarations,
	},
}

// census is the declaration-census question: how many distinct declarations
// carry a name, and where. It is the question `grep` answers worst and the one
// an agent asks before it can ask anything else about a homonym.
var census = question{
	ID:           "Q5_census",
	Ask:          "How many distinct declarations named withRetry exist, and where?",
	Language:     "typescript, go",
	Subject:      questions[0].Subject,
	Callers:      withRetryDeclarations,
	Declarations: withRetryDeclarations,
}

// scopeProbe is the diagnostic that separates a tool's extractor from the scope
// it was pointed at. graft drops a cross-file caller whose name is ambiguous
// inside the build, so the same question is asked three times against three
// builds of the same code: the whole corpus, one repository, one Go package.
type scopeProbe struct {
	ID      string  `json:"id"`
	Subject subject `json:"subject"`
	// Root is what graft build was pointed at, corpus-relative. Empty is the
	// corpus root.
	Root string `json:"root"`
	// Callers is the truth for the subject, expressed relative to Root.
	Callers []string `json:"ground_truth_callers"`
}

var scopeProbes = []scopeProbe{
	// The intermediate scope. One repository is not enough for Go: `api-db-go`
	// declares `withRetry` in two packages, so the name is still ambiguous inside
	// the build and the cross-file callers are still dropped. It is the probe
	// that shows the boundary is the build, not the repository.
	{
		ID:      "go_repository_scope",
		Subject: questions[1].Subject,
		Root:    "services/api-db-go",
		Callers: []string{
			"internal/infrastructure/postgres/client.go",
			"internal/infrastructure/postgres/retry_test.go",
		},
	},
	{
		ID:      "go_package_scope",
		Subject: questions[1].Subject,
		Root:    "services/api-db-go/internal/infrastructure/postgres",
		Callers: []string{"client.go", "retry_test.go"},
	},
	{
		ID:      "rust_repository_scope",
		Subject: questions[3].Subject,
		Root:    "services/kenalink-rs",
		Callers: []string{
			"src/api_rest/error.rs",
			"src/api_rest/routes_players.rs",
			"src/api_rest/routes_sessions.rs",
			"src/api_ws/mod.rs",
			"src/audio/songbird_engine.rs",
			"src/main.rs",
		},
	},
}

// crossRepoConsumers is the package dimension: which repositories consume
// `@kena/shared`. The truth is computed from the corpus rather than asserted --
// every file whose text imports the package specifier, mapped to the repository
// holding it, with the provider itself removed:
//
//	grep -rIn -E 'from +"@kena/shared|import +"@kena/shared|require\("@kena/shared' \
//	  --include='*.ts' --include='*.tsx' --include='*.js' --include='*.jsx' \
//	  --include='*.mjs' --include='*.cjs' . | grep -v /node_modules/
//
// 773 files in 23 directories, of which one is `libraries/library-shared`. That
// leaves 22 consumers, which is the count the earlier codebase-memory-mcp
// comparison classified by hand. A repository that declares the dependency
// without importing it -- `api-metrics` -- is correctly absent, and so are the
// two that only name it in comments.
var crossRepoConsumers = []string{
	"admin.kena.lan", "api-cdn", "api-gateway", "api-premium", "api-translations",
	"automation-module", "captcha.kena.bot", "core", "dash.kena.bot", "gateway",
	"greeter-module", "levels-module", "library-lavalink", "logs-module",
	"moderation-module", "modmail-module", "music-module", "sdk-module-ts",
	"tempVoice-module", "template-module", "tickets-module", "utils-module",
}
