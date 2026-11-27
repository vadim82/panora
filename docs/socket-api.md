# Private socket API

First-party clients use HTTP/JSON over the service’s private Unix socket. For
shell scripts, use the [CLI](cli.md) and its `--json` output. Settings mutations
use this API.

## Playback and service lifecycle

| Request | Result |
| --- | --- |
| `GET /v1/status` | Current service and playback status |
| `POST /v1/actions/{next,previous,pause,resume}` | Updated playback status |
| `POST /v1/actions/{action}/for-instance?instance=ID` | Apply the action only to the observed service instance |
| `POST /v1/exclusions/current?instance=ID&history=REF&image=ID` | Exclude the accepted image shown by the client if it is still current |
| `POST /v1/shutdown` | Acknowledgement with `stopping: true` and the current `instance_id` when available |

POST requests in this table take an empty body. The guarded route also accepts
`exclude` and `undo-exclude`. It requires exactly one nonempty `instance`
value of at most 128 bytes, taken from a recent status response. Shutdown
accepts the same parameter optionally. A mismatch returns HTTP 409
`instance_changed` before any action runs. Older services reject the guarded
route. If a response is lost, check status before retrying. Shutdown
acknowledges the request before the service stops.

Status and successful playback responses can include `current_image` alongside
`current`. The fields are `provider`, `image` (provider image ID), and optional
`original_path`, `dimensions` from validated bytes, and `attribution`. This
metadata describes the accepted entry in the same snapshot. Optional
`provider_name` is the installed provider's display name, and `source_name`
names the configured input or the accepted image's Omarchy theme. The service
restores accepted image metadata from the catalog at startup and updates it
after an accepted change. Polling
reads the snapshot without SQL, provider calls, original files or geometry
queries. Clients can still use entry IDs and cached previews when details are
unavailable.

Optional `previous_history` and `forward_history`
objects identify the immediate history entries around `current`. Each contains
an `id` (history reference) and `image_id` (catalog identity) from the same
snapshot as the cursor. An optional `content_revision` enables comparison
with a retained thumbnail of the bytes accepted for that entry. Clients can
request cached thumbnails using both IDs.
An optional `history_preview_revision` changes when exclusions are edited.
Playback still checks current eligibility when Previous or Next is invoked.

When the first upcoming choice has a warm thumbnail, status can include
`prepared_next_image_id`, `prepared_next_config_revision`, and
`prepared_next_revision`. The revision scopes reuse of a shown thumbnail.
Warming an additional choice behind it leaves the visible preview revision
unchanged. The visible choice can come from forward History or a fresh draw. Clients read it through
`GET /v1/images/{image_id}/preview` and may compare the revision with their live
configuration. During navigation, `pending_image_id` identifies the choice
whose intent has been saved before desktop dispatch. `current` and the cursor
continue to describe the last accepted wallpaper until the desktop acknowledges
the request. An unsuccessful or interrupted dispatch follows the usual
unconfirmed-state recovery. Optional `current_preview_content`,
`prepared_next_preview_content`, and `pending_preview_content` carry comparable
tokens for accepted, prepared, and pending images. `omarchy_theme_revision`
identifies the observed theme generation. Use content tokens only within the
same service instance and eligibility scope.

The current-image exclusion route requires exactly one `instance`, `history`
and positive integer `image` value from the status snapshot. The service checks
all three under the playback lock before applying the mark. A replaced service
returns `409 instance_changed`. A different accepted entry returns
`409 current_changed`. Refresh status before choosing whether to exclude again.
The older `/v1/actions/exclude` route still excludes the image current at the
time of execution.

## Client routes

### Window navigation

When status reports `navigation_supported`, the native window can
`POST /v1/navigation` with a JSON object containing `instance` from status,
the accepted history ID in `current` (empty before the first wallpaper), a
unique 32-character hexadecimal `token`, and a signed `offset` of one or two
positions. The service checks the instance and accepted cursor before the
request can select a target. A successful response returns updated playback
status after desktop acknowledgement.

`DELETE /v1/navigation` takes an object containing `instance` and `token`.
It supersedes matching work that has yet to dispatch. The canceled POST
reports `409 navigation_superseded`. Once desktop dispatch begins, it owns
the result and finishes before another wallpaper can dispatch. The client
reconciles that result before submitting any remaining destination. Recovery
after an uncertain dispatch requires new explicit input.

