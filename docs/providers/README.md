# Image providers

A provider finds candidates and locates originals. Panora checks image bytes
against each source's effective rules, then manages selection, cache, history
and rotation.

| Provider | Inputs |
| --- | --- |
| [Local files](local.md) | Folders containing JPEG, PNG, or static WebP files |
| [Wallhaven](wallhaven.md) | Named SFW searches |
| [Omarchy theme](omarchy.md) | The active theme’s native still backgrounds |

## Provider settings

Add, edit, enable, disable and remove inputs in **Sources**. Each input has a
stable ID in [`config.toml`](../configuration.md#user-configuration). Discovery
runs independently of rotation, and removing an input preserves its originals
and history. Navigation skips entries outside enabled inputs. See the
[folder](../configuration.md#local-folder-management) and
[search](../socket-api.md#wallhaven-search-routes) contracts.

See [refresh schedules](../configuration.md#refresh-schedules),
[status output](../cli.md#status-and-scripting), and
[retry policy](../architecture.md#provider-retry-policy). Reported dimensions and
metadata counts do not establish actual image eligibility.

Providers own finding/resolving images, provider-setting validation and field
metadata such as names, labels and types. The service owns saved configuration,
image rules, cache and rotation. The Panora app owns the settings forms and sends
edits through the service API. Installed providers may opt in to the native editor with a versioned
[editor manifest](editor-manifest.md). Wallhaven uses the same host controls
inside its inline search editor. Saving source settings establishes
configuration and validation. Provider availability is reported separately.

Panora supplies saved provider settings in each `config` object. Local receives
the folder path and depth, and Wallhaven receives the search filters. Panora
checks decoded images against the source's minimum size and aspect ratio.
Sources without overrides inherit the [shared image rules](../configuration.md#image-rules-for-each-source).
Providers may use optional hints to narrow discovery.

## How Panora gets images

Discovery saves candidates and page checkpoints in the catalog. Playback selects
from those records, checks local headers, asks the provider to resolve the chosen
original and validates its bytes through the
[shared cache](../architecture.md#shared-playback-cache). For example, a provider
may list both portrait and 32:9 images; common image rules determine which can play.
The [Next workflow](../architecture.md#what-happens-when-you-run-next) explains selection.

## Running a provider

The service runs the bundled local provider as `panora provider local`, one
child process per JSON-RPC call. Installed provider programs use the same
stdin/stdout contract. The service runs Wallhaven in process and handles the
active Omarchy theme through its built-in discovery path.

Both `panora provider local` and `panora provider wallhaven` are available for
protocol diagnostics. The command selects the provider and the JSON request
selects the operation. For a folder diagnostic in the terminal, use `panora scan`.

| Method | Purpose |
| --- | --- |
| `describe` | Report capabilities and setting fields |
| `validate` | Validate provider settings |
| `discover` | Return a page of candidates |
| `resolve` | Locate a selected candidate's original |

The [protocol](protocol.md) defines messages, paging, errors and process limits.
In Go, the [Provider interface](../../internal/source/protocol.go),
[process client](../../internal/source/client.go) and
[dispatcher](../../internal/source/rpc.go) connect implementations in `internal/sources/`.

## Custom provider programs

`serve --local-plugin /absolute/plugin-directory` substitutes a development local
provider using a [manifest](protocol.md#plugin-manifest). Folder settings and
image rules remain in TOML. The [Python fixtures](protocol.md#python-fixture)
exercise this contract independently of Go interfaces. Install a trusted external
provider with `panora provider install DIRECTORY` and restart Panora's service.
See [installed providers](../configuration.md#installed-providers) and the
[editor manifest](editor-manifest.md).
