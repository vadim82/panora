# Provider protocol

Provider programs accept [JSON-RPC 2.0](https://www.jsonrpc.org/specification)
requests on stdin and return responses on stdout. Panora's method and metadata
schema is **protocol 2**. This reference defines that contract;
[Providers](README.md) explains how it connects to playback.

The service's CLI/UI control API keeps its separate HTTP routes over a private
Unix socket. Providers do not run HTTP servers.

## Wire framing and limits

The host launches one process per call with an argument array, writes one UTF-8
JSON line to stdin, and closes stdin. The response must end with a newline and
match the request ID. The process must exit 0, including when it reports an
RPC/provider error.
Extra output, broken framing, or an abnormal exit fails the call. stdout is for
protocol messages; stderr is withheld from surfaced diagnostics.

Limits:

- Request: 64 KiB including its newline; manifest: 64 KiB; response: 1 MiB
  including its newline. Candidate IDs, cursors, and error messages are each
  limited to 4 KiB.
- Page size: 1–100 candidates. The playback coordinator imposes additional
  [job and selection budgets](../architecture.md#background-discovery).
- Individual operations: at most 30 seconds, or the remaining parent deadline.
  At most two subprocess operations run concurrently within one host process.
  The playback service serializes state-changing operations.
- stderr: retain at most 64 KiB internally, discard excess, and withhold it from
  surfaced errors. Raw protocol requests and responses are not logged.
- Linux/macOS cancellation, timeout, and response/stdout overflow kill the process group
  immediately. The host waits for the direct child; a 250 ms pipe-cleanup limit
  prevents inherited child pipes from hanging the caller. Process-group cleanup
  also runs after normal exit. There is no graceful shutdown protocol for these
  short-lived source jobs.

Only `HOME`, `PATH`, `LANG`, `LC_ALL`, `TMPDIR`, `SYSTEMROOT`, and the Go
race-runtime option `GORACE` are inherited. Graphical-session variables and
arbitrary account/token environment variables are excluded. A registered
executable still runs with the user's privileges: this is crash containment, not
an OS security sandbox.

## JSON-RPC implementation

The [RPC codec](../../internal/source/rpc.go) handles envelopes with Go's
`encoding/json` within the one-process-per-call lifecycle.

The host sends single calls with nonempty string IDs. The Go provider dispatcher
also accepts numeric and null IDs, echoing them without changing numeric
precision. Notifications omit `id`: they execute but produce no stdout, even on
method or provider failure. A null ID is a call, not a notification.

The dispatcher accepts batches of 1–100 entries and processes them sequentially.
Responses omit notifications; an all-notification batch produces no stdout.
Invalid entries receive individual errors. An empty or oversized batch receives
one Invalid Request error with a null ID. The whole batch shares the 64 KiB
input, 1 MiB output, and parent time budget. No partial output is emitted when
the response budget is exceeded. The host currently sends neither batches nor
notifications, so external providers only need single-call support for playback.

## Versions

`jsonrpc: "2.0"` identifies the standard message format. `params.protocol: 2`
identifies Panora's provider methods and metadata schema. Every method requires
that parameter. `describe` reports the same application version, and the manifest
must specify `protocol: 2`. Incompatible manifests and messages are rejected;
there is no version-1 compatibility layer. User configuration and playback state
have independent versions.

## Methods

All methods use named, case-sensitive parameters. Each requires `protocol: 2`;
unknown parameters are rejected. Provider-specific `config` and `ref` remain JSON values.

| Method | Other params | Result |
| --- | --- | --- |
| `describe` | None | Application protocol, ID/name, media/content kinds, operations, setting fields, optional supported filter hints |
| `validate` | Non-null `config` | `{}` on success |
| `discover` | Non-null `config`, explicit nullable `cursor`, `limit` from 1–100, optional `filter` | `candidates` array and explicit nullable `next_cursor` |
| `resolve` | Non-null `config`, candidate `id`, non-null opaque `ref` | `content` and optional dimensions/attribution |
| `suggest` (optional) | Declared `field` key, nonblank `prefix` (up to 128 UTF-8 bytes), `limit` from 1–10 | `values` array of at most `limit` nonblank strings, up to 128 UTF-8 bytes each |

`describe` works offline. Its `operations` must include `describe`, `validate`,
`discover` and `resolve`. Its
media kinds must include `image`. Local playback additionally requires ID `local`
and content kind `local`. Unknown optional result metadata is ignored. A nonempty
`requires` array in a description, candidate or resolved result is rejected; the
host supports no additional mandatory capabilities.

`suggest` is available for opted-in fields in an
[editor manifest](editor-manifest.md). Send only the field, prefix and limit.
Empty suggestions return `{"values":[]}`. The editor's suggestion request has a
two-second service deadline.

A description request and response:

```json
{
  "jsonrpc": "2.0",
  "id": "req-1",
  "method": "describe",
  "params": {"protocol": 2}
}
```

```json
{
  "jsonrpc": "2.0",
  "id": "req-1",
  "result": {
    "protocol": 2,
    "id": "local",
    "name": "Local folder",
    "media_kinds": ["image"],
    "content_kinds": ["local"],
    "operations": ["describe", "validate", "discover", "resolve"],
    "fields": [
      {"key": "root", "type": "text", "label": "Folder"},
      {"key": "max_depth", "type": "integer", "label": "Subfolder depth"}
    ]
  }
}
```

## Local image example

```json
{
  "jsonrpc": "2.0",
  "id": "req-2",
  "method": "discover",
  "params": {
    "protocol": 2,
    "config": {"root": "/home/alex/Pictures", "max_depth": 2},
    "cursor": null,
    "limit": 100
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": "req-2",
  "result": {
    "candidates": [{
      "id": "mountains.jpg",
      "media_kind": "image",
      "ref": {"path": "/home/alex/Pictures/mountains.jpg"},
      "content": {
        "media_kind": "image",
        "kind": "local",
        "path": "/home/alex/Pictures/mountains.jpg"
      }
    }],
    "next_cursor": null
  }
}
```

Local IDs are relative paths with `/` separators, stable within the configured
folder until renamed. The service keeps the configured folder ID separately for
history. The [SQLite catalog](../catalog.md#schema-and-identity)
combines the folder scope and candidate ID; Wallhaven IDs can be shared across searches.

Local candidates include `content` so the service can inspect each file without
a subprocess per image. It resolves only the selected candidate. `resolve` sends
the same `config`, candidate `id`, and `ref`, plus `protocol: 2`; its result contains
`content` in the same shape. The returned path must still match the inspected
candidate. Path and header checks run again before application; these checks do
not eliminate file-replacement races.

## Remote metadata

A remote `discover` result can locate a public HTTPS original without downloading
it:

```json
{
  "candidates": [{
    "id": "mountain-123",
    "media_kind": "image",
    "ref": {"key": "mountain-123"},
    "content": {
      "media_kind": "image",
      "kind": "https",
      "url": "https://images.example.org/mountain-123.png",
      "mime_type": "image/png"
    },
    "dimensions": {"width": 5120, "height": 1440},
    "attribution": {
      "title": "Mountain lake",
      "page_url": "https://example.org/images/mountain-123",
      "author": "Alex",
      "author_url": "https://example.org/alex",
      "source_url": "https://artist.example.org/mountain-lake",
      "license_url": "https://example.org/license"
    }
  }],
  "next_cursor": null
}
```

Only `image` media and `local`/`https` content are supported. A local location has
an absolute `path` and no `url`; HTTPS content has a `url` and no `path`. Optional
`mime_type` must be `image/jpeg` or `image/png`; it is a claim, not byte validation.
Candidate content may be omitted until `resolve`; resolved content is required.
Optional `sha256` is a 64-character lowercase hex digest of the original.
A digest supplied during discovery must match the resolved value. The shared
cache checks the digest before publishing downloaded or copied bytes.

Dimensions are optional on candidates and resolved results. Omit them or use
null when unknown. If present, both width and height must be positive integers.
Attribution fields are optional. `source_url` preserves an original source link
separately from the author and image-page links. `license_name` is a plain-text
label of up to 128 bytes that can accompany `license_url`. Titles, authors and each URL/path
are limited to 4 KiB. URLs must use HTTPS with a host and no user information, query, or
fragment. Signed/private URLs are outside this contract. These are syntax checks;
the shared cache also checks destinations, redirects, bytes and actual images.

An optional discovery hint can narrow a provider query:

```json
{
  "min_width": 5120,
  "min_height": 1440,
  "ratio": {"width": 32, "height": 9}
}
```

Pass this object as `params.filter`. A description's optional `filter_hints`
array advertises supported keys. Minimums (`min_width`, `min_height`) are
nonnegative. Each `ratio` or `ratios` entry has two positive integers.
`exact_resolutions` carries a list of `"WxH"` strings. Providers may ignore
hints. The service remains responsible for eligibility from actual image bytes.
The local provider ignores hints and continues to list all supported files.

Public messages, cursors and process arguments must not contain credentials,
access tokens or signed URLs. The contract has no credential storage.

## Local listing, filtering, and cursors

A folder's `path` maps to `config.root` and its effective depth maps to
`config.max_depth`. Root files are at depth 0. The provider lists regular
JPEG, PNG and WebP filenames. Playback checks headers, rejects animated images
and enforces rules and exclusions. Roots may be symlinks and links beneath the
root are skipped.

Each page walks the folder again, skipping the cursor's traversal offset and
reading one extra path to detect another page. The cursor fingerprints the root,
depth and host-supplied `config.revision`. Minimum dimensions and ratio do not
change that fingerprint. Folder contents are not snapshotted: changes between
pages can produce stale cursors or incomplete coverage. Listings never prove
deletion. The host restarts once from null on `invalid_cursor` within the same
job budget; another failure stops the attempt.

Background discovery [persists pages and checkpoints](../catalog.md#discovery-transactions)
in SQLite. Interrupted jobs resume committed cursors; completed scans restart at
the next refresh. Playback selects from committed candidates rather than scanning
on each Next. Duplicate IDs/cursors, unsupported content and resolved-path
substitution fail the relevant operation. The service rechecks containment and
actual image eligibility before application; file-replacement races remain
possible. [Local files](local.md) describes configuration and traversal.

## Errors and retry hints

Standard JSON-RPC errors use numeric codes: Parse error `-32700`, Invalid Request
`-32600`, Method not found `-32601`, Invalid params `-32602`, and Internal error
`-32603`. Invalid JSON/request envelopes use a null ID. A valid call with bad
method parameters keeps its ID. Unexpected Go errors become a generic Internal
error; raw errors, protocol payloads, and subprocess stderr are not surfaced.

Provider failures use application code `1000`, with their typed kind in `data`:

```json
{
  "jsonrpc": "2.0",
  "id": "req-4",
  "error": {
    "code": 1000,
    "message": "Try later.",
    "data": {"kind": "rate_limited", "retry_after_seconds": 120}
  }
}
```

Recognized kinds are `invalid_config`, `auth_required`, `auth_rejected`,
`rate_limited`, `unavailable`, `invalid_cursor`, `not_found`, `unsupported`, and
`internal`. Messages must be nonblank, plain text, at most 4 KiB, and free of
secrets/stack traces. The host preserves the kind as `source.Error`.

Only `rate_limited` and `unavailable` may include `retry_after_seconds`, an integer
from 0 to 86400. Missing means no recommendation; zero means retry is permitted
now. The host preserves the optional value but does not sleep or retry inside an
RPC call. Playback applies [bounded backoff](../architecture.md#provider-retry-policy)
and persists learned deadlines independently of normal refresh cadence. Generic
RPC errors preserve their numeric code; remote message/data are withheld from
surfaced errors.

Discovery/resolve failures do not add history or call the desktop. Desktop
application has a separate [recovery policy](../architecture.md#storage-and-recovery).

## Python fixture

The [local Python fixture](../../testdata/plugins/python-local/source.py) implements
the four single-call methods without importing Panora code. It always reports
`fixture.png` beneath the requested root, even if absent. Automated tests exercise
it through service playback. With Python 3 installed, run this read-only example
from the repository root:

```sh
printf '%s\n' '{"jsonrpc":"2.0","id":"demo","method":"discover","params":{"protocol":2,"config":{"root":"/tmp","max_depth":0},"cursor":null,"limit":1}}' \
  | python3 testdata/plugins/python-local/source.py
```

The [remote Python fixture](../../testdata/plugins/python-remote/source.py) supplies
HTTPS locations, attribution, unknown/known dimensions, filter hints, and a typed
rate-limit failure without network requests. Tests also use an independent
Python caller against the Go provider to check both directions of communication.
The fixtures implement the host's single-call path, not a general RPC server.

## Plugin manifest

The bundled provider needs no manifest. `serve --local-plugin /absolute/dir`
substitutes another local implementation. That directory contains `plugin.json`:

```json
{
  "id": "local",
  "name": "Python local contract fixture",
  "version": "0.1.0",
  "protocol": 2,
  "command": ["/usr/bin/env", "python3", "./source.py"]
}
```

The five members shown above are required. An optional `editor` member follows
the [editor manifest](editor-manifest.md). Unknown members are rejected.
This development override requires ID `local`. A relative executable resolves
against the manifest directory, which is also the working directory for relative
arguments. Panora launches the argument array directly. The Python examples
require Python 3.

For `panora provider install DIRECTORY`, provide a regular executable alongside
`plugin.json`. The first `command` element is its filename. Panora runs the program's
bounded `describe` during installation, copies the executable and manifest, and
loads the provider on the next service startup. External IDs start with a
lowercase letter and use up to 48 lowercase letters, digits or hyphens.
`local`, `wallhaven` and `omarchy` are reserved built-in IDs.
