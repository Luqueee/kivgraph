# ADR 0081: C# on the SCIP bridge

- **Status:** accepted with declared limitations
- **Date:** 2026-08-28
- **Revises:** ADR 0080

## Context

ADR 0080 said the SCIP bridge made a second SCIP language "a loader that runs
an indexer and names a package". C# is the test of that claim, and it was
chosen over Scala or Kotlin precisely because it uses a **different producer**:
scip-java and scip-dotnet share only the format. A second language on the same
producer would have proved nothing about the bridge.

## Decision

C# enters through `internal/csharploader`, which runs `scip-dotnet index` and
hands the decoded index to `internal/scip.Convert`. It writes no graph
semantics. The loader is roughly the size of Java's, and the parts that differ
are project discovery and what counts as build output.

scip-dotnet drives Roslyn, so its targets are resolved by the compiler and the
payload is authoritative.

**The indexer never sees the repository.** `dotnet restore` writes `obj/` and
`bin/` into the project it restores, which AGENTS.md forbids without an
exception, so the loader runs it in a scratch tree like the Java one does. See
ADR 0082 -- including the identity defect that shape introduces, which C#
inherits: the project name reaches every stable key and the tree is a fresh
directory per pass, so `convert` takes the identity from the repository and
only the file contents from the tree.

**A solution wins over a project.** Indexing one `.csproj` of a repository that
has several silently drops the rest, which reads as a repository with less code
rather than an index that looked at part of it. Discovery is deterministic:
two passes over one repository index the same thing.

## What the second producer cost, and what it found

The claim was that a second SCIP language is nearly free. It was not free, and
the difference is the useful part of this record: **three bugs in the bridge
were shaped like Java and only a different producer could expose them.**

1. **`addressable` dropped the entire language.** It excluded a symbol whose
   package and version were both `.`, because that is what scip-java writes on
   the package-qualifier occurrences of `package a.b.c;`. It is also what
   scip-dotnet writes on *every symbol a project declares*. The rule now tests
   the descriptors -- a bare namespace path is not a symbol -- which is what it
   always meant.

2. **Parameters were nodes in one language and not the other.** scip-java
   writes a parameter as `local N`, which was already excluded; scip-dotnet
   writes `Coverage/Catalog#Add().(shape)`, a fully qualified symbol. Ten of
   thirty-seven symbols in the fixture were parameters. The same concept is
   now absent in both, decided by the descriptor rather than by the producer.

3. **scip-dotnet sets no `enclosing_range`.** Every declaration would have
   spanned its own name, nothing would have contained anything, and every
   reference in the language would have been sourced at its file's module
   symbol -- `find_references` answering with files. The spans are rebuilt from
   the definition positions and the nesting the descriptors already encode.

It also sets no `SymbolInformation.Kind` and no signature, which the bridge
already tolerated: the kind falls back to the descriptor suffix and the
signature falls back to the descriptor path, which is unique per symbol and so
is a valid stable-key discriminator.

## Consequences

- The provenances `CSHARP_SCIP_DEF` and `CSHARP_SCIP_USE` are appended to the
  frozen numbering as codes `28` and `29`.
- `csharp` and `cs` are both accepted spellings. A repository that declares
  both is indexed once; `dedupeRepositories` is what makes that true, and it
  applies to any future language with more than one spelling.
- Indexing a C# repository runs `dotnet restore`, so a first pass needs the
  network.

## Limitations and risks

- **Every type relation is `EXTENDS`, never `IMPLEMENTS`.** The two differ only
  by what the target is, and scip-dotnet does not classify its symbols, so the
  bridge cannot tell an interface from a class. `EXTENDS` is the weaker claim.
  Reading it off the C# convention that an interface is named `IShape` would be
  inferring a fact from a name, which this project does not do.
- **The enclosing ranges are a reconstruction**, and it is named as one. It
  assumes declarations are contiguous and in source order within a document,
  which is true of C#. Where it is imprecise the cost is bounded: a span reaches
  to the next declaration instead of to its own closing brace, so a reference
  between the two is attributed to the previous member rather than to its
  parent. It never changes which symbol a reference points *at*.
- The generated sources the SDK writes -- `GlobalUsings.g.cs`, an assembly
  attributes file -- are excluded from the graph. Publishing them puts symbols
  nobody wrote in a graph, and they vanish on `dotnet clean`.
- Every use is `REFERENCES`, for the reason ADR 0080 gives.
- A multi-target project is indexed once per the framework scip-dotnet picks;
  a symbol that exists only under another target framework is absent.
