# Catalog and playback storage

The catalog retains image identity, discovery progress and playback state in one
service-owned SQLite database. `internal/catalog` owns its SQL and transactions.
Image bytes stay in files; [configuration](configuration.md) defines their paths
and budgets.

## Schema and identity

The SQL files define each schema version:

- [Base schema, version 1](../internal/catalog/schema.sql): images, discovery
  progress, input memberships, history, playback state and pending intent.
- [Content extension, version 2](../internal/catalog/cache_schema.sql): validated
  content records and image-to-content associations.
- [Retry extension, version 3](../internal/catalog/retry_schema.sql): scoped
  attempts, retry deadlines and sanitized failure stages/codes.
- [Exclusion extension, version 4](../internal/catalog/exclusion_schema.sql):
  image/content marks with stable list/restore IDs.
- [History extension, version 5](../internal/catalog/history_schema.sql): unique
  opaque public references for accepted history entries.
- [Local scan extension, version 6](../internal/catalog/local_scan_schema.sql):
  scan epochs and the last observation of each membership.

New databases initialize version 1, then upgrade to version 6. Upgrades from
versions 1–5 run in one transaction. A failure leaves the prior schema and data
intact.

Timestamps are UTC Unix milliseconds. JSON fields hold validated provider data,
not credentials. Foreign keys preserve provenance referenced by history or an
unfinished application.

| Table | Purpose |
| --- | --- |
| `images` | Stable identity and shared metadata |
| `discovery_progress` | Revision, scan token, cursor and completion per input |
| `input_images` | Candidate reference and provider/input/revision membership |
| `playback_history` | Ordered accepted requests and their committed membership |
| `playback_state` | Pause, current history entry and last-action time |
| `pending_apply` | At most one unfinished desktop application |
| `cached_content` | Known hash, file location, validated size/dimensions and usage time |
| `image_content` | Known content association, retained after file eviction |
| `image_exclusions` | Persistent image/content exclusions, creation order and stable restore IDs |
| `provider_retries` | Revision-scoped discovery/image retries and independent provider metadata/download quota gates |

Image uniqueness is `(provider_id, identity_scope, provider_image_id)`:

- Local IDs are relative paths scoped by folder ID plus SHA-256 of the normalized
  configured root. Equal filenames in different roots have different identities,
  including after a root edit under the same folder ID. Labels, enabled state,
  depth and refresh schedules do not change image identity.
- Wallhaven IDs are globally stable within that provider, using an empty scope.
  Two searches finding the same ID produce one image and two memberships.

New local scans use root-qualified scopes without a schema migration. Earlier
folder-only image rows, content associations, history and exclusion marks remain
intact. Rediscovery can create new rows for those files; validated identical bytes
still share content/cache files and content-hash exclusions. Old history remains
usable while its original root and relative path match the enabled folder. Local
exclusion responses use retained input membership for the folder ID, independently
of the internal image scope. Symlink retargeting and file replacement under an
unchanged configured path retain the existing cache behavior.

The integer image ID survives refresh within its identity scope. Upserts preserve referenced rows; SQL
REPLACE is not used. Missing optional metadata does not erase known attribution.
History retains removed inputs and old settings revisions. Each input has its
own discovery revision based on its settings and effective image rules. Fresh
selection uses enabled inputs at their current revisions. On startup, the
service rekeys memberships, history and pending references, and input retries
whose legacy provider revision matches the saved configuration. It restarts
the corresponding provider cursors because their fingerprints have changed.
Mismatched revisions stay available through retained history.

## Discovery transactions

1. Persist a fresh scan token, effective settings revision and null cursor, or
   resume an unfinished scan at its committed cursor.
2. Ask the provider for a page outside the SQL transaction.
3. In one short transaction, check the revision, scan token and expected cursor,
   then upsert identities/memberships and advance progress. Reject stale or
   repeated commits without changing newer work.
4. Store terminal cursors as SQL NULL and record the completed scan epoch.
   During local refresh, candidates from the last completed scan remain in
   selection while newly observed files become available. A completed scan
   retires unobserved local memberships from fresh selection. Remote scans
   accumulate metadata as pages and result order can vary across refreshes.

Opaque provider references remain associated with their input revision. Reported
dimensions do not bypass actual-byte eligibility checks. For local pages, the
coordinator inspects at most 1 MiB of each contained file's header. Header reads
share a 2.5-second budget and leave 500 milliseconds for the page transaction.
Fresh selection uses those dimensions for uncached files.
Unknown dimensions still allow a bounded materialization attempt, which validates
the actual bytes against saved rules.

## Catalog maintenance

The service periodically reclaims obsolete memberships, checkpoints, and
expired input retries in bounded batches. It retains memberships and checkpoints
touched in the past 24 hours and input retry deadlines from that same window.
History, pending intent, and exclusion origins retain their memberships.
Canonical image and content records stay in the database to keep identity and
known hashes stable when images reappear. Cache-file retention keeps its own
budget.

## Draft rule snapshots

Source image rules remain in TOML and require no schema change. Selection and
replay resolve rules from each candidate's retained provider/input membership;
shared Wallhaven image/content identity does not imply shared eligibility.

The read-only rule-preview query accepts at most 512 enabled provider/input/revision
triples. A SQL snapshot selects one current membership per unique image ID in
stable ID order and returns at most 200 rows plus one continuation row. Overlapping
Wallhaven searches do not double-count the same image. Source requests restrict
the input set before choosing memberships; shared-rule requests exclude sources
with independent rules. A second read-only query verifies current-revision
discovery completion before reporting a complete known scope. Stale/disabled input
memberships are outside the scope. The query closes its rows before any filesystem
or cache validation; it makes no schema changes or persistent writes. Metadata and
historical validated dimensions remain distinct from currently verified bytes.

