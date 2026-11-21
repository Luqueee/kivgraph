# Recall on real code, by language

How many of the files that depend on a declaration does `find_references` report? The ground truth is the language's own type checker: rename the declaration, type-check again, and the files that gain an error are the files that use it.

- Command: `go run ./benchmarks/language-recall -kivgraph <binary> -repo-root .`
- Date: 2026-11-21, seed 1, up to 100 declarations per language
- Kivgraph `0.10.0` on darwin/arm64, go1.26.11, Version 5.9.3, node v25.9.0, rustc 1.100.0 (0ef1bc386 2026-11-09) (Homebrew)

## Corpus

|language|repository|commit|
|---|---|---|
|go|[cobra](https://github.com/spf13/cobra)|`adbc8813901b`|
|typescript|[ky](https://github.com/sindresorhus/ky)|`3541888878c4`|
|rust|[fd](https://github.com/sharkdp/fd)|`b80cd53e6561`|
|dart|[collection](https://github.com/dart-lang/collection)|`96afcc2fce62`|

## Result

Intervals are 95 % percentile intervals from resampling declarations.

|language|configuration|declarations measured|with dependents|file recall|file precision|fully recalled|
|---|---|---|---|---|---|---|
|go|default|100|96|100.0 % (100.0 to 100.0)|100.0 % (100.0 to 100.0)|100.0 % (96.2 to 100.0)|
|typescript|default|100|85|100.0 % (100.0 to 100.0)|96.8 % (93.8 to 99.3)|100.0 % (95.7 to 100.0)|
|rust|default|100|86|100.0 % (100.0 to 100.0)|97.8 % (94.9 to 100.0)|100.0 % (95.7 to 100.0)|
|dart|default|100|73|99.0 % (96.8 to 100.0)|88.4 % (81.0 to 94.6)|98.6 % (92.6 to 99.8)|
|go|go.tests_excluded|100|96|65.9 % (59.4 to 74.6)|100.0 % (100.0 to 100.0)|57.3 % (47.3 to 66.7)|
|dart|dart.tests_excluded|100|73|74.0 % (65.1 to 82.7)|92.5 % (86.2 to 97.6)|67.1 % (55.7 to 76.8)|

`file recall` pools the dependent files of every declaration. `fully recalled` is the share of declarations with dependents for which no file was missed (Wilson interval). A `tests_excluded` configuration is the default one with that language's `include_tests: false`; each pair draws the same declarations.

## go (default)

- Declarations listed 443, eligible for the rename 442, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: 100.0 % (100.0 to 100.0).
- Declarations with no dependents for which Kivgraph still reports a file: 0 of 4.
- Completeness verdicts: COMPLETE 96, LOWER_BOUND 4.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 96.
- Not sampled: name not found in declaring position: 1; outline refused: 1.

|kind|declarations with dependents|file recall|
|---|---|---|
|alias|2|100.0 % (100.0 to 100.0)|
|const|18|100.0 % (100.0 to 100.0)|
|field|18|100.0 % (100.0 to 100.0)|
|func|17|100.0 % (100.0 to 100.0)|
|method|15|100.0 % (100.0 to 100.0)|
|type|15|100.0 % (100.0 to 100.0)|
|var|11|100.0 % (100.0 to 100.0)|

## typescript (default)

- Declarations listed 1134, eligible for the rename 398, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: n/a.
- Recall on ES private names (`#x`): 100.0 % (100.0 to 100.0) over 13 declarations. On every other declaration: 100.0 % (100.0 to 100.0).
- Declarations with no dependents for which Kivgraph still reports a file: 3 of 15.
- Completeness verdicts: COMPLETE 100.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 100.
- Not sampled: kind not sampled: 425; name not found in declaring position: 311; outline refused: 21.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|10|100.0 % (100.0 to 100.0)|
|function|9|100.0 % (100.0 to 100.0)|
|interface|2|100.0 % (100.0 to 100.0)|
|method|13|100.0 % (100.0 to 100.0)|
|property|13|100.0 % (100.0 to 100.0)|
|type|19|100.0 % (100.0 to 100.0)|
|variable|19|100.0 % (100.0 to 100.0)|

Declarations with a reported file the compiler did not flag (first 15 of 5). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`delay` (source/utils/delay.ts:9)|function|source/core/Ky.ts|
|`isRawNetworkError` (source/utils/is-network-error.ts:15)|function|source/core/Ky.ts|
|`ShouldRetryState` (source/types/retry.ts:9)|type|source/index.ts|
|`timeout` (source/utils/timeout.ts:11)|function|source/core/Ky.ts|
|`KyRequest` (source/types/request.ts:1)|type|source/types/hooks.ts|

## rust (default)

- Declarations listed 646, eligible for the rename 487, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: n/a.
- Declarations with no dependents for which Kivgraph still reports a file: 2 of 14.
- Completeness verdicts: COMPLETE 96, LOWER_BOUND 4.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 96.
- Not sampled: kind not sampled: 50; name not found in declaring position: 109.

|kind|declarations with dependents|file recall|
|---|---|---|
|constant|12|100.0 % (100.0 to 100.0)|
|enum|12|100.0 % (100.0 to 100.0)|
|enum_member|11|100.0 % (100.0 to 100.0)|
|field|12|100.0 % (100.0 to 100.0)|
|function|5|100.0 % (100.0 to 100.0)|
|method|4|100.0 % (100.0 to 100.0)|
|static|3|100.0 % (100.0 to 100.0)|
|static_method|11|100.0 % (100.0 to 100.0)|
|struct|11|100.0 % (100.0 to 100.0)|
|trait|2|100.0 % (100.0 to 100.0)|
|trait_method|2|100.0 % (100.0 to 100.0)|
|type_alias|1|100.0 % (100.0 to 100.0)|

Declarations with a reported file the compiler did not flag (first 15 of 3). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`TimeFilter` (src/filter/time.rs:7)|enum|src/main.rs|
|`Never` (src/cli.rs:863)|enum_member|src/cli.rs|
|`cmp` (src/dir_entry.rs:136)|method|src/dir_entry.rs|

## dart (default)

- Declarations listed 772, eligible for the rename 540, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 98.7 % (95.6 to 100.0). On test files: 100.0 % (100.0 to 100.0).
- Declarations with no dependents for which Kivgraph still reports a file: 7 of 27.
- Completeness verdicts: COMPLETE 100.
- Answers that said `COMPLETE` and missed at least one non-test file: 1 of 100.
- Not sampled: kind not sampled: 204; name not found in declaring position: 28.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|20|100.0 % (100.0 to 100.0)|
|extension|1|0.0 % (0.0 to 0.0)|
|field|21|100.0 % (100.0 to 100.0)|
|function|20|100.0 % (100.0 to 100.0)|
|method|3|100.0 % (100.0 to 100.0)|
|variable|8|100.0 % (100.0 to 100.0)|

Declarations with a missed file (first 15 of 1):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`IterableExtension` (lib/src/iterable_extensions.dart:20)|extension|lib/src/iterable_extensions.dart|COMPLETE|

Declarations with a reported file the compiler did not flag (first 15 of 11). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`containsKey` (lib/src/canonicalized_map.dart:107)|method|test/canonicalized_map_test.dart|
|`remove` (lib/src/priority_queue.dart:261)|method|test/priority_queue_test.dart|
|`addAll` (lib/src/wrappers.dart:509)|method|lib/src/equality_map.dart|
|`forEachWhile` (lib/src/list_extensions.dart:130)|method|test/extensions_test.dart|
|`UnionSet` (lib/src/union_set.dart:16)|class|lib/src/union_set.dart|
|`whereIndexed` (lib/src/list_extensions.dart:155)|method|test/extensions_test.dart|
|`removeWhere` (lib/src/wrappers.dart:795)|method|lib/src/wrappers.dart|
|`equals` (lib/src/equality.dart:212)|method|lib/src/equality.dart, test/algorithms_test.dart, test/extensions_test.dart|
|`UnionSetController` (lib/src/union_set_controller.dart:24)|class|lib/src/union_set_controller.dart|
|`where` (lib/src/wrappers.dart:112)|method|lib/src/wrappers.dart|
|`ListSlice` (lib/src/list_extensions.dart:337)|class|test/extensions_test.dart|

## go (go.tests_excluded)

- Declarations listed 443, eligible for the rename 442, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: 0.0 % (0.0 to 0.0).
- Declarations with no dependents for which Kivgraph still reports a file: 0 of 4.
- Completeness verdicts: COMPLETE 96, LOWER_BOUND 4.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 96.
- Not sampled: name not found in declaring position: 1; outline refused: 1.

|kind|declarations with dependents|file recall|
|---|---|---|
|alias|2|71.4 % (66.7 to 75.0)|
|const|18|79.5 % (68.6 to 90.9)|
|field|18|55.6 % (44.7 to 77.1)|
|func|17|82.6 % (65.0 to 95.7)|
|method|15|57.4 % (40.0 to 70.2)|
|type|15|60.9 % (52.7 to 84.6)|
|var|11|78.9 % (65.0 to 94.4)|

Declarations with a missed file (first 15 of 41):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`Completion` (completions.go:136)|alias|completions_test.go|COMPLETE|
|`BashCompSubdirsInDir` (bash_completions.go:33)|const|bash_completions_test.go, completions_test.go|COMPLETE|
|`ShellCompDirective` (completions.go:45)|type|active_help_test.go, completions_test.go|COMPLETE|
|`CompletionFunc` (completions.go:139)|alias|completions_test.go|COMPLETE|
|`FParseErrWhitelist` (command.go:42)|type|command_test.go|COMPLETE|
|`EnableTraverseRunHooks` (cobra.go:66)|var|command_test.go|COMPLETE|
|`DisableDescriptions` (completions.go:115)|field|completions_test.go|COMPLETE|
|`Group` (command.go:45)|type|command_test.go|COMPLETE|
|`defaultCaseInsensitive` (cobra.go:48)|const|command_test.go|COMPLETE|
|`Args` (command.go:93)|field|args_test.go, bash_completions_test.go, command_test.go, completions_test.go, fish_completions_test.go|LOWER_BOUND|
|`GenYamlTree` (doc/yaml_docs.go:53)|func|doc/yaml_docs_test.go|COMPLETE|
|`Traverse` (command.go:821)|method|command_test.go|COMPLETE|
|`ShellCompDirectiveNoFileComp` (completions.go:66)|const|active_help_test.go, completions_test.go|COMPLETE|
|`defaultCommandSorting` (cobra.go:47)|const|command_test.go|COMPLETE|
|`SuggestFor` (command.go:71)|field|bash_completions_test.go, command_test.go, doc/cmd_test.go|COMPLETE|

## dart (dart.tests_excluded)

- Declarations listed 772, eligible for the rename 540, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 98.7 % (95.6 to 100.0). On test files: 0.0 % (0.0 to 0.0).
- Declarations with no dependents for which Kivgraph still reports a file: 3 of 27.
- Completeness verdicts: COMPLETE 100.
- Answers that said `COMPLETE` and missed at least one non-test file: 1 of 100.
- Not sampled: kind not sampled: 204; name not found in declaring position: 28.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|20|63.6 % (47.1 to 80.0)|
|extension|1|0.0 % (0.0 to 0.0)|
|field|21|95.5 % (87.5 to 100.0)|
|function|20|72.7 % (56.7 to 86.2)|
|method|3|0.0 % (0.0 to 0.0)|
|variable|8|100.0 % (100.0 to 100.0)|

Declarations with a missed file (first 15 of 24):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`IterableExtension` (lib/src/iterable_extensions.dart:20)|extension|lib/src/iterable_extensions.dart|COMPLETE|
|`binarySearch` (lib/src/algorithms.dart:21)|function|test/algorithms_test.dart|COMPLETE|
|`binarySearchByCompare` (lib/src/list_extensions.dart:38)|method|test/extensions_test.dart|COMPLETE|
|`CombinedIterableView` (lib/src/combined_wrappers/combined_iterable.dart:16)|class|test/combined_wrapper/iterable_test.dart|COMPLETE|
|`lowerBound` (lib/src/algorithms.dart:70)|function|test/algorithms_test.dart|COMPLETE|
|`DelegatingList` (lib/src/wrappers.dart:151)|class|test/wrapper_test.dart|COMPLETE|
|`stronglyConnectedComponents` (lib/src/functions.dart:163)|function|test/functions_test.dart|COMPLETE|
|`groupFoldBy` (lib/src/iterable_extensions.dart:397)|method|test/extensions_test.dart|COMPLETE|
|`BoolList` (lib/src/boollist.dart:13)|class|test/boollist_test.dart|COMPLETE|
|`equalsIgnoreAsciiCase` (lib/src/comparators.dart:27)|function|test/ignore_ascii_case_test.dart|COMPLETE|
|`IterableZip` (lib/src/iterable_zip.dart:16)|class|test/iterable_zip_test.dart|COMPLETE|
|`quickSortBy` (lib/src/algorithms.dart:420)|function|test/algorithms_test.dart|COMPLETE|
|`UnorderedIterableEquality` (lib/src/equality.dart:254)|class|test/algorithms_test.dart, test/equality_test.dart, test/extensions_test.dart|COMPLETE|
|`mapMap` (lib/src/functions.dart:15)|function|test/functions_test.dart|COMPLETE|
|`IdentityEquality` (lib/src/equality.dart:95)|class|test/equality_test.dart|COMPLETE|

Declarations with a reported file the compiler did not flag (first 15 of 6). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`addAll` (lib/src/wrappers.dart:509)|method|lib/src/equality_map.dart|
|`UnionSet` (lib/src/union_set.dart:16)|class|lib/src/union_set.dart|
|`removeWhere` (lib/src/wrappers.dart:795)|method|lib/src/wrappers.dart|
|`equals` (lib/src/equality.dart:212)|method|lib/src/equality.dart|
|`UnionSetController` (lib/src/union_set_controller.dart:24)|class|lib/src/union_set_controller.dart|
|`where` (lib/src/wrappers.dart:112)|method|lib/src/wrappers.dart|

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
