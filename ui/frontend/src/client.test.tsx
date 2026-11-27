import { StrictMode } from 'react';
import userEvent from '@testing-library/user-event';
import { ImageSettings, ImageSettingsProvider } from './settings/ImageSettings';
import { act, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ClientProvider, createClient, usePanora } from './client';
import { configuration, installBridge, snapshot } from './test/fixtures';
import type { Snapshot } from './types';

function deferred<T>() {
  let resolve: (value: T) => void = () => {};
  const promise = new Promise<T>((complete) => {
    resolve = complete;
  });
  return { promise, resolve };
}

describe('client request ownership and recovery', () => {
  it('defers the initial settings read while service startup is busy and exposes its failure once idle', async () => {
    const mock = installBridge(),
      client = createClient();
    mock.Configuration.mockResolvedValue({
      code: 'config_invalid',
      error: 'Saved document is invalid.',
    });
    client.setServiceBusy(true);
    const stop = client.start();
    try {
      await Promise.resolve();
      expect(mock.Configuration).not.toHaveBeenCalled();
      client.setServiceBusy(false);
      await client.refresh();
      await waitFor(() => expect(client.getState().blocked).toBe(true));
      expect(client.getState().settingsError).toMatch(/Reload settings/);
      expect(mock.Configuration).toHaveBeenCalledTimes(1);
      mock.Configuration.mockResolvedValue({ configuration: structuredClone(configuration) });
      await client.refresh({ background: true });
      expect(client.getState().configuration?.revision).toBe('reviewed');
      expect(client.getState().blocked).toBe(true);
      client.setServiceBusy(true);
      client.setServiceBusy(false);
      await Promise.resolve();
      expect(mock.Configuration).toHaveBeenCalledTimes(2);
      await client.reload();
      expect(client.getState()).toMatchObject({ blocked: false, settingsError: '' });
    } finally {
      stop();
    }
  });
  it('uses the successful initial settings read without repeating it during a restart', async () => {
    const mock = installBridge(),
      client = createClient();
    mock.Configuration.mockResolvedValueOnce({
      configuration: structuredClone(configuration),
    }).mockResolvedValueOnce({ code: 'request_failed', error: 'Service is restarting.' });
    const stop = client.start();
    try {
      await waitFor(() => expect(client.getState().configuration?.revision).toBe('reviewed'));
      await Promise.resolve();
      expect(mock.Configuration).toHaveBeenCalledTimes(1);
      expect(client.getState()).toMatchObject({ blocked: false, settingsError: '' });
      expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
    } finally {
      stop();
    }
  });
  it('keeps lost playback separate from the settings lock and reconciles without replaying either mutation', async () => {
    const mock = installBridge(),
      client = createClient();
    await client.refresh();
    mock.ReplayHistory.mockRejectedValueOnce(new Error('lost playback response'));
    const lost = await client.request('ReplayHistory', ['retained-handle'], {
      mutation: true,
      settings: false,
    });
    expect(lost.code).toBe('request_failed');
    expect(client.getState()).toMatchObject({ uncertain: true, connected: false, blocked: false });
    expect(client.getState().snapshot?.status?.current).toEqual(snapshot.status?.current);
    expect(client.getState().snapshot?.error).toMatch(/may have completed/);
    await client.refresh();
    expect(client.getState()).toMatchObject({ uncertain: false, connected: true, blocked: false });
    expect(mock.ReplayHistory).toHaveBeenCalledTimes(1);
    mock.UpdateConfiguration.mockRejectedValueOnce(new Error('lost configuration response'));
    await client.request(
      'UpdateConfiguration',
      [{ revision: configuration.revision, interval: '1h' }],
      { mutation: true },
    );
    expect(client.getState()).toMatchObject({ blocked: true, uncertain: false });
    await client.refresh({ background: true });
    expect(client.getState().blocked).toBe(true);
    const retry = await client.request(
      'UpdateConfiguration',
      [{ revision: configuration.revision, interval: '1h' }],
      { mutation: true },
    );
    expect(retry.code).toBe('reload_required');
    expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(1);
    expect(await client.reload()).toMatchObject({ connected: true });
    expect(client.getState().blocked).toBe(false);
    expect(mock.ReplayHistory).toHaveBeenCalledTimes(1);
    expect(mock.UpdateConfiguration).toHaveBeenCalledTimes(1);
  });
  it('keeps a failed first configuration load blocked through later passive success until explicit reload', async () => {
    const mock = installBridge(),
      client = createClient();
    mock.Configuration.mockRejectedValue(new Error('unsupported saved document'));
    const stop = client.start();
    try {
      await waitFor(() => expect(client.getState().blocked).toBe(true));
      expect(client.getState().configuration).toBeNull();
      mock.Configuration.mockResolvedValue({ configuration: structuredClone(configuration) });
      await client.refresh({ background: true });
      expect(client.getState().configuration?.revision).toBe('reviewed');
      expect(client.getState().blocked).toBe(true);
      expect(client.getState().settingsError).toMatch(/Reload settings/);
      expect(
        (
          await client.request('UpdateConfiguration', [{ revision: 'reviewed', fit: 'crop' }], {
            mutation: true,
          })
        ).code,
      ).toBe('reload_required');
      expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
      await client.reload();
      expect(client.getState()).toMatchObject({ blocked: false, settingsError: '' });
    } finally {
      stop();
    }
  });
  it('blocks settings after an invalid ordinary poll until reload while playback remains available', async () => {
    const mock = installBridge(),
      client = createClient();
    await client.refresh();
    const previous = client.getState().configuration;
    mock.Configuration.mockResolvedValueOnce({
      code: 'config_invalid',
      error: 'Invalid settings.',
    });
    await client.refresh({ background: true });
    expect(client.getState()).toMatchObject({
      connected: true,
      blocked: true,
      configuration: previous,
      liveConfiguration: previous,
      snapshot: { status: snapshot.status },
    });
    expect(client.getState().settingsError).toMatch(/Reload settings/);
    expect(
      (
        await client.request('UpdateConfiguration', [{ revision: 'reviewed', fit: 'crop' }], {
          mutation: true,
        })
      ).code,
    ).toBe('reload_required');
    expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
    await client.action('next');
    expect(mock.Action).toHaveBeenCalledExactlyOnceWith('next');
    await client.refresh({ background: true });
    expect(client.getState().blocked).toBe(true);
    expect(await client.reload()).toMatchObject({ connected: true });
    expect(client.getState()).toMatchObject({ blocked: false, settingsError: '' });
  });
  it('keeps settings writes blocked when status reconnects before settings can be read', async () => {
    const mock = installBridge();
    const client = createClient();
    await client.refresh();
    mock.Status.mockResolvedValueOnce({ connected: false });
    await client.refresh();
    const read = deferred<Awaited<ReturnType<typeof mock.Configuration>>>();
    mock.Configuration.mockReturnValueOnce(read.promise);
    const reconnect = client.refresh({ background: true });
    await waitFor(() => expect(mock.Configuration).toHaveBeenCalledTimes(2));
    expect(client.getState().connected).toBe(false);
    read.resolve({ error: 'Saved settings are unavailable.' });
    await reconnect;
    expect(client.getState()).toMatchObject({ blocked: true, connected: true });
    expect(
      (
        await client.request(
          'UpdateFolder',
          [{ revision: 'reviewed', id: 'folder-1', enabled: false }],
          { mutation: true },
        )
      ).code,
    ).toBe('reload_required');
    expect(mock.UpdateFolder).not.toHaveBeenCalled();
    expect(await client.reload()).toMatchObject({ connected: true });
    expect(client.getState().blocked).toBe(false);
    expect(mock.UpdateFolder).not.toHaveBeenCalled();
  });

  it('claims one foreground action while joining a passive read and rejects stale Exclude identity without replay', async () => {
    const mock = installBridge(),
      client = createClient();
    await client.refresh();
    const read = deferred<Snapshot>();
    mock.Status.mockReturnValueOnce(read.promise);
    const passive = client.refresh({ background: true });
    await Promise.resolve();
    expect(client.getState().busy).toBe(false);
    const action = client.action('exclude');
    expect(client.getState().busy).toBe(true);
    expect(await client.action('next')).toBeNull();
    expect((await client.request('Capabilities')).code).toBe('busy');
    expect(mock.Action).not.toHaveBeenCalled();
    const newer = structuredClone(snapshot);
    newer.status!.current = { id: 'newer-image', image_id: 2 };
    newer.current = { ...newer.current!, id: 'newer-image', image_id: 2 };
    read.resolve(newer);
    await passive;
    await action;
    expect(mock.Action).not.toHaveBeenCalled();
    expect(mock.ExcludeCurrent).not.toHaveBeenCalled();
    expect(client.getState().busy).toBe(false);
    expect(client.getState().snapshot?.error).toMatch(/wallpaper changed/);
    // A fresh explicit click targets the reviewed image; no old click was replayed.
    await client.action('exclude');
    expect(mock.ExcludeCurrent).toHaveBeenCalledExactlyOnceWith('test-service', 'newer-image', 2);
  });
  it('drops retained metadata and cancels a queued Exclude when the service instance changes', async () => {
    const mock = installBridge();
    const client = createClient();
    const first = structuredClone(snapshot);
    first.status!.instance_id = 'first';
    mock.Status.mockResolvedValueOnce(first);
    await client.refresh();
    const read = deferred<Snapshot>();
    mock.Status.mockReturnValueOnce(read.promise);
    const passive = client.refresh({ background: true });
    const exclude = client.action('exclude');
    read.resolve({ connected: true, status: { ...first.status!, instance_id: 'second' } });
    await passive;
    await exclude;
    expect(client.getState().snapshot?.current).toBeUndefined();
    expect(mock.Action).not.toHaveBeenCalled();
    expect(mock.ExcludeCurrent).not.toHaveBeenCalled();
  });
  it.each([
    ['next', 1],
    ['previous', -1],
  ] as const)(
    'dispatches %s after status while configuration is still pending',
    async (direction, offset) => {
      const mock = installBridge();
      const client = createClient();
      const reviewed = {
        ...snapshot.status!,
        navigation_supported: true,
        history_cursor: 1,
        history_length: 2,
      };
      const initial = { ...snapshot, status: reviewed };
      mock.Status.mockResolvedValueOnce(initial);
      await client.refresh();

      const statusRead = deferred<Snapshot>();
      const configurationRead = deferred<Awaited<ReturnType<typeof mock.Configuration>>>();
      mock.Status.mockReturnValueOnce(statusRead.promise);
      mock.Configuration.mockReturnValueOnce(configurationRead.promise);
      const accepted = { ...reviewed, current: { id: 'navigated', image_id: 2 } };
      mock.Navigate.mockResolvedValueOnce({ connected: true, status: accepted });

      const refresh = client.refresh({ background: true });
      const request = { instance: 'test-service', current: 'accepted-1', token: 'click', offset };
      const action = client.action(direction, undefined, request);
      await waitFor(() => expect(mock.Status).toHaveBeenCalledTimes(2));
      expect(mock.Navigate).not.toHaveBeenCalled();
      expect(mock.Configuration).toHaveBeenCalledTimes(1);

      statusRead.resolve(initial);
      const newer = { ...configuration, revision: 'external-change' };
      try {
        await waitFor(() => expect(mock.Navigate).toHaveBeenCalledExactlyOnceWith(request));
        expect(mock.Configuration).toHaveBeenCalledTimes(2);
        expect(await action).toMatchObject({ status: accepted });
        expect(client.getState().liveConfiguration?.revision).toBe('reviewed');
      } finally {
        configurationRead.resolve({ configuration: newer });
        await refresh;
      }
      expect(client.getState()).toMatchObject({
        snapshot: { status: accepted },
        configuration: { revision: 'reviewed' },
        liveConfiguration: { revision: 'external-change' },
      });
    },
  );

  it.each([
    ['service instance', { instance_id: 'replacement' }],
    ['accepted entry', { current: { id: 'different', image_id: 2 } }],
    ['history cursor', { history_cursor: 0 }],
  ])('drops queued navigation when the %s changes during status', async (_change, changed) => {
    const mock = installBridge();
    const client = createClient();
    const reviewed = {
      ...snapshot.status!,
      navigation_supported: true,
      history_cursor: 1,
      history_length: 2,
    };
    mock.Status.mockResolvedValueOnce({ ...snapshot, status: reviewed });
    await client.refresh();

    const statusRead = deferred<Snapshot>();
    mock.Status.mockReturnValueOnce(statusRead.promise);
    const refresh = client.refresh({ background: true });
    const action = client.action('previous', undefined, {
      instance: 'test-service',
      current: 'accepted-1',
      token: 'click',
      offset: -1,
    });
    statusRead.resolve({ connected: true, status: { ...reviewed, ...changed } });
    expect(await action).toBeNull();
    await refresh;
    expect(mock.Navigate).not.toHaveBeenCalled();
    expect(mock.Action).not.toHaveBeenCalled();
  });

  it('keeps the new wallpaper when the service rejects a change after the last snapshot', async () => {
    const mock = installBridge();
    const client = createClient();
    await client.refresh();
    const current = { id: 'advanced-by-another-client', image_id: 2 };
    mock.ExcludeCurrent.mockResolvedValueOnce({
      connected: true,
      status: { ...snapshot.status, current },
      code: 'current_changed',
      error: 'The wallpaper changed before it could be excluded.',
    });
    expect(await client.action('exclude')).toMatchObject({ code: 'current_changed' });
    expect(client.getState()).toMatchObject({
      connected: true,
      uncertain: false,
      snapshot: { status: { current } },
    });
    expect(mock.ExcludeCurrent).toHaveBeenCalledExactlyOnceWith('test-service', 'accepted-1', 1);
    expect(mock.Action).not.toHaveBeenCalled();
  });
  it('preserves the connection when the native client rejects a busy action before dispatch', async () => {
    const mock = installBridge();
    const client = createClient();
    await client.refresh();
    mock.Action.mockResolvedValueOnce({ connected: false, code: 'busy', error: 'Try again.' });
    await client.action('next');
    expect(client.getState()).toMatchObject({ connected: true, uncertain: false, busy: false });
    expect(client.getState().snapshot).toMatchObject({ status: snapshot.status, code: 'busy' });
  });
});

