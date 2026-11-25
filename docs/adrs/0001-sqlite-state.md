# ADR 0001: SQLite for the image catalog and playback state

Accepted 2026-10-25.

## Context

Panora must retain image identity, provider/input provenance, discovery progress,
history, pause state and pending desktop changes. A page's images and discovery
cursor must commit together. The former `state.json` had no durable catalog;
keeping separate stores would allow history and catalog updates to diverge.

## Decision

Use one service-owned SQLite `state.db` for generated state. Providers and clients
access state through the service. Keep stable IDs and relationships in columns,
flexible provider metadata/references in JSON fields, and image bytes in files.
User preferences, including the playback interval, remain in `config.toml`.

Use one writer, foreign keys, rollback journaling, full synchronization and a
finite busy timeout. The pure-Go `modernc.org/sqlite` driver preserves the
Linux/macOS build workflow; CI verifies JSON and foreign-key support.
[Catalog storage](../catalog.md) defines the schema and transactions.

## Alternatives and consequences

A central versioned JSON snapshot could commit pages and cursors together, but
would rewrite the catalog and require custom durable publication and query code.
Per-image sidecars would need a cross-file commit protocol. An embedded document
database would still require packaging, recovery, indexes and schema management.
SQLite supplies these transactions and constraints at the cost of a driver and
schema migrations.

SQLite cannot atomically publish image files or change the desktop. Cache
reconciliation and pending-intent recovery handle those boundaries. Corrupt,
unknown or newer data must fail without reset. Legacy JSON is reported, not
silently deleted or imported; no importer is required for this unreleased app.

References: [atomic commits](https://www.sqlite.org/atomiccommit.html),
[JSON support](https://www.sqlite.org/json1.html),
[foreign keys](https://www.sqlite.org/foreignkeys.html).
