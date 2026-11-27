import { createContext, useContext, useEffect, useState, useSyncExternalStore } from 'react';
import type { ReactNode } from 'react';
import type {
  Bridge,
  Configuration,
  Entry,
  NavigationRequest,
  Page,
  Request,
  RequestOptions,
  Result,
  Snapshot,
} from './types';

type ReviewedImage = Pick<Entry, 'id' | 'image_id'> & { instance_id?: string };

export interface ClientState {
  snapshot: Snapshot | null;
  configuration: Configuration | null;
  liveConfiguration: Configuration | null;
  connected: boolean;
  busy: boolean;
  blocked: boolean;
  uncertain: boolean;
  settingsError: string;
  page: Page;
}
export interface ClientContext extends ClientState {
  getState(): ClientState;
  navigate(page: Page): void;
  refresh(options?: { background?: boolean }): Promise<Snapshot | null>;
  reload(): Promise<Snapshot | null>;
  request: Request;
  action(
    name: string,
    expectedImage?: ReviewedImage,
    navigationRequest?: NavigationRequest,
  ): Promise<Snapshot | null>;
  setServiceBusy(value: boolean): void;
}
const Context = createContext<ClientContext | null>(null);

export function usePanora(): ClientContext {
  const value = useContext(Context);
  if (!value) throw new Error('Panora components need a ClientProvider.');
  return value;
}

export function bridge<K extends keyof Bridge>(
  method: K,
  args?: Parameters<Bridge[K]>,
): ReturnType<Bridge[K]> {
  const client = (window as Window & { go?: { main?: { Client?: Bridge } } }).go?.main?.Client;
  if (!client) throw new Error('The native bridge is unavailable. Open the Panora app to connect.');
  return Reflect.apply(client[method], client, args || []) as ReturnType<Bridge[K]>;
}

export async function copyText(text: string): Promise<void> {
  const native = (
    window as Window & { runtime?: { ClipboardSetText(value: string): Promise<boolean> } }
  ).runtime;
  if (native?.ClipboardSetText) {
    if (!(await native.ClipboardSetText(text))) throw new Error('Could not write to clipboard.');
  } else {
    await navigator.clipboard.writeText(text);
  }
}

