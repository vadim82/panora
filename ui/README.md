# Native client

This Go module builds the Wails window. Its React frontend uses the
[private service socket](../docs/socket-api.md) for playback and settings.
The service owns discovery, rotation, and storage.

## Executable names

`panora-ui` opens the native client on Linux. The macOS bundle uses
`panora-client` internally. `panora` is the CLI and background service.

From the repo root after [development setup](../CONTRIBUTING.md#local-development):

```sh
mise exec -- task ui:dev    # Browser preview
mise exec -- task dev:build  # Native Linux build
```

See the [frontend guide](../docs/frontend.md) for native development and
[isolated review](../docs/frontend.md#isolated-native-review). Build the installed
app using the [Arch package](../docs/arch.md) or [macOS bundle](../docs/macos.md)
instructions. See [Using Panora](../docs/ui.md) for the app workflow.
