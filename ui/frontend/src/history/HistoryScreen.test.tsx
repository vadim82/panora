import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { StrictMode } from 'react';
import { describe, expect, it } from 'vitest';
import { ClientProvider, usePanora } from '../client';
import { installBridge, snapshot } from '../test/fixtures';
import type {
  ExclusionsResult,
  HistoryEntry,
  HistoryResult,
  PreviewResult,
  Snapshot,
} from '../types';
import { OnboardingProvider } from '../onboarding/Onboarding';
import { HistoryScreen } from './HistoryScreen';

function Harness() {
  const ui = usePanora();
  return (
    <>
      <button onClick={() => ui.navigate('history')}>Open History</button>
      <button onClick={() => ui.navigate('now')}>Open Now</button>
      <button onClick={() => void ui.refresh({ background: true })}>Poll</button>
      <button onClick={() => void ui.reload()}>Reload settings</button>
      <output data-testid="ready">
        {ui.configuration?.revision}:{String(ui.busy)}
      </output>
      <output data-testid="blocked">{String(ui.blocked)}</output>
      <section hidden={ui.page !== 'history'}>
        <HistoryScreen />
      </section>
    </>
  );
}
function mount(strict = false) {
  const app = (
    <ClientProvider>
      <OnboardingProvider>
        <Harness />
      </OnboardingProvider>
    </ClientProvider>
  );
  return render(strict ? <StrictMode>{app}</StrictMode> : app);
}
async function openHistory() {
  await waitFor(() => expect(screen.getByTestId('ready')).toHaveTextContent('reviewed:false'));
  await userEvent.click(screen.getByRole('button', { name: 'Open History' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Reload history' })).toBeEnabled());
}
const first = structuredClone(snapshot.current!);
const old: HistoryEntry = {
  id: 'stable-older',
  image_id: 2,
  provider: 'wallhaven',
  image: 'abc123',
  availability: 'unavailable',
  reason: 'filter_mismatch',
  accepted_at: '2026-10-30T12:00:00Z',
  attribution: { title: '<script>Mountain</script>', page_url: 'https://wallhaven.cc/w/abc123' },
};
describe('History and Excluded', () => {
  it('shows the credited Commons file and license in History', async () => {
    const api = installBridge();
    api.History.mockResolvedValue({
      page: {
        entries: [
          {
            ...old,
            provider: 'wikimedia_commons',
            attribution: {
              title: 'Glen Coe Rainbow',
              author: '© User:Colin / Wikimedia Commons',
              license_name: 'CC BY-SA 4.0',
              license_url: 'https://creativecommons.org/licenses/by-sa/4.0/',
              page_url: 'https://commons.wikimedia.org/wiki/File:Glen_Coe_Rainbow.jpg',
            },
          },
        ],
      },
    });
    mount();
    await openHistory();
    expect(screen.getByText('© User:Colin / Wikimedia Commons · CC BY-SA 4.0')).toBeVisible();
    expect(screen.getByRole('button', { name: 'View on website: Glen Coe Rainbow' })).toBeEnabled();
  });
  it('labels a previously saved untitled Wallhaven entry in both lists', async () => {
    const mock = installBridge();
    mock.History.mockResolvedValue({
      page: {
        entries: [
          { ...old, image: '94x38z', attribution: { page_url: 'https://wallhaven.cc/w/94x38z' } },
        ],
      },
    });
    mock.Exclusions.mockResolvedValue({
      page: {
        entries: [
          {
            id: 9,
            provider: 'wallhaven',
            image: '94x38z',
            page_url: 'https://wallhaven.cc/w/94x38z',
            excluded_at: '2026-11-22T00:00:00Z',
          },
        ],
      },
    });
    mount();
    await openHistory();
    expect(screen.getByText('Wallhaven · 94x38z')).toBeVisible();
    expect(
      screen.getByRole('button', { name: 'View on website: Wallhaven · 94x38z' }),
    ).toBeEnabled();
    await userEvent.click(screen.getByRole('button', { name: 'Excluded' }));
    expect(await screen.findByText('Wallhaven · 94x38z')).toBeVisible();
  });
  it.each(['recent', 'excluded'] as const)(
    'keeps %s actions gated if reconnecting to the same service cannot reload its page',
    async (mode) => {
      const mock = installBridge(),
        user = userEvent.setup();
      mock.Exclusions.mockResolvedValue({
        page: {
          entries: [
            {
              id: 9,
              provider: 'local',
              image: 'Marked image',
              excluded_at: '2026-11-04T12:00:00Z',
            },
          ],
        },
      });
      mount();
      await openHistory();
      if (mode === 'excluded') {
        await user.click(screen.getByRole('button', { name: 'Excluded' }));
        await waitFor(() =>
          expect(
            screen.getByRole('button', { name: 'Restore to rotation: Marked image' }),
          ).toBeEnabled(),
        );
      }
      mock.Status.mockResolvedValue({ connected: false });
      await user.click(screen.getByRole('button', { name: 'Poll' }));
      await waitFor(() => expect(screen.getByText(/Disconnected · Showing/)).toBeVisible());
      const reader = mode === 'recent' ? mock.History : mock.Exclusions;
      reader.mockResolvedValue({ error: 'The page could not be checked.' });
      mock.Status.mockResolvedValue(structuredClone(snapshot));
      await user.click(screen.getByRole('button', { name: 'Poll' }));
      expect(await screen.findByText('The page could not be checked.')).toBeVisible();
      const action =
        mode === 'recent' ? 'Display again: coast.jpg' : 'Restore to rotation: Marked image';
      expect(screen.getByRole('button', { name: action })).toBeDisabled();
      expect(screen.getByRole('button', { name: 'Reload history' })).toBeEnabled();
      if (mode === 'recent') mock.History.mockResolvedValue({ page: { entries: [first] } });
      else
        mock.Exclusions.mockResolvedValue({
          page: {
            entries: [
              {
                id: 9,
                provider: 'local',
                image: 'Marked image',
                excluded_at: '2026-11-04T12:00:00Z',
              },
            ],
          },
        });
      mock.Status.mockResolvedValue({ connected: false });
      await user.click(screen.getByRole('button', { name: 'Poll' }));
      await waitFor(() => expect(screen.getByText(/Disconnected · Showing/)).toBeVisible());
      mock.Status.mockResolvedValue(structuredClone(snapshot));
      await user.click(screen.getByRole('button', { name: 'Poll' }));
      await waitFor(() => expect(screen.getByRole('button', { name: action })).toBeEnabled());
      expect(screen.queryByText('The page could not be checked.')).not.toBeInTheDocument();
      expect(mock.ReplayHistory).not.toHaveBeenCalled();
      expect(mock.RestoreExclusion).not.toHaveBeenCalled();
    },
  );

  it.each(['replay', 'restore', 'open'] as const)(
    'does not send an old %s action after joining a background refresh from a replacement service',
    async (action) => {
      const mock = installBridge(),
        user = userEvent.setup();
      mock.Exclusions.mockResolvedValue({
        page: {
          entries: [
            {
              id: 9,
              provider: 'local',
              image: 'Marked image',
              excluded_at: '2026-11-04T12:00:00Z',
            },
          ],
        },
      });
      mount();
      await openHistory();
      if (action === 'restore') {
        await user.click(screen.getByRole('button', { name: 'Excluded' }));
        await waitFor(() =>
          expect(
            screen.getByRole('button', { name: 'Restore to rotation: Marked image' }),
          ).toBeEnabled(),
        );
      }
      let finish: (result: Snapshot) => void = () => {};
      mock.Status.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      );
      await user.click(screen.getByRole('button', { name: 'Poll' }));
      const name =
        action === 'replay'
          ? 'Display again: coast.jpg'
          : action === 'restore'
            ? 'Restore to rotation: Marked image'
            : 'Show in folder: coast.jpg';
      await user.click(screen.getByRole('button', { name }));
      const replacement = {
        ...structuredClone(snapshot),
        status: { ...snapshot.status, instance_id: 'replacement-service' },
      };
      mock.Status.mockResolvedValue(replacement);
      await act(async () => finish(replacement));
      await waitFor(() =>
        expect(screen.getByRole('button', { name: 'Reload history' })).toBeEnabled(),
      );
      expect(mock.ReplayHistory).not.toHaveBeenCalled();
      expect(mock.RestoreExclusion).not.toHaveBeenCalled();
      expect(mock.OpenHistory).not.toHaveBeenCalled();
      expect(mock.Action).not.toHaveBeenCalled();
    },
  );

  it('reloads availability once after reconnect, waiting for foreground recovery to finish', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    mount();
    await openHistory();
    mock.Status.mockResolvedValue({ connected: false, error: 'Service stopped.' });
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    await waitFor(() => expect(screen.getByText(/Disconnected · Showing/)).toBeVisible());
    expect(screen.getByRole('button', { name: 'Display again: coast.jpg' })).toBeDisabled();
    mock.History.mockResolvedValue({
      page: {
        entries: [{ ...first, availability: 'unavailable', reason: 'original_unavailable' }],
      },
    });
    mock.Status.mockResolvedValue(structuredClone(snapshot));
    await user.click(screen.getByRole('button', { name: 'Reload settings' }));
    expect(await screen.findByText('The original file is unavailable.')).toBeVisible();
    expect(mock.History).toHaveBeenCalledTimes(2);
    expect(screen.getByRole('button', { name: 'Display again: coast.jpg' })).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    expect(mock.History).toHaveBeenCalledTimes(2);
    expect(mock.ReplayHistory).not.toHaveBeenCalled();
    expect(mock.Action).not.toHaveBeenCalled();
  });

  it('starts exclusions at the first page on a replacement service and gates stale marks after read failure', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    mock.Exclusions.mockImplementation(async (before) => ({
      page: {
        entries: [
          {
            id: before ? 4 : 9,
            provider: 'local',
            image: 'Old exclusion',
            excluded_at: '2026-11-04T12:00:00Z',
          },
        ],
        next_before: before ? 0 : 9,
      },
    }));
    mount();
    await openHistory();
    await user.click(screen.getByRole('button', { name: 'Excluded' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Older →' })).toBeEnabled());
    await user.click(screen.getByRole('button', { name: 'Older →' }));
    await waitFor(() => expect(screen.getByText('Page 2 · 1 image')).toBeVisible());
    let finish: (result: ExclusionsResult) => void = () => {};
    mock.Exclusions.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    mock.Status.mockResolvedValue({
      ...structuredClone(snapshot),
      status: { ...snapshot.status, instance_id: 'replacement-service' },
    });
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    await waitFor(() => expect(mock.Exclusions).toHaveBeenCalledTimes(3));
    expect(mock.Exclusions).toHaveBeenLastCalledWith(0);
    expect(
      screen.getByRole('button', { name: 'Restore to rotation: Old exclusion' }),
    ).toBeDisabled();
    await act(async () => finish({ error: 'Exclusions are temporarily unavailable.' }));
    expect(await screen.findByText('Exclusions are temporarily unavailable.')).toBeVisible();
    expect(
      screen.getByRole('button', { name: 'Restore to rotation: Old exclusion' }),
    ).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Reload history' })).toBeEnabled();
    mock.Exclusions.mockResolvedValue({
      page: {
        entries: [
          { id: 9, provider: 'local', image: 'New exclusion', excluded_at: '2026-11-04T12:00:00Z' },
        ],
      },
    });
    await user.click(screen.getByRole('button', { name: 'Reload history' }));
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Restore to rotation: New exclusion' }),
      ).toBeEnabled(),
    );
    expect(screen.getByText('Page 1 · 1 image')).toBeVisible();
    expect(mock.RestoreExclusion).not.toHaveBeenCalled();
    expect(mock.ReplayHistory).not.toHaveBeenCalled();
  });

  it('discards a page read that joined a status refresh from another service', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    let finishStatus: (result: Snapshot) => void = () => {},
      finishHistory: (result: HistoryResult) => void = () => {};
    mount();
    await waitFor(() => expect(screen.getByTestId('ready')).toHaveTextContent('reviewed:false'));
    mock.Status.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishStatus = resolve;
        }),
    );
    mock.History.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishHistory = resolve;
        }),
    );
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    await user.click(screen.getByRole('button', { name: 'Open History' }));
    await act(async () =>
      finishStatus({
        ...structuredClone(snapshot),
        status: { ...snapshot.status, instance_id: 'new-service' },
      }),
    );
    await waitFor(() => expect(mock.History).toHaveBeenCalledTimes(1));
    mock.History.mockResolvedValue({
      page: { entries: [{ ...first, original_path: '/New.jpg' }] },
    });
    await act(async () => finishHistory({ page: { entries: [{ ...old, image_id: 99 }] } }));
    await waitFor(() => expect(screen.getByRole('heading', { name: 'New.jpg' })).toBeVisible());
    expect(
      screen.queryByRole('heading', { name: '<script>Mountain</script>' }),
    ).not.toBeInTheDocument();
    expect(mock.History).toHaveBeenCalledTimes(2);
    expect(mock.Preview.mock.calls.map((call) => call[0])).not.toContain(99);
    expect(mock.ReplayHistory).not.toHaveBeenCalled();
  });

  it('waits for the new history page before reading thumbnails on return from Now', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    mock.Preview.mockResolvedValue({
      preview: { image_id: 1, available: true, mime_type: 'image/png', data: 'AAAA' },
    });
    mount();
    await openHistory();
    expect(await screen.findByRole('img', { name: 'Preview of coast.jpg' })).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Open Now' }));

    let finishHistory: (result: HistoryResult) => void = () => {};
    mock.History.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishHistory = resolve;
        }),
    );
    await user.click(screen.getByRole('button', { name: 'Open History' }));
    expect(mock.History).toHaveBeenCalledTimes(2);
    expect(mock.Preview).toHaveBeenCalledTimes(1);
    await act(async () => finishHistory({ page: { entries: [first] } }));
    expect(await screen.findByRole('img', { name: 'Preview of coast.jpg' })).toBeVisible();
    expect(mock.Preview.mock.calls).toEqual([[1], [1]]);
  });

  it('waits for the prior visit read to settle without using its stale thumbnail', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    let finishPreview: (result: PreviewResult) => void = () => {};
    mock.Preview.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finishPreview = resolve;
        }),
    );
    mock.Preview.mockResolvedValue({
      preview: { image_id: 1, available: true, mime_type: 'image/png', data: 'BBBB' },
    });
    mount();
    await openHistory();
    await waitFor(() => expect(mock.Preview).toHaveBeenCalledTimes(1));
    await user.click(screen.getByRole('button', { name: 'Open Now' }));
    await user.click(screen.getByRole('button', { name: 'Open History' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Reload history' })).toBeEnabled(),
    );
    expect(mock.Preview).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('img', { name: 'Preview of coast.jpg' })).not.toBeInTheDocument();

    await act(async () =>
      finishPreview({
        preview: { image_id: 1, available: true, mime_type: 'image/png', data: 'AAAA' },
      }),
    );
    expect(await screen.findByRole('img', { name: 'Preview of coast.jpg' })).toHaveAttribute(
      'src',
      'data:image/png;base64,BBBB',
    );
    expect(mock.Preview.mock.calls).toEqual([[1], [1]]);
  });

  it('distinguishes preview contention from a missing cached preview', async () => {
    const mock = installBridge();
    mock.History.mockResolvedValue({ page: { entries: [first, old] } });
    mock.Preview.mockImplementation(async (id) =>
      id === first.image_id
        ? { code: 'busy', error: 'The cache is busy.' }
        : { preview: { image_id: id, available: false, reason: 'not_cached' } },
    );
    mount();
    await openHistory();
    const blocked = screen.getByRole('listitem', { name: 'coast.jpg' }),
      missing = screen.getByRole('listitem', { name: '<script>Mountain</script>' });
    expect(
      await within(blocked).findByText('Preview busy. Reload history to try again.'),
    ).toBeVisible();
    expect(within(missing).getByText('Preview unavailable')).toBeVisible();
    expect(mock.Preview.mock.calls).toEqual([[1], [2]]);
  });

  it('discards late thumbnails when a new service reuses an image id', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    let finish: (result: PreviewResult) => void = () => {};
    mock.Preview.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    mount();
    await openHistory();
    await waitFor(() => expect(mock.Preview).toHaveBeenCalledTimes(1));
    mock.Preview.mockResolvedValue({
      preview: { image_id: 1, available: true, mime_type: 'image/png', data: 'BBBB' },
    });
    mock.History.mockResolvedValue({
      page: { entries: [{ ...first, original_path: '/New.jpg' }] },
    });
    mock.Status.mockResolvedValue({
      ...structuredClone(snapshot),
      status: { ...snapshot.status, instance_id: 'new-service' },
    });
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    await waitFor(() =>
      expect(screen.getByRole('img', { name: 'Preview of New.jpg' })).toHaveAttribute(
        'src',
        'data:image/png;base64,BBBB',
      ),
    );
    await act(async () =>
      finish({ preview: { image_id: 1, available: true, mime_type: 'image/png', data: 'AAAA' } }),
    );
    expect(screen.getByRole('img', { name: 'Preview of New.jpg' })).toHaveAttribute(
      'src',
      'data:image/png;base64,BBBB',
    );
    expect(mock.History).toHaveBeenCalledTimes(2);
    expect(mock.ReplayHistory).not.toHaveBeenCalled();
  });

  it('identifies each image in origin, recovery and restore actions without relying on surrounding visual cards', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    mock.History.mockResolvedValue({
      page: {
        entries: [
          first,
          { ...old, id: 'unavailable-source', reason: 'outside_enabled_inputs' },
          {
            ...first,
            id: 'excluded-image',
            image_id: 3,
            original_path: '/Pictures/Archive.jpg',
            availability: 'unavailable',
            reason: 'excluded',
          },
        ],
      },
    });
    mock.Exclusions.mockResolvedValue({
      page: {
        entries: [
          { id: 12, provider: 'local', image: 'Archive.jpg', excluded_at: '2026-11-01T12:00:00Z' },
          { id: 13, provider: 'local', image: 'Other.jpg', excluded_at: '2026-11-01T12:00:00Z' },
        ],
      },
    });
    mount();
    await openHistory();
    expect(screen.getByRole('listitem', { name: 'coast.jpg' })).toBeVisible();
    expect(screen.getByRole('listitem', { name: '<script>Mountain</script>' })).toBeVisible();
    expect(
      screen.getByRole('button', { name: 'Review sources: <script>Mountain</script>' }),
    ).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Manage excluded: Archive.jpg' })).toBeEnabled();
    await user.click(
      screen.getByRole('button', { name: 'View on website: <script>Mountain</script>' }),
    );
    expect(mock.OpenHistory).toHaveBeenCalledWith('unavailable-source', 2, 'page');
    await user.click(screen.getByRole('button', { name: 'Excluded' }));
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Restore to rotation: Archive.jpg' }),
      ).toBeEnabled(),
    );
    expect(screen.getByRole('button', { name: 'Restore to rotation: Other.jpg' })).toBeEnabled();
    expect(screen.getByRole('listitem', { name: 'Archive.jpg' })).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Restore to rotation: Other.jpg' }));
    expect(mock.RestoreExclusion).toHaveBeenCalledWith(13);
    expect(mock.ReplayHistory).not.toHaveBeenCalled();
    expect(mock.Action).not.toHaveBeenCalled();
  });

  it('uses stable handles, bounded scoped pages and fresh safe origin actions', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    mock.History.mockImplementation(async (before) => ({
      page: {
        entries: before
          ? [{ ...old, availability: 'unverified', reason: 'recheck_on_replay' }]
          : [first, old],
        next_before: before ? '' : 'stable-older',
      },
    }));
    mock.Preview.mockImplementation(async (id) => ({
      preview: { image_id: id, available: id === 1, mime_type: 'image/png', data: 'AAAA' },
    }));
    mount();
    await openHistory();
    expect(screen.getByRole('heading', { name: '<script>Mountain</script>' })).toBeVisible();
    expect(document.querySelector('script')).toBeNull();
    expect(screen.getByText('Does not match the current image rules.')).toBeVisible();
    const replay = screen.getAllByRole('button', { name: /^Display again:/ });
    expect(replay[0]).toBeEnabled();
    expect(replay[1]).toBeDisabled();
    expect(replay[0]).toHaveAccessibleName('Display again: coast.jpg');
    expect(replay[1]).toHaveAccessibleName('Display again: <script>Mountain</script>');
    expect(replay[1]).toHaveAccessibleDescription('Does not match the current image rules.');
    expect(
      screen.getByRole('button', { name: 'Review image settings: <script>Mountain</script>' }),
    ).toBeEnabled();
    expect(
      screen.getByRole('button', { name: 'View on website: <script>Mountain</script>' }),
    ).toBeEnabled();
    await waitFor(() => expect(mock.Preview.mock.calls.map((call) => call[0])).toEqual([1, 2]));
    await user.click(screen.getByRole('button', { name: 'Show in folder: coast.jpg' }));
    expect(mock.OpenHistory).toHaveBeenCalledWith(first.id, first.image_id, 'folder');
    await user.click(screen.getByRole('button', { name: 'Older →' }));
    await waitFor(() => expect(screen.getByText('Page 2 · 1 image')).toBeVisible());
    expect(mock.History).toHaveBeenLastCalledWith('stable-older');
    await user.click(
      screen.getByRole('button', { name: 'Display again: <script>Mountain</script>' }),
    );
    expect(mock.ReplayHistory).toHaveBeenCalledWith('stable-older');
    expect(mock.Action).not.toHaveBeenCalled();
  });
  it('preserves entries after pruned cursors and does not turn playback failures into settings locks', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    mock.History.mockResolvedValue({ page: { entries: [first], next_before: 'pruned' } });
    mount();
    await openHistory();
    mock.History.mockResolvedValueOnce({
      error: 'The history cursor was pruned. Reload history.',
      code: 'history_not_found',
    });
    await user.click(screen.getByRole('button', { name: 'Older →' }));
    expect(await screen.findByText(/cursor was pruned/)).toBeVisible();
    expect(screen.getByRole('heading', { name: 'coast.jpg' })).toBeVisible();
    expect(screen.getByText('Page 1 · 1 image')).toBeVisible();
    mock.Configuration.mockResolvedValue({ error: 'settings unavailable' });
    await user.click(screen.getByRole('button', { name: 'Reload settings' }));
    await waitFor(() => expect(screen.getByTestId('blocked')).toHaveTextContent('true'));
    mock.ReplayHistory.mockResolvedValue({
      ...structuredClone(snapshot),
      code: 'desktop_failed',
      error: 'Wallpaper could not be applied.',
    });
    const replay = screen.getByRole('button', { name: 'Display again: coast.jpg' });
    replay.focus();
    await user.keyboard('{Enter}');
    await waitFor(() => expect(screen.getByText('Wallpaper could not be applied.')).toBeVisible());
    expect(screen.getByTestId('blocked')).toHaveTextContent('true');
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Display again: coast.jpg' })).toHaveFocus(),
    );
    expect(mock.ReplayHistory).toHaveBeenCalledTimes(1);
  });
  it('pages exclusions independently of history and restores a mark outside retained history', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    let restored = false;
    mock.Exclusions.mockImplementation(async (before) => ({
      page: {
        entries: restored
          ? []
          : [
              {
                id: before ? 4 : 9,
                provider: 'local',
                image: before ? 'Outside retained history' : 'Recent exclusion',
                excluded_at: '2026-10-30T12:00:00Z',
              },
            ],
        next_before: before ? 0 : 9,
      },
    }));
    mock.RestoreExclusion.mockImplementation(async () => {
      restored = true;
      return structuredClone(snapshot);
    });
    mount();
    await openHistory();
    await user.click(screen.getByRole('button', { name: 'Excluded' }));
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Recent exclusion' })).toBeVisible(),
    );
    await user.click(screen.getByRole('button', { name: 'Older →' }));
    expect(await screen.findByRole('heading', { name: 'Outside retained history' })).toBeVisible();
    expect(mock.Exclusions).toHaveBeenLastCalledWith(9);
    const restore = screen.getByRole('button', {
      name: 'Restore to rotation: Outside retained history',
    });
    restore.focus();
    await user.keyboard('{Enter}');
    await waitFor(() => expect(screen.getByText(/No excluded images/)).toBeVisible());
    expect(mock.RestoreExclusion).toHaveBeenCalledWith(4);
    expect(mock.ReplayHistory).not.toHaveBeenCalled();
    expect(mock.Action).not.toHaveBeenCalled();
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'History', level: 1 })).toHaveFocus(),
    );
  });
  it('reconciles a lost replay response with reads and never repeats the mutation', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    mount();
    await openHistory();
    mock.ReplayHistory.mockRejectedValue(new Error('lost response'));
    await user.click(screen.getByRole('button', { name: 'Display again: coast.jpg' }));
    expect(await screen.findByText(/action may have completed/)).toBeVisible();
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Display again: coast.jpg' })).toBeEnabled(),
    );
    expect(mock.ReplayHistory).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('blocked')).toHaveTextContent('false');
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    expect(mock.ReplayHistory).toHaveBeenCalledTimes(1);
    expect(mock.Action).not.toHaveBeenCalled();
  });
  it('refreshes availability after navigation and cancels obsolete thumbnail work', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    let finish: (result: HistoryResult) => void = () => {};
    mock.History.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    mount(true);
    await waitFor(() => expect(screen.getByTestId('ready')).toHaveTextContent('reviewed:false'));
    await user.click(screen.getByRole('button', { name: 'Open History' }));
    await waitFor(() => expect(mock.History).toHaveBeenCalledTimes(1));
    await user.click(screen.getByRole('button', { name: 'Open Now' }));
    await act(async () => finish({ page: { entries: [first] } }));
    expect(mock.Preview).not.toHaveBeenCalled();
    mock.History.mockResolvedValue({
      page: { entries: [{ ...first, availability: 'unavailable', reason: 'excluded' }] },
    });
    await user.click(screen.getByRole('button', { name: 'Open History' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Display again: coast.jpg' })).toBeDisabled(),
    );
    await user.click(screen.getByRole('button', { name: 'Open Now' }));
    mock.History.mockResolvedValue({ page: { entries: [first] } });
    await user.click(screen.getByRole('button', { name: 'Open History' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Display again: coast.jpg' })).toBeEnabled(),
    );
    const list = screen.getByRole('list', { name: 'Recently displayed wallpapers' });
    expect(within(list).getAllByRole('listitem')).toHaveLength(1);
  });
});
