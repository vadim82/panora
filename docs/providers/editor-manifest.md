# Provider editor manifest

The optional `editor` member of an installed provider's `plugin.json` is an
independently versioned JSON declaration. Protocol 2 providers can omit it and
continue to supply candidates to existing sources. Panora continues to own
image rules, source names, rotation participation, validation of image bytes,
and revision-checked writes. Providers own validation of their own settings.

## Version 1

`editor` contains `version: 1` and a required `groups` array. Groups are
rendered in array order. Each has a unique `id`, an optional visible `label`,
and a required `fields` array. An empty array is valid. An editor can contain
up to 16 groups and 48 fields. Each field has a unique `key`, a `label` and
one of the host `control` values below. Group IDs and field keys start with a
lowercase letter and use up to 48 lowercase letters, digits, underscores or
hyphens. `constructor` and `prototype` are reserved field keys.

| Control | Setting value | Additional members |
| --- | --- | --- |
| `text` | String | Optional `placeholder` |
| `integer` | JSON integer in JavaScript's safe range | Optional `min`, `max` |
| `checkbox` | Boolean | |
| `select` | String | Ordered `options` with unique string `value` and display `label` |
| `multivalue` | Array of up to 32 strings | Optional `placeholder` and `suggest: "suggest"` |
| `bitmask` | String of `0` and `1`, one per option | Ordered `options` with unique string `value` and display `label` |
| `swatches` | Array of up to 32 lowercase `#rrggbb` strings | Ordered preset `options` with `#rrggbb` values and display labels |

Fields can declare `default`, `help`, `required` and
`visible_when: {"key": "...", "equals": ...}`. A condition compares another
field's scalar value exactly. Hiding a field preserves its draft and
does not change the provider's validation rules. Panora displays defaults
for settings absent from the saved object. Values are sent as the provider's
`config` object. The service rejects a save if the provider's `validate`
operation fails or the reviewed revision is stale. The client retains the
draft and permits continuing after settings have been reviewed.

Every editor includes the host's **Image rules**. Choose inherited shared rules
or customize a source's resolution and aspect ratio separately. Changes are
stored with the configured source outside its provider-specific settings.

Malformed and unsupported editor declarations leave the affected provider's
editor unavailable and explain the recovery action. Other installed providers
and built-in editors remain accessible. The declarative fields can display text
and use Panora controls. They cannot load executable UI, HTML or styling.

## Suggestions

For a field that declares `suggest`, also advertise the optional
[`suggest` operation](protocol.md#methods) from `describe`. The generic editor
asks the user before sending prefixes to the provider. Typing sends the field
key and prefix after a debounce while focused. The request excludes the source
name, settings object and other draft values. Up to ten returned strings may be
shown. Superseded and dismissed responses are ignored. Arrow keys move the
highlight, Enter applies the highlighted or manually entered value, and Escape
closes the suggestions. Manual entry remains available on empty results and
provider failures.

## Run two editor fixtures

Python 3 is required for this example. The provider reads regular image files
from a directory you select and leaves the originals in place. The fixture's
tags, captions and presentation preferences are examples of settings.

From the Panora checkout, stage and install both fixtures using the CLI:

```sh
fixtures=$(mktemp -d)
for fixture in fixture-images fixture-album; do
  mkdir "$fixtures/$fixture"
  cp "docs/providers/examples/editor/$fixture.json" "$fixtures/$fixture/plugin.json"
  install -m 755 docs/providers/examples/editor/fixture.py "$fixtures/$fixture/fixture.py"
  panora provider install "$fixtures/$fixture"
done
```

Restart the service and open **Sources → Add source**. Select **Fixture images**
or **Fixture album**. Use an absolute directory that contains ordinary supported
images. The first form reveals its caption after selecting **Featured**, and tag
suggestions include **forest** for `fo` after enabling suggestions. The second
form reveals **Album size** after unchecking **Shuffle order**. To check validation,
save a nonexistent folder or a featured album with an empty caption. To check
rule independence, save one fixture with shared rules and the other with
custom minimums and ratio. Reopen both editors to review the results.

See [installed providers](../configuration.md#installed-providers) for the
installation directory and saved input behavior.