// One coordinator per window. It owns client requests, never service scheduling.
// Keeping the current snapshot synchronous lets a queued action recheck fresh reads
// before React has committed the corresponding render.
export function createClient() {
  let state: ClientState = {
    snapshot: null,
    configuration: null,
    liveConfiguration: null,
    connected: false,
    busy: false,
    blocked: false,
    uncertain: false,
    settingsError: '',
    page: 'now',
  };
  let foreground = false,
    serviceBusy = false;
  let refreshInFlight: {
    status: Promise<void>;
    complete: Promise<Snapshot | null>;
  } | null = null;
  const listeners = new Set<() => void>();
  const getState = () => state;
  function update(patch: Partial<ClientState> = {}) {
    state = { ...state, ...patch, busy: foreground || serviceBusy };
    for (const listener of listeners) listener();
  }
  function adopt(configuration: Configuration) {
    update({ configuration, liveConfiguration: configuration });
  }
  function settingsReadFailed() {
    update({
      blocked: true,
      settingsError:
        'Settings could not be loaded. Reload settings before making changes. Your edits are kept.',
    });
  }
  function accept(result: Snapshot) {
    const previous = state.snapshot;
    const before = previous?.status?.current,
      after = result.status?.current;
    if (!result.connected && !result.status && previous?.status) {
      result = { ...result, status: previous.status, current: previous.current };
    } else if (
      result.connected &&
      !result.current &&
      previous?.status?.instance_id === result.status?.instance_id &&
      before &&
      after &&
      before.id === after.id &&
      before.image_id === after.image_id
    ) {
      result = { ...result, current: previous?.current };
    }
    update({ snapshot: result, connected: result.connected });
  }
  function refresh({ background = false } = {}): Promise<Snapshot | null> {
    if (refreshInFlight) return refreshInFlight.complete;
    if (foreground || serviceBusy) return Promise.resolve(null);
    if (!background) foreground = true;
    // Publish both phases before notifying components.
    let statusReady!: () => void;
    const status = new Promise<void>((resolve) => {
      statusReady = resolve;
    });
    const complete = Promise.resolve().then(async () => {
      try {
        const result = await bridge('Status');
        // Reconnect must verify saved settings before restoring configuration writes.
        const reconnecting = result.connected && !state.connected && Boolean(state.configuration);
        if (result.connected) update({ uncertain: false });
        if (!reconnecting) accept(result);
        // Navigation rechecks the published snapshot while configuration loads.
        statusReady();
        if (result.connected) {
          let configuration: Configuration | undefined;
          let invalid = false;
          try {
            const result = await bridge('Configuration');
            configuration = result.configuration;
            invalid = result.code === 'config_invalid';
          } catch {
            // Treat a failed bridge call and an unavailable configuration alike.
          }
          if (configuration) {
            if (state.configuration) update({ liveConfiguration: configuration });
            else adopt(configuration);
          } else if (invalid || reconnecting || !state.configuration) settingsReadFailed();
          if (reconnecting) accept(result);
        }
        return result;
      } catch {
        accept({
          connected: false,
          code: 'service_unavailable',
          error: 'Panora could not be reached. Reconnect to check its current state.',
        });
        statusReady();
        return null;
      } finally {
        refreshInFlight = null;
        if (!background) foreground = false;
        update();
      }
    });
    refreshInFlight = { status, complete };
    update();
    return complete;
  }
  async function loadSettings() {
    if (foreground || serviceBusy) return false;
    foreground = true;
    update();
    try {
      if (refreshInFlight) await refreshInFlight.complete;
      const result = await bridge('Configuration');
      if (!result.configuration) throw new Error('Settings could not be loaded.');
      adopt(result.configuration);
      update({ blocked: false, settingsError: '' });
      return true;
    } catch {
      settingsReadFailed();
      return false;
    } finally {
      foreground = false;
      update();
    }
  }
  const request = (async (
    method: keyof Bridge,
    args: unknown[] = [],
    { mutation = false, settings = true, shouldSend }: RequestOptions = {},
  ) => {
    const failure = (code: string, error: string) => ({ code, error });
    const guard = () => {
      if (shouldSend && !shouldSend())
        return failure('workflow_changed', 'Setup changed before the request was sent.');
      if (serviceBusy)
        return failure('busy', 'A service operation is running. Try again when it finishes.');
      if (!state.connected)
        return failure('service_unavailable', 'Reconnect to Panora before continuing.');
      if (mutation && settings && state.blocked)
        return failure(
          'reload_required',
          'Reload settings before making another change. Your edits are kept.',
        );
      if (
        mutation &&
        !settings &&
        (state.uncertain || state.snapshot?.status?.uncertain || state.snapshot?.status?.busy)
      )
        return failure('busy', 'Check the current playback state before continuing.');
      return null;
    };
    if (foreground || serviceBusy)
      return failure('busy', 'Another operation is running. Try again when it finishes.');
    const denied = guard();
    if (denied) return denied;
    foreground = true;
    update();
    try {
      if (refreshInFlight) await refreshInFlight.complete;
      const changed = guard();
      if (changed) return changed;
      const result = (await bridge(method, args as never)) as Result;
      if (result.configuration) adopt(result.configuration);
      if (mutation && !settings && result.code !== 'busy') {
        update({ uncertain: !result.connected });
        accept(result as Snapshot);
      } else if (result.status) accept(result as Snapshot);
      else if (
        mutation &&
        settings &&
        result.error &&
        !['invalid_settings', 'invalid_request', 'busy'].includes(result.code || '')
      )
        update({ blocked: true });
      if (result.code === 'request_failed' && settings)
        return {
          ...result,
          error: mutation
            ? 'The change may have completed. Reload settings and check the current state before trying again.'
            : 'Panora did not respond. Reconnect or try this read again.',
        };
      return result;
    } catch {
      if (mutation && settings) update({ blocked: true });
      if (mutation && !settings) {
        update({ uncertain: true });
        accept({
          connected: false,
          code: 'request_failed',
          error:
            'The action may have completed. Reconnect to check the current state before trying again.',
        });
      }
      return failure(
        'request_failed',
        mutation
          ? settings
            ? 'The change may have been saved. Reload settings before trying again. Your edits are kept.'
            : 'The action may have completed. Reconnect and check the current state before trying again.'
          : 'Panora did not respond. Reconnect or try this read again.',
      );
    } finally {
      foreground = false;
      update();
    }
  }) as Request;
  async function action(
    name: string,
    expectedImage?: ReviewedImage,
    navigationRequest?: NavigationRequest,
  ): Promise<Snapshot | null> {
    if (foreground || serviceBusy || !state.connected || state.uncertain) return null;
    const navigation = name === 'next' || name === 'previous';
    const { current, instance_id } = state.snapshot?.status ?? {};
    const expected = expectedImage ?? (current && { ...current, instance_id });
    foreground = true;
    update();
    try {
      if (refreshInFlight) await (navigation ? refreshInFlight.status : refreshInFlight.complete);
      const status = state.snapshot?.status;
      if (
        !state.connected ||
        state.uncertain ||
        serviceBusy ||
        !status ||
        status.busy ||
        (status.uncertain && !navigation) ||
        (navigation && status.setup_required) ||
        (name === 'previous' &&
          typeof status.history_cursor === 'number' &&
          status.history_cursor < 1)
      )
        return null;
      let result: Snapshot;
      if (name === 'exclude') {
        if (
          !expected?.instance_id ||
          status.instance_id !== expected.instance_id ||
          status.current?.id !== expected.id ||
          status.current?.image_id !== expected.image_id
        ) {
          accept({
            ...state.snapshot!,
            code: 'current_changed',
            error:
              'The wallpaper changed before it could be excluded. Review the current image and try again.',
          });
          return null;
        }
        result = await bridge('ExcludeCurrent', [
          expected.instance_id,
          expected.id,
          expected.image_id,
        ]);
      } else if (navigationRequest) {
        if (
          !status.navigation_supported ||
          status.instance_id !== navigationRequest.instance ||
          (status.current?.id ?? '') !== navigationRequest.current
        )
          return null;
        result = await bridge('Navigate', [navigationRequest]);
      } else {
        result = await bridge('Action', [name]);
      }
      if (result.code === 'busy' && !result.status) {
        const latest = { ...state.snapshot!, error: result.error, code: result.code };
        update({ snapshot: latest });
        return latest;
      }
      update({ uncertain: !result.connected });
      if (result.code === 'navigation_superseded')
        accept({ ...result, error: undefined, code: undefined });
      else accept(result);
      return result;
    } catch {
      const result: Snapshot = {
        connected: false,
        error: 'The action may have completed. Reconnect to check status before trying again.',
        code: 'request_failed',
      };
      update({ uncertain: true });
      accept(result);
      return result;
    } finally {
      foreground = false;
      update();
    }
  }
  return {
    getState,
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    navigate(page: Page) {
      update({ page });
    },
    refresh,
    async reload() {
      const loaded = await loadSettings();
      const latest = await refresh();
      return loaded && latest?.connected ? latest : null;
    },
    request,
    action,
    setServiceBusy(value: boolean) {
      serviceBusy = value;
      update();
    },
    start() {
      void refresh();
      const poll = () => {
        // WKWebView can report hidden for a visible native window. Keep one
        // bounded read poll while mounted; wallpaper timing stays in the service.
        void refresh({ background: true });
      };
      const timer = window.setInterval(poll, 3000);
      document.addEventListener('visibilitychange', poll);
      return () => {
        window.clearInterval(timer);
        document.removeEventListener('visibilitychange', poll);
      };
    },
  };
}
export function ClientProvider({ children }: { children: ReactNode }) {
  const [client] = useState(createClient);
  const state = useSyncExternalStore(client.subscribe, client.getState);
  useEffect(() => client.start(), [client]);
  return <Context.Provider value={{ ...client, ...state }}>{children}</Context.Provider>;
}
