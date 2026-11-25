# ADR 0002: One bounded playback cache for all providers

Accepted 2026-10-25.

## Decision

Copy selected local originals or download selected online originals into one
service-owned playback cache. Validate them before passing a cached filename to
the desktop. Providers discover and resolve images; originals remain untouched.

Bound the cache by configurable image count and bytes. Reuse content
and evict least recently used unprotected entries. Protect displayed and pending
images; fail a new change if they leave insufficient capacity. Do not materialize
entire folders or searches.

## Consequences

Every selected image follows the same validation and publication process. Local
images incur a copy; Omarchy Fit can create an additional display-specific file
in its render cache.

SQLite retains identity, origins, attribution and history independently of cached
filenames. Eviction removes copies, not records; the image limit does not limit
the catalog. File publication requires reconciliation because SQL transactions
cannot include external files.

See [cache behavior](../architecture.md#shared-playback-cache),
[paths and budgets](../configuration.md#default-file-locations), and
[transaction rules](../catalog.md#file-publication-and-reconciliation).
