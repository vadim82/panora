import {
  Button,
  Field,
  FormActions,
  Notice,
  NumberInput,
  Select,
  TextInput,
} from '../components/ui';
import { useCallback, useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { usePanora } from '../client';
import type { CacheStatus, Configuration, PreferenceEdit, RotationMode } from '../types';
import { bytes, restoreFormFocus, revealInvalid } from './forms';

const intervals = [
  ['5m', '5 minutes'],
  ['15m', '15 minutes'],
  ['30m', '30 minutes'],
  ['1h', '1 hour'],
  ['3h', '3 hours'],
  ['6h', '6 hours'],
  ['24h', '24 hours'],
];
const presetFor = (value: string) =>
  ({
    '5m': '5m',
    '5m0s': '5m',
    '15m': '15m',
    '15m0s': '15m',
    '30m': '30m',
    '30m0s': '30m',
    '1h': '1h',
    '1h0m0s': '1h',
    '3h': '3h',
    '3h0m0s': '3h',
    '6h': '6h',
    '6h0m0s': '6h',
    '24h': '24h',
    '24h0m0s': '24h',
  })[value];
interface Draft {
  interval: string;
  rotation_mode: RotationMode;
  cache_max_images: string;
  cache_max_bytes: string;
}
interface Editor {
  baseline: Configuration;
  draft: Draft;
  custom: boolean;
}
function saved(config: Configuration): Draft {
  return {
    interval: config.saved.interval,
    rotation_mode: config.saved.rotation_mode || 'balance_sources',
    cache_max_images: String(config.saved.cache_max_images),
    cache_max_bytes: String(config.saved.cache_max_bytes / 1048576),
  };
}
function changed(editor: Editor) {
  const before = saved(editor.baseline);
  return (Object.keys(before) as (keyof Draft)[]).filter(
    (key) => before[key] !== editor.draft[key],
  );
}
function adopt(previous: Editor | null, config: Configuration, preserve = true): Editor {
  const draft = saved(config),
    keys = previous && preserve ? changed(previous) : [];
  if (previous) for (const key of keys) Object.assign(draft, { [key]: previous.draft[key] });
  return {
    baseline: config,
    draft,
    custom:
      previous && keys.includes('interval') ? previous.custom : !presetFor(config.saved.interval),
  };
}
export function RotationSettings() {
  const client = usePanora(),
    { request } = client;
  const [editor, setEditor] = useState<Editor | null>(() =>
    client.configuration ? adopt(null, client.configuration) : null,
  );
  const cacheScope = client.connected ? client.snapshot?.status?.instance_id || '' : null;
  const [storageError, setStorageError] = useState<{
    field: 'cache_max_images' | 'cache_max_bytes';
    value: string;
    message: string;
  } | null>(null);
  const [notice, setNotice] = useState(''),
    [measurement, setMeasurement] = useState<{
      scope: string;
      cache: CacheStatus | null;
      error: string;
      measuredAt: Date | null;
    } | null>(null),
    [cacheLoading, setCacheLoading] = useState(false);
  const [configFileError, setConfigFileError] = useState('');
  const activeMeasurement =
      cacheScope !== null && measurement?.scope === cacheScope ? measurement : null,
    cache = activeMeasurement?.cache,
    cacheError = activeMeasurement?.error,
    measuredAt = activeMeasurement?.measuredAt;
  const cacheRequested = useRef(false),
    cacheRead = useRef<Promise<void> | null>(null),
    cacheEpoch = useRef(0),
    cacheScopeRef = useRef(cacheScope),
    form = useRef<HTMLFormElement>(null),
    storage = useRef<HTMLDetailsElement>(null);
  cacheScopeRef.current = cacheScope;
  useEffect(() => {
    if (client.configuration) {
      const config = client.configuration;
      setEditor((previous) => (previous?.baseline === config ? previous : adopt(previous, config)));
    }
  }, [client.configuration]);
  useEffect(() => {
    cacheEpoch.current++;
    cacheRequested.current = false;
    cacheRead.current = null;
    setMeasurement(null);
    setCacheLoading(false);
  }, [cacheScope]);
  const readCache = useCallback(async () => {
    if (cacheRead.current) return cacheRead.current;
    const scope = cacheScopeRef.current,
      epoch = cacheEpoch.current;
    if (scope === null) return;
    setCacheLoading(true);
    const pending = request('CacheStatus')
      .then((result) => {
        if (epoch !== cacheEpoch.current || scope !== cacheScopeRef.current) return;
        if (result.code === 'busy') {
          cacheRequested.current = false;
          return;
        }
        if (result.cache) {
          setMeasurement({ scope, cache: result.cache, error: '', measuredAt: new Date() });
        } else
          setMeasurement((previous) => ({
            scope,
            cache: previous?.scope === scope ? previous.cache : null,
            measuredAt: previous?.scope === scope ? previous.measuredAt : null,
            error: result.error || 'Cache usage could not be read.',
          }));
      })
      .finally(() => {
        if (cacheRead.current === pending) {
          cacheRead.current = null;
          setCacheLoading(false);
        }
      });
    cacheRead.current = pending;
    return pending;
  }, [request]);
  useEffect(() => {
    if (client.connected && client.configuration && !client.busy && !cacheRequested.current) {
      cacheRequested.current = true;
      void readCache();
    }
  }, [client.connected, client.configuration, client.busy, cacheScope, readCache]);
  const draft = editor?.draft || {
      interval: '',
      rotation_mode: 'balance_sources',
      cache_max_images: '',
      cache_max_bytes: '',
    },
    keys = editor ? changed(editor) : [];
  const current = client.liveConfiguration || client.configuration,
    intervalOverride = current?.restart_required.includes('interval');
  const storageOverride =
    current &&
    (current.saved.cache_max_images !== current.effective.cache_max_images ||
      current.saved.cache_max_bytes !== current.effective.cache_max_bytes);
  const preset = editor?.custom ? 'custom' : presetFor(draft.interval) || 'custom';
  function update<K extends keyof Draft>(key: K, value: Draft[K]) {
    setEditor((previous) =>
      previous ? { ...previous, draft: { ...previous.draft, [key]: value } } : previous,
    );
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (
      !editor ||
      client.busy ||
      client.blocked ||
      !client.connected ||
      !keys.length ||
      !form.current?.reportValidity()
    )
      return;
    const edit: PreferenceEdit = { revision: editor.baseline.revision };
    for (const key of keys) {
      if (key === 'interval') edit.interval = draft.interval.trim();
      else if (key === 'rotation_mode') edit.rotation_mode = draft.rotation_mode;
      else
        edit[key] =
          key === 'cache_max_bytes' ? Math.round(Number(draft[key]) * 1048576) : Number(draft[key]);
    }
    const invalidStorage = (['cache_max_images', 'cache_max_bytes'] as const).find((key) => {
      const value = edit[key];
      return value !== undefined && (!Number.isSafeInteger(value) || value < 1);
    });
    if (invalidStorage) {
      if (storage.current) storage.current.open = true;
      setStorageError({
        field: invalidStorage,
        value: draft[invalidStorage],
        message:
          invalidStorage === 'cache_max_bytes'
            ? 'This storage limit is too large. Enter a smaller value.'
            : 'Enter a positive whole image count within the supported range.',
      });
      setNotice('');
      restoreFormFocus(
        invalidStorage === 'cache_max_bytes' ? 'storage-mib' : 'storage-images',
        client,
        client.getState().page,
        document.activeElement,
      );
      return;
    }
    const page = client.getState().page,
      origin = document.activeElement;
    const result = await client.request('UpdateConfiguration', [edit], { mutation: true });
    if (result.configuration) {
      setEditor(adopt(null, result.configuration, false));
      setNotice('Changes saved.');
      await client.refresh();
      await readCache();
      restoreFormFocus(
        keys.includes('rotation_mode') ? 'rotation-mode' : 'interval-preset',
        client,
        page,
        origin,
      );
    } else
      setNotice(
        (result.error || 'Settings could not be saved.') +
          (client.getState().blocked
            ? ' Reload settings before trying again. Your edits are kept.'
            : ' Your edits are kept.'),
      );
  }
  async function resetInterval() {
    if (!editor) return;
    const page = client.getState().page,
      origin = document.activeElement;
    const result = await client.request(
      'UpdateConfiguration',
      [{ revision: editor.baseline.revision, interval: editor.baseline.saved.interval }],
      { mutation: true },
    );
    if (result.configuration) {
      const config = result.configuration;
      setEditor((previous) => adopt(previous, config));
      setNotice('Saved interval is now active. Rotation pause and other pending edits are kept.');
      await client.refresh();
      restoreFormFocus('interval-preset', client, page, origin);
    } else
      setNotice(
        (result.error || 'The interval could not be applied.') +
          (client.getState().blocked ? ' Reload settings before trying again.' : ''),
      );
  }
  async function reload() {
    if (!(await client.reload())) {
      setNotice(
        'Settings could not be reloaded. Try Reload settings again. Your pending edits are kept.',
      );
      return;
    }
    await readCache();
    setNotice('Settings reloaded. Your pending edits are kept for review.');
  }
  async function refreshUsage() {
    const page = client.getState().page,
      origin = document.activeElement;
    await readCache();
    restoreFormFocus('refresh-cache', client, page, origin);
  }
  const usage = cache?.usage,
    unknown = Boolean(cache?.usage_error) || !usage || usage.state === 'unknown';
  return (
    <section
      id="rotation-storage"
      className="settings-panel"
      aria-labelledby="rotation-settings-title"
    >
      <h2 id="rotation-settings-title">Wallpaper timing</h2>
      <form
        id="rotation-storage-form"
        ref={form}
        onSubmit={save}
        onInvalid={(event) => {
          revealInvalid(event);
          if ((event.target as HTMLElement).id === 'rotation-interval')
            setEditor((previous) => (previous ? { ...previous, custom: true } : previous));
        }}
      >
        <fieldset id="rotation-storage-fields" disabled={client.busy || !editor}>
          <div className="rules-grid">
            <Field htmlFor="interval-preset">
              Change wallpaper every{' '}
              <Select
                id="interval-preset"
                value={preset}
                onChange={(event) => {
                  const value = event.target.value;
                  setEditor((previous) =>
                    previous
                      ? {
                          ...previous,
                          custom: value === 'custom',
                          draft: {
                            ...previous.draft,
                            interval: value === 'custom' ? previous.draft.interval : value,
                          },
                        }
                      : previous,
                  );
                  if (value === 'custom')
                    setTimeout(() => document.getElementById('rotation-interval')?.focus(), 0);
                }}
              >
                {intervals.map(([value, label]) => (
                  <option key={value} value={value}>
                    {label}
                  </option>
                ))}
                <option value="custom">Custom interval…</option>
              </Select>
            </Field>
            <Field
              htmlFor="rotation-interval"
              id="custom-interval"
              hidden={preset !== 'custom'}
              help="For example, 30m for 30 minutes or 1h for one hour."
            >
              Custom interval{' '}
              <TextInput
                id="rotation-interval"

                required
                maxLength={64}
                placeholder="30m"
                value={draft.interval}
                onChange={(event) => update('interval', event.target.value)}
              />
            </Field>
            <Field
              htmlFor="rotation-mode"
              help={
                draft.rotation_mode === 'shuffle_all_images'
                  ? 'Give each distinct eligible image an equal chance.'
                  : 'Give each eligible provider an equal chance.'
              }
            >
              Rotation mode
              <Select
                id="rotation-mode"
                value={draft.rotation_mode}
                onChange={(event) => update('rotation_mode', event.target.value as RotationMode)}
              >
                <option value="balance_sources">Balance sources</option>
                <option value="shuffle_all_images">Shuffle all images</option>
              </Select>
            </Field>
          </div>
          <details id="storage-options" ref={storage}>
            <summary>Storage limits</summary>
            <div className="rules-grid">
              <Field
                htmlFor="storage-images"
                error={
                  storageError?.field === 'cache_max_images' &&
                  storageError.value === draft.cache_max_images
                    ? storageError.message
                    : undefined
                }
              >
                Maximum cached images{' '}
                <NumberInput
                  id="storage-images"

                  min="1"
                  max={Number.MAX_SAFE_INTEGER}
                  step="1"
                  required
                  value={draft.cache_max_images}
                  onChange={(event) => update('cache_max_images', event.target.value)}
                />
              </Field>
              <Field
                htmlFor="storage-mib"
                error={
                  storageError?.field === 'cache_max_bytes' &&
                  storageError.value === draft.cache_max_bytes
                    ? storageError.message
                    : undefined
                }
              >
                Storage limit (MiB){' '}
                <NumberInput
                  id="storage-mib"

                  min="0.000001"
                  step="any"
                  required
                  value={draft.cache_max_bytes}
                  onChange={(event) => update('cache_max_bytes', event.target.value)}
                />
              </Field>
            </div>
            <p className="subtext">
              Only downloaded and temporary copies count toward these limits. Your original images
              are kept.
            </p>
            {storageOverride && current && (
              <p id="storage-effective" className="subtext">
                Saved limits: {current.saved.cache_max_images} images /{' '}
                {bytes(current.saved.cache_max_bytes)}. Currently using:{' '}
                {current.effective.cache_max_images} images /{' '}
                {bytes(current.effective.cache_max_bytes)}.
              </p>
            )}
          </details>
        </fieldset>
        {intervalOverride && current && (
          <p id="rotation-effective" className="subtext">
            Using {current.effective.interval} for this session instead of the saved{' '}
            {current.saved.interval}. Saving the interval replaces this temporary interval override.
            {client.snapshot?.status?.paused ? ' Rotation is paused; saving keeps it paused.' : ''}
          </p>
        )}
        {intervalOverride && (
          <Button
            id="use-saved-interval"
            type="button"
            className="outline"
            disabled={
              client.busy || client.blocked || !client.connected || keys.includes('interval')
            }
            onClick={() => void resetInterval()}
          >
            Use saved interval now
          </Button>
        )}
        {keys.length > 0 && (
          <p id="rotation-storage-draft" className="subtext">
            Unsaved changes.
          </p>
        )}
        <FormActions
          id="rotation-storage-actions"

          hidden={Boolean(editor) && !keys.length && !client.blocked}
        >
          <Button
            id="save-rotation-storage"
            className="primary"
            type="submit"
            disabled={client.busy || client.blocked || !client.connected || !editor || !keys.length}
          >
            Save changes
          </Button>
          <Button
            id="cancel-rotation-storage"
            type="button"
            disabled={client.busy || !keys.length}
            onClick={() => {
              if (editor) setEditor(adopt(null, editor.baseline, false));
              setNotice('Unsaved rotation and storage edits canceled.');
              restoreFormFocus('interval-preset', client, client.page, document.activeElement);
            }}
          >
            Cancel edits
          </Button>
          <Button
            id="reload-rotation-storage"
            type="button"
            className="outline"
            disabled={client.busy}
            onClick={() => void reload()}
          >
            Reload settings
          </Button>
        </FormActions>
      </form>
      <Notice id="rotation-storage-notice" hidden={!notice}>
        {notice}
      </Notice>
      <details>
        <summary>Storage usage</summary>
        {current?.cache_path && (
          <p className="subtext settings-path">
            Playback cache: <code>{current.cache_path}</code>
          </p>
        )}
        <div className="usage-card">
          <p id="cache-measured-at">
            {measuredAt
              ? 'Last checked at ' +
                measuredAt.toLocaleTimeString() +
                '. Refresh usage after playback to check current storage.'
              : 'Not yet checked.'}
          </p>
          <p id="cache-usage">
            {!cache
              ? 'Usage has not been checked.'
              : unknown
                ? 'Storage usage is unavailable. Try Refresh usage.'
                : usage &&
                  `${usage.images} managed images · ${bytes(usage.bytes)}. ${usage.images - usage.protected_images} disposable images · ${bytes(Math.max(0, usage.bytes - usage.protected_bytes))}.`}
          </p>
          <p id="cache-protection">
            {!unknown &&
              usage &&
              `${usage.protected_images} protected images · ${bytes(usage.protected_bytes)} are retained for current or pending playback.`}
          </p>
          <Notice id="cache-health">
            {cacheError
              ? cacheError +
                (cache ? ' Other usage values are from the last successful measurement.' : '')
              : usage?.state === 'protected_over_budget'
                ? 'Protected images exceed the budget. They are kept; eligible disposable files are cleaned up.'
                : cache?.last_apply_error?.message ||
                  cache?.usage_error?.message ||
                  (cache ? 'Original files remain outside this managed cache.' : '')}
          </Notice>
          <Button
            id="refresh-cache"
            className="outline"
            pending={cacheLoading}
            disabled={client.busy || !client.connected}
            onClick={() => void refreshUsage()}
          >
            {cacheLoading ? 'Checking usage…' : 'Refresh usage'}
          </Button>
        </div>
      </details>
      {current?.config_path && (
        <div className="settings-subsection">
          <h2>Configuration file</h2>
          <p className="subtext settings-path">
            <code>{current.config_path}</code>
          </p>
          <FormActions>
            <Button
              type="button"
              disabled={!client.connected || client.busy}
              onClick={async () => {
                const result = await request('OpenConfiguration', [], { settings: false });
                setConfigFileError(
                  result.opened ? '' : result.error || 'The file could not be opened.',
                );
              }}
            >
              Open configuration file
            </Button>
          </FormActions>
          <Notice tone="error" hidden={!configFileError}>
            {configFileError}
          </Notice>
        </div>
      )}
    </section>
  );
}
