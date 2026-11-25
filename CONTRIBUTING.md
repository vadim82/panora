# Contributing to Panora

Use [GitHub issues](https://github.com/vadim82/panora/issues) for ordinary bugs
and feature requests. Include the platform, Panora revision, steps to reproduce,
and what you expected to happen. Follow the [security policy](SECURITY.md) for
sensitive reports.

## Local development

Install [mise](https://mise.jdx.dev/) and Python 3.11 or newer. From the repo root:

```sh
mise install
mise exec -- task ui:install
mise exec -- task setup
```

`panora serve` runs the Go service. The playback CLI commands and the native
Wails window use its private Unix socket. The optional Omarchy bar uses the
CLI. The React UI lives in `ui/`.

### Edit the UI with hot reload

Run `mise exec -- task ui:dev` and open
[the populated preview](http://127.0.0.1:5173/?fixture=populated). Vite reloads
React and CSS changes in the browser. This mode uses fixture data and runs
without the Go service. See [other preview states](docs/frontend.md#browser-preview).

### Run the native window and real service

On Omarchy, run `mise exec -- task dev`. Close the window and rerun the command
to pick up changes. Starting playback or clicking Next can change your wallpaper.
See [native development on Omarchy](docs/frontend.md#native-development-on-omarchy)
for dependencies, profile and logs, or the [macOS development guide](docs/macos-development.md).

While it runs, use the CLI in another terminal from the repo root:

```sh
./build/panora status --socket "$PWD/build/dev/run/control.sock"
```

### Exercise the service and CLI without opening the UI

Start the disposable service with a fake wallpaper setter:

```sh
mise exec -- go -C ui run ./cmd/dev-service
```

Copy the path printed after `Socket:`. In another terminal, use that path:

```sh
mise exec -- go run ./cmd/panora status --socket /path/from/Socket
```

Without an image folder this shows the setup-required state. To test playback
against sample images, pass `--images /absolute/sample-folder` to the fixture
service. Stop it with Ctrl-C. See the [foreground service guide](docs/cli.md#foreground-service-and-controls)
for running `panora serve` directly with the normal wallpaper backend.

### Run checks

Run `mise exec -- task check` before submitting. After Go edits, run
`mise exec -- task fmt` first. `mise exec -- task setup` installs the
prek pre-commit hook. It checks staged Go formatting, frontend formatting and
lint, and diff whitespace without rewriting files. Use
`mise exec -- prek run` to run the staged checks on demand.

If you used the old setup, rerun `mise install` and
`mise exec -- task setup`. Setup clears Panora's old repository-local
`.githooks` setting. If another `core.hooksPath` is configured, setup
preserves it and you can run the staged checks on demand.

## Desktop verification

Automated tests use private state and fake desktop setters. For an actual
desktop trial, isolate config, state, cache, data and the control socket.
Record the environment, source revision, observed result and cleanup.
Preserve the original theme, wallpaper, bindings, workspace and focus
and intervene only in owned processes. A successful IPC request verifies
acceptance. A renderer observation establishes visible output.

For a private X11 trial, choose an explicitly unused display, use private
authentication and disable TCP. Avoid Xvfb `-displayfd` on a host with live
XWayland, where it can select and unlink the live socket. Physical suspend
trials need attended recovery or verified automatic wake.

Explain the problem and resulting behavior in the pull request. Include
tests and a screenshot or recording when relevant. Link evidence with a
clear source revision and scope. Keep temporary capture directories,
fixtures containing private data and machine-specific logs out of Git.
