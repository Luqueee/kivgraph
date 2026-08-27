# docstring-simulation

Question: would the prose this code already carries answer what the graph cannot reach?

Generated 2026-08-25T22:16:30Z from commit `255da24-dirty` on darwin/arm64 with go1.26.4, over published generation `000194`.

Command: `go run ./benchmarks/docstring-simulation`. Dataset: `benchmarks/intent-token-cost/questions.json`, 24 questions.

## The corpus one window is shared by

|repository|symbols|share|
|---|---|---|
|`kivgraph`|24610|76%|
|`go-svc-a`|7393|23%|
|`go-svc-e`|416|1%|

## What the prose is

|documented symbols|corpus|bytes|files read|files not on disk|
|---|---|---|---|---|
|10622|32419|2.68 MB|759|0|

## What each arm costs and buys

|indexed text|borrowed hit worth|terms|postings|per symbol|found|first|
|---|---|---|---|---|---|---|
|names only|1.00|2456|347989|10.73|6 of 24|2|
|names only + guessed words|1.00|2456|347989|10.73|11 of 24|4|
|prose|1.00|5986|603808|18.63|5 of 24|2|
|prose + guessed words|1.00|5986|603808|18.63|7 of 24|5|
|prose|0.30|5986|603808|18.63|6 of 24|2|
|prose + guessed words|0.30|5986|603808|18.63|9 of 24|4|
|literals|1.00|4700|408815|12.61|5 of 24|1|
|literals + guessed words|1.00|4700|408815|12.61|10 of 24|4|
|literals|0.30|4700|408815|12.61|7 of 24|3|
|literals + guessed words|0.30|4700|408815|12.61|16 of 24|4|
|prose and literals|1.00|6834|656334|20.25|4 of 24|2|
|prose and literals + guessed words|1.00|6834|656334|20.25|9 of 24|5|
|prose and literals|0.30|6834|656334|20.25|6 of 24|2|
|prose and literals + guessed words|0.30|6834|656334|20.25|10 of 24|5|

|question|repo|names|names only + guessed words w=1.00|prose w=1.00|prose + guessed words w=1.00|prose w=0.30|prose + guessed words w=0.30|literals w=1.00|literals + guessed words w=1.00|literals w=0.30|literals + guessed words w=0.30|prose and literals w=1.00|prose and literals + guessed words w=1.00|prose and literals w=0.30|prose and literals + guessed words w=0.30|
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
|which code refuses to publish a generation when the disk is nearly full|`kivgraph`|1|1|1|**not offered**|**not offered**|4|5|**not offered**|6|1|1|**not offered**|**not offered**|4|6|
|how does a reader tell that a snapshot file does not belong to the generation it was found beside|`kivgraph`|5|5|3|1|2|1|2|**not offered**|**not offered**|8|8|1|2|1|2|
|where is every tool of this server registered|`kivgraph`|10|10|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|
|how is a page token from a different question rejected|`kivgraph`|**not offered**|**not offered**|1|**not offered**|1|**not offered**|1|**not offered**|1|**not offered**|1|**not offered**|1|**not offered**|1|
|where does the daemon decide which client owns it|`kivgraph`|4|4|4|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|5|6|**not offered**|**not offered**|**not offered**|**not offered**|
|which command prints the build provenance as json|`kivgraph`|**not offered**|**not offered**|4|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|
|where is the compiler's own library path handed to the rust analyzer|`kivgraph`|**not offered**|**not offered**|4|2|1|6|1|4|4|**not offered**|2|3|1|7|1|
|how does a traversal keep from walking the whole graph|`kivgraph`|1|1|1|1|1|1|1|3|1|1|1|1|1|1|1|
|why a program on the far machine bound to an outside address is skipped|`go-svc-e`|**not offered**|**not offered**|2|**not offered**|6|**not offered**|2|**not offered**|**not offered**|**not offered**|3|**not offered**|9|**not offered**|2|
|where the connection is aborted because the machine proves a different identity than before|`go-svc-e`|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|10|**not offered**|**not offered**|**not offered**|7|**not offered**|**not offered**|**not offered**|**not offered**|
|where one rejected forward request does not tear down the whole shared link|`go-svc-e`|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|
|where the far end learns no more bytes are coming from one side|`go-svc-e`|**not offered**|**not offered**|8|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|10|**not offered**|**not offered**|**not offered**|**not offered**|
|where changing one setting on disk leaves the user's comments and ordering untouched|`go-svc-e`|**not offered**|**not offered**|8|2|1|2|1|**not offered**|**not offered**|**not offered**|3|2|1|4|1|
|where an oversized picture upload is turned away instead of being stored|`go-svc-e`|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|
|where starting a second background copy is refused while one already runs|`go-svc-e`|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|1|1|1|3|**not offered**|5|**not offered**|7|
|where tailing the daemon output detects the file shrank and reopens it|`go-svc-e`|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|3|1|**not offered**|8|**not offered**|**not offered**|**not offered**|**not offered**|
|where a bulk transfer refuses to run when the destination looks like live production|`go-svc-a`|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|
|where preparing the tables stops early because something else already created them|`go-svc-a`|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|6|**not offered**|6|**not offered**|**not offered**|**not offered**|**not offered**|
|where an unexpected crash inside a request becomes a normal error reply instead of raw html|`go-svc-a`|**not offered**|**not offered**|1|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|2|**not offered**|1|**not offered**|1|**not offered**|1|
|how it goes back to the remote coordinator only after several good replies in a row|`go-svc-a`|5|5|**not offered**|**not offered**|**not offered**|7|**not offered**|**not offered**|**not offered**|7|**not offered**|**not offered**|**not offered**|8|**not offered**|
|at startup, where absent table shards produce a printed command an operator can paste|`go-svc-a`|**not offered**|**not offered**|**not offered**|6|1|**not offered**|3|**not offered**|5|**not offered**|4|**not offered**|2|**not offered**|4|
|where the label naming who produced a reply falls back to a placeholder|`go-svc-a`|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|
|where an old variable that is no longer read is reported at boot|`go-svc-a`|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|3|5|6|3|**not offered**|**not offered**|**not offered**|**not offered**|
|why a partial save with no usable fields is refused instead of touching the record|`go-svc-a`|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|**not offered**|

## Limitations

- the docstring is taken with a text rule and not a parser: a loader would attach slightly more, so the prose arm is a floor and not a ceiling
- this index is built in memory over a published snapshot; it prices the retrieval a schema version would buy and not the cost of persisting it
- the prose arm indexes every comment block above a declaration, including the ones a loader might decline to emit, such as a directive or a licence header
- 24 questions over 3 repositories: a set this size states a direction, not a rate
