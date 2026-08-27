# The raw evidence was withdrawn

This benchmark's `raw/` directory held the untrimmed transcripts and tool
answers of every arm, and it was removed on 2026-08-28.

It is stated here rather than left as a gap because the point of publishing raw
evidence is that a reader can check the report against it, and a directory that
quietly stops existing looks like a report nobody can audit any more.

**Why.** The runs were measured against a private workspace, and the
transcripts carried its source code verbatim -- not repository names, which can
be renamed, but the code itself. There is no redaction of a source dump that
leaves it both readable and private, so it was withdrawn rather than rewritten.

**What this costs.** The numbers in `report.md` and `results.json` can no longer
be traced back to the exact bytes that produced them. They keep their command,
commit, environment, dataset and seed, so the run can be reproduced against a
corpus of the reader's own; it cannot be re-derived from ours.

**What was kept.** Everything that describes the measurement rather than the
corpus. Repository names in the surviving files are pseudonyms, applied
consistently, and they carry the language of what they stood for so the shape
of the result still means something.
