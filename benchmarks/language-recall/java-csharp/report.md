# Recall on real code, by language

How many of the files that depend on a declaration does `find_references` report? The ground truth is the language's own type checker: rename the declaration, type-check again, and the files that gain an error are the files that use it.

- Command: `go run ./benchmarks/language-recall -kivgraph <binary> -repo-root .`
- Date: 2026-11-20, seed 1, up to 100 declarations per language
- Kivgraph `0.10.0` on darwin/arm64, go1.26.11, rustc 1.100.0 (0ef1bc386 2026-11-09) (Homebrew), javac 21.0.13.1, dotnet 8.0.134

## Corpus

|language|repository|commit|
|---|---|---|
|java|[jsoup](https://github.com/jhy/jsoup)|`d90d57521d93`|
|csharp|[GuardClauses](https://github.com/ardalis/GuardClauses)|`ad43aa02babf`|

## Result

Intervals are 95 % percentile intervals from resampling declarations.

|language|configuration|declarations measured|with dependents|file recall|file precision|fully recalled|
|---|---|---|---|---|---|---|
|java|default|100|98|61.1 % (54.7 to 69.4)|96.6 % (92.2 to 99.5)|58.2 % (48.3 to 67.4)|
|csharp|default|53|40|47.4 % (26.3 to 70.0)|54.0 % (43.3 to 71.0)|47.5 % (32.9 to 62.5)|
|java|java.tests_included|100|98|100.0 % (100.0 to 100.0)|96.8 % (93.4 to 99.2)|100.0 % (96.2 to 100.0)|
|csharp|csharp.tests_included|53|40|49.1 % (27.9 to 71.9)|54.9 % (44.4 to 72.1)|50.0 % (35.2 to 64.8)|

`file recall` pools the dependent files of every declaration. `fully recalled` is the share of declarations with dependents for which no file was missed (Wilson interval). `go.tests_excluded` and `dart.tests_excluded` are the default configuration with that language's `include_tests: false`; each pair draws the same declarations. `java.tests_included` and `csharp.tests_included` are the default configuration with `include_tests: true`, because those two do not read their tests by default.

## java (default)

- Declarations listed 3493, eligible for the rename 2327, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: 0.0 % (0.0 to 0.0).
- Declarations with no dependents for which Kivgraph still reports a file: 1 of 2.
- Completeness verdicts: COMPLETE 95, LOWER_BOUND 5.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 95.
- Not sampled: kind not sampled: 409; name not found in declaring position: 757.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|21|48.9 % (41.8 to 60.5)|
|enum|13|65.6 % (54.6 to 80.5)|
|field|21|70.2 % (50.0 to 100.0)|
|interface|5|53.6 % (36.4 to 71.4)|
|method|19|63.4 % (47.2 to 76.9)|
|term|19|93.1 % (82.1 to 100.0)|

Declarations with a missed file (first 15 of 41):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`KeyVal` (src/main/java/org/jsoup/helper/HttpConnection.java:1393)|class|src/test/java/org/jsoup/helper/HttpConnectionTest.java|COMPLETE|
|`FilterResult` (src/main/java/org/jsoup/select/NodeFilter.java:29)|enum|src/test/java/org/jsoup/nodes/ElementTest.java, src/test/java/org/jsoup/nodes/LeafNodeTest.java, src/test/java/org/jsoup/select/TraversorTest.java|COMPLETE|
|`Response` (src/main/java/org/jsoup/Connection.java:911)|interface|src/test/java/org/jsoup/helper/HttpConnectionTest.java, src/test/java/org/jsoup/integration/ConnectIT.java, src/test/java/org/jsoup/integration/ConnectTest.java, src/test/java/org/jsoup/integration/ProxyTest.java, src/test/java/org/jsoup/nodes/BuildEntities.java, src/test/java/org/jsoup/parser/StreamParserTest.java|COMPLETE|
|`KeyVal` (src/main/java/org/jsoup/Connection.java:1045)|interface|src/test/java/org/jsoup/helper/HttpConnectionTest.java, src/test/java/org/jsoup/integration/ConnectTest.java, src/test/java/org/jsoup/nodes/FormElementTest.java|COMPLETE|
|`ValidationException` (src/main/java/org/jsoup/helper/ValidationException.java:9)|class|src/test/java/org/jsoup/helper/HttpConnectionTest.java, src/test/java/org/jsoup/helper/RegexTest.java, src/test/java/org/jsoup/helper/ValidateTest.java, src/test/java/org/jsoup/nodes/ElementTest.java, src/test/java/org/jsoup/nodes/NodeTest.java, src/test/java/org/jsoup/nodes/TextNodeTest.java, src/test/java/org/jsoup/parser/TagTest.java, src/test/java/org/jsoup/safety/SafelistTest.java|COMPLETE|
|`QuirksMode` (src/main/java/org/jsoup/nodes/Document.java:549)|enum|src/test/java/org/jsoup/parser/HtmlParserTest.java, src/test/java/org/jsoup/parser/TokeniserStateTest.java, src/test/java/org/jsoup/select/CompiledQueryTest.java, src/test/java/org/jsoup/select/WebDomQueryTest.java|COMPLETE|
|`wantsRe2j` (src/main/java/org/jsoup/helper/Regex.java:64)|method|src/test/java/org/jsoup/helper/RegexTest.java|COMPLETE|
|`TextNode` (src/main/java/org/jsoup/nodes/TextNode.java:12)|class|src/test/java/org/jsoup/helper/W3CDomTest.java, src/test/java/org/jsoup/nodes/ElementTest.java, src/test/java/org/jsoup/nodes/NodeIteratorTest.java, src/test/java/org/jsoup/nodes/NodeStreamTest.java, src/test/java/org/jsoup/nodes/NodeTest.java, src/test/java/org/jsoup/nodes/PrinterTest.java, src/test/java/org/jsoup/nodes/TextNodeTest.java, src/test/java/org/jsoup/parser/HtmlParserTest.java, src/test/java/org/jsoup/parser/HtmlTextHandlingTest.java, src/test/java/org/jsoup/parser/HtmlTreeBuilderStateTest.java, src/test/java/org/jsoup/parser/HtmlTreeBuilderTest.java, src/test/java/org/jsoup/parser/PositionTest.java, src/test/java/org/jsoup/parser/TokeniserStateTest.java, src/test/java/org/jsoup/parser/TokeniserTest.java, src/test/java/org/jsoup/parser/XmlTreeBuilderTest.java, src/test/java/org/jsoup/select/ElementsTest.java, src/test/java/org/jsoup/select/NodesTest.java, src/test/java/org/jsoup/select/SelectorTest.java, src/test/java/org/jsoup/select/TraversorTest.java, src/test/java/org/jsoup/select/XpathTest.java|COMPLETE|
|`TokeniserState` (src/main/java/org/jsoup/parser/TokeniserState.java:13)|enum|src/test/java/org/jsoup/parser/TokeniserStateTest.java|COMPLETE|
|`NodeFilter` (src/main/java/org/jsoup/select/NodeFilter.java:25)|interface|src/test/java/org/jsoup/nodes/ElementTest.java, src/test/java/org/jsoup/nodes/LeafNodeTest.java, src/test/java/org/jsoup/select/TraversorTest.java|COMPLETE|
|`Matcher` (src/main/java/org/jsoup/helper/Regex.java:103)|interface|src/test/java/org/jsoup/helper/RegexTest.java|LOWER_BOUND|
|`consumeLetterSequence` (src/main/java/org/jsoup/parser/CharacterReader.java:551)|method|src/test/java/org/jsoup/parser/CharacterReaderTest.java|COMPLETE|
|`NamespaceBindings` (src/main/java/org/jsoup/internal/NamespaceBindings.java:11)|class|src/test/java/org/jsoup/internal/NamespaceBindingsTest.java|COMPLETE|
|`Method` (src/main/java/org/jsoup/Connection.java:53)|enum|src/test/java/org/jsoup/helper/HttpConnectionTest.java, src/test/java/org/jsoup/integration/ConnectTest.java, src/test/java/org/jsoup/nodes/FormElementTest.java|LOWER_BOUND|
|`current` (src/main/java/org/jsoup/internal/QueryMode.java:20)|method|src/test/java/org/jsoup/select/CompiledQueryTest.java, src/test/java/org/jsoup/select/QueryCompatTest.java|COMPLETE|

Declarations with a reported file the compiler did not flag (first 15 of 5). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`AuthShim` (src/main/java/org/jsoup/helper/AuthenticationHandler.java:61)|interface|src/main/java11/org/jsoup/helper/RequestAuthHandler.java|
|`Method` (src/main/java/org/jsoup/Connection.java:53)|enum|src/main/java11/org/jsoup/helper/HttpClientExecutor.java|
|`CaseSensitivity` (src/main/java/org/jsoup/internal/CssMatching.java:102)|enum|src/main/java/org/jsoup/internal/CssMatching.java|
|`Response` (src/main/java/org/jsoup/helper/HttpConnection.java:832)|class|src/main/java/org/jsoup/helper/CookieUtil.java, src/main/java/org/jsoup/helper/RequestDispatch.java, src/main/java/org/jsoup/helper/RequestExecutor.java, src/main/java11/org/jsoup/helper/HttpClientExecutor.java|
|`HEAD(false)` (src/main/java/org/jsoup/Connection.java:62)|term|src/main/java/org/jsoup/helper/HttpConnection.java|

## csharp (default)

- Declarations listed 328, eligible for the rename 53, measured 53 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 66.7 % (30.0 to 100.0). On test files: 42.2 % (19.0 to 67.4).
- Declarations with no dependents for which Kivgraph still reports a file: 4 of 13.
- Completeness verdicts: COMPLETE 44, LOWER_BOUND 9.
- Answers that said `COMPLETE` and missed at least one non-test file: 2 of 44.
- Not sampled: kind not sampled: 209; name not found in declaring position: 66.

|kind|declarations with dependents|file recall|
|---|---|---|
|method|40|47.4 % (26.3 to 68.4)|

Declarations with a missed file (first 15 of 21):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`Zero` (src/GuardClauses/GuardAgainstZeroExtensions.cs:40)|method|test/GuardClauses.UnitTests/GuardAgainstZero.cs|LOWER_BOUND|
|`NullOrEmpty` (src/GuardClauses/GuardAgainstNullExtensions.cs:121)|method|test/GuardClauses.UnitTests/GuardAgainstNullOrEmpty.cs|COMPLETE|
|`Zero` (src/GuardClauses/GuardAgainstZeroExtensions.cs:100)|method|test/GuardClauses.UnitTests/GuardAgainstZero.cs|LOWER_BOUND|
|`NegativeOrZero` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:234)|method|test/GuardClauses.UnitTests/GuardAgainstNegativeOrZero.cs|COMPLETE|
|`NegativeOrZero` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:253)|method|test/GuardClauses.UnitTests/GuardAgainstNegativeOrZero.cs|COMPLETE|
|`Negative` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:132)|method|src/GuardClauses/GuardAgainstNegativeExtensions.cs, src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs|COMPLETE|
|`NullOrOutOfRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:256)|method|test/GuardClauses.UnitTests/GuardAgainstNullOrOutOfRangeForNullableInt.cs|COMPLETE|
|`Negative` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:113)|method|test/GuardClauses.UnitTests/GuardAgainstNegative.cs|COMPLETE|
|`EnumOutOfRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:84)|method|test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForEnum.cs|COMPLETE|
|`Negative` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:39)|method|test/GuardClauses.UnitTests/GuardAgainstNegative.cs|COMPLETE|
|`Zero` (src/GuardClauses/GuardAgainstZeroExtensions.cs:138)|method|src/GuardClauses/GuardAgainstZeroExtensions.cs|LOWER_BOUND|
|`Negative` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:58)|method|test/GuardClauses.UnitTests/GuardAgainstNegative.cs|COMPLETE|
|`Zero` (src/GuardClauses/GuardAgainstZeroExtensions.cs:60)|method|test/GuardClauses.UnitTests/GuardAgainstZero.cs|LOWER_BOUND|
|`InvalidFormat` (src/GuardClauses/GuardAgainstInvalidFormatExtensions.cs:21)|method|test/GuardClauses.UnitTests/GuardAgainstInvalidFormatTests.cs|COMPLETE|
|`NegativeOrZero` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:273)|method|src/GuardClauses/GuardAgainstNegativeExtensions.cs|COMPLETE|

