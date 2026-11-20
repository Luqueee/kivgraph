# Recall on real code, by language

How many of the files that depend on a declaration does `find_references` report? The ground truth is the language's own type checker: rename the declaration, type-check again, and the files that gain an error are the files that use it.

- Command: `go run ./benchmarks/language-recall -kivgraph <binary> -repo-root .`
- Date: 2026-11-20, seed 1, up to 100 declarations per language
- Kivgraph `0.10.0` on darwin/arm64, go1.26.11, rustc 1.100.0 (0ef1bc386 2026-11-09) (Homebrew), Python 3.14.10, mypy 2.4.1 (compiled: yes), pyright 1.1.414

## Corpus

|language|repository|commit|
|---|---|---|
|python|[click](https://github.com/pallets/click)|`934813e4d421`|

## Result

Intervals are 95 % percentile intervals from resampling declarations.

|language|configuration|declarations measured|with dependents|file recall|file precision|fully recalled|
|---|---|---|---|---|---|---|
|python|default|100|71|82.9 % (74.0 to 89.5)|85.6 % (70.8 to 96.1)|67.6 % (56.1 to 77.3)|
|python|python.tests_excluded|100|71|58.2 % (51.4 to 67.1)|88.5 % (80.6 to 94.8)|50.7 % (39.3 to 62.0)|
|python|python.exact|100|83|100.0 % (100.0 to 100.0)|95.9 % (92.2 to 98.6)|100.0 % (95.6 to 100.0)|
|python|python.exact.tests_excluded|100|83|66.7 % (59.4 to 75.5)|95.4 % (90.6 to 99.2)|63.9 % (53.1 to 73.4)|

`file recall` pools the dependent files of every declaration. `fully recalled` is the share of declarations with dependents for which no file was missed (Wilson interval). A `tests_excluded` configuration is the default one with that language's `include_tests: false`; each pair draws the same declarations.

## python (default)

- Declarations listed 638, eligible for the rename 621, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 86.0 % (77.2 to 92.5). On test files: 76.5 % (58.3 to 87.5).
- Declarations with no dependents for which Kivgraph still reports a file: 10 of 29.
- Completeness verdicts: COMPLETE 60, LOWER_BOUND 40.
- Answers that said `COMPLETE` and missed at least one non-test file: 1 of 60.
- Not sampled: kind not sampled: 16; name not found in declaring position: 1.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|23|93.7 % (87.3 to 98.4)|
|function|25|84.1 % (73.3 to 93.3)|
|method|10|0.0 % (0.0 to 0.0)|
|variable|13|95.2 % (84.6 to 100.0)|

Declarations with a missed file (first 15 of 23):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`decorator` (src/click/decorators.py:75)|function|src/click/decorators.py|LOWER_BOUND|
|`unstyle` (src/click/termui.py:591)|function|tests/test_utils.py|LOWER_BOUND|
|`takes_value` (src/click/parser.py:196)|method|src/click/parser.py|LOWER_BOUND|
|`_available_shells` (src/click/shell_completion.py:407)|variable|tests/test_shell_completion.py|LOWER_BOUND|
|`format_usage` (src/click/core.py:1269)|method|src/click/core.py|LOWER_BOUND|
|`call_on_close` (src/click/core.py:587)|method|src/click/types.py, tests/test_context.py|LOWER_BOUND|
|`get_short_help_str` (src/click/core.py:1337)|method|src/click/core.py|LOWER_BOUND|
|`Abort` (src/click/exceptions.py:282)|class|tests/test_utils.py|LOWER_BOUND|
|`visible_input` (src/click/testing.py:279)|function|src/click/testing.py|LOWER_BOUND|
|`shell_complete` (src/click/core.py:965)|method|src/click/core.py, src/click/shell_completion.py|LOWER_BOUND|
|`KeepOpenFile` (src/click/utils.py:194)|class|tests/test_utils.py|LOWER_BOUND|
|`secho` (src/click/termui.py:603)|function|tests/test_termui.py|LOWER_BOUND|
|`_get_windows_console_stream` (src/click/_compat.py:561)|function|src/click/_compat.py|COMPLETE|
|`abort` (src/click/core.py:693)|method|src/click/decorators.py|LOWER_BOUND|
|`FuncParamType` (src/click/types.py:165)|class|tests/test_info_dict.py|LOWER_BOUND|

Declarations with a reported file the compiler did not flag (first 15 of 11). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`STDOUT_HANDLE` (src/click/_winconsole.py:50)|variable|src/click/_winconsole.py|
|`GetConsoleMode` (src/click/_winconsole.py:41)|variable|src/click/_winconsole.py|
|`WriteConsoleW` (src/click/_winconsole.py:40)|variable|src/click/_winconsole.py|
|`group` (src/click/decorators.py:254)|function|src/click/__init__.py, src/click/core.py, tests/test_arguments.py, tests/test_basic.py, tests/test_chain.py, tests/test_commands.py, tests/test_context.py, tests/test_defaults.py, tests/test_formatting.py, tests/test_normalization.py, tests/test_options.py, tests/typing/typing_group_kw_options.py|
|`GetStdHandle` (src/click/_winconsole.py:38)|variable|src/click/_winconsole.py|
|`c_ssize_p` (src/click/_winconsole.py:35)|variable|src/click/_winconsole.py|
|`ERROR_OPERATION_ABORTED` (src/click/_winconsole.py:58)|variable|src/click/_winconsole.py|
|`_WindowsConsoleReader` (src/click/_winconsole.py:114)|class|src/click/_winconsole.py|
|`ReadConsoleW` (src/click/_winconsole.py:39)|variable|src/click/_winconsole.py|
|`AFTER_BAR` (src/click/_termui_impl.py:33)|variable|src/click/_termui_impl.py|
|`_WindowsConsoleWriter` (src/click/_winconsole.py:149)|class|src/click/_winconsole.py|

## python (python.tests_excluded)

- Declarations listed 638, eligible for the rename 621, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 86.0 % (77.2 to 92.5). On test files: 0.0 % (0.0 to 0.0).
- Declarations with no dependents for which Kivgraph still reports a file: 10 of 29.
- Completeness verdicts: COMPLETE 71, LOWER_BOUND 29.
- Answers that said `COMPLETE` and missed at least one non-test file: 1 of 71.
- Not sampled: kind not sampled: 16; name not found in declaring position: 1.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|23|57.0 % (50.0 to 69.5)|
|function|25|68.2 % (53.7 to 82.6)|
|method|10|0.0 % (0.0 to 0.0)|
|variable|13|81.0 % (67.9 to 100.0)|

Declarations with a missed file (first 15 of 35):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`MissingParameter` (src/click/exceptions.py:136)|class|tests/test_arguments.py, tests/test_options.py|COMPLETE|
|`decorator` (src/click/decorators.py:75)|function|src/click/decorators.py|LOWER_BOUND|
|`unstyle` (src/click/termui.py:591)|function|tests/test_utils.py|COMPLETE|
|`takes_value` (src/click/parser.py:196)|method|src/click/parser.py|LOWER_BOUND|
|`_available_shells` (src/click/shell_completion.py:407)|variable|tests/test_shell_completion.py|COMPLETE|
|`format_usage` (src/click/core.py:1269)|method|src/click/core.py|LOWER_BOUND|
|`call_on_close` (src/click/core.py:587)|method|src/click/types.py, tests/test_context.py|LOWER_BOUND|
|`get_short_help_str` (src/click/core.py:1337)|method|src/click/core.py|LOWER_BOUND|
|`Abort` (src/click/exceptions.py:282)|class|tests/test_utils.py|COMPLETE|
|`visible_input` (src/click/testing.py:279)|function|src/click/testing.py|LOWER_BOUND|
|`split_arg_string` (src/click/parser.py:127)|function|tests/test_parser.py|COMPLETE|
|`shell_complete` (src/click/core.py:965)|method|src/click/core.py, src/click/shell_completion.py|LOWER_BOUND|
|`KeepOpenFile` (src/click/utils.py:194)|class|tests/test_utils.py|COMPLETE|
|`secho` (src/click/termui.py:603)|function|tests/test_termui.py, tests/test_testing.py|COMPLETE|
|`STRING` (src/click/types.py:1078)|variable|tests/test_info_dict.py, tests/test_options.py|COMPLETE|

Declarations with a reported file the compiler did not flag (first 15 of 11). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`STDOUT_HANDLE` (src/click/_winconsole.py:50)|variable|src/click/_winconsole.py|
|`GetConsoleMode` (src/click/_winconsole.py:41)|variable|src/click/_winconsole.py|
|`WriteConsoleW` (src/click/_winconsole.py:40)|variable|src/click/_winconsole.py|
|`group` (src/click/decorators.py:254)|function|src/click/__init__.py, src/click/core.py|
|`GetStdHandle` (src/click/_winconsole.py:38)|variable|src/click/_winconsole.py|
|`c_ssize_p` (src/click/_winconsole.py:35)|variable|src/click/_winconsole.py|
|`ERROR_OPERATION_ABORTED` (src/click/_winconsole.py:58)|variable|src/click/_winconsole.py|
|`_WindowsConsoleReader` (src/click/_winconsole.py:114)|class|src/click/_winconsole.py|
|`ReadConsoleW` (src/click/_winconsole.py:39)|variable|src/click/_winconsole.py|
|`AFTER_BAR` (src/click/_termui_impl.py:33)|variable|src/click/_termui_impl.py|
|`_WindowsConsoleWriter` (src/click/_winconsole.py:149)|class|src/click/_winconsole.py|

## python (python.exact)

- Declarations listed 852, eligible for the rename 832, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: 100.0 % (100.0 to 100.0).
- Declarations with no dependents for which Kivgraph still reports a file: 4 of 17.
- Completeness verdicts: COMPLETE 86, LOWER_BOUND 14.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 86.
- Not sampled: kind not sampled: 16; name not found in declaring position: 4.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|20|100.0 % (100.0 to 100.0)|
|constant|19|100.0 % (100.0 to 100.0)|
|function|19|100.0 % (100.0 to 100.0)|
|method|10|100.0 % (100.0 to 100.0)|
|variable|15|100.0 % (100.0 to 100.0)|

Declarations with a reported file the compiler did not flag (first 15 of 7). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`BaseCommand` (src/click/core.py:837)|class|tests/test_context.py|
|`AFTER_BAR` (src/click/_termui_impl.py:36)|constant|src/click/_termui_impl.py|
|`WIN` (src/click/_compat.py:10)|constant|src/click/_compat.py|
|`format_message` (src/click/exceptions.py:123)|method|tests/test_arguments.py|
|`should_strip_ansi` (src/click/_compat.py:496)|function|src/click/_compat.py|
|`auto_wrap_for_ansi` (src/click/_compat.py:519)|function|src/click/_compat.py, src/click/utils.py|
|`secondary_opts` (src/click/core.py:2115)|variable|src/click/core.py|

## python (python.exact.tests_excluded)

- Declarations listed 852, eligible for the rename 832, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: 0.0 % (0.0 to 0.0).
- Declarations with no dependents for which Kivgraph still reports a file: 3 of 17.
- Completeness verdicts: COMPLETE 90, LOWER_BOUND 10.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 90.
- Not sampled: kind not sampled: 16; name not found in declaring position: 4.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|20|53.9 % (44.2 to 66.2)|
|constant|19|66.7 % (55.1 to 82.1)|
|function|19|82.8 % (71.4 to 93.1)|
|method|10|81.8 % (61.5 to 100.0)|
|variable|15|90.5 % (80.0 to 100.0)|

Declarations with a missed file (first 15 of 30):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`HelpFormatter` (src/click/formatting.py:102)|class|tests/test_custom_classes.py, tests/test_formatting.py|COMPLETE|
|`COMMANDLINE` (src/click/core.py:157)|constant|tests/test_context.py|COMPLETE|
|`progressbar` (src/click/termui.py:283)|function|tests/test_termui.py|COMPLETE|
|`nargs` (src/click/core.py:2131)|variable|tests/test_options.py|COMPLETE|
|`CompletionItem` (src/click/shell_completion.py:55)|class|tests/test_shell_completion.py|COMPLETE|
|`MissingParameter` (src/click/exceptions.py:136)|class|tests/test_arguments.py, tests/test_options.py|COMPLETE|
|`UUID` (src/click/types.py:1093)|constant|tests/test_basic.py, tests/test_info_dict.py|LOWER_BOUND|
|`getvalue` (src/click/formatting.py:278)|method|tests/test_formatting.py|LOWER_BOUND|
|`ENVIRONMENT` (src/click/core.py:159)|constant|tests/test_context.py|COMPLETE|
|`Context` (src/click/core.py:169)|class|tests/test_arguments.py, tests/test_commands.py, tests/test_context.py, tests/test_custom_classes.py, tests/test_info_dict.py, tests/test_options.py, tests/test_parser.py, tests/test_types.py, tests/typing/typing_aliased_group.py|LOWER_BOUND|
|`BOOL` (src/click/types.py:1090)|constant|tests/test_info_dict.py|COMPLETE|
|`password_option` (src/click/decorators.py:395)|function|tests/typing/typing_password_option.py|COMPLETE|
|`BaseCommand` (src/click/core.py:837)|class|tests/test_commands.py|COMPLETE|
|`OptionParser` (src/click/parser.py:255)|class|tests/test_parser.py|COMPLETE|
|`FuncParamType` (src/click/types.py:165)|class|tests/test_info_dict.py|COMPLETE|

Declarations with a reported file the compiler did not flag (first 15 of 5). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`AFTER_BAR` (src/click/_termui_impl.py:36)|constant|src/click/_termui_impl.py|
|`WIN` (src/click/_compat.py:10)|constant|src/click/_compat.py|
|`should_strip_ansi` (src/click/_compat.py:496)|function|src/click/_compat.py|
|`auto_wrap_for_ansi` (src/click/_compat.py:519)|function|src/click/_compat.py, src/click/utils.py|
|`secondary_opts` (src/click/core.py:2115)|variable|src/click/core.py|

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
- Python is checked with mypy, which Kivgraph does not use: its exact mode reads Pyright and its default mode a syntactic worker. mypy reports nothing through a value it types as `Any` -- an unannotated parameter, which is what a pytest fixture is -- so a use reached only that way is not in the truth. The Python truth is a lower bound: a file Kivgraph reports beyond it counts against precision whether or not it is a real use.
- After an import fails mypy binds the imported name to `Any`, which would hide every use behind a re-export in a package's `__init__`. The oracle follows the rename through: each import that fails for the renamed name is rewritten to the new name and the check runs again until none fails.
- `python.tests_excluded`, `python.exact` and `python.exact.tests_excluded` are the default configuration with `python.include_tests: false`, with `analyzer_mode: exact` over Pyright, and with both. Each samples from what its own graph lists, and the two modes do not list the same declarations, so the four do not draw the same sample.
- mypy checks the package and its tests. The repository's `docs` and `examples` are scripts that each define modules of the same name, which it cannot check together, so a file Kivgraph reports there is left out of the comparison instead of being counted against it.
- A name bound in more than one place survives the rename of one binding: a constant assigned in both branches of an `if`, an attribute annotated on one line and assigned on the next. mypy reports nothing then, and the uses count against precision. So do the uses on a line marked `# type: ignore`.
- mypy does not check code under a platform test that is false where it runs (`if sys.platform == "win32"`), so declarations used only there have no truth on this machine and their uses count against precision.