Optional status fields `previous_beyond` and `forward_beyond` identify history
entries two positions from the accepted cursor. `prepared_beyond` identifies
the second verified upcoming thumbnail, when available. These references are
scoped to their status snapshot and the reviewed configuration. The neighbor
preview endpoint checks up to two entries in each direction.

### Desktop capabilities

`GET /v1/capabilities` returns a version-1 read-only snapshot for the running
service's active backend choice. It takes no query parameters or body. Inspection
has a 15-second service deadline; each backend probe and geometry query is bounded
to three seconds, command output is bounded, and child processes are joined.

| Field | Meaning |
| --- | --- |
| `requested_backend`, `backend` | Active requested choice and, when unambiguous, resolved adapter |
| `state` | `ready`, `partial` (backend ready with geometry/containment limits), `unavailable`, or `ambiguous` |
| `backends` | Per-adapter `name`, `ready`, optional sanitized failure `code`/`message`; explicit choices probe only that adapter |
| `presentations` | Fit choice, current `available` flag, supported `upscale` flag and optional reason |
| `assignment_scope`, `independent_outputs` | Global assignment scope; independent output assignment is always false |
| `geometry` | `known`, `ambiguous`, `unavailable` or `unsupported`, detected `displays` and optional failure |

Detected displays include name, native width/height, scale and transform. At most
32 active displays with bounded names are returned. Multiple Omarchy displays
are ambiguous as a single target, while native global crop remains available.
Noctalia returns unsupported geometry instead of invented sizes. macOS reports
oriented full-frame backing pixels through the companion helper, with scale
reported separately. Multiple displays remain ambiguous for Match monitor. macOS
scope is connected screens in current Spaces; other Spaces remain unverified.

