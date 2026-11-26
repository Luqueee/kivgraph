# ADR 0165: Include indexed file hashes in the snapshot proof

Two generations can have identical declarations and edges but different source
bytes. A digest that omits the indexed file hashes cannot distinguish them.
The v2 graph digest includes `FileRow.ContentHash` for each file. An absent
hash stays absent; it is not proof of source freshness.

The writer records this digest in `snapshot.content.v2.sha256` and in the
snapshot header. The reader never uses the old `snapshot.content.sha256` as
proof: it cannot tell whether the cached file belongs to this graph. With only
the old record, the normal upgrade path derives the snapshot from LadybugDB.
It neither rewrites the generation nor reports corruption. A native build is
needed to derive it; the next full index publishes a directly mappable cache.

Mapping and derivation agree on publication identity. A token obtained before
the upgrade must be refreshed by repeating the reference query. This change
does not alter stable keys, the LadybugDB schema, or the snapshot binary layout.
