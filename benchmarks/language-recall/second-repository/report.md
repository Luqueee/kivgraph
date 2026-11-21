# Recall on real code, by language

How many of the files that depend on a declaration does `find_references` report? The ground truth is the language's own type checker: rename the declaration, type-check again, and the files that gain an error are the files that use it.

- Command: `go run ./benchmarks/language-recall -kivgraph <binary> -repo-root .`
- Date: 2026-11-21, seed 1, up to 100 declarations per language
- Kivgraph `0.10.0` on darwin/arm64, go1.26.11, Version 6.0.3, node v25.9.0, rustc 1.100.0 (0ef1bc386 2026-11-09) (Homebrew)

## Corpus

|language|repository|commit|
|---|---|---|
|typescript|[got](https://github.com/sindresorhus/got)|`6b5aafc1cd53`|
|rust|[ripgrep](https://github.com/BurntSushi/ripgrep)|`fdf7d95485ec`|
|dart|[args](https://github.com/dart-lang/args)|`7a2dfb528768`|

## Result

Intervals are 95 % percentile intervals from resampling declarations.

|language|configuration|declarations measured|with dependents|file recall|file precision|fully recalled|
|---|---|---|---|---|---|---|
|typescript|default|100|86|98.7 % (96.0 to 100.0)|70.3 % (55.6 to 82.5)|97.7 % (91.9 to 99.4)|
|rust|default|100|87|100.0 % (100.0 to 100.0)|81.1 % (68.1 to 93.3)|100.0 % (95.8 to 100.0)|
|dart|default|100|88|100.0 % (100.0 to 100.0)|91.3 % (78.5 to 100.0)|100.0 % (95.8 to 100.0)|
|dart|dart.tests_excluded|100|88|65.1 % (55.8 to 76.2)|95.1 % (86.9 to 100.0)|70.5 % (60.2 to 79.0)|

`file recall` pools the dependent files of every declaration. `fully recalled` is the share of declarations with dependents for which no file was missed (Wilson interval). A `tests_excluded` configuration is the default one with that language's `include_tests: false`; each pair draws the same declarations.

## typescript (default)

- Declarations listed 3251, eligible for the rename 1084, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 98.1 % (95.1 to 100.0). On test files: 99.1 % (96.5 to 100.0).
- Recall on ES private names (`#x`): 100.0 % (100.0 to 100.0) over 4 declarations. On every other declaration: 98.6 % (96.1 to 100.0).
- Declarations with no dependents for which Kivgraph still reports a file: 9 of 14.
- Completeness verdicts: LOWER_BOUND 100.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 0.
- Not sampled: kind not sampled: 1257; name not found in declaring position: 910.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|15|100.0 % (100.0 to 100.0)|
|function|13|100.0 % (100.0 to 100.0)|
|interface|1|100.0 % (100.0 to 100.0)|
|method|10|72.7 % (41.7 to 100.0)|
|property|15|100.0 % (100.0 to 100.0)|
|type|16|100.0 % (100.0 to 100.0)|
|variable|16|100.0 % (100.0 to 100.0)|

Declarations with a missed file (first 15 of 2):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`dnsCache` (source/core/options.ts:2438)|method|source/core/options.ts, test/create.ts|LOWER_BOUND|
|`maxHeaderSize` (source/core/options.ts:3456)|method|source/core/options.ts|LOWER_BOUND|

Declarations with a reported file the compiler did not flag (first 15 of 41). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`RequestError` (source/core/errors.ts:49)|class|source/index.ts|
|`publishRequestStart` (source/core/diagnostics-channel.ts:119)|function|source/index.ts|
|`ClientRequest` (source/core/timed-out.ts:238)|interface|source/core/index.ts, source/core/options.ts, source/core/timed-out.ts, source/core/utils/http2-client.ts, source/core/utils/is-client-request.ts, source/core/utils/timer.ts, test/early-307-redirects.ts, test/promise.ts, test/retry.ts, test/stream.ts, test/timed-out.ts, test/timeout.ts|
|`HttpsOptions` (source/core/options.ts:687)|type|source/index.ts|
|`Options` (source/core/options.ts:1663)|class|benchmark/index.ts, source/as-promise/index.ts, source/core/errors.ts, source/core/index.ts, source/create.ts, source/index.ts, source/types.ts, test/arguments.ts, test/cookies.ts, test/create.ts, test/error.ts, test/headers.ts, test/normalize-arguments.ts, test/pagination.ts, test/redirects.ts, test/stream.ts, test/types/events.ts, test/unix-socket.ts|
|`stripUrlAuth` (source/core/utils/strip-url-auth.ts:4)|function|source/core/errors.ts, source/core/index.ts, source/core/response.ts|
|`ParseError` (source/core/response.ts:170)|class|source/index.ts|
|`SchemaValidationError` (source/core/errors.ts:290)|class|source/index.ts|
|`publishRetry` (source/core/diagnostics-channel.ts:131)|function|source/index.ts|
|`encoding` (source/core/options.ts:3335)|method|source/as-promise/index.ts, source/core/index.ts, source/core/options.ts, test/arguments.ts, test/cache.ts, test/cookies.ts, test/encoding.ts, test/gzip.ts, test/http.ts, test/pagination-options.ts, test/pagination.ts, test/response-parse.ts, test/stream.ts|
|`StreamOptions` (source/types.ts:72)|type|source/index.ts|
|`HTTPError` (source/core/errors.ts:164)|class|source/index.ts|
|`applyUrlOverride` (source/core/options.ts:1101)|function|source/index.ts|
|`RequestEvents` (source/core/index.ts:167)|type|source/index.ts|
|`WeakableMap` (source/core/utils/weakable-map.ts:1)|class|source/core/index.ts, test/weakable-map.ts|

## rust (default)

- Declarations listed 4828, eligible for the rename 4211, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: n/a.
- Declarations with no dependents for which Kivgraph still reports a file: 0 of 13.
- Completeness verdicts: COMPLETE 97, LOWER_BOUND 3.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 97.
- Not sampled: kind not sampled: 195; name not found in declaring position: 422; outline refused: 1.

|kind|declarations with dependents|file recall|
|---|---|---|
|constant|8|100.0 % (100.0 to 100.0)|
|enum|9|100.0 % (100.0 to 100.0)|
|enum_member|9|100.0 % (100.0 to 100.0)|
|field|9|100.0 % (100.0 to 100.0)|
|function|3|100.0 % (100.0 to 100.0)|
|method|5|100.0 % (100.0 to 100.0)|
|static|9|100.0 % (100.0 to 100.0)|
|static_method|6|100.0 % (100.0 to 100.0)|
|struct|8|100.0 % (100.0 to 100.0)|
|trait|7|100.0 % (100.0 to 100.0)|
|trait_method|8|100.0 % (100.0 to 100.0)|
|type_alias|6|100.0 % (100.0 to 100.0)|

Declarations with a reported file the compiler did not flag (first 15 of 10). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`Captures` (crates/matcher/src/lib.rs:379)|trait|crates/matcher/tests/test_matcher.rs, crates/matcher/tests/util.rs, crates/pcre2/src/matcher.rs, crates/printer/src/util.rs, crates/regex/src/matcher.rs|
|`new_captures` (crates/matcher/src/lib.rs:583)|trait_method|crates/printer/src/util.rs|
|`start` (crates/matcher/src/lib.rs:94)|method|crates/printer/src/json.rs, crates/printer/src/standard.rs, crates/printer/src/util.rs|
|`Mode` (crates/core/flags/lowargs.rs:153)|enum|crates/core/main.rs|
|`LineStep` (crates/searcher/src/lines.rs:49)|struct|crates/printer/src/standard.rs|
|`SinkError` (crates/searcher/src/sink.rs:18)|trait|crates/printer/src/summary.rs, crates/printer/src/util.rs|
|`WalkState` (crates/ignore/src/walk.rs:1327)|enum|crates/core/main.rs, crates/ignore/benches/workloads.rs, crates/ignore/examples/walk.rs|
|`Sink` (crates/searcher/src/sink.rs:102)|trait|crates/printer/src/json.rs, crates/printer/src/standard.rs, crates/printer/src/summary.rs|
|`ParallelVisitor` (crates/ignore/src/walk.rs:1373)|trait|crates/ignore/benches/workloads.rs|
|`Matcher` (crates/matcher/src/lib.rs:546)|trait|crates/core/search.rs, crates/matcher/tests/test_matcher.rs, crates/matcher/tests/util.rs, crates/pcre2/src/matcher.rs, crates/printer/src/json.rs, crates/printer/src/standard.rs, crates/printer/src/summary.rs, crates/printer/src/util.rs, crates/regex/src/matcher.rs, crates/searcher/src/searcher/core.rs, crates/searcher/src/searcher/glue.rs, crates/searcher/src/searcher/mod.rs, crates/searcher/src/testutil.rs|

## dart (default)

- Declarations listed 252, eligible for the rename 163, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: 100.0 % (100.0 to 100.0).
- Declarations with no dependents for which Kivgraph still reports a file: 0 of 12.
- Completeness verdicts: COMPLETE 100.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 100.
- Not sampled: kind not sampled: 86; name not found in declaring position: 3.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|17|100.0 % (100.0 to 100.0)|
|field|34|100.0 % (100.0 to 100.0)|
|function|13|100.0 % (100.0 to 100.0)|
|method|24|100.0 % (100.0 to 100.0)|

Declarations with a reported file the compiler did not flag (first 15 of 3). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`Option` (lib/src/option.dart:36)|class|lib/src/arg_results.dart|
|`AllowAnythingParser` (lib/src/allow_anything_parser.dart:13)|class|example/arg_parser/example.dart, lib/args.dart, lib/command_runner.dart, lib/src/allow_anything_parser.dart, lib/src/arg_results.dart, lib/src/parser.dart, test/allow_anything_test.dart, test/args_test.dart, test/command_parse_test.dart, test/parse_performance_test.dart, test/parse_test.dart, test/test_utils.dart, test/trailing_options_test.dart, test/usage_test.dart|
|`ArgResults` (lib/src/arg_results.dart:28)|class|test/allow_anything_test.dart, test/args_test.dart, test/command_parse_test.dart, test/parse_test.dart, test/trailing_options_test.dart|

## dart (dart.tests_excluded)

- Declarations listed 252, eligible for the rename 163, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: 0.0 % (0.0 to 0.0).
- Declarations with no dependents for which Kivgraph still reports a file: 0 of 12.
- Completeness verdicts: COMPLETE 100.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 100.
- Not sampled: kind not sampled: 86; name not found in declaring position: 3.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|17|70.0 % (56.8 to 88.0)|
|field|34|84.5 % (73.0 to 96.2)|
|function|13|83.3 % (72.2 to 100.0)|
|method|24|41.1 % (30.2 to 61.4)|

Declarations with a missed file (first 15 of 26):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`padRight` (lib/src/utils.dart:7)|function|test/utils_test.dart|COMPLETE|
|`allowTrailingOptions` (lib/src/arg_parser.dart:34)|field|test/allow_anything_test.dart, test/trailing_options_test.dart|COMPLETE|
|`wrapTextAsLines` (lib/src/utils.dart:85)|function|test/utils_test.dart|COMPLETE|
|`parse` (lib/src/arg_parser.dart:333)|method|test/allow_anything_test.dart, test/args_test.dart, test/command_parse_test.dart, test/parse_performance_test.dart, test/parse_test.dart, test/test_utils.dart, test/trailing_options_test.dart|COMPLETE|
|`ArgParserException` (lib/src/arg_parser_exception.dart:6)|class|test/test_utils.dart|COMPLETE|
|`UsageException` (lib/src/usage_exception.dart:5)|class|test/test_utils.dart|COMPLETE|
|`option` (lib/src/arg_results.dart:97)|method|test/parse_test.dart|COMPLETE|
|`Option` (lib/src/option.dart:36)|class|test/args_test.dart|COMPLETE|
|`CommandRunner` (lib/command_runner.dart:23)|class|test/allow_anything_test.dart, test/command_runner_test.dart, test/command_test.dart, test/test_utils.dart|COMPLETE|
|`wrapText` (lib/src/utils.dart:37)|function|test/utils_test.dart|COMPLETE|
|`addMultiOption` (lib/src/arg_parser.dart:248)|method|test/args_test.dart, test/parse_test.dart, test/usage_test.dart|COMPLETE|
|`addSeparator` (lib/src/arg_parser.dart:327)|method|test/allow_anything_test.dart, test/usage_test.dart|COMPLETE|
|`arguments` (lib/src/arg_results.dart:53)|field|test/allow_anything_test.dart, test/args_test.dart|COMPLETE|
|`findByNameOrAlias` (lib/src/arg_parser.dart:366)|method|test/args_test.dart|COMPLETE|
|`Command` (lib/command_runner.dart:262)|class|test/command_runner_test.dart, test/test_utils.dart|COMPLETE|

Declarations with a reported file the compiler did not flag (first 15 of 2). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`Option` (lib/src/option.dart:36)|class|lib/src/arg_results.dart|
|`AllowAnythingParser` (lib/src/allow_anything_parser.dart:13)|class|example/arg_parser/example.dart, lib/args.dart, lib/command_runner.dart, lib/src/allow_anything_parser.dart, lib/src/arg_results.dart, lib/src/parser.dart|

## What this does not show

- The oracle and Kivgraph share each language's type checker for Go, TypeScript and Dart. Go is checked with `go/packages` and `go/types` on both sides, TypeScript with the repository's own `tsc` on one side and the same checker inside the worker on the other, Dart with `dart analyze` on one side and the Analysis Server on the other. Type resolution is not what this measures there: it measures which files and build variants are loaded, how resolved uses become edges, and what is filtered on the way out. Rust is the exception: the oracle is `rustc` through `cargo check` and Kivgraph reads `rust-analyzer`, two different front ends.
- The unit is the file, not the reference. A rename makes the compiler reject some uses and not others (after a failed import, the uses of the imported name are not reported), so reference-level ground truth would differ by language. A file is a dependent if it gains an error.
- Implementation and inheritance relations are left out of the truth: an error that says a type no longer satisfies an interface or a base class is not a use of the renamed name, and `find_references` is not the tool for it.
- The sample frame is what Kivgraph lists for each file, so a declaration Kivgraph never recorded cannot be sampled. A recall measured this way is conditional on the declaration being known.
- Only declarations whose name appears in its declaring position on its first line are renamed, so the rename is exact. Declarations written another way are counted as not sampled.
- Go and Dart declarations are sampled from non-test files; references from test files are part of the truth and are reported apart, and the default arm reads them (`go.include_tests` and `dart.include_tests` are true by default). The `tests_excluded` arm sets that key to false for one language, and its test files are then missed by construction.
- One repository per language: the figures describe these repositories at these commits, not the language. A second repository would show how much they move.
- A declaration with no dependents says nothing about recall. It is reported through the share of such declarations for which Kivgraph still reports a reference.
- The TypeScript dependencies are installed with `npm install` and are not pinned by a lockfile, so a rerun can resolve other versions. The Rust dependencies are fetched with `--locked`.
- `rustc` stops after name resolution when that fails, so an error that needs type inference can be hidden behind an unresolved name elsewhere. The same holds across a workspace: a crate that fails to compile hides every crate and target that depends on it, so on a multi-crate workspace the truth is a lower bound and the precision is too (on `ripgrep`, 25 of 33 extra files are in other crates and 8 in targets of the declaring crate). The Rust truth cannot contain a file that does not use the name.
- Renaming a Dart class breaks its constructors' own declarations (a constructor is written with the class's name), and an `implements` clause is an implementation relation: those errors are left out of the truth. The Dart dependencies are resolved with `dart pub get`, and the repository ships no lockfile.
- Java and C# read their tests by default too (ADR 0142); their `tests_excluded` arm misses every test file by construction.
- `scip-dotnet` resolves a call written as an extension method (`Guard.Against.Negative(0L)`) to the first overload of the name whatever its arguments, and records no occurrence for a generic call with explicit type arguments (`Negative<int>(...)`). The C# corpus is a library of overloaded extension methods, so its recall is mostly that: the other overloads have no reference in the index, and Kivgraph answers `COMPLETE` for them.
- Java is checked by running `javac` over the main and the test sources at once, with the classpath Maven resolves, so one failing file does not hide another; Kivgraph reads `scip-java`, which drives the same compiler through the build. A Java or C# constructor is written with its type's name, so renaming the type breaks the constructors' own declarations: those errors are left out, as for Dart.
- C# is checked with `dotnet build` on the solution, and MSBuild does not compile a project whose reference failed: a rename in the library leaves the test project unbuilt, so its files are missing from the truth. The C# truth is a lower bound and so is its precision, as for a Rust workspace. Kivgraph reads `scip-dotnet`, which drives Roslyn, the same compiler.
