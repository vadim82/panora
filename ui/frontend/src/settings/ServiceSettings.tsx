import { ConfirmDialog } from '../components/ui/ConfirmDialog';
import { Button, FormActions, Notice } from '../components/ui';
import { useCallback, useEffect, useRef, useState } from 'react';
import { bridge, usePanora } from '../client';
import type { Page, ServiceResult } from '../types';
import { restoreFormFocus } from './forms';

type Operation = 'start' | 'stop' | 'login_on' | 'login_off' | 'remove_startup';
interface Confirmation {
  operation: 'stop' | 'remove_startup';
  origin: HTMLElement | null;
  page: Page;
}
function canCommand(result: ServiceResult | null, operation: Operation) {
  const state = result?.service;
  if (!state?.available || result?.busy) return false;
  const linux = state.platform === 'linux';
  if (operation === 'start') return !state.running && !state.start_blocked;
  if (operation === 'stop') return Boolean(state.running && (!linux || state.managed));
  if (operation === 'remove_startup') return state.platform === 'darwin';
  return !linux || state.login_state !== 'unknown';
}
export function ServiceSettings() {
  const client = usePanora(),
    clientRef = useRef(client);
  clientRef.current = client;
  const [result, setResult] = useState<ServiceResult | null>(null),
    resultRef = useRef<ServiceResult | null>(null);
  const [working, setWorking] = useState(false),
    workingRef = useRef(false),
    readPending = useRef<Promise<void> | null>(null);
  const [pending, setPending] = useState<Confirmation | null>(null);
  const state = result?.service,
    blocked = working || Boolean(result?.busy) || client.busy;
  const publish = useCallback((next: ServiceResult) => {
    resultRef.current = next;
    setResult(next);
    clientRef.current.setServiceBusy(Boolean(workingRef.current || next.busy));
  }, []);
  const read = useCallback(() => {
    if (readPending.current) return readPending.current;
    if (workingRef.current) return Promise.resolve();
    const pending = Promise.resolve().then(async () => {
      try {
        publish(await bridge('ServiceState'));
      } catch {
        publish({
          error: 'Service controls could not be read. Panora will check again automatically.',
        });
      } finally {
        readPending.current = null;
      }
    });
    readPending.current = pending;
    return pending;
  }, [publish]);
  useEffect(() => {
    void read();
    const interval = setInterval(() => void read(), 3000);
    return () => clearInterval(interval);
  }, [read]);
  useEffect(() => {
    if (client.page === 'settings') void read();
  }, [client.page, read]);
  async function command(
    operation: Operation,
    origin: HTMLElement | null = document.activeElement instanceof HTMLElement
      ? document.activeElement
      : null,
  ) {
    const host = clientRef.current;
    if (workingRef.current || resultRef.current?.busy || host.getState().busy) return;
    const page = host.getState().page;
    workingRef.current = true;
    setWorking(true);
    host.setServiceBusy(true);
    let performed = false;
    try {
      // Claim ownership before joining passive reads; Go revalidates installed ownership.
      await host.refresh();
      if (readPending.current) await readPending.current;
      if (!canCommand(resultRef.current, operation)) {
        publish({
          ...resultRef.current,
          error:
            resultRef.current?.error ||
            'Service state changed. Review the current state before trying again.',
        });
        return;
      }
      performed = true;
      publish(await bridge('ServiceCommand', [operation]));
    } catch {
      publish({
        ...resultRef.current,
        error:
          'The operation may have completed. Check the reported service state before trying again.',
      });
    } finally {
      workingRef.current = false;
      setWorking(false);
      host.setServiceBusy(Boolean(resultRef.current?.busy));
      await host.refresh();
      if (performed) await read();
      if (origin?.id) restoreFormFocus(origin.id, host, page, origin);
    }
  }
  function confirm(operation: Confirmation['operation'], origin: HTMLElement) {
    if (blocked || !canCommand(resultRef.current, operation)) return;
    setPending({
      operation,
      origin,
      page: client.getState().page,
    });
  }
  function closeConfirmation(run = false) {
    if (!pending) return;
    const captured = pending;
    setPending(null);
    if (run && client.getState().page === captured.page)
      void command(captured.operation, captured.origin);
  }
  const remove = pending?.operation === 'remove_startup';
  const serviceAction = state?.running ? 'stop' : 'start';
  const error = result?.error || state?.error;
  const linux = state?.platform === 'linux';
  const mac = state?.platform === 'darwin';
  const can = (operation: Operation) => !blocked && canCommand(result, operation);
  let loginText = 'Login startup requires the installed app and its default connection.';
  if (linux && !state.available)
    loginText = 'Login startup is unavailable. Check the systemd user service.';
  if (state?.available) {
    if (state.login_state === 'disabled_by_system')
      loginText =
        'Login startup is configured, but macOS has disabled this background item. Check System Settings.';
    else if (state.login_state === 'unknown')
      loginText =
        'The saved login registration could not be verified. Check its state before relying on it.';
    else if (state.login_state === 'enabled_for_session')
      loginText = 'Startup is enabled for the current session only.';
    else if (state.start_at_login)
      loginText = linux ? 'Starts at your next graphical login.' : 'Starts at your next login.';
    else loginText = 'Starts when you open Panora.';
  }
  let serviceText =
    'Automatic service controls require the installed app and its default connection. An explicit socket connection stays connect-only.';
  if (linux && !state.available) serviceText = 'The installed systemd user service is unavailable.';
  if (state?.available) {
    if (state.running)
      serviceText = state.managed
        ? 'Running.'
        : linux
          ? 'Connected to a separately started service. Stop it from where it was launched before starting the installed service.'
          : 'Connected to a separately started service. Stop it before starting the bundled service.';
    else
      serviceText = state.start_blocked
        ? 'Service startup is unavailable. Resolve the reported service or socket problem.'
        : 'Stopped or disconnected. Start service to reconnect.';
  }
  if (state?.needs_restart)
    loginText +=
      ' The installed app location changed. Stop and start the service to use this copy.';
  return (
    <section
      id="service-management"
      className="settings-panel"
      aria-labelledby="service-settings-title"
    >
      <h2 id="service-settings-title">Startup</h2>
      <div className="service-login-field">
        <label className="checkbox-field">
          <input
            id="service-login"
            type="checkbox"
            aria-describedby="service-login-state"
            disabled={!can(state?.start_at_login ? 'login_off' : 'login_on')}
            checked={Boolean(state?.start_at_login)}
            onChange={(event) => void command(event.target.checked ? 'login_on' : 'login_off')}
          />
          <span>Start Panora at login</span>
        </label>
        <p id="service-login-state" className="subtext">
          {loginText}
        </p>
      </div>
      <Notice tone="error" id="service-error" hidden={!error}>
        {error}
      </Notice>
      <details id="service-options">
        <summary>Background service</summary>
        <p>
          Closing the window keeps wallpapers changing. Stop ends rotation and image checks; your
          wallpaper stays in place.
        </p>
        <Notice id="service-state">
          {working || result?.busy ? 'A service operation is in progress…' : serviceText}
        </Notice>
        <FormActions>
          <Button
            id="service-toggle"
            type="button"
            disabled={!can(serviceAction)}
            onClick={(event) => {
              if (serviceAction === 'stop') confirm('stop', event.currentTarget);
              else void command('start', event.currentTarget);
            }}
          >
            {serviceAction === 'stop' ? 'Stop service…' : 'Start service'}
          </Button>
        </FormActions>
        {!linux && (
          <FormActions>
            <Button
              id="service-logs"
              type="button"
              disabled={blocked || !state?.available}
              onClick={async () => {
                try {
                  const opened = await bridge('OpenServiceLogs');
                  if (!opened.opened)
                    publish({
                      ...resultRef.current,
                      error: opened.error || 'Logs folder is unavailable.',
                    });
                } catch {
                  publish({ ...resultRef.current, error: 'Logs folder is unavailable.' });
                }
              }}
            >
              Open logs folder
            </Button>
            {mac && (
              <Button
                id="service-remove"
                type="button"
                disabled={!can('remove_startup')}
                onClick={(event) => confirm('remove_startup', event.currentTarget)}
              >
                Prepare to move or remove app…
              </Button>
            )}
          </FormActions>
        )}
        <p id="service-log-path" className="subtext">
          {state?.log_command ? (
            <>
              View service logs with <code>{state.log_command}</code>.
            </>
          ) : state?.log_directory ? (
            'Service logs: ' +
            state.log_directory +
            ' · Current and previous files are limited to 1 MiB each.'
          ) : null}
        </p>
      </details>
      <ConfirmDialog
        id="service-confirm"
        open={Boolean(pending)}
        active={client.page === 'settings' && (!remove || mac)}
        title={remove ? 'Prepare to move or remove Panora?' : 'Stop Panora service?'}
        description={
          remove
            ? 'This stops the managed service and removes Panora’s login/startup registration. A separately started service keeps running. Settings, history and original images are kept. Quit the client before moving or deleting the app.'
            : 'Rotation and discovery stop. The last wallpaper and saved settings remain. Background polling will not restart it.'
        }
        confirmLabel={remove ? 'Remove startup' : 'Stop service'}
        confirmID="service-confirm-yes"
        cancelID="service-confirm-cancel"
        returnFocusID={pending?.origin?.id}
        disabled={blocked}
        onCancel={() => closeConfirmation()}
        onConfirm={() => closeConfirmation(true)}
      />
    </section>
  );
}
