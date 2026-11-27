# Native frontend development

The Wails client lives in [`ui/`](../ui/README.md). React and TypeScript render
the window from embedded assets. The Go service owns playback and saved
settings. See [executable names](../ui/README.md#executable-names).

## Tools and commands

Install the tools from `mise.toml` and use the pnpm version in
[`package.json`](../ui/package.json). Keep the lockfile in sync with dependency
edits. Vitest and React Testing Library cover user workflows. ESLint checks
TypeScript and hooks, and Prettier formats source.

Start with the [development setup](../CONTRIBUTING.md#local-development).

| Command | Result |
| --- | --- |
| `mise exec -- task dev` | Build/open the native Omarchy window with a paused checkout-local service |
| `mise exec -- task dev:build` | Build the CLI/service and native Linux window without launching them |
| `mise exec -- task ui:dev` | Browser preview with Vite hot updates |
| `mise exec -- task ui:fmt` | Format React source and frontend configuration |
| `mise exec -- task ui:check` | Strict types, lint, format check, component tests and production build |
| `mise exec -- task ui:build` | Type-check and build the static assets embedded by Wails |
| `mise exec -- task check` | Frontend checks plus the required Go/tooling checks for both modules |

For component test watch mode, run from `ui`:

```sh
mise exec -- corepack pnpm test:watch
```

## Native development on Omarchy

After installing the repository's build tools/frontend dependencies, use
`mise exec -- task dev` from the repository root. The native build requires GTK3,
WebKitGTK 4.1 development libraries and pkg-config. The task explains missing
dependencies; it does not install packages or register a launcher/startup service.

The task builds `build/panora` and `build/panora-ui`, then starts a real service
with automatic rotation initially paused. Its private profile is `build/dev/`:

| Path | Contents |
| --- | --- |
| `config/panora/config.toml` | Settings, created by the service on first use and preserved afterward |
| `state/` and `cache/` | Retained development catalog/history and disposable cache |
| `run/control.sock` | Private service connection, supplied to both executables automatically |
| `service.log` and `ui.log` | Appended development process output |

Close the window or press Ctrl-C to stop processes started by the task. The
wrapper preserves the development profile and leaves the last wallpaper in
place. If a service is already reachable at the profile's socket, the task
reuses it as-is and leaves it running. Only one wrapper session per checkout
can run at a time.

## Browser preview

Start `task ui:dev`, then open
[the populated preview](http://127.0.0.1:5173/?fixture=populated). The development
banner identifies simulated data. Select a state with the `fixture` query:

| Value | Preview |
| --- | --- |
| `populated` | Wallpapers, sources, History and settings |
| `first-run` | Local folder first, with optional Wallhaven enrollment in Sources |
| `empty` | Configured sources with no accepted wallpaper or history |
| `source-error` | A source failure and automatic retry details |
| `sources` | Several included/turned-off folders/searches, long paths and an unavailable input |
| `disconnected` | Retained last-known state and disabled stale actions |
| `protected-cache` | Protected cache usage above its budget |

Add `&appearance=mac` to a fixture URL to review macOS control styles in the
browser. Set the browser’s light or dark appearance to review each palette.
The native app selects these styles from the Wails runtime platform.

The mock bridge and sample images in `frontend/src/dev` are development-only.
`tools/verify_frontend.mjs` checks that the production build excludes them
and uses external assets under the installed Content Security Policy. Vite
provides hot updates in the browser. Rebuild to update the native client.

Rule previews use sample metadata and report unknown eligibility. Review real
matches with the [isolated service](#isolated-native-review).

## Code structure

All frontend paths below are relative to `ui/frontend/src`.

| File or directory | Ownership |
| --- | --- |
| `main.tsx`, `App.tsx` | React root, providers, navigation, page visibility, global feedback and error boundary |
| `client.tsx` | Typed bridge adapter and one request/state coordinator per window |
| `types.ts` | Wails bridge arguments and Go JSON results |
| `now/NowScreen.tsx` | Accepted image, bounded preview, playback/origin controls and contextual recovery |
| `sources/` | Unified source list and selection, folder/search editors and disclosed source health |
| `history/HistoryScreen.tsx` | Paginated History/Excluded, replay, Restore and original-location actions |
| `settings/` | Image rules, presentation, timing, storage and installed-service controls |
| `onboarding/Onboarding.tsx` | Source choice and explicit first wallpaper/rotation, shared by Now and Sources |
| `components/ui/` | Shared native controls, fields, feedback, form actions and Radix confirmation dialogs |
| `test/`, `*.test.tsx` | Shared fixtures and component/coordinator workflow checks |

`createClient()` owns the latest snapshot and reviewed configuration.
`ClientProvider` subscribes through `useSyncExternalStore`. Explicit requests
wait for relevant background reads and recheck their preconditions. Next and
Previous in a connected window join the status phase of an active poll.
The configuration phase continues to refresh saved settings and preview
invalidation. Reconnect still waits for configuration before restoring the
connection, and settings operations join the complete refresh. Polling is
read-only. Reconcile lost mutations with reads. Revision conflicts and
unconfirmed settings saves require explicit reload.

Feature screens own drafts, local validation and request handlers. The service
validates saved settings. Reload refreshes untouched fields and retains edits.
If a folder or search was removed, the editor offers to save the draft as a
new input. `ImageSettingsProvider` carries image settings drafts across pages.
Scope storage readings to the connection and service instance, and scope
capabilities to the active backend too. Read capabilities only on Settings or
Sources, and storage diagnostics only when Storage usage is open on Settings.
Poll installed-service state while Settings is visible or lifecycle work is in
progress. These passive reads do not occupy the foreground command gate. Async
completion should preserve focus when the user moved it elsewhere.

In Source status, Wallhaven catalog counts are read when the disclosure opens,
after a reported successful discovery check, or on explicit status refresh.
The draft image-rule estimate uses a debounce scoped to the search, reviewed
configuration revision, current rules and service instance. Ordinary status
polls leave that estimate in place.

## Shared UI components

`components/ui/index.tsx` exports small wrappers around ordinary native elements:

| Component | Usage |
| --- | --- |
| `Button` | Native button props and ref; optional `variant="primary"` or `"destructive"`; `pending` disables the button and exposes `aria-busy`. The default type is `button`; save actions specify `submit`. |
| `Field` | Native label props with required `htmlFor`; children contain the short label text and matching control. A `.ui-field` wrapper keeps optional `help` and `error` outside the label, connected through `aria-describedby`; an error also sets `aria-invalid`. Put helper text in those props so it does not become part of the control name. |
| `TextInput`, `NumberInput`, `Select` | Native input/select props and refs, sharing CSS metrics. Values and validation stay in the feature. Checkboxes and the folder picker remain native. |
| `FormActions` | Ordinary div props with consistent button spacing and wrapping. |
| `Notice` | Paragraph props with `tone="info"`, `"success"` or `"error"`; uses status or alert semantics. |

For example, a feature keeps its controlled input and save handler:

```tsx
<Field htmlFor="source-name" help="Name shown in Sources.">
  Name
  <TextInput id="source-name" value={name} onChange={(event) => setName(event.target.value)} />
</Field>
<FormActions>
  <Button type="submit" variant="primary" disabled={!changed}>Save changes</Button>
  <Button onClick={cancel}>Cancel</Button>
</FormActions>
```

`components/ui/ConfirmDialog.tsx` uses Radix for modal semantics and focus
management. Pass the owning page's visibility as `active` so navigation
dismisses the confirmation. Initial focus goes to Cancel. The dialog invokes
the confirm callback at most once per opening and preserves focus already
moved elsewhere. The feature still owns request and revision checks.

`forms.css` supplies shared control sizing, the modal backdrop and scroll
lock. Use the static backdrop in the installed app because the default
Radix Overlay injects styles blocked by its Content Security Policy.

## Native build and review

`frontend/dist` is generated and ignored. Build it before native-tag Go checks or
manual native builds: Wails embeds `all:frontend/dist`, not TypeScript source or a
development URL. Use the [isolated review procedure](#isolated-native-review)
for a private service with a fake wallpaper setter.

The macOS packager builds from a private snapshot using the locked frontend
dependencies. See [macOS packaging](macos-development.md#build-and-install).

## Isolated native review

For a native review with a real service and a fake wallpaper setter, run from
the repository root:

```sh
mise exec -- go -C ui run ./cmd/dev-service --images /absolute/sample-images --interval 30s
```

The harness prints its private socket path. In another terminal, build the assets
and native client, then pass that path:

```sh
mise exec -- task ui:build
mkdir -p build
mise exec -- go -C ui build -tags 'wails,production,webkit2_41' -o ../build/panora-ui .
./build/panora-ui --socket /absolute/private/control.sock
```

The harness starts paused with a fresh private profile and a one-pixel minimum.
Omit `--images` for first-run enrollment. Use `--display WIDTHxHEIGHT` to
simulate display geometry for framing and Match monitor. An enrolled
Wallhaven search uses the public SFW API. Close and reopen the window to
check service continuity. Ctrl-C removes the private profile and lets you
check disconnection from the review client.

On macOS, use `wails,production` for the manual build. After `ui:build`, run the
native-tag bridge checks from the repository root:

```sh
mise exec -- go -C ui test -race -tags 'wails,production' ./...
mise exec -- go -C ui vet -tags 'wails,production' ./...
```

On Linux, use the same `wails,production,webkit2_41` tags for the native
bridge checks. See [verification](verification.md#omarchy-and-linux) for
ordinary Wayland results.

## Review the native window

Check the [interface requirements](ui.md) at 960×760 and 480×570 logical pixels.
Exercise long paths, portrait images, disclosures, empty and error states, and
keyboard focus through confirmation, recovery and cancellation. Use a private
service profile and a fixture setter for native checks without wallpaper
changes. Record the tested revision, scope and environment with the results.
See [verification](verification.md) for the current evidence.
