# intent-token-cost

Question: which files hold the answer to a question that names no symbol

Generated 2026-08-25T18:09:41Z from commit `be5cd3ea` on darwin/arm64 with go1.26.4, generation `000191`, counted in `cl100k_base`.

Command: `go run ./benchmarks/intent-token-cost -server /tmp/kivgraph-witness-bin`. Dataset: `benchmarks/intent-token-cost/questions.json` v1, 8 questions over `kivgraph`, native arm scoped to git grep -l -i over internal and cmd.

Ground truth: established by reading the implementation, never from a tool answer: every file below was located by searching for the identifiers that implement the behaviour, which the asker does not know yet and the phrase therefore cannot contain; every answer file must exist in the indexed corpus, which is the registered repository and not this worktree

## Accuracy first

|question|class|native rank|kivgraph rank|
|---|---|---|---|
|which code refuses to publish a generation when the disk is nearly full|common_vocabulary|25 of 33|1 of 5|
|how does a reader tell that a snapshot file does not belong to the generation it was found beside|common_vocabulary|2 of 7|5 of 7|
|where is every tool of this server registered|common_vocabulary|**not offered** (75 files)|9 of 9|
|how is a page token from a different question rejected|rare_vocabulary|**not offered** (0 files)|**not offered** (8 files)|
|where does the daemon decide which client owns it|rare_vocabulary|**not offered** (54 files)|4 of 4|
|which command prints the build provenance as json|rare_vocabulary|3 of 75|**not offered** (9 files)|
|where is the compiler's own library path handed to the rust analyzer|rare_vocabulary|**not offered** (1 files)|**not offered** (5 files)|
|how does a traversal keep from walking the whole graph|common_vocabulary|**not offered** (14 files)|1 of 6|

Found at any rank: native 3 of 8, kivgraph 5 of 8. First: native 0, kivgraph 2.

## Then cost

|question|native answer|kivgraph answer|answer|session|
|---|---|---|---|---|
|which code refuses to publish a generation when the disk is nearly full|259|266|0.97x|1.00x|
|how does a reader tell that a snapshot file does not belong to the generation it was found beside|58|329|0.18x|0.96x|
|where is every tool of this server registered|619|338|1.83x|1.12x|
|how is a page token from a different question rejected|0|312|0.00x|0.89x|
|where does the daemon decide which client owns it|459|216|2.12x|1.09x|
|which command prints the build provenance as json|685|323|2.12x|1.67x|
|where is the compiler's own library path handed to the rust analyzer|9|269|0.03x|0.85x|
|how does a traversal keep from walking the whole graph|127|267|0.48x|0.94x|

On the 2 questions both arms answered: 317 vs 595 tokens = **0.53x**. A search that finds nothing is cheap, so this is the only cost comparison that is like for like.

Over every question: answer 2216 vs 2320 tokens = **0.96x**, median 0.72x. Session 1.00x, ceiling 1.10x from 22358 body tokens.

## What each phrase could not match

- `which code refuses to publish a generation when the disk is nearly full`: which, the, disk, nearly
- `how does a reader tell that a snapshot file does not belong to the generation it was found beside`: does, tell, that, belong, the, it
- `where is every tool of this server registered`: where
- `where does the daemon decide which client owns it`: where, does, the, which, it
- `which command prints the build provenance as json`: which, the
- `where is the compiler's own library path handed to the rust analyzer`: where, the, handed
- `how does a traversal keep from walking the whole graph`: does, walking, the

## Limitations

- a native search that matches nothing costs zero tokens and answers nothing, so the totals over every question flatter whichever arm missed; the shared-hit factor is the honest one
- the ground truth is one reader's judgement of which file answers each question; a second reader may accept a file this run scores as a miss
- 8 questions over one repository at generation 191: a set this size states a direction, not a rate
- measured on darwin/arm64 with go1.26.4: the native arm walks a working tree, so its cost is not portable across checkouts
- 3 question(s) are not answered by this tool at any rank; the vocabulary of the phrase is not in the graph