## Playback transactions

Commit `pending_apply` for an existing image and membership before dispatching
the desktop request. Omarchy completes Fit preparation before this commit.
On acknowledgement, save history, position and last-action
time and clear the intent in one transaction. History is limited to 100 entries.
Each accepted entry has a unique opaque
`public_ref` independent of private SQL row IDs and array positions. Migration
assigns references to existing rows in the same transaction as the schema update;
subsequent rewrites retain them. New accepted entries receive random handles.
Pruning removes an entry without reusing its handle. Image references are the
stable integer `images.id` within this database, independent of cache filenames.
They survive history pruning because canonical image identity rows are retained.

An unconfirmed dispatch retains its intent and pauses playback. A definite
pre-dispatch rejection from the Omarchy theme guard restores the prior state,
including any earlier pending intent. If accepted history cannot be saved, the
service stops with intent intact. Pause persists in SQLite. Interval and cache
budgets remain TOML configuration.

## File publication and reconciliation

Publish a fully validated cache file atomically, then save its SQL association
before recording desktop intent. If SQL fails, the complete unassociated file
can be reused by hash or evicted. Startup removes incomplete stages. Missing, nonregular or
size-mismatched files are cache misses; origin, known hash and history remain.
Playback reuse verifies the cache filename, regular file, recorded byte count,
and full SHA-256 under the cache lock. It reuses validated dimensions after
checking the image header. If the header disagrees with the recorded dimensions,
the cache fully decodes the verified bytes to recover the correct metadata. A
missing file or failed verification falls back to materialization from the origin.
Downloads and decoding of unpublished staging files can run alongside previews of
published files. The cache still serializes publication and eviction with
reads and retains exclusive ownership until staging finishes.

SQLite usage timestamps record successful materialization. Filesystem modification time determines
cache eviction order. Eviction deletes cache copies, never user originals
or catalog provenance. SQL transactions cannot atomically include image files or
desktop effects.

Runtime cache reductions change file retention only. Unique file/byte usage is
measured from the managed playback directory, not the number of `image_content`
rows. Shared content is counted once; cache eviction leaves image/content
associations, validated dimensions, history and exclusions available for origin
resolution and rematerialization. Budget application and eviction are serialized
with active reads/publication. Protected current/pending files survive a reduction
even if they exceed the new budget; [cache status](socket-api.md#cache-status) reports
that pressure explicitly. No schema change is needed for budget edits.

## Opening and compatibility

The pure-Go driver is pinned to `modernc.org/sqlite` v1.61.0, preserving Linux/macOS
builds without cgo. One writer connection uses foreign keys, a finite busy timeout,
rollback journaling (`DELETE`) and full synchronization. Connection-local settings
are reapplied on replacement connections; startup verifies the selected settings,
JSON support, schema shape and integrity.

Read `user_version` before persistent changes. Initialize only a genuinely empty
database; reject unrecognized version 0, corrupt, incomplete or newer schemas
without reset. Supported upgrades are transactional and tested for rollback.
SQLite recovery journals are database state, not cache files; do not remove them
as a recovery shortcut.

[ADR 0001](adrs/0001-sqlite-state.md) records the storage choice and alternatives.
[ADR 0002](adrs/0002-shared-playback-cache.md) records shared cache ownership.

## Refresh health

Refresh timestamps and per-attempt metadata counts are process-local. Learned
retry limits persist in `provider_retries`. Failed refreshes preserve earlier
memberships. An empty completed local scan retires its prior candidates. See
[health semantics](architecture.md#background-discovery) and
[status fields](socket-api.md#refresh-health-fields).

Retry keys include provider, input, discovery revision, scope and internal image
identity. Provider-wide quota keys use empty input/revision and image zero;
image materialization failures keep their own identity. No raw errors, URLs,
provider responses or credentials are stored. Attempts saturate at 16 and
millisecond deadlines round up to avoid shortening a hint during persistence.
Input and provider gates learned from one failure upsert in one transaction.
Successful operations remove their own record and expired shared gates; an
in-flight success cannot erase a newer deadline. An expired old-revision retry
can be reclaimed. Provider quotas continue to apply across settings edits and
restart.

## Exclusions

Each `image_exclusions` row marks a stable image ID and, when known, its validated
content hash. A null hash still excludes that identity; another image with the
same bytes can be recognized only after validation establishes its hash. Marks survive history pruning,
cache eviction, input edits/removal and restart. History positions are not
exclusion handles.

The row ID uses SQLite AUTOINCREMENT: restoring deletes a mark and a future mark
cannot reuse its handle. Repeated exclusion of the same image leaves the existing
ID, time and captured hash unchanged. Undo removes the greatest still-active ID.
Restoring one row leaves matching hashes from other active marks effective.

An exclusion change reads the replacement set of image IDs/hashes inside its
transaction and returns it to playback only after commit. Failure leaves the
in-memory set unchanged. Selection checks this set without SQL per candidate.
Materialization records newly validated hash associations before checking for
excluded identical content. Excluded images never reach desktop intent/application.

Newest-first keyset pages use `before` and a 1–10 entry limit. They read origin
identity and retained attribution without opening source files or downloading
images. Exclude/restore never delete user originals or cache files.
