# Wallhaven

The bundled Wallhaven provider searches the public API for safe-for-work
JPEG and PNG originals. The running service calls it directly for metadata
and image locations. Use `panora provider wallhaven` for standalone protocol
diagnostics. Playback validates and retains selected images in the
[shared cache](../architecture.md#shared-playback-cache).

## Saved searches

Add saved searches to the [configuration](../configuration.md#user-configuration):

```toml
[[providers]]
id = "wallhaven"
enabled = true

[[providers.searches]]
id = "mountains"
query = "mountains"

[[providers.searches]]
id = "space"
query = "space"
```

## Search settings

Saved search tables require an `id`. The optional `query` can be blank, including
when no filters are selected. A blank `name` displays the search terms, or
**Wallhaven** if the terms are blank. `enabled` defaults to true.
The service API generates IDs independently of labels. The settings below apply
to saved searches and direct provider calls; `revision` is host-generated.
`refresh_interval` controls scheduling independently of playback.

The [search API](../socket-api.md#wallhaven-search-routes) adds, edits, enables, disables
and removes searches while the service runs. Saving validates syntax offline,
without contacting Wallhaven: availability remains unverified until discovery
reports its result. Failed discovery keeps the saved choice. The provider must
also be enabled; an input edit never enables a disabled provider implicitly.

| Setting | Default | Meaning |
| --- | --- | --- |
| `query` | Empty | Optional free text and Wallhaven search syntax, at most 2048 bytes |
| `categories` | `"100"` for legacy TOML | General/anime/people bit mask. `"000"` means all three and is the default when adding in the app |
| `tags` | `[]` | Up to 16 distinct tag names, each at most 128 bytes |
| `tag_match` | `"any"` for new saves in the app or via POST, `"all"` when omitted in TOML or provider settings | `"any"` matches at least one selected tag. `"all"` requires every selected tag. |
| `colors` | `[]` | Up to six distinct `"#RRGGBB"` swatches. Match any selected color |
| `sorting` | `"relevance"` | `date_added`, `relevance`, `random`, `views`, `favorites`, or `toplist` |
| `order` | `"desc"` | `asc` or `desc` |
| `max_pages` | `20` | Upstream page cap, from 1 to 100 |
| `top_range` | `"1M"` | Toplist period: `1d`, `3d`, `1w`, `1M`, `3M`, `6M`, or `1y` |
| `revision` | Omitted | Optional host settings revision, at most 128 bytes |

An omitted or empty TOML category mask keeps the legacy General default. To
search all categories in TOML, set `categories = "000"` or `"111"`. An empty
sorting, order or top-range value uses its listed default.
An omitted `max_pages` uses 20; an explicit value must be 1–100. Empty refresh
interval inherits the provider schedule. Labels have a 256-byte limit without
NUL or newlines. Existing id/query-only TOML keeps its defaults and stable IDs.

Panora prefixes structured tags with `+` and places the free-text query after
them. With **All**, tags `mountains` and `night sky` and keywords `city -cars`
send `+mountains +"night sky" city -cars` in one query. With **Any**, Panora
searches `+mountains city -cars` and `+"night sky" city -cars` and combines
the results by stable image ID. Zero or one tag works the same in either mode.
The standalone Wallhaven `id:123` syntax cannot be combined with tags.
See [tag entry and suggestion availability](wallhaven-tags.md).

Safe-for-work selection is fixed to `purity=100`, and results are checked again
before returning them. API keys and other credentials are not supported.

The optional `params.filter` hints send a minimum size as `atleast=WxH` or
exact sizes as `resolutions=WxH,WxH`. A single shape or a list maps to `ratios`.
Playback uses the saved search's effective rules, including inherited rules.
**Same shape as my screen** on a single supported 5120 × 1440 display sends
`ratios=32x9`. Discovery checks display geometry on each refresh and omits the
hint when the display geometry is unavailable. A changed ratio restarts
pagination at page one. Playback screens known dimensions before selecting a
candidate and checks the decoded original again against the size and shape.
**Similar to my screen** omits the upstream ratio hint so nearby shapes are
retained. Minimum dimensions or exact resolution hints still apply, and the
same bounded paging continues across display geometry changes. Playback
checks the [5% tolerance](../configuration.md#match-monitor) locally against
the current display.

Each selected color maps to the nearest supported Wallhaven swatch by squared
RGB distance, using the published swatch order to break ties. Colors that map
to the same swatch share a query. Panora sends one `colors` value per request
and combines results across swatches. Matching uses Wallhaven’s search results.
The source keeps the original selected hex values so they remain editable.

## Read-only discovery

With Go installed, run this from the repository root:

```sh
printf '%s\n' '{"jsonrpc":"2.0","id":"wallhaven-demo","method":"discover","params":{"protocol":2,"config":{"query":"nature","categories":"100","max_pages":1},"cursor":null,"limit":1,"filter":{"min_width":5120,"min_height":1440,"ratio":{"width":32,"height":9}}}}' \
  | go run ./cmd/panora provider wallhaven
```

This contacts Wallhaven and returns one candidate's metadata. It downloads no
image bytes and needs no running service. `describe` and `validate` work offline;
`discover` and `resolve` make metadata requests. The [shared protocol](protocol.md)
specifies request and response envelopes.

## Images and attribution

Candidates use Wallhaven's stable image ID. The original location comes from the
API's `path` field, rather than a thumbnail. The provider accepts public HTTPS
originals on `w.wallhaven.cc` and returns the Wallhaven page link.

The same Wallhaven image discovered by multiple searches retains one global
image identity and shared cache association. Editing/removing a search preserves
history and provenance; navigation skips disabled/removed inputs. Other enabled
searches can still select that image. Search edits invalidate discovery revisions
and cancel obsolete jobs without creating another rotation timer.

Dimensions are omitted when unknown; reported values remain metadata claims.

`resolve` fetches fresh `/w/ID` metadata for the chosen ID. If present, the uploader
username is retained as `attribution.author`; it does not establish the image's
artist. A supplied uploader URL and the API's original `source` link are retained
as `author_url` and `source_url` when they meet the public HTTPS URL rules.
Links with credentials, queries, or fragments are omitted.

## Paging and failures

Wallhaven returns up to 24 rows per request. A branch pairs each distinct
upstream color with either all selected tags in **All** mode or one selected
tag in **Any** mode. One branch suffices when a search has neither color nor
multiple Any tags. Each branch receives `max_pages` divided by the number of
branches, rounded down with a minimum of one. For example, two Any tags and
three distinct swatches with `max_pages = 20` get up to three pages each.
When the number of branches exceeds the page limit, each branch gets one page.

Multi-tag Any searches fetch at most four branch pages per discovery call,
cycling through all branches at each upstream page number. Other searches
continue to fetch up to six swatch pages per call. Every request uses the
configured sort and direction. For multi-tag Any, Panora merges the current
batch by date, views or favorites when that metric is selected. For relevance,
toplist and random it interleaves branch ranks, because the API returns no
comparable scores. The combined ordering is batch-local. A returned page lists
each image ID once, and all batches, upstream pages and searches share that
image’s catalog identity.

A cursor binds discovery settings and filter hints, tracks exhausted branches,
and carries one Wallhaven random seed across all branch requests. Changing
query inputs, tag match mode, or effective geometry restarts paging. Short
provider calls can refetch their current batch to resume at its offset. Each
call shares a 10-second deadline across requests. A failed branch discards
the current batch, leaving earlier commits available. The service honors retry
limits and resumes from its last committed cursor. Resolution makes one
request with a 10-second timeout. Each response is limited to 1 MiB and
redirects are rejected. A completed scan does not imply deletion.

| Outcome | Provider result |
| --- | --- |
| No matches | Successful empty candidate page |
| HTTP 401 / 403 | `auth_required` / `auth_rejected` |
| HTTP 404 | `not_found` |
| HTTP 429 | `rate_limited`, with a valid Retry-After hint when supplied |
| HTTP 5xx or transport failure | `unavailable`; server retry hints are retained |
| Invalid cursor | `invalid_cursor` |

Malformed, oversized or incompatible metadata fails without exposing response
bodies. Unsupported formats and non-SFW results are skipped. Retry hints are
bounded to one day; calls do not sleep or retry internally.

[Offline fixtures](../../internal/sources/wallhaven/) cover paging, filters and
typed failures.