function ConnectionProbe() {
  const client = usePanora();
  return (
    <>
      <output>{client.connected ? 'Connected' : 'Disconnected'}</output>
      <button onClick={() => void client.refresh()}>Read connection</button>
      <button onClick={() => void client.reload()}>Recover settings</button>
      <output data-testid="settings-error">{client.settingsError}</output>
      <output data-testid="accepted-id">{client.snapshot?.status?.current?.id}</output>
    </>
  );
}
it('waits for a connected startup read and keeps real image drafts through reconnects', async () => {
  const mock = installBridge(),
    user = userEvent.setup();
  mock.Status.mockResolvedValue({ connected: false, code: 'service_unavailable' });
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <ConnectionProbe />
        <ImageSettings />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
  await waitFor(() => expect(mock.Status).toHaveBeenCalled());
  await act(async () => {
    await Promise.resolve();
  });
  expect(mock.Configuration).not.toHaveBeenCalled();
  expect(screen.getByTestId('settings-error')).toBeEmptyDOMElement();
  mock.Status.mockResolvedValue(structuredClone(snapshot));
  await user.click(screen.getByRole('button', { name: 'Read connection' }));
  await waitFor(() => expect(screen.getByLabelText('Fit')).toBeEnabled());
  expect(mock.Configuration).toHaveBeenCalledTimes(1);
  await user.click(screen.getByText('Image filters', { selector: 'summary' }));
  await user.selectOptions(screen.getByLabelText('Minimum resolution'), 'custom');
  const width = screen.getByRole('spinbutton', { name: 'Minimum width (pixels)' });
  await user.clear(width);
  await user.type(width, '2048');
  mock.Configuration.mockResolvedValueOnce({ code: 'config_invalid', error: 'Invalid settings.' });
  await user.click(screen.getByRole('button', { name: 'Read connection' }));
  await waitFor(() =>
    expect(screen.getByTestId('settings-error')).toHaveTextContent('Reload settings'),
  );
  expect(width).toHaveValue(2048);
  await user.click(screen.getByRole('button', { name: 'Recover settings' }));
  await waitFor(() => expect(screen.getByTestId('settings-error')).toBeEmptyDOMElement());
  expect(width).toHaveValue(2048);
  mock.Status.mockResolvedValue({ connected: false, code: 'service_unavailable' });
  await user.click(screen.getByRole('button', { name: 'Read connection' }));
  await waitFor(() => expect(screen.getByText('Disconnected')).toBeVisible());
  const newer = structuredClone(configuration);
  newer.revision = 'external-change';
  newer.rules.min_width = 2400;
  mock.Configuration.mockResolvedValue({ configuration: newer });
  mock.Status.mockResolvedValue(structuredClone(snapshot));
  await user.click(screen.getByRole('button', { name: 'Read connection' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Save changes' })).toBeEnabled());
  expect(width).toHaveValue(2048);
  expect(screen.getByTestId('settings-error')).toBeEmptyDOMElement();
  expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
  expect(mock.Action).not.toHaveBeenCalled();
});
it('StrictMode polls hidden native documents without wallpaper actions and cleans up on unmount', async () => {
  vi.useFakeTimers();
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
  const mock = installBridge();
  const view = render(
    <StrictMode>
      <ClientProvider>
        <ConnectionProbe />
      </ClientProvider>
    </StrictMode>,
  );
  try {
    await act(async () => {
      for (let count = 0; count < 8; count++) await Promise.resolve();
    });
    expect(screen.getByText('Connected')).toBeInTheDocument();
    expect(screen.getByTestId('accepted-id')).toHaveTextContent('accepted-1');
    expect(vi.getTimerCount()).toBe(1);
    mock.Status.mockClear();
    const newer = structuredClone(snapshot);
    newer.status!.current = { id: 'scheduled-image', image_id: 2 };
    newer.current = { ...newer.current!, id: 'scheduled-image', image_id: 2 };
    mock.Status.mockResolvedValue(newer);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3000);
    });
    expect(mock.Status).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('accepted-id')).toHaveTextContent('scheduled-image');
    expect(mock.Action).not.toHaveBeenCalled();
    view.unmount();
    expect(vi.getTimerCount()).toBe(0);
    mock.Status.mockClear();
    document.dispatchEvent(new Event('visibilitychange'));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6000);
    });
    expect(mock.Status).not.toHaveBeenCalled();
  } finally {
    view.unmount();
    vi.useRealTimers();
  }
});

it('does not accept a passive response after a newer same-instance status', async () => {
  const mock = installBridge(),
    client = createClient();
  await client.refresh();
  const pending = deferred<Awaited<ReturnType<typeof mock.CacheStatus>>>();
  mock.CacheStatus.mockReturnValue(pending.promise);
  const read = client.request('CacheStatus', [], { passive: true });
  await waitFor(() => expect(mock.CacheStatus).toHaveBeenCalled());
  const newer = structuredClone(snapshot);
  newer.status!.paused = !snapshot.status!.paused;
  mock.Status.mockResolvedValue(newer);
  await client.refresh({ background: true });
  pending.resolve(structuredClone(snapshot));
  await read;
  expect(client.getState().snapshot?.status?.paused).toBe(newer.status!.paused);
});
