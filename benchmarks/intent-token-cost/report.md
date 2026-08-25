# intent-token-cost

Question: which files hold the answer to a question that names no symbol

Generated 2026-08-25T19:09:17Z from commit `328694fe-dirty` on darwin/arm64 with go1.26.4, generation `000191`, counted in `cl100k_base`.

Command: `go run ./benchmarks/intent-token-cost -server /tmp/kivgraph-witness-bin`. Dataset: `benchmarks/intent-token-cost/questions.json` v2, 24 questions over `api-db-go`, `kivgraph`, `mole`, native arm scoped to git grep -l -i over internal and cmd, in the checkout the question names.

Ground truth: established by reading the implementation, never from a tool answer: every file below was located by searching for the identifiers that implement the behaviour, which the asker does not know yet and the phrase therefore cannot contain -- checked mechanically, by splitting every declared name of the answer file into words and refusing any phrase that shares one; every answer file was confirmed present in the indexed corpus, and each repository name maps to the registered checkout the graph indexed rather than to a worktree, so both arms walk the same tree

## Accuracy first

|question|repo|class|native rank|kivgraph rank|
|---|---|---|---|---|
|which code refuses to publish a generation when the disk is nearly full|kivgraph|common_vocabulary|25 of 33|1 of 10|
|how does a reader tell that a snapshot file does not belong to the generation it was found beside|kivgraph|common_vocabulary|2 of 6|5 of 10|
|where is every tool of this server registered|kivgraph|common_vocabulary|**not offered** (72 files)|9 of 10|
|how is a page token from a different question rejected|kivgraph|rare_vocabulary|**not offered** (0 files)|**not offered** (10 files)|
|where does the daemon decide which client owns it|kivgraph|rare_vocabulary|**not offered** (54 files)|4 of 10|
|which command prints the build provenance as json|kivgraph|rare_vocabulary|3 of 71|**not offered** (10 files)|
|where is the compiler's own library path handed to the rust analyzer|kivgraph|rare_vocabulary|**not offered** (1 files)|**not offered** (10 files)|
|how does a traversal keep from walking the whole graph|kivgraph|common_vocabulary|**not offered** (11 files)|1 of 10|
|why a program on the far machine bound to an outside address is skipped|mole|common_vocabulary|**not offered** (0 files)|**not offered** (10 files)|
|where the connection is aborted because the machine proves a different identity than before|mole|rare_vocabulary|**not offered** (0 files)|**not offered** (10 files)|
|where one rejected forward request does not tear down the whole shared link|mole|common_vocabulary|**not offered** (0 files)|**not offered** (10 files)|
|where the far end learns no more bytes are coming from one side|mole|rare_vocabulary|**not offered** (0 files)|**not offered** (10 files)|
|where changing one setting on disk leaves the user's comments and ordering untouched|mole|common_vocabulary|**not offered** (0 files)|**not offered** (10 files)|
|where an oversized picture upload is turned away instead of being stored|mole|rare_vocabulary|**not offered** (0 files)|**not offered** (10 files)|
|where starting a second background copy is refused while one already runs|mole|common_vocabulary|1 of 1|**not offered** (10 files)|
|where tailing the daemon output detects the file shrank and reopens it|mole|rare_vocabulary|1 of 1|**not offered** (10 files)|
|where a bulk transfer refuses to run when the destination looks like live production|api-db-go|rare_vocabulary|**not offered** (3 files)|**not offered** (10 files)|
|where preparing the tables stops early because something else already created them|api-db-go|rare_vocabulary|**not offered** (0 files)|**not offered** (10 files)|
|where an unexpected crash inside a request becomes a normal error reply instead of raw html|api-db-go|rare_vocabulary|**not offered** (7 files)|**not offered** (10 files)|
|how it goes back to the remote coordinator only after several good replies in a row|api-db-go|rare_vocabulary|**not offered** (0 files)|5 of 10|
|at startup, where absent table shards produce a printed command an operator can paste|api-db-go|common_vocabulary|**not offered** (2 files)|**not offered** (10 files)|
|where the label naming who produced a reply falls back to a placeholder|api-db-go|rare_vocabulary|**not offered** (13 files)|**not offered** (10 files)|
|where an old variable that is no longer read is reported at boot|api-db-go|common_vocabulary|12 of 13|**not offered** (10 files)|
|why a partial save with no usable fields is refused instead of touching the record|api-db-go|common_vocabulary|2 of 185|**not offered** (10 files)|

### Per repository

|repository|questions|native found|kivgraph found|
|---|---|---|---|
|`api-db-go`|8|2|1|
|`kivgraph`|8|3|5|
|`mole`|8|2|0|

### Why a zero happened

|cause|questions|what it would cost to fix|
|---|---|---|
|crowded out by other repositories|0|a parameter the asker already has|
|outranked inside its own repository|4|weights, and nothing persisted|
|unreachable: the file carries no term|14|a schema version, five loaders and a full rebuild|

Found at any rank: native 7 of 24, kivgraph 6 of 24. First: native 2, kivgraph 2.

## Then cost