Missing/ambiguous backends and partial geometry are successful snapshots, not
mutation errors. An unavailable inspector returns `unsupported` (HTTP 501).
Canceled requests use `canceled` (408), deadlines use `timeout` (504), and unexpected
inspection failures use `capabilities_failed` (422), without raw command output.
Readiness is a command check, not a visual compatibility claim. See
[Fit capabilities](fit.md#capability-inspection) for presentation limits.

### Cache status

`GET /v1/cache` reports playback-cache budgets and usage without evicting files.
It takes no body or query parameters. The response contains:

| Field | Meaning |
| --- | --- |
| `version` | API version 1 |
| `saved`, `effective` | Persisted and running `max_images`/`max_bytes` budgets |
| `usage` | Unique managed `images`/`bytes`, their `protected_images`/`protected_bytes`, and `state` |
| `last_apply_error` | Optional sanitized error from the latest explicit budget application; cleared by a successful budget retry |
| `usage_error` | Optional current measurement failure; usage counters are unknown rather than a claim of an empty cache |

Usage state is `within_budget`, `over_budget` (cleanup incomplete),
`protected_over_budget` (protected files alone exceed a limit), or `unknown`.
Counters are usable only when state is not `unknown`. Unknown/unrelated files,
originals and Omarchy's separate rendered cache are not counted.

The request shares playback mutation ownership while capturing current/pending
protection. Busy playback returns HTTP 409. Inspection has a cooperative
five-second deadline; filesystem operations can outlast cancellation.
Panora applies a saved cache budget and removes eligible older copies to meet
a lower limit. Check `last_apply_error` if application is incomplete.
`cache_cleanup_failed` means the saved budget is active and eviction is incomplete.
`cache_limits_pending` means the running budget still differs from the saved
budget. To retry either case, reload settings and explicitly PATCH the saved
limits again. See [runtime cache budgets](configuration.md#runtime-cache-budgets).

### Save the accepted wallpaper

`POST /v1/wallpapers/save` takes one JSON object with `instance`, `history`,
positive integer `image_id`, and the reviewed configuration `revision`. Provide
`folder` to explicitly use a native picker destination, or omit it to use
`save_folder` from that configuration revision. The operation copies matching,
validated full-resolution cache bytes into a complete, non-overwriting file
outside the managed cache. It requires a destination that supports atomic
hard-link publication. It returns `version`, the actual absolute `path`, and
`already_saved`. A repeated save finds the same verified content. No provider
or desktop operation runs, and playback history, pause, and timing are preserved.

The service checks the accepted wallpaper and instance while holding its action
lock. `current_changed`, `instance_changed`, and `config_conflict` require
review before retrying. `save_folder` means the destination needs attention, and
`image_unavailable` means the accepted full-resolution bytes could not be verified.
After a successful first save, the client can remember the chosen folder by
patching `save_folder` through the usual revisioned configuration API.

### Configuration routes

| Request | Result |
| --- | --- |
| `GET /v1/configuration` | Version-1 envelope with saved `toml`, exact-byte `revision`, active `config_path` and playback `cache_path`, `folders`/`searches`, saved `rules`, structured `saved`/`effective` settings and `restart_required` field names |
| `PATCH /v1/configuration` | Update named preferences using a reviewed revision; return the updated configuration |
| `POST /v1/configuration/folders` | Add a local folder with `revision`/`path` and optional `name`, `enabled`, `max_depth`, `refresh_interval` |
| `PATCH /v1/configuration/folders` | Edit a local folder using `revision`, `id` and at least one supported folder setting |
| `DELETE /v1/configuration/folders` | Remove a local folder using a `revision`/`id` object; preserve originals and history |

For example, the POST body is `{"revision":"<reviewed revision>","path":"/absolute/Wallpapers"}`.
The service generates the ID and derives the label from the folder basename.
All mutation bodies are limited to 16 KiB. Unknown or case-mismatched fields,
duplicate keys, null values, wrong types, extra JSON objects and query parameters
are rejected. Responses are bounded to
`18 × 1 MiB + 65536` bytes to allow JSON escaping of the saved TOML and repeated
folder paths/labels.

`config_conflict` and `busy` return HTTP 409; `invalid_request` returns 400;
`invalid_settings`, `config_invalid` and `provider_disabled` return 422; oversized
bodies return 413; write failures return 500.
Reload settings after a conflicting or uncertain save and review before retrying.
`config_write_uncertain` means rename succeeded and directory sync failed. Panora
uses the published runtime preferences, but durability is uncertain. Never
automatically retry a mutation. `folder_not_found` returns HTTP 404.

The revision is the SHA-256 of the exact saved bytes. The writer validates
and stages a file of at most 1 MiB, then rechecks the revision under the advisory
lock before publishing by atomic rename. Pre-publication failures preserve saved and active values.
Malformed or unsupported settings stay on disk and return `config_invalid`
without parser context. Symlinked settings can be read but cannot be written
through the API. See [direct edits](configuration.md#service-owned-writes).

Folder mutations return the updated configuration. PATCH accepts `name`, `path`,
`enabled`, `max_depth` and `refresh_interval`; omitted fields retain their saved
values. IDs cannot be replaced. Empty name/refresh interval restores the derived
label/provider schedule; depth `-1` restores global inheritance (`0` means root
files only). For example:

```json
{"revision":"<reviewed revision>","id":"<folder ID>","enabled":false}
```

Folder summaries include the derived `name`, `path`, effective `enabled`, saved
`input_enabled`, nullable depth override `max_depth`, `effective_max_depth` and
effective `refresh_interval`. Provider-specific validation and discovery stay in
the service. See [folder behavior](configuration.md#local-folder-management).

A preference PATCH supplies `revision` and at least one of the following fields.
Omitted fields retain saved values; explicit `false` and depth `0` are updates.
Provider/input edits and arbitrary TOML are not accepted.

| Fields | Validation and effect |
| --- | --- |
| `min_width`, `min_height` | Positive integers; next playback action |
| `ratio`, `ratio_presets` | `any`, a positive integer custom ratio, `monitor`, `monitor-similar`, or `presets` with a nonempty supported preset array; next playback action |
| `ratio_tolerance_pixels` | Nonnegative safe integer, default 10. Applies to pixel-based ratio checks for all sources on the next playback action |
| `max_depth` | Nonnegative global folder depth; discovery reconciles it |
| `backend`, `fit`, `upscale` | Validate readiness and presentation support before saving; apply explicit fields to the next wallpaper action without reapplying the current image |
| `rotation_mode` | `balance_sources` (default) or `shuffle_all_images`. Applies to fresh Next and scheduled choices without changing the current image, history cursor, or rotation deadline |
| `interval` | Positive Go duration; update the running scheduler after publication and supersede a temporary startup override |
| `cache_max_images`, `cache_max_bytes` | Positive integers; apply the saved budget at runtime after publication |

For example: `{"revision":"<reviewed revision>","min_width":1920,"min_height":1080,"ratio":"16:9"}`.
For multiple shapes, use `"ratio":"presets","ratio_presets":["16:9","21:9"]`.
Clear the array when switching to another mode. See
[preset matching](configuration.md#image-eligibility) for the rounded 21:9
policy and compatibility.
Configuration responses include `rule_resolution` (`resolved`, `unavailable`,
`ambiguous` or `unsupported`), optional `ratio`, `reason`, `backend` and `display`.
An unresolved monitor action returns `rule_unavailable` (422) without a desktop
application. See [Match monitor](configuration.md#match-monitor) for geometry scope.
A no-op returns the original document/revision. Failed validation keeps all prior
values; no part of an invalid multi-field edit is published. `saved` contains the
persisted preferences, while `effective` contains values currently in use,
including CLI overrides. Both include `rotation_mode`. `restart_required` compares these semantically for
durations. Image `rules` are saved eligibility preferences, read by the next
playback action, rather than a claim about the wallpaper currently on screen.
An interval edit that changes the active duration starts a fresh deadline from
save completion; equivalent durations preserve it. Pause stays paused. An explicit
interval matching TOML still replaces a different startup override without
changing the document revision. Unrelated saves preserve overrides. See
[rotation timing](configuration.md#rotation-timing) for restart examples.

Desktop edits return `backend_unavailable`, `backend_ambiguous` or
`unsupported_presentation` (HTTP 422) when the requested active choice cannot be
used. These errors preserve the complete saved document and running values.
Explicit Fit/upscale edits also supersede their corresponding temporary flags;
omitted fields retain overrides. A same-as-saved edit can update active values
without a new revision. See [runtime desktop settings](configuration.md#runtime-desktop-settings).

Folder/search Add and Update accept a complete `image_rules` object with
`min_width`, `min_height` and `ratio`, plus optional `ratio_presets` and
`exact_resolutions`. `inherit_rules:true`
selects the shared legacy defaults and cannot accompany `image_rules`. Omitted
rules on Add create permissive explicit rules; omission on Update preserves the
saved block. Read models include nullable `image_rules` and resolved
`effective_rules`; folder depth remains separate. See
[source rule semantics](configuration.md#image-rules-for-each-source).

### Draft image-rule previews

`POST /v1/rules/preview` accepts `{"revision":"<reviewed revision>","rules":
{"min_width":1920,"min_height":1080,"max_depth":2,"ratio":"presets",
"ratio_presets":["16:9","21:9"]}}`. This read-only operation does not save the
draft, call providers, download/recover originals, change cache recency or send a
desktop request. It accepts at most 16 KiB, rejects unknown/duplicate/null fields
in both objects and takes no query parameters. Revisions use the configuration
API's exact-byte snapshot; stale revisions return `config_conflict` (409).
The optional `rules.ratio_tolerance_pixels` previews a different pixel
tolerance. An omitted value uses the saved tolerance for the reviewed revision.

To inspect one saved source, add both `provider_id` and `input_id` to the
request. The response echoes them and uses `scope:"source_catalog"`; it inspects
only that enabled input's current revision. A source draft can include
`exact_resolutions`, an array of up to 12 `"WxH"` strings. Without the two IDs,
the scope is `"inherited_catalog"` and only sources inheriting shared rules
are inspected. Providing only one
identity field returns `invalid_request`. An unenrolled or disabled identity
returns zero inspected images and `complete:false` with
`reason:"source_not_enabled_or_enrolled"`. Clients must save changed folder paths
or search settings before requesting catalog inspection for those drafts.

The response includes `version`, `revision`, draft `rules`, `rule_resolution`,
`scope`, `validation`, `limit:200`,
`inspected`, `matched`, `rejected`, `unknown`, `complete`, `reasons` and an
optional partial-result `reason`. Counts describe unique images in the current
enabled catalog snapshot. The preview examines original local headers,
validates cached bytes by hash and full decode, and can use dimensions already
decoded by active Omarchy theme discovery. `validation` is
`"local_headers_and_validated_cache"` or, for an inspected Omarchy theme
source, `"discovery_decoded_dimensions"`. Metadata from remote providers and
retained dimensions after eviction count as unknown. Playback fully decodes
selected images. Counts omit exclusions (`exclusions_applied:false`).

`reasons` reports `below_minimum`, `ratio_mismatch`, `outside_scope`,
`unknown_remote`, `unavailable_bytes` and `inspection_budget`. Rejection reasons
can overlap for one image. `inspected = matched + rejected + unknown`; unknown
does not mean rejected. Complete means the captured known scope was examined,
not that remote eligibility is known or discovery is exhaustive.

Work is bounded to 200 images, 512 inputs, 32 MiB of file-size allowance, 64 million
cached pixels and a two-second cooperative deadline. `complete:false` explains
image/input/file/time limits, cache contention, unresolved geometry, unfinished
discovery or a draft depth differing from the catalog's scan. It never scans a
larger directory to fill missing rows. Status and playback ownership remain
available during the scan. A settings writer returns `busy` (409); invalid draft
rules return `invalid_settings` (422), cancellation returns `canceled`, and a
catalog failure returns `preview_failed`. The OS may still block an individual
filesystem operation; regular-file nonblocking opens reject replaced FIFOs.

### History routes

Use `panora history` or `panora history ls` to list the newest accepted wallpaper
requests. `panora history replay ID` replays a retained entry using current image
rules. Both support `--socket` and `--json`; `history ls` also supports `--limit`
(1–10, default 10) and `--before ID` for older entries. Human output marks the
current entry with `*` and shows the stable replay ID and availability.

| Request | Result |
| --- | --- |
| `GET /v1/history?before=&limit=10` | Newest-first retained entries, `current_id` and optional `next_before` |
| `POST /v1/history/ID/replay` | Replay the selected entry; return ordinary playback status |

History pages are limited to ten entries and 2 MiB, including JSON escaping.
Entry `id` is an opaque URL-safe handle of at most 64 characters. Keep it as a
string; neither its encoding nor private SQL row positions are client contracts.
`image_id` is the stable positive catalog image ID within the service database,
used for identity-based image previews. Status
`current` also includes these two references.

Each listed entry includes provider/input identity, provider image ID, original
local path when applicable, accepted time, current marker, dimensions when known
and retained attribution. Validated dimensions survive cache-file eviction; without validated metadata,
`dimensions` is null. Provider-reported dimensions do not establish eligibility.
`availability` is `cached`, `unverified` or `unavailable`. `cached` only reports
file presence and recorded size; bytes are rechecked on replay. `reason` explains
an exclusion, disabled/removed input or changed root, known cached-image rule mismatch,
missing original, invalid configuration, or the need to recheck on replay.
Remote sources with no cache are unverified; listing does not test network access.

Replay takes an empty body and no query parameters. It preserves the entry's
accepted time and history length, moves the current position after backend
acceptance, and resets the normal rotation interval. Paused rotation stays
paused. An unconfirmed application retains pending intent and pauses
rotation, matching Next/Previous.
`history_not_found` (HTTP 404) means a handle was pruned or is absent; reload the
list. `history_excluded` and `history_unavailable` return HTTP 422 without changing
the current position. A busy playback operation returns 409. Clients must not
retry replay automatically after a lost or uncertain response.

### Image previews

`GET /v1/images/IMAGE_ID/preview` returns a still thumbnail for a positive catalog
image ID from history/status. Use `control.ReadPreview` from Go clients. The route
accepts no body, paths, URLs or custom dimensions and uses the same private
socket as playback controls. Add `?history=HISTORY_ID` to request a neighboring
entry for the Now strip. This variant checks the history identity and known
eligibility from saved rules and committed metadata before reading cache bytes.
It returns `history_unavailable` when the entry is stale, ineligible, or lacks
cached metadata. Neither variant applies a wallpaper.

The version-1 JSON response includes `image_id` and `available`. Available results
also include `width`, `height`, `mime_type: "image/png"`, base64-encoded `data`,
and a `content_revision` denoting the verified cache bytes.
The thumbnail fits within 512×512 pixels, keeps aspect ratio/transparency and is
never enlarged. PNG bytes are limited to 2 MiB; the complete JSON response/client
read is bounded to 3 MiB. Clients should label it as an image preview rather than
claiming that it shows verified desktop pixels or the selected Fit rendering.

| Result | Meaning |
| --- | --- |
| `available: false`, `reason: "not_cached"` | The catalog knows the image but has no validated cache association |
| `available: false`, `reason: "content_unavailable"` | Its recorded cache file is missing, evicted, changed, unsupported or unsafe to decode |
| `preview_not_found` (404) | Unknown image ID; reload history |
| `invalid_request` (400) | Invalid/path-like ID, query parameters or body |
| `busy` (409) | Another operation owns the cache; request a preview later |
| `canceled` (408), `timeout` (504) | Request/service cancellation or the three-second cooperative work budget expired |

Preview reads only retained cache content. A missing result stays unavailable
until ordinary playback materializes it. Omit the history ID to inspect
excluded entries or entries from disabled or removed sources whose validated
bytes are still cached. Preview leaves recency and eligibility unchanged.

Each preview request checks regular-file identity, recorded size, and SHA-256
against the retained file (up to 128 MiB and 64 million pixels). On a cache
miss it decodes the verified JPEG, PNG, or static WebP bytes and encodes the
thumbnail under the playback cache lock. Requests for the same validated
image and preview size reuse the encoded PNG without decoding the full-size
image again. The service retains up to 64 thumbnails and 16 MiB of thumbnail
data in memory, discarding the least recently used thumbnails as needed.
Filesystem stalls and decoder work can outlast the cooperative deadline.

### Installed provider editor routes

`GET /v1/configuration` may include `provider_editors` and `inputs`. The
editor descriptors contain `id`, `name` and either an `editor` declaration or
an actionable `editor_error`. Configured inputs include `provider_id`, `id`,
`name`, `input_enabled`, effective `enabled`, an object of provider `values`,
and source image rules. A saved input can outlive its provider installation.

| Request | Edit body |
| --- | --- |
| `POST /v1/configuration/inputs` | `revision`, `provider_id`, source settings |
| `PATCH /v1/configuration/inputs` | `revision`, `provider_id`, `id`, changed source settings |
| `DELETE /v1/configuration/inputs` | `revision`, `provider_id`, `id` |

Source settings are `name`, `enabled`, `values`, and either `image_rules` or
`inherit_rules`. A supplied `values` object replaces provider settings for the
input. Save responses carry the updated configuration. Edits are limited to 16
KiB and reuse the revision conflict and validation behavior of the built-in
source editors. Removal keeps originals and history.

`POST /v1/providers/suggest` accepts `provider_id`, `field`, `prefix` and
`limit` in a JSON object of at most 1 KiB. It uses the declared operation and
returns `{"version":1,"values":[...]}` when available. It reads suggestions only, with a
two-second deadline, and never applies the source draft. See the
[versioned editor contract](providers/editor-manifest.md).

### Omarchy theme routes

`POST /v1/configuration/omarchy-theme` adds the singleton source. Send the
reviewed configuration `revision`. The service supplies its ID and starts with
shared image rules unless `image_rules` is supplied. `PATCH` adds the source ID
and updates `enabled`, `image_rules` or `inherit_rules: true`. `DELETE` takes
only the reviewed revision and source ID. All three use the same revision
and conflict rules as other source edits. Read back `omarchy_theme` in the
configuration envelope.

`GET /v1/omarchy-theme?page=1&limit=4` returns the active theme, display
palette, discovery state, total and eligible counts, and a bounded thumbnail
page. Pages are one-based. A limit of 0 on page 1 reads only the summary and
revision. Limits from 1 to 12 return thumbnail JPEG data as base64. A page
beyond the end is clamped, and an unresolved display ratio has a null eligible
count. The route reads discovery-prepared thumbnails and rules.

### Wallhaven search routes

| Request | Result |
| --- | --- |
| `POST /v1/configuration/searches` | Add a search with `revision` and optional supported settings |
| `PATCH /v1/configuration/searches` | Edit with `revision`, `id` and at least one supported setting |
| `DELETE /v1/configuration/searches` | Remove with a `revision`/`id` object; preserve history and provenance |
| `GET /v1/wallhaven/candidates?id=…&revision=…` | Count discovered and currently eligible candidates for a saved and enabled search |

Each edit returns the updated configuration, including `searches` summaries.
Names are separate from generated stable IDs. Summaries include the display `name`, saved `custom_name`, query, tags, effective `tag_match`, colors,
resolved provider settings, effective `enabled`, saved `input_enabled`, effective
refresh interval and fixed `purity: "sfw"`. Discovery health reports availability
separately; saving does not probe the provider or claim a verified search.

POST/PATCH accept `name`, `query`, `enabled`, `categories`, `tags`, `tag_match`, `colors`,
`sorting`, `order`, `max_pages`, `top_range`, `refresh_interval`, and either
`image_rules` or `inherit_rules: true`. An `image_rules` block can include
`exact_resolutions` as a list of `"WxH"` strings. A blank name displays the
query or **Wallhaven**. POST defaults to all SFW categories and
`tag_match: "any"`. Existing searches without a stored mode return
`tag_match: "all"`. PATCH keeps omitted fields unchanged and accepts empty
arrays to clear tags or colors.
Send a `"000"` category mask to clear category selections. `max_pages` must
be 1–100 when supplied.

For example, add a search with:

```json
{"revision":"<reviewed revision>","name":"Mountain landscapes","query":"mountains","categories":"100","sorting":"relevance"}
```

The same 16 KiB limit, strict JSON decoding, revision conflict handling and
no-retry rule as [folder/preference routes](#configuration-routes) apply.
`search_not_found` returns HTTP 404. Credentials and purity controls are rejected.
See [Wallhaven](providers/wallhaven.md#search-settings) for supported values.

The candidate count is a read-only snapshot of the current source revision's
catalog. It returns `version`, `discovered`, nullable `eligible`,
`rule_resolution`, and the browser `search_url` when available. Eligibility
applies the search's effective image rules and active exclusions using known
dimensions. The URL includes the upstream filters and opens Wallhaven's
results page. An unresolved ratio returns a null
`eligible` field. This read leaves discovery, playback, and the cache alone.
Originals still undergo full validation at selection time.

### Exclusion routes

Socket routes use the version-1 JSON envelope:

| Request | Result |
| --- | --- |
| `POST /v1/actions/exclude` | Exclude current; status response |
| `POST /v1/actions/undo-exclude` | Undo latest active mark; status response |
| `GET /v1/exclusions?before=0&limit=10` | Bounded exclusion list |
| `POST /v1/exclusions/12/restore` | Restore that mark; status response |

Mutations take an empty body. Invalid/repeated query parameters, negative cursors,
invalid limits or nonpositive restore IDs return `invalid_request`; absent marks
return HTTP 404 and busy actions return 409. Status includes `current_excluded`
and `exclusion_undo_available`.

### Refresh health fields

Background discovery has independent `refreshing`/`refresh_warnings` fields and
does not make playback `busy`. `refresh_health_version: 1` and `providers` are
additive fields of the version-1 API.

| Scope | Fields and meaning |
| --- | --- |
| Provider | Enabled state, summary state, total/omitted input counts and input details; at most 512 input details across providers |
| Input | `id`, discovery `revision`, `state`, effective `interval`, nullable `last_attempt`, `last_success`, `next_refresh`, `counts`, optional sanitized `failure` |
| Counts | `committed_metadata_in_attempt`: pages and entries from the latest attempt, including partial progress; `null` while running. `known_eligible` remains `null` for this metadata-only scope |
| Retry | `scope`, `attempts`, `next_retry`, sanitized failure stage/code/message; materialization also has `affected_images` and earliest active deadline. At most two scopes per provider/input |

Input states are `disabled`, `pending`, `refreshing`, `ready`, `empty` or `failed`.
Provider summaries prioritize failed → refreshing → pending → ready → empty →
disabled and include omitted inputs. Ready means metadata committed; empty means
zero candidates committed. Neither proves a matching decoded image. Failed
refreshes retain last success; health timestamps/counts reset on restart while
retry gates persist.

Retry stages distinguish discovery, resolve, download and decode. Provider scopes
separate metadata/download quota gates; input scopes separate discovery and
per-image failures. Discovery counts never include materialization failures.
Manual navigation honors the same gates as automatic work; validated cached
images remain usable. There is no manual retry/cancel endpoint. See the
[retry policy](architecture.md#provider-retry-policy) for delays and reset rules.

A cold catalog can return `discovery_pending`: retry after discovery progresses.
Automatic rotation retries that state after one second. A completed empty refresh
instead returns the normal no-matching-image error. See
[background discovery](architecture.md#background-discovery) for scheduling.

## Thin shell snapshot (JSON v1)

`panora shell snapshot [--socket PATH] [--preview | --cover-flow]` is a read-only adapter for
small shell clients. It never starts the service, applies an image, recovers an
original or changes settings. A missing/unreachable service produces a successful
JSON read with `connected:false` and `code:service_unavailable`; malformed status
uses `code:invalid_status`. Consumers must check `version` and `connected`.

Version 1 exposes `instance_id`, `paused`, `busy`, `uncertain`, `setup_required`,
`can_previous`, `warning`, optional `next_at`, and optional `image`. Image contains
string `id` and `history_id`, a bounded plain-text `title`/`origin`, optional
`source_name` and `width`/`height`, and `preview_state`. Status-only polling uses
generic image labels and `preview_state:not_requested`; retain details only for the matching service,
image and history identity. A new service instance invalidates old preview data.

`--preview` uses current-image metadata from status, reads the cached preview,
and checks the latest service and image identity within a six-second deadline.
Optional `image.data_uri` is a
PNG data URI only after validating the response version, image ID, MIME, decoded
size (at most 2 MiB) and dimensions (at most 512×512). Missing/invalid previews use
`unavailable`/`invalid`; the client can keep controls usable without a thumbnail.
An identity change during those reads discards the old details. No paths, raw
provider diagnostics, settings, secrets or original file bytes are projected.

Clients can use the optional opaque `flow_revision` together with the instance,
image and history identity to reconcile neighbors. Its presence also advertises
`--cover-flow` support. That option includes the current-image fields from
`--preview` and an optional `flow` object with `previous` and `next` previews.
Each available neighbor has a string `id`, a validated PNG `data_uri` and, for a
history entry, its `history_id`. Next prefers the service's prepared upcoming
image and otherwise uses the immediate forward history entry. Missing, invalid and
unconfirmed neighbors are omitted. A changed flow revision during the read
omits the whole `flow` object. The two neighbor reads each have a one-second
deadline within the shared six-second deadline. A Cover Flow response is bounded
to 9 MiB.

Poll small snapshots while the popup is closed. Only request Cover Flow while
the popup is open. Revalidate periodically while open to reflect cache eviction
or changed eligibility.
Send explicit `next`, `previous`, `pause` and `resume` commands with
`--instance ID` from the snapshot. Check status after a lost response and let
the user choose whether to try again.

The [Omarchy integration guide](../integrations/omarchy/README.md#optional-hyprland-shortcuts)
shows how to use an absolute CLI path for native Next and Previous shortcuts.
Use the same private socket as the service when you override the default path.


## Alpha Coders editor

`POST /v1/providers/alpha-coders` takes one JSON object with an `action`.
Use the existing [provider input routes](#installed-provider-editor-routes)
to save an input. The private socket is the only editor entry point.

| Action | Additional fields | Result |
| --- | --- | --- |
| `status` | None | Whether a local credential is present |
| `connect` | `key` | Validates and replaces the credential, then returns connection state |
| `remove` | None | Removes the credential and returns connection state |
| `tags` | `prefix` | Up to ten approved tags with IDs and catalog totals before filters |
| `check` | `revision`, `settings`, optional `image_rules` | Distinct candidate count, per-query page and candidate counts, partial status, and sample locations |
| `thumbnail` | `thumbnail` | A bounded public CDN thumbnail as an image data URL |
| `catalog` | `revision`, `input_id` | Saved discovered and eligible counts read from the catalog |

A check reads the supplied draft and inspects up to two metadata pages per
selected tag or keyword. Omit `image_rules` to inherit the saved shared rules.
Checks fetch no originals and make no catalog or playback changes. Connection
actions return only the connection state. Credentials are excluded from
configuration, status, discovery references, and candidate URLs.
