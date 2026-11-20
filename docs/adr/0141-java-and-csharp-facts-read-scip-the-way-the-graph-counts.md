# ADR 0141: Java and C# facts read SCIP the way the graph counts

## Status

Accepted. Changes the line and column of every Java and C# row, publishes a C#
`partial` type once, and adds a reference from an instantiation to its type.
Needs `kivgraph index --full` for those two languages.

## Context

Java and C# reach the graph through one converter, `internal/scip`, from the
index `scip-java` and `scip-dotnet` write. Neither language had been measured:
the language-recall benchmark had no arm for them because the indexers were not
installed. Installing them and adding the arm (`jsoup`, `GuardClauses`) showed
three defects before a single figure could be produced.

1. **A `partial` type aborted the index.** SCIP gives every part of a C#
   `partial` type the same symbol, so the type arrived once per file and the fact
   set was refused: `symbol "GuardClauses.GuardClauseExtensions" is defined by
   two files ... so two declarations share one identity`. Nothing was published,
   for any repository of the pass.
2. **Positions were one too low.** SCIP counts lines and characters from zero
   and every other producer of the graph counts from one. The converter
   published SCIP's numbers, so every Java and C# row named the line above the
   declaration (`Element.attribute` at `311`, written at `312`), and `get_source`
   reported an unchanged file as `re-anchored +1`.
3. **Building a type was not a use of it.** `new T(...)` is recorded by both
   producers as a use of the constructor alone. A class that is only ever thrown
   (`ValidationException`) was answered as used by nobody outside its file, with
   a `COMPLETE` verdict.

## Decision

All three are fixed in the converter, where both languages share them.

- A symbol defined in several documents is published from one: the first by
  path, so the choice does not follow the order the producer wrote. The other
  parts are not published again. What they contain keeps its own symbols, and a
  use written straight in the body of such a part, outside any member, is owned
  by that file's own scope: an owner in another file is what the snapshot
  refuses. The relations of the type are stated once, from the publishing file.
- Lines and columns are converted to one-based as they leave the converter. A
  file's module symbol starts at line `1`.
- A reference to a constructor (`<init>` for `scip-java`, `.ctor` for
  `scip-dotnet`, with the overload number SCIP appends) also produces a
  reference to its type, at the same position, when the repository declares it.

No stable key changes: a key is built from the qualified name, the kind and the
signature, and none of the three touches them.

## Consequences

Measured on `jsoup` (`100` declarations) with tests read: file recall `100 %`,
precision `96.8 %`, and no `COMPLETE` answer missing a non-test file. Before the
third fix it was `97.6 %` with three such answers.

- `get_file_outline` lists a `partial` type in one of its files only. Its
  members are listed in the file each is written in.
- Not fixed, because the producer does not say it: `scip-dotnet` resolves a
  call written as an extension method to the first overload of the name, and
  records nothing for a generic call with explicit type arguments. On
  `GuardClauses`, a library of overloaded extension methods, recall is `49.1 %`
  and the other overloads are answered `COMPLETE` with no caller. That is a
  wrong absence this change does not remove.
- Not changed: `java.include_tests` and `csharp.include_tests` default to
  `false`. With that default `jsoup` recall is `61.1 %`, every miss a test file.
