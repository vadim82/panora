# Architecture

One foreground service discovers images, chooses wallpapers, records history and
runs the rotation timer. Providers find candidates; desktop adapters apply files
validated by playback. CLI/UI clients control this service through HTTP/JSON over
a private Unix socket.

With no enabled input, the service reports `setup_required`. Discovery wakes
the same scheduler when a source is enabled. See the
[configuration reference](configuration.md#first-run-defaults) for defaults.

## Ownership

| Package | Responsibility |
| --- | --- |
| `cmd/panora` | Entry point and operating-system signals |
| `internal/cli` | Command flags, output, provider and backend wiring |
| `internal/service` | Startup, control listener and shutdown coordination |
| `internal/control` | Private socket transport and versioned API |
| `internal/appservice` | Installed user-service status and explicit lifecycle operations |
| `internal/playback` | Eligibility, fair selection, history, recovery and scheduling |
| `internal/catalog` | SQLite schema, identity, provenance and transactions |
| `internal/cache` | Selected-file copy/download, validation, publication and eviction |
| `internal/source` | Provider interface, versioned editor manifests, program registry and bounded JSON-RPC child processes |
| `internal/omarchytheme` | Read-only active theme state, palette and still enumeration |
| `internal/sources/*` | Local traversal, Wallhaven and Alpha Coders metadata implementations |
| `internal/desktop` | Backend readiness, Fit preparation and wallpaper application |
| `internal/localfiles`, `internal/filelock` | Shared path checks and advisory locks |

The CLI injects provider implementations into playback through the provider
interface. The installed service starts the local provider from its own
executable and calls Wallhaven in process.
[Dependency tests](../internal/cli/architecture_test.go) enforce those package
boundaries. [Provider commands](providers/README.md#running-a-provider) expose
the same operations over JSON-RPC. Playback reads Omarchy theme state and
delegates desktop command serialization and dispatch to the adapter.

The [native client](../ui/README.md) is a separate Go module. It and the
[Omarchy bar](../integrations/omarchy/README.md) are clients of the same
service. The [frontend guide](frontend.md) covers request coordination and
embedded assets. The [socket reference](socket-api.md) covers revision checks,
stable IDs, and read-only projections.

## Installed macOS service lifecycle

`internal/appservice` owns the installed app's LaunchAgent registration and bounded
start/stop/restart operations. It uses `internal/control` and serializes explicit
operations across clients with `internal/filelock`. The Wails bridge accepts a fixed
operation allowlist. The native bundle and user environment determine executable,
registration and log paths.

One Aqua user-session job runs the sibling `panora serve` executable with absolute
arguments. `RunAtLoad` follows the opt-in login preference and `KeepAlive` stays false.
The bundled service owns a separate AppKit menu agent from socket readiness
through shutdown. The agent locks to one socket and verifies the service
instance before sending playback or shutdown requests. The Wails window can
quit independently. Polling observes state. The app reuses a compatible
external service and preserves unverifiable or incompatible owners. Explicit
Restart stops and joins the verified
job before starting it. See [startup controls](macos.md#background-service-controls),
[paths and log limits](configuration.md#default-file-locations), and
[verified scope](status.md#platform-coverage).

Homebrew registrations use the stable `opt/panora/bin/panora` wrapper. The
wrapper starts the binary in its versioned keg so the service and bundled
helpers come from the same build. The managed process observes Homebrew's link.
When it points to a complete replacement keg, the service drains its work and
replaces itself in the same launchd job. The registration and login preference
stay in place. A stopped service uses the new keg when it next starts. Opening
the first supporting Homebrew build can migrate an older managed registration
from the same formula.

## Installed Omarchy service lifecycle

The Arch package supplies `/usr/lib/systemd/user/panora.service`. The unit
belongs to the graphical session and starts Panora with a bounded Wayland
and Omarchy IPC readiness check. The managed process publishes the same
private socket as foreground `serve`. `Restart=no` keeps explicit Stop
effective. Login enablement and a running service are independent.
After reporting readiness, the packaged service makes a bounded, best-effort
attempt to register and enable its bar widget for users without a registration.
The widget derives its visibility from the existing read-only socket snapshot.

`internal/appservice` serializes managed operations and checks the packaged
unit and socket peer before it can Stop or Restart a process. Compatible
external service instances remain usable. Their lifecycle stays with the
process owner. The native installed client starts the service on open or an
explicit Start. Native status polling and shell calls remain read-only
with respect to startup. The service still owns the backend, state and the
only timer.

The Arch install script maintains a root-owned
`/var/lib/panora-package-lifetime` file in the installation root. The CLI delegates
package lifetime verification to `internal/appservice`. After verifying its
unit and main process, the packaged service holds a shared lock on this inode
and watches for unlink. Compatible upgrades reuse the inode. After a successful
upgrade and Arch's user-manager reload, the package's post-transaction hook
checks each live user manager for an active stock unit still using the replaced
executable. It requests `try-restart` with conflicting jobs left in place.
The hook preserves the login registration and skips offline roots.

After removing the package files, pacman unlinks the marker and waits once
for the shared locks to drain. Each service cancels its own startup or playback
and releases its lock after cleanup. An admission concurrent with removal
must acquire the lock and verify a live link after installing its watch.

## Desktop adapter commands

Adapters implement the Go `WallpaperBackend` interface under
[`internal/desktop`](../internal/desktop). The service supplies a validated
file and presentation options. Desktop rendering remains with the OS or shell.

| Desktop | Integration |
| --- | --- |
| Omarchy | `omarchy theme bg set <path>` hands the file to the shell |
| Niri / Noctalia | `noctalia msg wallpaper-set <path>` requests a global image |
| macOS | The [Swift helper](../tools/macos-wallpaper.swift) invokes AppKit through `panora-macos-wallpaper set <path> <fit> <upscale>` |

The shared runner bounds subprocess lifetime, stdout (64 KiB), and stderr
(4 KiB). Panora checks the adapter result before committing history.
The [desktop guide](desktop.md) covers the Space observer, placement and
renderer limits.

To add an adapter, provide a bounded readiness probe and verify repeated
changes, placement, and recovery in a real supported session. Keep rendering
and platform-specific dependencies behind the adapter boundary.

## What happens when you run Next?

1. Reload provider settings and resolve image rules per provider/input identity.
   A missing source block inherits the legacy global rule. Geometry failures
   affect only sources using a screen shape rule. If Next has forward history, try
   that first. Previous only navigates history. Skip disabled inputs or unusable
   entries with an explanation; use validated cached bytes when available.
2. For a fresh choice, read committed candidates for the enabled input revisions.
   Independent background discovery commits each page with its next cursor.
   A slow refresh does not hold the playback action lock. Exclude Wallhaven
   candidates with known nonmatching dimensions from the draw. Previously
   decoded dimensions take precedence over the search listing's dimensions.
   Shuffle also uses known dimensions for other HTTPS candidates. Local
   discovery reads bounded image headers before committing each page. Selection
   uses those dimensions for uncached local candidates and checks actual bytes
   only for the selected candidates. Previously cached local images can be
   rematerialized and checked again after eviction.
3. Avoid the current identity and known matching content hashes while alternatives
   remain. **Balance sources** chooses a provider uniformly, then an input
   uniformly, then a candidate. **Shuffle all images** chooses uniformly from
   distinct candidates across inputs, using catalog image IDs and known content
   hashes to group memberships. It then chooses an eligible membership of the
   selected image.
4. Reuse validated cached bytes when available; otherwise resolve and materialize
   the origin. Verify actual format, dimensions and ratio against that membership's
   resolved source rules. Shared image/cache identity never chooses an arbitrary
   search's rule. History replay retains its committed source membership and
   rechecks that source's current rules. Remove failed candidates
   from this selection attempt and give another provider a turn when possible.
   Shuffle all images tries other distinct images before choosing a different
   membership of a failed image.
5. Check backend readiness and complete any Omarchy Fit preparation. Commit
   the pending intent at the dispatch boundary and send the desktop command.
   On success, atomically update history/cursor/last-action and clear the intent.
   This restarts the service's single rotation interval.

If only the current content matches, it can play again. Selection failures never
relax filters or erase accepted history. Status remains readable while one mutation runs;
a competing mutation returns `busy`. Status describes accepted requests, not
observed screen pixels. External changes and diagnostic `debug set` are untracked.
The status snapshot carries current-image metadata from the committed catalog
entry. The native window and bar read it without paging History or probing the
desktop.

Actions have a cooperative 25-second deadline. A stalled filesystem call can
outlast cancellation. Fresh selection budgets 100,000 candidates across inputs.
Catalog rows count before eligibility filtering. If the pool exceeds the
budget, Panora reports `catalog_failed` and asks you to narrow inputs.
Selection makes at most 32 attempts of up to four seconds each. See
[configuration](configuration.md) for image and cache limits.

## Upcoming wallpaper preparation

The playback service warms up to two choices after it has an accepted wallpaper,
including while rotation is paused. It uses eligible forward History first, then
draws from committed discovery candidates using the saved rotation mode. It
validates the bytes and warms the normal thumbnail cache. Both choices share the
playback cache and the existing provider retry budgets, with an eight-second
deadline per preparation pass. The current wallpaper and any first prepared
choice are protected during preparation of the second.

The background worker snapshots the accepted cursor, exclusions, backend and
saved configuration. Navigation cancels an in-progress warmup before entering
the desktop path. Next rechecks the prepared selection and current rules, and an
accepted Next can promote the second slot immediately. Configuration, cursor,
exclusion or observed theme changes invalidate old preparation. The queue is
in-memory and dies with the service. Preparing or reading previews leaves
accepted history, the desktop and the rotation schedule alone.

The native window keeps one unapplied navigation destination. The service
cancels matching preparation up to the dispatch boundary, including Omarchy
Fit preparation. It serializes dispatched writes and advances history only
after acknowledgement. A dispatch failure or lost acknowledgement preserves
the existing uncertain intent and clears the window's queued destination.

## Background discovery

After acquiring the control socket, one coordinator refreshes enabled inputs,
including while rotation is paused. Each input's next refresh is measured from
completion of its previous job. [Configuration](configuration.md#refresh-schedules)
defines `refresh_interval` and restart behavior. Missed deadlines produce one
refresh, not a burst of catch-up jobs. Discovery never applies images.

At most four jobs run concurrently: one local and three remote. Reserving local
capacity leaves the subprocess client's second slot available to playback
resolution. Inputs wait in deadline/key order, so a repeatedly due input cannot
jump ahead of older work. Each job has a three-second timeout and a page budget
of `max(1, 1000 / enabled_input_count)`.

The coordinator checks discovery settings every second, cancels obsolete jobs,
and waits for canceled work to finish before reusing its slot or input key.
Each page rechecks the enabled input revision before its catalog transaction.
The revision is specific to each input's provider settings and effective image
rules. Edits to siblings and schedule changes leave that input's catalog intact.
Invalid configuration cancels discovery until corrected; committed catalog and
playback history remain. Schedule edits recalculate the next deadline from the
last completion; active jobs finish under the new cadence.

Playback reads committed candidates without waiting for refresh. While discovery
runs, an empty matching catalog returns `discovery_pending`; rotation retries
after one second. A successful empty refresh is distinct from pending work.
A complete local scan removes unobserved members from fresh selection. Partial
scans preserve candidates from the last complete scan. Background maintenance
prunes obsolete rows in bounded batches between playback actions. The
[catalog reference](catalog.md#catalog-maintenance) defines what it retains.

The coordinator publishes immutable, revision-checked provider/input health
snapshots. Status does not wait for workers or SQL. Health timestamps and counts
reset on restart; checkpoints and retry gates persist. Counts describe committed
metadata, with actual-byte eligibility unknown. See
[status fields](socket-api.md#refresh-health-fields).

## Exclusions

Playback filters excluded image IDs and content hashes using an in-memory set
refreshed after each committed change. After validating newly materialized
bytes, it checks for identical content before committing desktop intent.
The native Exclude action binds the displayed service, history entry and
image to the mutation. Playback verifies those identities while holding the
action lock, before changing exclusions.
Manual and automatic selection share the check. See [CLI commands](cli.md#exclude-and-restore-images)
and [catalog storage](catalog.md#exclusions).

The built-in Omarchy theme source observes the native theme generation and
prepares its active collection off the rotation path. Discovery enrolls its
verified stills in the same catalog and prepares a bounded in-memory thumbnail
snapshot. The socket gallery reads that snapshot. Playback uses the existing
cache, exclusion and history paths and checks that a choice's original is still
in the active collection. The theme observation can move the scheduler's next
attempt after a native intro, while the scheduler remains the sole rotation timer.

## Provider retry policy

Playback blocks failed work until its retry deadline. Transient discovery,
resolution, download and cache failures retry after 5 seconds, doubling to a
five-minute cap; successful
operations reset their own attempt counter. Failure retries replace the normal
refresh cadence until recovery. Authentication rejection/requirement, invalid
provider settings, unsupported or missing resources and invalid image bytes use
a one-hour retry delay. Image-rule mismatches do not create a retry gate, so
changed rules take effect immediately. Shutdown/request cancellation is not
recorded as an outage.

An unavailable local root uses the transient discovery delay. Its retry is
scoped to that folder and revision, and normal checks resume after a successful
scan. See [local folder errors](providers/local.md#settings).

Typed `Retry-After` seconds or HTTP dates can extend the delay up to the provider
protocol's 24-hour maximum. A 429, or an unavailable response carrying a positive
hint, also creates a provider-wide gate. Authentication failures share the
one-hour gate across that provider's affected request stage. Metadata quotas cover
discovery and resolution; original downloads use a separate gate. Checks run before
provider calls, between discovery pages and before downloading selected bytes.
Already in-flight calls can finish. A success cannot clear a newer unexpired
gate learned by another operation.

Discovery retry deadlines belong to an input and settings revision. A failed
materialization blocks only that image/input/revision, leaving other candidates
and metadata discovery available. Cached bytes are checked before network gates and remain usable;
healthy providers continue independently. Changing input discovery settings
starts a new revision, while provider-wide limits survive edits and restart.
Manual Next/Previous use the same gates. No manual refresh/retry/cancel endpoint
is exposed; obsolete jobs cancel on configuration changes and shutdown joins work.

Related retry gates commit together; failed persistence stops the service so
restart cannot bypass learned limits. [Catalog storage](catalog.md#refresh-health)
defines retry keys and persistence. [Status fields](socket-api.md#refresh-health-fields)
define the bounded retry summaries.

## Storage and recovery

The service owns one database writer and playback cache directory for its lifetime.
Advisory locks reject competing state, cache and socket owners.
[Configuration](configuration.md#default-file-locations) defines paths and budgets,
and the [catalog](catalog.md) owns schema, transactions and file reconciliation.
Provider calls, file copies and desktop operations run outside SQL transactions.

An unconfirmed dispatch preserves its pending intent and pauses rotation.
A successful explicit Next or Previous resolves uncertainty before Resume.
When Omarchy rejects a stale theme before dispatch, playback restores the
previous state, including any earlier uncertain intent. An accepted apply whose
history cannot be committed stops the service with intent intact. Legacy JSON
state is reported without reset or import. The
[storage ADRs](adrs/README.md) record the design decisions.

## Shared playback cache

Local copies and public HTTPS downloads share one staging and validation path.
The desktop receives a cached filename. Content hashes let different providers
reuse shared bytes. [Catalog and cache reconciliation](catalog.md#file-publication-and-reconciliation)
describes reuse and eviction. If protected current and pending files leave
insufficient capacity, the new change fails.

HTTPS destinations must use port 443 and public addresses. DNS answers are checked
and pinned. Redirects are bounded and rechecked, and proxies are disabled.
Copies, downloads and decoding have finite byte, pixel and time bounds. See
[playback cache budgets](configuration.md#runtime-cache-budgets) and the separate
[Fit cache](fit.md#generated-files).

## Settings publication

Settings writers hold the advisory file lock. The service also serializes
settings publication with discovery, which checks the current input revision
before committing a page. The
[configuration API](socket-api.md#configuration-routes) defines revision checks,
publication failure and uncertain-save handling.

A cache-budget edit applies under the playback action lock after publication.
The shared cache mutex serializes it with preview reads, publication and eviction.
Playback's cache owns a separate, bounded in-memory store of encoded thumbnails.
Full-size cache identity and content are verified before serving a thumbnail,
including when the encoded bytes are already in memory.
See [cache status](socket-api.md#cache-status), [image previews](socket-api.md#image-previews)
and [draft-rule snapshots](catalog.md#draft-rule-snapshots).

## Shutdown and shell clients

The service acknowledges a socket shutdown request before signaling the
composer. It cancels and joins playback and discovery, drains requests, then
releases storage and socket ownership. Closing the native window only cancels
that client's requests.

The bar uses the [shell snapshot](socket-api.md#thin-shell-snapshot-json-v1)
and sends explicit CLI playback commands. Snapshot reads preserve the
service, image, and history identity of any retained preview. They do not
start the service, write settings, or recover original files.