Declarations with a reported file the compiler did not flag (first 15 of 11). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`Zero` (src/GuardClauses/GuardAgainstZeroExtensions.cs:20)|method|test/GuardClauses.UnitTests/GuardAgainstZero.cs|
|`StringTooShort` (src/GuardClauses/GuardAgainstStringLengthExtensions.cs:24)|method|test/GuardClauses.UnitTests/GuardAgainstStringTooShort.cs|
|`Null` (src/GuardClauses/GuardAgainstNullExtensions.cs:28)|method|test/GuardClauses.UnitTests/GuardAgainstNull.cs|
|`Negative` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:19)|method|test/GuardClauses.UnitTests/GuardAgainstNegative.cs|
|`NegativeOrZero` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:158)|method|src/GuardClauses/GuardAgainstStringLengthExtensions.cs, test/GuardClauses.UnitTests/GuardAgainstNegativeOrZero.cs|
|`StringTooLong` (src/GuardClauses/GuardAgainstStringLengthExtensions.cs:53)|method|test/GuardClauses.UnitTests/GuardAgainstStringTooLong.cs|
|`NullOrEmpty` (src/GuardClauses/GuardAgainstNullExtensions.cs:92)|method|src/GuardClauses/GuardAgainstNotFoundExtensions.cs, src/GuardClauses/GuardAgainstNullExtensions.cs, test/GuardClauses.UnitTests/GuardAgainstNullOrEmpty.cs|
|`OutOfRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:118)|method|test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForDateTime.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForDecimal.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForDouble.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForFloat.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForInt.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForShort.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForStructIComparable.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForTimeSpan.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForUint.cs|
|`NullOrOutOfRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:229)|method|test/GuardClauses.UnitTests/GuardAgainstNullOrOutOfRangeForInt.cs, test/GuardClauses.UnitTests/GuardAgainstNullOrOutOfRangeForNullableInt.cs|
|`InvalidInput` (src/GuardClauses/GuardAgainstInvalidFormatExtensions.cs:52)|method|test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForInvalidInput.cs|
|`OutOfSQLDateRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:177)|method|test/GuardClauses.UnitTests/GuardAgainstOutOfSQLDateRange.cs|

## java (java.tests_included)

- Declarations listed 3493, eligible for the rename 2327, measured 100 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 100.0 % (100.0 to 100.0). On test files: 100.0 % (100.0 to 100.0).
- Declarations with no dependents for which Kivgraph still reports a file: 1 of 2.
- Completeness verdicts: COMPLETE 93, LOWER_BOUND 7.
- Answers that said `COMPLETE` and missed at least one non-test file: 0 of 93.
- Not sampled: kind not sampled: 409; name not found in declaring position: 757.

|kind|declarations with dependents|file recall|
|---|---|---|
|class|21|100.0 % (100.0 to 100.0)|
|enum|13|100.0 % (100.0 to 100.0)|
|field|21|100.0 % (100.0 to 100.0)|
|interface|5|100.0 % (100.0 to 100.0)|
|method|19|100.0 % (100.0 to 100.0)|
|term|19|100.0 % (100.0 to 100.0)|

Declarations with a reported file the compiler did not flag (first 15 of 6). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`Response` (src/main/java/org/jsoup/Connection.java:911)|interface|src/test/java11/org/jsoup/integration/HttpClientConnectIT.java|
|`AuthShim` (src/main/java/org/jsoup/helper/AuthenticationHandler.java:61)|interface|src/main/java11/org/jsoup/helper/RequestAuthHandler.java|
|`Method` (src/main/java/org/jsoup/Connection.java:53)|enum|src/main/java11/org/jsoup/helper/HttpClientExecutor.java, src/test/java11/org/jsoup/helper/HttpClientExecutorTest.java|
|`CaseSensitivity` (src/main/java/org/jsoup/internal/CssMatching.java:102)|enum|src/main/java/org/jsoup/internal/CssMatching.java|
|`Response` (src/main/java/org/jsoup/helper/HttpConnection.java:832)|class|src/main/java/org/jsoup/helper/CookieUtil.java, src/main/java/org/jsoup/helper/RequestDispatch.java, src/main/java/org/jsoup/helper/RequestExecutor.java, src/main/java11/org/jsoup/helper/HttpClientExecutor.java|
|`HEAD(false)` (src/main/java/org/jsoup/Connection.java:62)|term|src/main/java/org/jsoup/helper/HttpConnection.java, src/test/java/org/jsoup/helper/HttpConnectionTest.java, src/test/java/org/jsoup/integration/ConnectTest.java|

## csharp (csharp.tests_included)

- Declarations listed 328, eligible for the rename 53, measured 53 (oracle failed on 0, Kivgraph refused 0).
- Recall on non-test files: 66.7 % (30.0 to 100.0). On test files: 44.4 % (20.3 to 69.8).
- Declarations with no dependents for which Kivgraph still reports a file: 4 of 13.
- Completeness verdicts: COMPLETE 44, LOWER_BOUND 9.
- Answers that said `COMPLETE` and missed at least one non-test file: 2 of 44.
- Not sampled: kind not sampled: 209; name not found in declaring position: 66.

|kind|declarations with dependents|file recall|
|---|---|---|
|method|40|49.1 % (27.4 to 70.0)|

Declarations with a missed file (first 15 of 20):

|declaration|kind|missed|verdict|
|---|---|---|---|
|`Zero` (src/GuardClauses/GuardAgainstZeroExtensions.cs:40)|method|test/GuardClauses.UnitTests/GuardAgainstZero.cs|LOWER_BOUND|
|`NullOrEmpty` (src/GuardClauses/GuardAgainstNullExtensions.cs:121)|method|test/GuardClauses.UnitTests/GuardAgainstNullOrEmpty.cs|COMPLETE|
|`Zero` (src/GuardClauses/GuardAgainstZeroExtensions.cs:100)|method|test/GuardClauses.UnitTests/GuardAgainstZero.cs|LOWER_BOUND|
|`NegativeOrZero` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:234)|method|test/GuardClauses.UnitTests/GuardAgainstNegativeOrZero.cs|COMPLETE|
|`NegativeOrZero` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:253)|method|test/GuardClauses.UnitTests/GuardAgainstNegativeOrZero.cs|COMPLETE|
|`Negative` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:132)|method|src/GuardClauses/GuardAgainstNegativeExtensions.cs, src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs|COMPLETE|
|`NullOrOutOfRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:256)|method|test/GuardClauses.UnitTests/GuardAgainstNullOrOutOfRangeForNullableInt.cs|COMPLETE|
|`Negative` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:113)|method|test/GuardClauses.UnitTests/GuardAgainstNegative.cs|COMPLETE|
|`EnumOutOfRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:84)|method|test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForEnum.cs|COMPLETE|
|`Negative` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:39)|method|test/GuardClauses.UnitTests/GuardAgainstNegative.cs|COMPLETE|
|`Zero` (src/GuardClauses/GuardAgainstZeroExtensions.cs:138)|method|src/GuardClauses/GuardAgainstZeroExtensions.cs|LOWER_BOUND|
|`Negative` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:58)|method|test/GuardClauses.UnitTests/GuardAgainstNegative.cs|COMPLETE|
|`Zero` (src/GuardClauses/GuardAgainstZeroExtensions.cs:60)|method|test/GuardClauses.UnitTests/GuardAgainstZero.cs|LOWER_BOUND|
|`NegativeOrZero` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:273)|method|src/GuardClauses/GuardAgainstNegativeExtensions.cs|COMPLETE|
|`OutOfRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:202)|method|test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForDateTime.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForDecimal.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForDouble.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForFloat.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForInt.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForShort.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForStructIComparable.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForTimeSpan.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForUint.cs|COMPLETE|