|question|native answer|kivgraph answer|answer|session|
|---|---|---|---|---|
|which code refuses to publish a generation when the disk is nearly full|259|297|0.87x|0.99x|
|how does a reader tell that a snapshot file does not belong to the generation it was found beside|47|303|0.16x|0.96x|
|where is every tool of this server registered|593|299|1.98x|1.13x|
|how is a page token from a different question rejected|0|287|0.00x|0.90x|
|where does the daemon decide which client owns it|459|295|1.56x|1.06x|
|which command prints the build provenance as json|647|290|2.23x|1.70x|
|where is the compiler's own library path handed to the rust analyzer|9|298|0.03x|0.84x|
|how does a traversal keep from walking the whole graph|100|293|0.34x|0.92x|
|why a program on the far machine bound to an outside address is skipped|0|305|0.00x|0.80x|
|where the connection is aborted because the machine proves a different identity than before|0|303|0.00x|0.86x|
|where one rejected forward request does not tear down the whole shared link|0|305|0.00x|0.93x|
|where the far end learns no more bytes are coming from one side|0|313|0.00x|0.69x|
|where changing one setting on disk leaves the user's comments and ordering untouched|0|309|0.00x|0.92x|
|where an oversized picture upload is turned away instead of being stored|0|309|0.00x|0.84x|
|where starting a second background copy is refused while one already runs|7|318|0.02x|0.86x|
|where tailing the daemon output detects the file shrank and reopens it|6|299|0.02x|0.88x|
|where a bulk transfer refuses to run when the destination looks like live production|30|313|0.10x|0.85x|
|where preparing the tables stops early because something else already created them|0|318|0.00x|0.90x|
|where an unexpected crash inside a request becomes a normal error reply instead of raw html|70|304|0.23x|0.73x|
|how it goes back to the remote coordinator only after several good replies in a row|0|291|0.00x|0.86x|
|at startup, where absent table shards produce a printed command an operator can paste|19|304|0.06x|0.87x|
|where the label naming who produced a reply falls back to a placeholder|153|322|0.48x|0.84x|
|where an old variable that is no longer read is reported at boot|139|318|0.44x|0.94x|
|why a partial save with no usable fields is refused instead of touching the record|2093|323|6.48x|1.46x|

On the 2 questions both arms answered: 306 vs 600 tokens = **0.51x**. A search that finds nothing is cheap, so this is the only cost comparison that is like for like.

Over every question: answer 4631 vs 7316 tokens = **0.63x**, median 0.05x. Session 0.96x, ceiling 1.08x from 55386 body tokens.

## What each phrase could not match

- `which code refuses to publish a generation when the disk is nearly full`: which, the, disk, nearly
- `how does a reader tell that a snapshot file does not belong to the generation it was found beside`: does, tell, that, belong, the, it
- `where is every tool of this server registered`: where
- `where does the daemon decide which client owns it`: where, does, the, which, it
- `which command prints the build provenance as json`: which, the
- `where is the compiler's own library path handed to the rust analyzer`: where, the, handed
- `how does a traversal keep from walking the whole graph`: does, walking, the
- `why a program on the far machine bound to an outside address is skipped`: the, machine, an
- `where the connection is aborted because the machine proves a different identity than before`: where, the, because, machine, than
- `where one rejected forward request does not tear down the whole shared link`: where, does, tear, the
- `where the far end learns no more bytes are coming from one side`: where, the, learns, are, coming
- `where changing one setting on disk leaves the user's comments and ordering untouched`: where, disk, the
- `where an oversized picture upload is turned away instead of being stored`: where, an, picture, upload, turned, away, instead, being
- `where starting a second background copy is refused while one already runs`: where, while
- `where tailing the daemon output detects the file shrank and reopens it`: where, tailing, the, shrank, reopens, it
- `where a bulk transfer refuses to run when the destination looks like live production`: where, the
- `where preparing the tables stops early because something else already created them`: where, the, stops, early, because, something, else, them
- `where an unexpected crash inside a request becomes a normal error reply instead of raw html`: where, an, crash, becomes, instead
- `how it goes back to the remote coordinator only after several good replies in a row`: it, goes, the, good
- `at startup, where absent table shards produce a printed command an operator can paste`: where, an, can
- `where the label naming who produced a reply falls back to a placeholder`: where, the, who, falls
- `where an old variable that is no longer read is reported at boot`: where, an, that, longer
- `why a partial save with no usable fields is refused instead of touching the record`: instead, the

## Limitations

- the working tree carried uncommitted changes: git grep walked them, and so did the server binary only if it was rebuilt from them
- a native search that matches nothing costs zero tokens and answers nothing, so the totals over every question flatter whichever arm missed; the shared-hit factor is the honest one
- the ground truth is one reader's judgement of which file answers each question; a second reader may accept a file this run scores as a miss
- 24 questions over 3 repositories at generation 191: a set this size states a direction, not a rate
- measured on darwin/arm64 with go1.26.4: the native arm walks a working tree, so its cost is not portable across checkouts
- 18 question(s) are not answered by this tool at any rank; the vocabulary of the phrase is not in the graph
