import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { ClientProvider, usePanora } from '../client';
import { ImageSettingsProvider } from '../settings/ImageSettings';
import { configuration, installBridge, snapshot } from '../test/fixtures';
import type { PreviewResult, Snapshot } from '../types';
import { NowScreen } from './NowScreen';
import { OnboardingProvider } from '../onboarding/Onboarding';

function Harness() {
  const ui = usePanora();
  return (
    <>
      <button onClick={() => void ui.refresh({ background: true })}>Poll</button>
      <button onClick={() => ui.navigate('history')}>Leave Now</button>
      <button onClick={() => ui.navigate('now')}>Return to Now</button>
      <output data-testid="page">{ui.page}</output>
      <p role="alert">{ui.snapshot?.error}</p>
      <section hidden={ui.page !== 'now'}>
        <NowScreen />
      </section>
    </>
  );
}
function mount() {
  return render(
    <ClientProvider>
      <ImageSettingsProvider>
        <OnboardingProvider>
          <Harness />
        </OnboardingProvider>
      </ImageSettingsProvider>
    </ClientProvider>,
  );
}
async function ready() {
  await waitFor(() => expect(screen.getByRole('button', { name: 'Next wallpaper' })).toBeEnabled());
}
describe('Now', () => {
  it('saves from Image actions, remembers the folder and reports an already saved wallpaper', async () => {
    const mock = installBridge();
    const user = userEvent.setup();
    const path = '/Pictures/Favorites';
    mock.PickSaveFolder.mockResolvedValueOnce({ canceled: true }).mockResolvedValue({ path });
    mock.UpdateConfiguration.mockResolvedValue({
      configuration: { ...configuration, revision: 'changed', save_folder: path },
    });
    mock.SaveWallpaper.mockResolvedValueOnce({
      saved: true,
      folder_name: 'Favorites',
    }).mockResolvedValueOnce({ saved: true, already_saved: true, folder_name: 'Favorites' });
    mount();
    await ready();
    const menu = screen.getByLabelText('Image actions');
    const disclosure = menu.closest('details');
    expect(disclosure).toContainElement(screen.getByRole('button', { name: 'Save' }));
    expect(disclosure?.open).toBe(false);
    menu.focus();
    await user.keyboard('{Enter}{Tab}');
    const save = screen.getByRole('button', { name: 'Save' });
    expect(save).toHaveFocus();
    await user.keyboard('{Enter}');
    expect(menu).toHaveFocus();
    expect(menu.closest('details')?.open).toBe(false);
    expect(mock.SaveWallpaper).not.toHaveBeenCalled();
    expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
    await user.click(menu);
    await user.click(save);
    expect(await screen.findByText('Saved to Favorites')).toBeVisible();
    await waitFor(() =>
      expect(mock.UpdateConfiguration).toHaveBeenCalledWith({
        revision: configuration.revision,
        save_folder: path,
      }),
    );
    await user.click(menu);
    await waitFor(() => expect(save).toBeEnabled());
    await user.click(save);
    expect(await screen.findByText('Already saved')).toBeVisible();
    expect(mock.PickSaveFolder).toHaveBeenCalledTimes(2);
    expect(mock.SaveWallpaper).toHaveBeenLastCalledWith(
      expect.objectContaining({ revision: 'changed', folder: undefined }),
    );
  });

  it('sends the reviewed wallpaper identity after a picker and avoids remembering a failed save', async () => {
    const mock = installBridge();
    const user = userEvent.setup();
    const current = snapshot.status!.current!;
    let release!: (folder: { path: string }) => void;
    mock.PickSaveFolder.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = resolve;
        }),
    );
    mock.SaveWallpaper.mockResolvedValueOnce({
      error: 'The wallpaper changed. Review the current wallpaper and try again.',
      code: 'current_changed',
    });
    mount();
    await ready();
    await user.click(screen.getByLabelText('Image actions'));
    await user.click(screen.getByRole('button', { name: 'Save' }));
    const latest = structuredClone(snapshot);
    latest.status!.current = { ...current, id: 'newly-accepted', image_id: 2 };
    mock.Status.mockResolvedValue(latest);
    await act(() => release({ path: '/Pictures/Favorites' }));
    expect(await screen.findByText(/The wallpaper changed/)).toBeVisible();
    expect(mock.SaveWallpaper).toHaveBeenCalledWith(
      expect.objectContaining({ history: current.id, image_id: current.image_id }),
    );
    expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
  });

  it.each(['previous', 'next'] as const)(
    'recovers a persisted uncertain apply through explicit %s',
    async (action) => {
      const mock = installBridge();
      const uncertain = structuredClone(snapshot);
      uncertain.status!.uncertain = true;
      if (action === 'previous') {
        uncertain.status!.history_cursor = 1;
        uncertain.status!.history_length = 2;
      } else {
        delete uncertain.status!.current;
        delete uncertain.current;
        uncertain.status!.history_cursor = -1;
        uncertain.status!.history_length = 0;
      }
      mock.Status.mockResolvedValue(uncertain);
      mock.Action.mockResolvedValue(structuredClone(snapshot));
      mount();
      await screen.findByRole('heading', { name: 'Wallpaper change unconfirmed' });
      const recover = screen.getByRole('button', { name: new RegExp(`${action} wallpaper`, 'i') });
      await waitFor(() => expect(recover).toBeEnabled());
      const resume = screen.getByRole('button', {
        name: action === 'previous' ? 'Resume rotation' : 'Start wallpapers',
      });
      expect(resume).toBeDisabled();
      expect(mock.Action).not.toHaveBeenCalled();
      await userEvent.click(recover);
      expect(mock.Action).toHaveBeenCalledExactlyOnceWith(action);
      await waitFor(() =>
        expect(screen.queryByRole('heading', { name: 'Wallpaper change unconfirmed' })).toBeNull(),
      );
      expect(screen.getByRole('button', { name: 'Resume rotation' })).toBeEnabled();
    },
  );
  it.each([
    ['Wallhaven', '7680 × 2160 · 32:9 · Wallhaven'],
    ['Wide landscapes', '7680 × 2160 · 32:9 · Wide landscapes · Wallhaven'],
  ])('shows the Wallhaven ID and distinct details for %s', async (searchName, details) => {
    const mock = installBridge();
    const saved = structuredClone(snapshot);
    saved.current = {
      ...saved.current!,
      provider: 'wallhaven',
      image: '94x38z',
      source_id: 'search-1',
      dimensions: { width: 7680, height: 2160 },
      original_path: undefined,
      attribution: { page_url: 'https://wallhaven.cc/w/94x38z' },
    };
    mock.Status.mockResolvedValue(saved);
    mock.Configuration.mockResolvedValue({
      configuration: {
        ...configuration,
        searches: [
          {
            id: 'search-1',
            name: searchName,
            query: 'landscape',
            enabled: true,
            input_enabled: true,
            categories: '111',
            sorting: 'toplist',
            order: 'desc',
            top_range: '1M',
            max_pages: 1,
            purity: '100',
            refresh_interval: '5m',
          },
        ],
      },
    });
    mount();
    await ready();
    expect(screen.getByRole('heading', { name: '94x38z' })).toBeVisible();
    expect(await screen.findByText(details)).toBeVisible();
    expect(screen.getByRole('button', { name: /website/i })).toBeInTheDocument();
  });
  it('clears the loading state when returning to a cached image before another preview finishes', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    const original = structuredClone(snapshot);
    let current = original;
    let finish: (value: PreviewResult) => void = () => {};
    mock.Status.mockImplementation(async () => structuredClone(current));
    mock.Preview.mockImplementation(async (imageID) =>
      imageID === 1
        ? { preview: { image_id: 1, available: true, mime_type: 'image/png', data: 'AAAA' } }
        : new Promise((resolve) => {
            finish = resolve;
          }),
    );
    mount();
    await ready();
    await screen.findByRole('img', { name: 'Wallpaper: coast.jpg' });
    current = {
      ...original,
      status: { ...original.status, current: { id: 'accepted-2', image_id: 2 } },
      current: {
        ...original.current!,
        id: 'accepted-2',
        image_id: 2,
        original_path: '/Pictures/forest.jpg',
      },
    };
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    await waitFor(() => expect(mock.Preview).toHaveBeenCalledTimes(2));
    expect(document.getElementById('preview-frame')).toHaveAttribute('aria-busy', 'true');
    current = original;
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    expect(await screen.findByRole('img', { name: 'Wallpaper: coast.jpg' })).toHaveAttribute(
      'src',
      'data:image/png;base64,AAAA',
    );
    await waitFor(() =>
      expect(document.getElementById('preview-frame')).toHaveAttribute('aria-busy', 'false'),
    );
    await act(async () =>
      finish({ preview: { image_id: 2, available: true, mime_type: 'image/png', data: 'BBBB' } }),
    );
    expect(screen.getByRole('img', { name: 'Wallpaper: coast.jpg' })).toHaveAttribute(
      'src',
      'data:image/png;base64,AAAA',
    );
    expect(mock.Preview).toHaveBeenCalledTimes(2);
    expect(mock.Action).not.toHaveBeenCalled();
  });

  it('sends playback controls to the bridge and renders the accepted image and pause state', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    let current = structuredClone(snapshot);
    const coast = structuredClone(snapshot.current!);
    const forest = {
      ...coast,
      id: 'accepted-2',
      image_id: 2,
      original_path: '/Pictures/Wallpapers/forest.jpg',
    };
    mock.Status.mockImplementation(async () => structuredClone(current));
    mock.Action.mockImplementation(async (name) => {
      if (name === 'next')
        current = {
          ...current,
          current: forest,
          status: {
            ...current.status,
            current: { id: forest.id, image_id: forest.image_id },
            history_cursor: 1,
            history_length: 2,
          },
        };
      if (name === 'previous')
        current = {
          ...current,
          current: coast,
          status: {
            ...current.status,
            current: { id: coast.id, image_id: coast.image_id },
            history_cursor: 0,
          },
        };
      if (name === 'resume')
        current = {
          ...current,
          status: { ...current.status, paused: false, next_at: '2026-11-01T13:00:00Z' },
        };
      if (name === 'pause') current = { ...current, status: { ...current.status, paused: true } };
      return structuredClone(current);
    });
    mock.Preview.mockImplementation(async (imageID) => ({
      preview: {
        image_id: imageID,
        available: true,
        mime_type: 'image/png',
        data: imageID === 1 ? 'AAAA' : 'BBBB',
      },
    }));
    mount();
    await ready();
    expect(await screen.findByRole('img', { name: 'Wallpaper: coast.jpg' })).toHaveAttribute(
      'src',
      'data:image/png;base64,AAAA',
    );
    expect(screen.getByRole('button', { name: 'Previous wallpaper' })).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'Next wallpaper' }));
    expect(await screen.findByRole('heading', { name: 'forest.jpg' })).toBeVisible();
    expect(await screen.findByRole('img', { name: 'Wallpaper: forest.jpg' })).toHaveAttribute(
      'src',
      'data:image/png;base64,BBBB',
    );
    expect(screen.getByText('Rotation paused')).toBeVisible();
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Previous wallpaper' })).toBeEnabled(),
    );
    await user.click(screen.getByRole('button', { name: 'Previous wallpaper' }));
    expect(await screen.findByRole('img', { name: 'Wallpaper: coast.jpg' })).toHaveAttribute(
      'src',
      'data:image/png;base64,AAAA',
    );
    expect(screen.getByRole('button', { name: 'Previous wallpaper' })).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'Resume rotation' }));
    expect(await screen.findByRole('button', { name: 'Pause rotation' })).toBeEnabled();
    expect(screen.getByText(/Next change at/)).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Pause rotation' }));
    expect(await screen.findByRole('button', { name: 'Resume rotation' })).toBeEnabled();
    expect(screen.getByText('Rotation paused')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    expect(mock.Action.mock.calls.map(([name]) => name)).toEqual([
      'next',
      'previous',
      'resume',
      'pause',
    ]);
    expect(screen.getByRole('img', { name: 'Wallpaper: coast.jpg' })).toBeVisible();
  });
  it('loads adjacent cached thumbnails by image ID and keeps keyboard navigation available without them', async () => {
    const mock = installBridge();
    const user = userEvent.setup();
    const status = structuredClone(snapshot.status!);
    mock.Status.mockResolvedValue({
      ...snapshot,
      status: {
        ...status,
        history_cursor: 1,
        history_length: 3,
        previous_history: { id: 'older', image_id: 2 },
        forward_history: { id: 'newer', image_id: 3 },
      },
    });
    mock.Preview.mockImplementation(async (imageID) => ({
      preview: {
        image_id: imageID,
        available: imageID !== 2,
        mime_type: 'image/png',
        data: imageID === 3 ? 'BBBB' : 'AAAA',
      },
    }));
    mock.Action.mockResolvedValue({
      ...snapshot,
      status: { ...status, history_cursor: 2, history_length: 3 },
    });
    mount();
    await ready();
    const previous = screen.getByRole('button', { name: 'Previous wallpaper' });
    const next = screen.getByRole('button', { name: 'Next wallpaper' });
    expect(previous).toBeEnabled();
    expect(previous.querySelector('.neighbor-chevron')).toBeInTheDocument();
    await waitFor(() =>
      expect(next.querySelector('img')).toHaveAttribute('src', 'data:image/png;base64,BBBB'),
    );
    expect(next.querySelector('.neighbor-chevron')).toBeInTheDocument();
    expect(mock.Preview.mock.calls.map(([id]) => id).sort()).toEqual([1, 2, 3]);
    expect(mock.Action).not.toHaveBeenCalled();
    expect(mock.History).not.toHaveBeenCalled();
    expect(mock.PreviewNeighbor.mock.calls).toEqual([
      [2, 'older'],
      [3, 'newer'],
    ]);

    screen.getByRole('group', { name: 'Wallpaper navigation' }).focus();
    await user.keyboard('{ArrowRight}');
    expect(mock.Action).toHaveBeenCalledExactlyOnceWith('next');
    expect(next.querySelector('img')).toBeNull();
    expect(next.querySelector('.neighbor-chevron')).toBeInTheDocument();
  });

  it('moves a decoded neighbor into view while both the center read and apply are pending', async () => {
    const mock = installBridge();
    const initial = structuredClone(snapshot);
    initial.status!.history_cursor = 1;
    initial.status!.history_length = 2;
    initial.status!.previous_history = { id: 'older', image_id: 2 };
    mock.Status.mockResolvedValue(initial);
    mock.Preview.mockReturnValue(new Promise(() => {}));
    mock.PreviewNeighbor.mockResolvedValueOnce({ code: 'busy' }).mockResolvedValue({
      preview: { image_id: 2, available: true, mime_type: 'image/png', data: 'BBBB' },
    });
    mock.Action.mockReturnValue(new Promise(() => {}));
    mount();
    await ready();

    const previous = screen.getByRole('button', { name: 'Previous wallpaper' });
    const neighbor = await waitFor(() => {
      const image = previous.querySelector('img');
      expect(image).toHaveAttribute('src', 'data:image/png;base64,BBBB');
      return image!;
    });
    expect(screen.getByText('Loading image…')).toBeVisible();
    fireEvent.load(neighbor);
    fireEvent.click(previous);
    expect(mock.Action).toHaveBeenCalledExactlyOnceWith('previous');
    expect(document.querySelector('.wallpaper-optimistic')).toHaveAttribute('data-ready', 'true');
    expect(screen.getByText('Applying wallpaper…')).toBeVisible();
    expect(mock.PreviewNeighbor).toHaveBeenCalledTimes(2);
  });

  it('shows a decoded prepared neighbor immediately, tracks the persisted pending ID, and rolls back on failure', async () => {
    const mock = installBridge();
    const initial = structuredClone(snapshot);
    initial.status!.prepared_next_image_id = 9;
    let finish!: (value: Snapshot) => void;
    mock.Status.mockResolvedValue(initial);
    mock.Preview.mockImplementation(async (id) => ({
      preview: {
        image_id: id,
        available: true,
        mime_type: 'image/png',
        data: id === 9 ? 'BBBB' : id === 10 ? 'CCCC' : 'AAAA',
      },
    }));
    mock.Action.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    mount();
    await ready();
    const next = screen.getByRole('button', { name: 'Next wallpaper' });
    await waitFor(() =>
      expect(next.querySelector('img')).toHaveAttribute('src', 'data:image/png;base64,BBBB'),
    );
    fireEvent.load(next.querySelector('img')!);
    const remote = structuredClone(initial);
    remote.status!.pending_image_id = 10;
    mock.Status.mockResolvedValue(remote);
    const deck = screen.getByRole('group', { name: 'Wallpaper navigation' });
    deck.focus();
    fireEvent.keyDown(deck, { key: 'ArrowRight' });
    fireEvent.keyDown(deck, { key: 'ArrowRight' });
    expect(mock.Action).toHaveBeenCalledTimes(1);
    expect(screen.getByText('Applying wallpaper…')).toBeVisible();
    expect(document.querySelector('.wallpaper-optimistic')).toHaveAttribute('data-ready', 'true');
    await waitFor(() => expect(mock.Preview).toHaveBeenCalledWith(10));
    const pendingImage = await waitFor(() => {
      const image = document.querySelector('.wallpaper-optimistic');
      expect(image).toHaveAttribute('src', 'data:image/png;base64,CCCC');
      return image!;
    });
    fireEvent.load(pendingImage);
    expect(pendingImage).toHaveAttribute('data-ready', 'true');
    expect(screen.getByAltText(/Wallpaper:/)).toHaveAttribute('src', 'data:image/png;base64,AAAA');
    await act(async () => finish({ ...initial, code: 'selection_failed', error: 'Failed' }));
    await waitFor(() => expect(document.querySelector('.wallpaper-optimistic')).toBeNull());
    expect(deck).toHaveFocus();
  });

  it.each([9, 1])(
    'keeps image %s through confirmed acceptance until its preview loads',
    async (targetID) => {
      const mock = installBridge();
      const initial = structuredClone(snapshot);
      initial.status!.prepared_next_image_id = targetID;
      mock.Status.mockResolvedValue(initial);
      let finish!: (value: Snapshot) => void;
      let finishPreview!: (value: PreviewResult) => void;
      let targetReads = 0;
      mock.Preview.mockImplementation(async (id) => {
        if (id === targetID && ++targetReads === (targetID === 1 ? 3 : 2))
          return new Promise((resolve) => {
            finishPreview = resolve;
          });
        return {
          preview: {
            image_id: id,
            available: true,
            mime_type: 'image/png',
            data: id === 1 ? 'AAAA' : 'BBBB',
          },
        };
      });
      mock.Action.mockImplementation(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      );
      mount();
      await ready();
      fireEvent.load(await screen.findByAltText(/Wallpaper:/));
      const next = screen.getByRole('button', { name: 'Next wallpaper' });
      await waitFor(() => expect(next.querySelector('img')).not.toBeNull());
      fireEvent.load(next.querySelector('img')!);
      fireEvent.click(next);
      expect(document.querySelector('.wallpaper-optimistic')).toHaveAttribute('data-ready', 'true');
      const accepted = structuredClone(initial);
      accepted.status!.current = { id: 'accepted-new', image_id: targetID };
      accepted.current = { ...accepted.current!, id: 'accepted-new', image_id: targetID };
      await act(async () => finish(accepted));
      await waitFor(() => expect(finishPreview).toBeDefined());
      expect(document.querySelector('.wallpaper-optimistic')).toHaveAttribute('data-ready', 'true');
      await act(async () =>
        finishPreview({
          preview: {
            image_id: targetID,
            available: true,
            mime_type: 'image/png',
            data: targetID === 1 ? 'AAAA' : 'BBBB',
          },
        }),
      );
      expect(document.querySelector('.wallpaper-optimistic')).not.toBeNull();
      fireEvent.load(await screen.findByAltText(/Wallpaper:/));
      await waitFor(() => expect(document.querySelector('.wallpaper-optimistic')).toBeNull());
      expect(document.querySelector('.wallpaper-departing')).toHaveAttribute('hidden');
    },
  );

  it('promotes decoded thumbnails and reuses the departing center until eligibility changes', async () => {
    const mock = installBridge();
    const initial = structuredClone(snapshot);
    initial.status!.current_preview_content = 'first-content';
    initial.status!.prepared_next_image_id = 9;
    initial.status!.prepared_next_preview_content = 'second-content';
    mock.Status.mockResolvedValue(initial);
    mock.Preview.mockImplementation(async (id) => ({
      preview: {
        image_id: id,
        content_revision: id === 1 ? 'first-content' : 'second-content',
        available: true,
        mime_type: 'image/png',
        data: id === 1 ? 'AAAA' : 'BBBB',
      },
    }));
    const accepted = structuredClone(snapshot);
    accepted.status!.current = { id: 'accepted-2', image_id: 9 };
    accepted.status!.current_preview_content = 'second-content';
    accepted.status!.previous_history = {
      id: 'accepted-1',
      image_id: 1,
      content_revision: 'first-content',
    };
    accepted.status!.history_cursor = 1;
    accepted.status!.history_length = 2;
    let finish!: (result: Snapshot) => void;
    mock.Action.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    mount();
    await ready();
    fireEvent.load(await screen.findByAltText(/Wallpaper:/));
    const next = screen.getByRole('button', { name: 'Next wallpaper' });
    await waitFor(() => expect(next.querySelector('img')).not.toBeNull());
    fireEvent.load(next.querySelector('img')!);
    fireEvent.click(next);
    expect(document.querySelector('.wallpaper-optimistic')).toHaveAttribute('data-ready', 'true');
    await act(async () => finish(accepted));
    await waitFor(() =>
      expect(screen.getByAltText(/Wallpaper:/)).toHaveAttribute(
        'src',
        'data:image/png;base64,BBBB',
      ),
    );
    const previous = screen.getByRole('button', { name: 'Previous wallpaper' });
    expect(previous.querySelector('img')).toHaveAttribute('src', 'data:image/png;base64,AAAA');
    expect(mock.Preview.mock.calls).toEqual([[1], [9]]);
    expect(mock.PreviewNeighbor).not.toHaveBeenCalled();

    accepted.status!.history_preview_revision = 1;
    mock.Status.mockResolvedValue(accepted);
    mock.PreviewNeighbor.mockResolvedValue({
      preview: { image_id: 1, available: false, reason: 'history_unavailable' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Poll' }));
    await waitFor(() => expect(mock.PreviewNeighbor).toHaveBeenCalledWith(1, 'accepted-1'));
    expect(previous.querySelector('img')).toBeNull();
  });

  it('shows the next queued target and skips a superseded preparation', async () => {
    const mock = installBridge();
    const initial = structuredClone(snapshot);
    initial.status!.navigation_supported = true;
    initial.status!.current_preview_content = 'first-content';
    initial.status!.prepared_next_image_id = 9;
    initial.status!.prepared_next_preview_content = 'second-content';
    initial.status!.prepared_beyond = { id: '', image_id: 10, content_revision: 'third-content' };
    mock.Status.mockResolvedValue(initial);
    mock.Preview.mockImplementation(async (id) => ({
      preview: {
        image_id: id,
        content_revision:
          id === 1 ? 'first-content' : id === 9 ? 'second-content' : 'third-content',
        available: true,
        mime_type: 'image/png',
        data: id === 1 ? 'AAAA' : id === 9 ? 'BBBB' : 'CCCC',
      },
    }));
    const completions: ((value: Snapshot) => void)[] = [];
    mock.Navigate.mockImplementation(
      () =>
        new Promise((resolve) => {
          completions.push(resolve);
        }),
    );
    mount();
    await ready();
    fireEvent.load(await screen.findByAltText(/Wallpaper:/));
    const next = screen.getByRole('button', { name: 'Next wallpaper' });
    await waitFor(() => expect(next.querySelector('img')).not.toBeNull());
    fireEvent.load(next.querySelector('img')!);
    fireEvent.click(next);
    expect(document.querySelector('.wallpaper-optimistic[data-ready="true"]')).toHaveAttribute(
      'src',
      'data:image/png;base64,BBBB',
    );
    const later = await waitFor(() => {
      const image = next.querySelector('img');
      expect(image).toHaveAttribute('src', 'data:image/png;base64,CCCC');
      return image!;
    });
    fireEvent.load(later);
    fireEvent.click(next);
    expect(document.querySelector('.wallpaper-optimistic[data-ready="true"]')).toHaveAttribute(
      'src',
      'data:image/png;base64,CCCC',
    );
    expect(screen.getByAltText(/Wallpaper:/)).toHaveAttribute('src', 'data:image/png;base64,AAAA');
    await waitFor(() =>
      expect(mock.CancelNavigation).toHaveBeenCalledExactlyOnceWith(
        initial.status!.instance_id,
        mock.Navigate.mock.calls[0][0].token,
      ),
    );
    await act(async () =>
      completions[0]({ ...initial, code: 'navigation_superseded', error: 'Superseded' }),
    );
    await waitFor(() => expect(mock.Navigate).toHaveBeenCalledTimes(2));
    expect(mock.Navigate.mock.calls.map(([r]) => [r.current, r.offset])).toEqual([
      ['accepted-1', 1],
      ['accepted-1', 2],
    ]);
    const accepted = structuredClone(initial);
    accepted.status!.current = { id: 'accepted-2', image_id: 10 };
    accepted.status!.current_preview_content = 'third-content';
    accepted.status!.history_cursor = 1;
    accepted.current = { ...accepted.current!, id: 'accepted-2', image_id: 10 };
    await act(async () => completions[1](accepted));
    expect(screen.getByAltText(/Wallpaper:/)).toHaveAttribute('src', 'data:image/png;base64,CCCC');
    expect(mock.Action).not.toHaveBeenCalled();
    expect(mock.Preview.mock.calls.filter(([id]) => id === 10)).toHaveLength(1);
  });

  it.each([false, true])(
    'reconciles a reversal after dispatch, with uncertain apply = %s',
    async (uncertain) => {
      const mock = installBridge();
      const initial = structuredClone(snapshot);
      initial.status!.navigation_supported = true;
      mock.Status.mockResolvedValue(initial);
      const completions: ((value: Snapshot) => void)[] = [];
      mock.Navigate.mockImplementation(
        () =>
          new Promise((resolve) => {
            completions.push(resolve);
          }),
      );
      mount();
      await ready();
      fireEvent.click(screen.getByRole('button', { name: 'Next wallpaper' }));
      const previous = screen.getByRole('button', { name: 'Previous wallpaper' });
      expect(previous).toBeEnabled();
      fireEvent.click(previous);
      expect(mock.Navigate).toHaveBeenCalledTimes(1);
      const result = structuredClone(initial);
      if (uncertain) {
        result.code = 'apply_uncertain';
        result.error = 'Desktop acknowledgement lost';
        result.status!.uncertain = true;
      } else {
        result.status!.current = { id: 'accepted-2', image_id: 2 };
        result.status!.history_cursor = 1;
        result.current = { ...result.current!, id: 'accepted-2', image_id: 2 };
      }
      await act(async () => completions[0](result));
      if (uncertain) {
        expect(
          await screen.findByText('Wallpaper change unconfirmed', { selector: 'h2' }),
        ).toBeVisible();
        expect(mock.Navigate).toHaveBeenCalledTimes(1);
      } else {
        await waitFor(() => expect(mock.Navigate).toHaveBeenCalledTimes(2));
        expect(mock.Navigate.mock.calls[1][0]).toMatchObject({ current: 'accepted-2', offset: -1 });
        await act(async () => completions[1](initial));
      }
    },
  );

  it('drops a speculative preview when another image is accepted', async () => {
    const mock = installBridge();
    const initial = structuredClone(snapshot);
    initial.status!.prepared_next_image_id = 9;
    mock.Status.mockResolvedValue(initial);
    mock.Preview.mockImplementation(async (id) => ({
      preview: { image_id: id, available: true, mime_type: 'image/png', data: 'AAAA' },
    }));
    let finish!: (value: Snapshot) => void;
    mock.Action.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    mount();
    await ready();
    const next = screen.getByRole('button', { name: 'Next wallpaper' });
    await waitFor(() => expect(next.querySelector('img')).not.toBeNull());
    fireEvent.load(next.querySelector('img')!);
    fireEvent.click(next);
    expect(document.querySelector('.wallpaper-optimistic')).not.toBeNull();
    const result = structuredClone(initial);
    result.status!.current = { id: 'different', image_id: 10 };
    await act(async () => finish(result));
    await waitFor(() => expect(document.querySelector('.wallpaper-optimistic')).toBeNull());
  });

  it('retries busy accepted and prepared previews without waiting for another status change', async () => {
    const mock = installBridge();
    const initial = structuredClone(snapshot);
    initial.status!.prepared_next_image_id = 9;
    mock.Status.mockResolvedValue(initial);
    const attempts = new Map<number, number>();
    mock.Preview.mockImplementation(async (id) => {
      const count = (attempts.get(id) || 0) + 1;
      attempts.set(id, count);
      return count < 3
        ? { code: 'busy' }
        : { preview: { image_id: id, available: true, mime_type: 'image/png', data: 'AAAA' } };
    });
    mount();
    await ready();
    await waitFor(() => expect(screen.getByAltText(/Wallpaper:/)).toBeVisible());
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: 'Next wallpaper' }).querySelector('img'),
      ).not.toBeNull(),
    );
    expect(attempts.get(1)).toBe(3);
    expect(attempts.get(9)).toBe(3);
  });

  it('keeps loading and recovers an accepted preview after temporary request errors', async () => {
    const mock = installBridge();
    mock.Preview.mockResolvedValueOnce({ code: 'timeout' });
    mock.Preview.mockResolvedValueOnce({ code: 'request_failed' });
    mock.Preview.mockResolvedValueOnce({ code: 'busy' });
    mount();
    await ready();
    await waitFor(() => expect(mock.Preview).toHaveBeenCalled());
    expect(screen.queryByText('Preview unavailable')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Retry preview' })).not.toBeInTheDocument();
    expect(await screen.findByRole('img', { name: 'Wallpaper: coast.jpg' })).toBeVisible();
    expect(mock.Preview).toHaveBeenCalledTimes(4);
    expect(mock.Action).not.toHaveBeenCalled();
  });

  it('reloads a prepared image when its generation changes without dropping Previous', async () => {
    const mock = installBridge();
    const initial = structuredClone(snapshot);
    initial.status!.prepared_next_image_id = 9;
    initial.status!.prepared_next_revision = 1;
    initial.status!.previous_history = { id: 'older', image_id: 2 };
    mock.PreviewNeighbor.mockResolvedValue({
      preview: { image_id: 2, available: true, mime_type: 'image/png', data: 'BBBB' },
    });
    mock.Status.mockResolvedValue(initial);
    mount();
    await ready();
    const previous = screen.getByRole('button', { name: 'Previous wallpaper' });
    await waitFor(() => expect(previous.querySelector('img')).not.toBeNull());
    await waitFor(() => expect(mock.Preview).toHaveBeenCalledWith(9));
    mock.Preview.mockClear();
    const updated = structuredClone(initial);
    updated.status!.prepared_next_revision = 2;
    mock.Status.mockResolvedValue(updated);
    fireEvent.click(screen.getByRole('button', { name: 'Poll' }));
    await waitFor(() => expect(mock.Preview).toHaveBeenCalledWith(9));
    expect(previous.querySelector('img')).toHaveAttribute('src', 'data:image/png;base64,BBBB');
    expect(mock.PreviewNeighbor).toHaveBeenCalledExactlyOnceWith(2, 'older');
  });

  it('ignores a prepared image from an older configuration revision', async () => {
    const mock = installBridge();
    const initial = structuredClone(snapshot);
    initial.status!.prepared_next_image_id = 9;
    initial.status!.prepared_next_config_revision = 'old';
    initial.status!.forward_history = { id: 'forward', image_id: 3 };
    mock.Status.mockResolvedValue(initial);
    mount();
    await ready();
    await waitFor(() => expect(mock.PreviewNeighbor).toHaveBeenCalledWith(3, 'forward'));
    expect(mock.Preview).not.toHaveBeenCalledWith(9);
  });

  it('ignores a status reply arriving after a failed navigation', async () => {
    const mock = installBridge();
    const initial = structuredClone(snapshot);
    let statusReply!: (value: Snapshot) => void;
    let finish!: (value: Snapshot) => void;
    mock.Status.mockResolvedValueOnce(initial).mockImplementation(
      () =>
        new Promise((resolve) => {
          statusReply = resolve;
        }),
    );
    mock.Action.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    mount();
    await ready();
    fireEvent.click(screen.getByRole('button', { name: 'Next wallpaper' }));
    await waitFor(() => expect(statusReply).toBeDefined());
    await act(async () => finish({ ...initial, code: 'selection_failed' }));
    const stale = structuredClone(initial);
    stale.status!.pending_image_id = 99;
    await act(async () => statusReply(stale));
    expect(mock.Preview).not.toHaveBeenCalledWith(99);
    expect(screen.queryByText('Applying wallpaper…')).toBeNull();
  });

  it('respects reduced motion for a ready neighbor', async () => {
    const mock = installBridge();
    const initial = structuredClone(snapshot);
    initial.status!.prepared_next_image_id = 9;
    mock.Status.mockResolvedValue(initial);
    mock.Preview.mockImplementation(async (id) => ({
      preview: { image_id: id, available: true, mime_type: 'image/png', data: 'AAAA' },
    }));
    let finish!: (value: Snapshot) => void;
    mock.Action.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const animate = vi.fn();
    const originalAnimate = HTMLElement.prototype.animate;
    const originalMatchMedia = window.matchMedia;
    HTMLElement.prototype.animate = animate;
    window.matchMedia = vi.fn().mockReturnValue({ matches: true });
    try {
      mount();
      await ready();
      const next = screen.getByRole('button', { name: 'Next wallpaper' });
      await waitFor(() => expect(next.querySelector('img')).not.toBeNull());
      fireEvent.load(next.querySelector('img')!);
      animate.mockClear();
      fireEvent.click(next);
      expect(document.querySelector('.wallpaper-optimistic')).toHaveAttribute('data-ready', 'true');
      expect(animate).not.toHaveBeenCalled();
      await act(async () => finish(initial));
    } finally {
      HTMLElement.prototype.animate = originalAnimate;
      window.matchMedia = originalMatchMedia;
    }
  });

  it('keeps the previous pixels visible and decorative while a confirmed preview loads', async () => {
    const mock = installBridge();
    const user = userEvent.setup();
    let finish: (result: PreviewResult) => void = () => {};
    mock.Preview.mockImplementation(async (imageID) =>
      imageID === 1
        ? { preview: { image_id: 1, available: true, mime_type: 'image/png', data: 'AAAA' } }
        : new Promise((resolve) => {
            finish = resolve;
          }),
    );
    const forest = {
      ...snapshot.current!,
      id: 'accepted-2',
      image_id: 2,
      original_path: '/forest.png',
    };
    mock.Action.mockResolvedValue({
      ...snapshot,
      current: forest,
      status: { ...snapshot.status, current: forest, history_cursor: 1, history_length: 2 },
    });
    mount();
    await ready();
    fireEvent.load(await screen.findByRole('img', { name: 'Wallpaper: coast.jpg' }));
    await user.click(screen.getByRole('button', { name: 'Next wallpaper' }));

    await waitFor(() => expect(mock.Preview).toHaveBeenCalledWith(2));
    const retained = document.querySelector('.wallpaper-retained');
    expect(retained).toHaveAttribute('src', 'data:image/png;base64,AAAA');
    expect(retained).toHaveAttribute('aria-hidden', 'true');
    expect(screen.queryByRole('img', { name: 'Wallpaper: coast.jpg' })).toBeNull();

    await act(async () =>
      finish({ preview: { image_id: 2, available: true, mime_type: 'image/png', data: 'BBBB' } }),
    );
    fireEvent.load(await screen.findByRole('img', { name: 'Wallpaper: forest.png' }));
    expect(document.querySelector('.wallpaper-retained')).toBeNull();
  });

  it.each(['navigation', 'reconnect', 'explicit retry'] as const)(
    'recovers an unavailable accepted-image preview after %s without changing wallpapers',
    async (recovery) => {
      const mock = installBridge(),
        user = userEvent.setup();
      mock.Preview.mockResolvedValueOnce({
        preview: { image_id: 1, available: false, reason: 'cache_unavailable' },
      });
      mount();
      await ready();
      await waitFor(() => expect(mock.Preview).toHaveBeenCalledTimes(1));
      expect(screen.queryByRole('img', { name: 'Wallpaper: coast.jpg' })).not.toBeInTheDocument();
      await user.click(screen.getByRole('button', { name: 'Poll' }));
      await waitFor(() =>
        expect(screen.getByRole('button', { name: 'Next wallpaper' })).toBeEnabled(),
      );
      expect(mock.Preview).toHaveBeenCalledTimes(1);
      if (recovery === 'explicit retry') {
        await user.click(screen.getByRole('button', { name: 'Retry preview' }));
      } else if (recovery === 'navigation') {
        await user.click(screen.getByRole('button', { name: 'Leave Now' }));
        await user.click(screen.getByRole('button', { name: 'Return to Now' }));
      } else {
        mock.Status.mockResolvedValueOnce({ connected: false, code: 'service_unavailable' });
        await user.click(screen.getByRole('button', { name: 'Poll' }));
        await waitFor(() =>
          expect(screen.getByRole('button', { name: 'Next wallpaper' })).toBeDisabled(),
        );
        await user.click(screen.getByRole('button', { name: 'Poll' }));
      }
      expect(await screen.findByRole('img', { name: 'Wallpaper: coast.jpg' })).toBeVisible();
      expect(mock.Preview).toHaveBeenCalledTimes(2);
      expect(mock.Action).not.toHaveBeenCalled();
      expect(mock.UpdateConfiguration).not.toHaveBeenCalled();
    },
  );
  it.each(['service instance', 'accepted history handle', 'accepted cache path'] as const)(
    'refreshes a successful preview after its %s changes even when the image ID is reused',
    async (identity) => {
      const mock = installBridge(),
        user = userEvent.setup();
      let current: Snapshot = structuredClone(snapshot);
      current.status!.instance_id = 'initial-service';
      current.status!.current!.path = '/cache/original.png';
      mock.Status.mockImplementation(async () => structuredClone(current));
      mock.Preview.mockResolvedValueOnce({
        preview: { image_id: 1, available: true, mime_type: 'image/png', data: 'AAAA' },
      });
      mount();
      await ready();
      expect(await screen.findByRole('img', { name: 'Wallpaper: coast.jpg' })).toHaveAttribute(
        'src',
        'data:image/png;base64,AAAA',
      );
      await user.click(screen.getByRole('button', { name: 'Poll' }));
      await user.click(screen.getByRole('button', { name: 'Leave Now' }));
      await user.click(screen.getByRole('button', { name: 'Return to Now' }));
      expect(mock.Preview).toHaveBeenCalledTimes(1);
      let finish: (result: PreviewResult) => void = () => {};
      mock.Preview.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      );
      const id =
        identity === 'accepted history handle'
          ? 'replacement-accepted'
          : current.status!.current!.id;
      current = {
        ...current,
        status: {
          ...current.status,
          instance_id:
            identity === 'service instance' ? 'replacement-service' : current.status!.instance_id,
          current: {
            ...current.status!.current!,
            id,
            path:
              identity === 'accepted cache path'
                ? '/cache/recovered.png'
                : current.status!.current!.path,
          },
        },
        current: { ...current.current!, id, original_path: '/Pictures/replacement.jpg' },
      };
      await user.click(screen.getByRole('button', { name: 'Poll' }));
      await waitFor(() => expect(mock.Preview).toHaveBeenCalledTimes(2));
      expect(screen.queryByRole('img')).not.toBeInTheDocument();
      expect(screen.getByText('Loading image…')).toBeVisible();
      await act(async () =>
        finish({ preview: { image_id: 1, available: true, mime_type: 'image/png', data: 'BBBB' } }),
      );
      expect(
        await screen.findByRole('img', { name: 'Wallpaper: replacement.jpg' }),
      ).toHaveAttribute('src', 'data:image/png;base64,BBBB');
      expect(mock.Preview).toHaveBeenLastCalledWith(1);
      expect(mock.Action).not.toHaveBeenCalled();
    },
  );
  it('preserves the full preview and last accepted image across disconnects, with safe origin actions', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    mount();
    await ready();
    const image = await screen.findByRole('img', { name: 'Wallpaper: coast.jpg' });
    expect(image).toBeVisible();
    await user.click(screen.getByLabelText('Image actions'));
    await user.click(screen.getByRole('button', { name: 'Show in folder' }));
    expect(mock.OpenCurrent).toHaveBeenCalledWith(1, 'folder');
    expect(screen.queryByText('Picture details')).not.toBeInTheDocument();
    mock.Status.mockResolvedValue({ connected: false, code: 'service_unavailable' });
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    expect(screen.getByRole('img', { name: 'Wallpaper: coast.jpg' })).toBeVisible();
    expect(screen.getByText(/Last known image/)).toBeVisible();
    expect(screen.getByRole('button', { name: 'Next wallpaper' })).toBeDisabled();
    await user.click(screen.getByLabelText('Image actions'));
    expect(screen.getByRole('button', { name: 'Show in folder' })).toBeDisabled();
  });
  it('discards an obsolete preview and keeps image identity separate from metadata', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    let finish: (result: PreviewResult) => void = () => {};
    mock.Preview.mockImplementation(async (id) =>
      id === 1
        ? new Promise((resolve) => {
            finish = resolve;
          })
        : { preview: { image_id: id, available: true, mime_type: 'image/png', data: 'BBBB' } },
    );
    mount();
    await ready();
    await waitFor(() => expect(mock.Preview).toHaveBeenCalledWith(1));
    const next: Snapshot = {
      ...structuredClone(snapshot),
      status: { ...snapshot.status, current: { id: 'new-image', image_id: 2 } },
      current: {
        ...snapshot.current!,
        id: 'new-image',
        image_id: 2,
        original_path: '/Pictures/new.png',
      },
    };
    mock.Status.mockResolvedValue(next);
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    expect(await screen.findByRole('img', { name: 'Wallpaper: new.png' })).toHaveAttribute(
      'src',
      'data:image/png;base64,BBBB',
    );
    await act(async () =>
      finish({ preview: { image_id: 1, available: true, mime_type: 'image/png', data: 'AAAA' } }),
    );
    expect(screen.getByRole('img', { name: 'Wallpaper: new.png' })).toHaveAttribute(
      'src',
      'data:image/png;base64,BBBB',
    );
  });
  it('cancels a queued Exclude when a passive read changes the reviewed image', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    mount();
    await ready();
    let finish: (result: Snapshot) => void = () => {};
    mock.Status.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    await user.click(screen.getByRole('button', { name: 'Poll' }));
    await user.click(screen.getByLabelText('Image actions'));
    await user.click(screen.getByRole('button', { name: 'Exclude image' }));
    await act(async () =>
      finish({
        ...structuredClone(snapshot),
        status: { ...snapshot.status, current: { id: 'changed', image_id: 2 } },
      }),
    );
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent(/wallpaper changed before/),
    );
    expect(mock.Action).not.toHaveBeenCalled();
    expect(mock.ExcludeCurrent).not.toHaveBeenCalled();
  });
  it('keeps the image menu scoped to the current wallpaper and restores focus after Exclude', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    const current = structuredClone(snapshot);
    current.status!.exclusion_undo_available = true;
    mock.Status.mockResolvedValue(current);
    mock.ExcludeCurrent.mockResolvedValue({
      ...current,
      status: { ...current.status, current_excluded: true },
    });
    mount();
    await ready();
    const menu = screen.getByLabelText('Image actions');
    await user.click(menu);
    expect(screen.queryByRole('button', { name: 'Undo last exclusion' })).not.toBeInTheDocument();
    screen.getByRole('button', { name: 'Exclude image' }).focus();
    await user.keyboard('{Enter}');
    await waitFor(() => expect(menu).toHaveFocus());
    expect(menu.closest('details')?.open).toBe(false);
    expect(screen.getByText(/This image stays on the desktop/)).toBeVisible();
    expect(mock.ExcludeCurrent).toHaveBeenCalledExactlyOnceWith('test-service', 'accepted-1', 1);
    expect(mock.Action).not.toHaveBeenCalled();
  });
  it('explains no-match recovery without dropping the accepted wallpaper', async () => {
    const mock = installBridge(),
      user = userEvent.setup();
    mock.Status.mockResolvedValue({
      ...structuredClone(snapshot),
      status: {
        ...snapshot.status,
        last_error: { code: 'selection_failed', message: 'technical selection message' },
      },
    });
    mount();
    await ready();
    expect(screen.getByRole('heading', { name: 'No wallpaper is ready' })).toBeVisible();
    expect(screen.getByText(/Your last accepted wallpaper is kept/)).toBeVisible();
    expect(screen.getByText(/Each folder and search has its own image filters/)).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Review image filters' }));
    expect(screen.getByTestId('page')).toHaveTextContent('providers');
    expect(mock.Action).not.toHaveBeenCalled();
  });
});