Declarations with a reported file the compiler did not flag (first 15 of 11). These are unaudited: a use the compiler does not reject, such as one in a comment, is not a false positive.

|declaration|kind|extra|
|---|---|---|
|`Zero` (src/GuardClauses/GuardAgainstZeroExtensions.cs:20)|method|test/GuardClauses.UnitTests/GuardAgainstZero.cs|
|`StringTooShort` (src/GuardClauses/GuardAgainstStringLengthExtensions.cs:24)|method|test/GuardClauses.UnitTests/GuardAgainstStringTooShort.cs|
|`Null` (src/GuardClauses/GuardAgainstNullExtensions.cs:28)|method|test/GuardClauses.UnitTests/GuardAgainstNull.cs|
|`Negative` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:19)|method|test/GuardClauses.UnitTests/GuardAgainstNegative.cs|
|`NegativeOrZero` (src/GuardClauses/GuardAgainstNegativeExtensions.cs:158)|method|src/GuardClauses/GuardAgainstStringLengthExtensions.cs, test/GuardClauses.UnitTests/GuardAgainstNegativeOrZero.cs|
|`StringTooLong` (src/GuardClauses/GuardAgainstStringLengthExtensions.cs:53)|method|test/GuardClauses.UnitTests/GuardAgainstStringTooLong.cs|
|`NullOrEmpty` (src/GuardClauses/GuardAgainstNullExtensions.cs:92)|method|src/GuardClauses/GuardAgainstNotFoundExtensions.cs, src/GuardClauses/GuardAgainstNullExtensions.cs, test/GuardClauses.UnitTests/GuardAgainstNullOrEmpty.cs|
|`OutOfRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:118)|method|test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForDateTime.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForDecimal.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForDouble.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForFloat.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForInt.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForShort.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForStructIComparable.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForTimeSpan.cs, test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForUint.cs|
|`NullOrOutOfRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:229)|method|test/GuardClauses.UnitTests/GuardAgainstNullOrOutOfRangeForInt.cs, test/GuardClauses.UnitTests/GuardAgainstNullOrOutOfRangeForNullableInt.cs|
|`InvalidInput` (src/GuardClauses/GuardAgainstInvalidFormatExtensions.cs:52)|method|test/GuardClauses.UnitTests/GuardAgainstOutOfRangeForInvalidInput.cs|
|`OutOfSQLDateRange` (src/GuardClauses/GuardAgainstOutOfRangeExtensions.cs:177)|method|test/GuardClauses.UnitTests/GuardAgainstOutOfSQLDateRange.cs|

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
- Java and C# do not read their tests by default (`java.include_tests` and `csharp.include_tests` are false), so their default arm misses every test file by construction and the `tests_included` arm is the one that reads them.
- `scip-dotnet` resolves a call written as an extension method (`Guard.Against.Negative(0L)`) to the first overload of the name whatever its arguments, and records no occurrence for a generic call with explicit type arguments (`Negative<int>(...)`). The C# corpus is a library of overloaded extension methods, so its recall is mostly that: the other overloads have no reference in the index, and Kivgraph answers `COMPLETE` for them.
- Java is checked by running `javac` over the main and the test sources at once, with the classpath Maven resolves, so one failing file does not hide another; Kivgraph reads `scip-java`, which drives the same compiler through the build. A Java or C# constructor is written with its type's name, so renaming the type breaks the constructors' own declarations: those errors are left out, as for Dart.
- C# is checked with `dotnet build` on the solution, and MSBuild does not compile a project whose reference failed: a rename in the library leaves the test project unbuilt, so its files are missing from the truth. The C# truth is a lower bound and so is its precision, as for a Rust workspace. Kivgraph reads `scip-dotnet`, which drives Roslyn, the same compiler.
