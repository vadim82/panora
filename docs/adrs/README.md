# Architecture decisions

| ADR | Decision | Accepted |
| --- | --- | --- |
| [0001](0001-sqlite-state.md) | SQLite for catalog and playback state | 2026-10-25 |
| [0002](0002-shared-playback-cache.md) | One bounded playback cache for all providers | 2026-10-25 |

See [architecture](../architecture.md) for behavior and [catalog storage](../catalog.md)
for the schema and transaction rules.

Schema 5 retains stable public history handles separately from rewritten private
row positions; see [catalog identity and transactions](../catalog.md).
