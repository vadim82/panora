import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { useLayoutEffect } from 'react';
import type { ReactNode } from 'react';
import { App } from '../App';
import { ClientProvider, usePanora } from '../client';
import { ImageSettingsProvider } from '../settings/ImageSettings';
import { configuration, installBridge } from '../test/fixtures';
import type { Configuration, ConfiguredSearch, Snapshot } from '../types';

function sourceFirst() {
  const api = installBridge();
  let config: Configuration = {
    ...structuredClone(configuration),
    folders: [],
    searches: [],
    rules: { ...configuration.rules, min_width: 1, min_height: 1, ratio: 'any', ratio_presets: [] },
  };
  let current: Snapshot = {
    connected: true,
    status: {
      instance_id: 'initial',
      paused: true,
      busy: false,
      uncertain: false,
      setup_required: true,
      providers: [],
    },
  };
  let accepted = 0;
  let folderState = 'ready';
  api.Configuration.mockImplementation(async () => ({ configuration: structuredClone(config) }));
  api.Status.mockImplementation(async () => structuredClone(current));
  api.AddFolderSettings.mockImplementation(async (edit) => {
    config = {
      ...config,
      revision: config.revision + '-folder',
      folders: [
        {
          ...configuration.folders[0],
          path: edit.path!,
          name: edit.path!.split('/').pop()!,
          effective_max_depth: edit.max_depth ?? 2,
        },
      ],
    };
    current = {
      ...current,
      status: {
        ...current.status,
        setup_required: false,
        providers: [
          {
            id: 'local',
            enabled: true,
            state: folderState,
            inputs: [{ id: config.folders[0].id, state: folderState }],
          },
        ],
      },
    };
    return { configuration: structuredClone(config) };
  });
  api.AddSearch.mockImplementation(async (edit) => {
    const search: ConfiguredSearch = {
      id: 'wallhaven-1',
      name: edit.name || edit.query!,
      query: edit.query!,
      enabled: true,
      input_enabled: true,
      categories: edit.categories || '100',
      sorting: edit.sorting || 'relevance',
      order: edit.order || 'desc',
      top_range: edit.top_range || '1M',
      max_pages: edit.max_pages || 20,
      purity: '100',
      refresh_interval: edit.refresh_interval || '5m',
    };
    config = { ...config, revision: config.revision + '-search', searches: [search] };
    current = { ...current, status: { ...current.status, setup_required: false } };
    return { configuration: structuredClone(config) };
  });
  async function action(name: string): Promise<Snapshot> {
    if (name === 'next') {
      const remote = config.searches.length > 0;
      current = {
        connected: true,
        status: {
          ...current.status,
          current: {
            id: 'first-' + ++accepted,
            image_id: accepted,
            source_id: remote ? 'wallhaven-1' : 'folder-1',
          },
          paused: true,
          setup_required: false,
        },
        current: {
          id: 'first-' + accepted,
          image_id: accepted,
          source_id: remote ? 'wallhaven-1' : 'folder-1',
          provider: remote ? 'wallhaven' : 'local',
          availability: 'cached',
          attribution: { title: 'First wallpaper' },
          dimensions: { width: 3360, height: 2060 },
          ...(remote ? {} : { original_path: '/Chosen wallpapers/coast.png' }),
        },
      };
    }
    if (name === 'resume' || name === 'pause')
      current = { ...current, status: { ...current.status, paused: name === 'pause' } };
    return structuredClone(current);
  }
  api.Action.mockImplementation(action);
  function mount(extra?: ReactNode) {
    return render(
      <ClientProvider>
        <ImageSettingsProvider>
          {extra}
          <App />
        </ImageSettingsProvider>
      </ClientProvider>,
    );
  }
  return {
    api,
    mount,
    action,
    get config() {
      return config;
    },
    get current() {
      return current;
    },
    set current(value: Snapshot) {
      current = value;
    },
    set folderState(value: string) {
      folderState = value;
    },
  };
}
async function ready() {
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Set up Wallhaven in Sources' })).toBeEnabled(),
  );
}
const firstWallpaper = () => within(screen.getByRole('region', { name: 'Start wallpapers' }));
const navigation = () => within(screen.getByRole('navigation', { name: 'Main navigation' }));

function SourceReadGate() {
  const { page, setServiceBusy } = usePanora();
  useLayoutEffect(() => {
    if (page === 'providers') setServiceBusy(true);
  }, [page, setServiceBusy]);
  return <button onClick={() => setServiceBusy(false)}>Finish source read</button>;
}

describe('source-first onboarding', () => {
  it('starts with either real source action and permits a Wallhaven-only first wallpaper without filters or folder setup', async () => {
    const flow = sourceFirst(),
      user = userEvent.setup();
    flow.mount();
    await ready();
    expect(screen.getByRole('button', { name: 'Choose folder and start' })).toBeEnabled();
    expect(screen.queryByRole('list', { name: 'Setup steps' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Preview matches' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Set up Wallhaven in Sources' }));
    const query = await screen.findByRole('textbox', { name: 'Search terms (optional)' });
    await user.type(query, 'forest');
    await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
    await waitFor(() => expect(flow.api.AddSearch).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Start wallpapers' })).toBeEnabled(),
    );
    expect(flow.config.folders).toHaveLength(0);
    expect(flow.api.PickFolder).not.toHaveBeenCalled();
    expect(flow.api.Action).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Start wallpapers' }));
    expect(await screen.findByRole('heading', { name: 'First wallpaper' })).toBeVisible();
    expect(flow.api.Action.mock.calls.map(([name]) => name)).toEqual(['next', 'resume']);
    expect(flow.api.UpdateConfiguration).not.toHaveBeenCalled();
  });
  it('retains a source-opening request through a concurrent read and preserves an existing search draft', async () => {
    const flow = sourceFirst(),
      user = userEvent.setup();
    flow.mount(<SourceReadGate />);
    await ready();
    await user.click(screen.getByRole('button', { name: 'Set up Wallhaven in Sources' }));
    expect(screen.getByRole('heading', { name: 'Sources', level: 1 })).toBeVisible();
    expect(
      screen.queryByRole('textbox', { name: 'Search terms (optional)' }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Finish source read' }));
    const query = await screen.findByRole('textbox', { name: 'Search terms (optional)' });
    expect(query).toHaveFocus();
    await user.type(query, 'saved draft');
    await user.click(navigation().getByRole('button', { name: 'Now' }));
    await user.click(screen.getByRole('button', { name: 'Set up Wallhaven in Sources' }));
    await user.click(screen.getByRole('button', { name: 'Finish source read' }));
    await waitFor(() =>
      expect(screen.getByRole('textbox', { name: 'Search terms (optional)' })).toHaveFocus(),
    );
    expect(screen.getByRole('textbox', { name: 'Search terms (optional)' })).toHaveValue(
      'saved draft',
    );
    expect(flow.api.AddSearch).not.toHaveBeenCalled();
    expect(flow.api.Action).not.toHaveBeenCalled();
  });
  it('keeps the source editor for an empty library with saved custom rules', async () => {
    const flow = sourceFirst();
    flow.api.Configuration.mockResolvedValue({
      configuration: { ...flow.config, rules: { ...flow.config.rules, min_width: 3840 } },
    });
    flow.api.PickFolder.mockResolvedValue({ path: '/Chosen wallpapers' });
    flow.mount();
    await ready();
    expect(
      screen.queryByRole('button', { name: 'Choose folder and start' }),
    ).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Add a folder' }));
    expect(await screen.findByRole('button', { name: 'Add source' })).toBeEnabled();
    expect(flow.api.AddFolderSettings).not.toHaveBeenCalled();
    expect(flow.api.Action).not.toHaveBeenCalled();
  });

  it('keeps picker Cancel harmless, then starts from the exact folder without an editor', async () => {
    const flow = sourceFirst(),
      user = userEvent.setup();
    flow.mount();
    await ready();
    const choose = screen.getByRole('button', { name: 'Choose folder and start' });
    await user.click(choose);
    await waitFor(() => expect(flow.api.PickFolder).toHaveBeenCalledTimes(1));
    expect(flow.api.AddFolderSettings).not.toHaveBeenCalled();
    expect(flow.api.Action).not.toHaveBeenCalled();
    await waitFor(() => expect(choose).toBeEnabled());
    flow.api.PickFolder.mockResolvedValue({
      name: 'Chosen wallpapers',
      path: '/Chosen wallpapers ',
    });
    await user.dblClick(choose);
    expect(await screen.findByRole('heading', { name: 'First wallpaper' })).toBeVisible();
    expect(flow.api.PickFolder).toHaveBeenCalledTimes(2);
    expect(flow.api.AddFolderSettings).toHaveBeenCalledExactlyOnceWith({
      path: '/Chosen wallpapers ',
      revision: 'reviewed',
    });
    expect(screen.queryByRole('button', { name: 'Add source' })).not.toBeInTheDocument();
    expect(flow.api.Action.mock.calls.map(([name]) => name)).toEqual(['next', 'resume']);
    expect(flow.api.AddSearch).not.toHaveBeenCalled();
  });

  it.each(['empty', 'failed'])(
    'keeps a saved %s folder without touching the wallpaper',
    async (state) => {
      const flow = sourceFirst();
      flow.folderState = state;
      flow.api.PickFolder.mockResolvedValue({ path: '/Chosen wallpapers' });
      flow.mount();
      await ready();
      await userEvent.click(screen.getByRole('button', { name: 'Choose folder and start' }));
      expect(await firstWallpaper().findByText(/Check its access and image rules/)).toBeVisible();
      expect(flow.api.AddFolderSettings).toHaveBeenCalledTimes(1);
      expect(flow.api.Action).not.toHaveBeenCalled();
      expect(screen.getByRole('button', { name: 'Start wallpapers' })).toBeEnabled();
    },
  );

  it.each(['config_conflict', 'request_failed'])(
    'never starts after a %s folder save',
    async (code) => {
      const flow = sourceFirst();
      flow.api.PickFolder.mockResolvedValue({ path: '/Chosen wallpapers' });
      flow.api.AddFolderSettings.mockResolvedValue({ code, error: 'Folder save not confirmed.' });
      flow.mount();
      await ready();
      await userEvent.click(screen.getByRole('button', { name: 'Choose folder and start' }));
      expect(await screen.findByRole('button', { name: 'Reload settings' })).toBeVisible();
      expect(flow.api.AddFolderSettings).toHaveBeenCalledTimes(1);
      expect(flow.api.Action).not.toHaveBeenCalled();
    },
  );

  it('waits for read-only discovery of the enrolled folder before displaying', async () => {
    const flow = sourceFirst();
    flow.folderState = 'pending';
    flow.api.PickFolder.mockResolvedValue({ path: '/Chosen wallpapers' });
    flow.mount();
    await ready();
    await userEvent.click(screen.getByRole('button', { name: 'Choose folder and start' }));
    await waitFor(() => expect(flow.api.AddFolderSettings).toHaveBeenCalledTimes(1));
    expect(flow.api.Action).not.toHaveBeenCalled();
    const providers = flow.current.status!.providers!;
    flow.current = {
      ...flow.current,
      status: {
        ...flow.current.status,
        providers: [
          {
            ...providers[0],
            state: 'ready',
            inputs: [{ ...providers[0].inputs[0], state: 'ready' }],
          },
        ],
      },
    };
    expect(await screen.findByRole('heading', { name: 'First wallpaper' })).toBeVisible();
    expect(flow.api.Action.mock.calls.map(([action]) => action)).toEqual(['next', 'resume']);
  });

  it('does not replay a first Next that may have already displayed the wallpaper', async () => {
    const flow = sourceFirst();
    flow.api.PickFolder.mockResolvedValue({ path: '/Chosen wallpapers' });
    flow.api.Action.mockImplementationOnce(async () => {
      await flow.action('next');
      return { connected: false, code: 'request_failed' };
    });
    flow.mount();
    await ready();
    await userEvent.click(screen.getByRole('button', { name: 'Choose folder and start' }));
    expect(await firstWallpaper().findByText(/may have been displayed/)).toBeVisible();
    expect(flow.api.Action.mock.calls.map(([action]) => action)).toEqual(['next']);
    await userEvent.click(firstWallpaper().getByRole('button', { name: 'Check status' }));
    expect(await firstWallpaper().findByRole('button', { name: 'Resume rotation' })).toBeEnabled();
    expect(flow.api.Action.mock.calls.map(([action]) => action)).toEqual(['next']);
    await userEvent.click(firstWallpaper().getByRole('button', { name: 'Resume rotation' }));
    await waitFor(() =>
      expect(flow.api.Action.mock.calls.map(([action]) => action)).toEqual(['next', 'resume']),
    );
  });

  it('does not save into a replacement service after the picker closes', async () => {
    const flow = sourceFirst();
    let picked!: (value: { path: string }) => void;
    flow.api.PickFolder.mockImplementation(() => new Promise((resolve) => (picked = resolve)));
    flow.mount();
    await ready();
    await userEvent.click(screen.getByRole('button', { name: 'Choose folder and start' }));
    flow.current = {
      ...flow.current,
      status: { ...flow.current.status, instance_id: 'replacement' },
    };
    await act(async () => picked({ path: '/Chosen wallpapers' }));
    expect(await screen.findByText(/Setup changed before the request/)).toBeVisible();
    expect(flow.api.AddFolderSettings).not.toHaveBeenCalled();
    expect(flow.api.Action).not.toHaveBeenCalled();
  });

  it('opens and cancels online setup without saving a search or requesting playback', async () => {
    const flow = sourceFirst();
    flow.mount();
    await ready();
    await userEvent.click(screen.getByRole('button', { name: 'Set up Wallhaven in Sources' }));
    expect(await screen.findByText(/enables network requests.*safe-for-work/)).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(flow.api.AddSearch).not.toHaveBeenCalled();
    expect(flow.api.Action).not.toHaveBeenCalled();
  });

  it('keeps an unknown first display gated across pages and reconciles without replaying Next', async () => {
    const flow = sourceFirst(),
      user = userEvent.setup();
    flow.mount();
    await ready();
    await user.click(screen.getByRole('button', { name: 'Set up Wallhaven in Sources' }));
    await user.type(
      await screen.findByRole('textbox', { name: 'Search terms (optional)' }),
      'mountains',
    );
    await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Start wallpapers' })).toBeEnabled(),
    );
    flow.api.Action.mockImplementationOnce(async (name) => ({
      ...(await flow.action(name)),
      error: 'Response was lost.',
      code: 'request_failed',
    }));
    await user.click(screen.getByRole('button', { name: 'Start wallpapers' }));
    await firstWallpaper().findByText(/first wallpaper may have been displayed/);
    expect(flow.api.Action.mock.calls.map(([name]) => name)).toEqual(['next']);
    await user.click(navigation().getByRole('button', { name: 'History' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Reload history' })).toBeEnabled(),
    );
    expect(screen.getByRole('button', { name: 'Display again: coast.jpg' })).toBeDisabled();
    expect(screen.getByText(/Check the first wallpaper on Now/)).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Show in folder: coast.jpg' }));
    expect(flow.api.OpenHistory).toHaveBeenCalled();
    flow.api.Exclusions.mockResolvedValue({
      page: {
        entries: [
          { id: 7, provider: 'local', image: '/old.png', excluded_at: '2026-11-01T12:00:00Z' },
        ],
      },
    });
    flow.api.RestoreExclusion.mockImplementation(async () => structuredClone(flow.current));
    await user.click(screen.getByRole('button', { name: 'Excluded' }));
    await user.click(await screen.findByRole('button', { name: 'Restore to rotation: /old.png' }));
    expect(flow.api.RestoreExclusion).toHaveBeenCalledWith(7);
    expect(flow.api.ReplayHistory).not.toHaveBeenCalled();
    await user.click(navigation().getByRole('button', { name: 'Now' }));
    expect(screen.getByRole('button', { name: 'Start wallpapers' })).toBeDisabled();
    await user.click(screen.getByRole('button', { name: 'Check status' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Resume rotation' })).toBeEnabled(),
    );
    expect(flow.api.Action).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole('button', { name: 'Resume rotation' }));
    await waitFor(() =>
      expect(flow.api.Action.mock.calls.map(([name]) => name)).toEqual(['next', 'resume']),
    );
    expect(flow.config.searches[0].query).toBe('mountains');
  });
  it('keeps saved sources after no matches, does not replay on reads, and permits optional Settings navigation', async () => {
    const flow = sourceFirst(),
      user = userEvent.setup();
    flow.mount();
    await ready();
    await user.click(screen.getByRole('button', { name: 'Set up Wallhaven in Sources' }));
    await user.type(
      await screen.findByRole('textbox', { name: 'Search terms (optional)' }),
      'forest',
    );
    await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Start wallpapers' })).toBeEnabled(),
    );
    flow.api.Action.mockResolvedValueOnce({
      ...flow.current,
      error: 'No matching images.',
      code: 'selection_failed',
    });
    await user.click(screen.getByRole('button', { name: 'Start wallpapers' }));
    expect(
      await firstWallpaper().findByText(/No matching images.*Your sources are saved/),
    ).toBeVisible();
    expect(screen.getByRole('button', { name: 'Start wallpapers' })).toBeEnabled();
    await user.click(screen.getByRole('button', { name: 'Settings' }));
    expect(screen.getByRole('button', { name: 'Use all images' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    await user.click(navigation().getByRole('button', { name: 'Now' }));
    expect(flow.api.Action).toHaveBeenCalledTimes(1);
    expect(flow.config.searches).toHaveLength(1);
    await user.click(screen.getByRole('button', { name: 'Start wallpapers' }));
    await waitFor(() =>
      expect(flow.api.Action.mock.calls.map(([name]) => name)).toEqual(['next', 'next', 'resume']),
    );
  });
  it.each(['unchanged', 'navigation', 'another page'] as const)(
    'checks a lost Resume result without repeating actions and preserves %s focus',
    async (focusChoice) => {
      const flow = sourceFirst(),
        user = userEvent.setup();
      flow.mount();
      await ready();
      await user.click(screen.getByRole('button', { name: 'Set up Wallhaven in Sources' }));
      await user.type(
        await screen.findByRole('textbox', { name: 'Search terms (optional)' }),
        'forest',
      );
      await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
      await waitFor(() =>
        expect(screen.getByRole('button', { name: 'Start wallpapers' })).toBeEnabled(),
      );
      flow.api.Action.mockImplementationOnce(flow.action).mockImplementationOnce(async (name) => ({
        ...(await flow.action(name)),
        error: 'Resume response lost.',
        code: 'request_failed',
      }));
      await user.click(screen.getByRole('button', { name: 'Start wallpapers' }));
      expect(await firstWallpaper().findByText(/rotation could not be confirmed/)).toBeVisible();
      expect(screen.getByRole('button', { name: 'Resume rotation' })).toBeDisabled();
      let finish!: (value: Snapshot) => void;
      flow.api.Status.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      );
      const check = screen.getByRole('button', { name: 'Check status' });
      check.focus();
      await user.keyboard('{Enter}');
      expect(screen.getByRole('region', { name: 'Start wallpapers' })).toHaveAttribute(
        'aria-busy',
        'true',
      );
      const settings = screen.getByRole('button', { name: 'Settings' });
      if (focusChoice === 'navigation') settings.focus();
      else if (focusChoice === 'another page') await user.click(settings);
      const chosenFocus = document.activeElement;
      await act(async () => finish(structuredClone(flow.current)));
      await waitFor(() =>
        expect(screen.queryByRole('region', { name: 'Start wallpapers' })).not.toBeInTheDocument(),
      );
      expect(flow.api.Action.mock.calls.map(([name]) => name)).toEqual(['next', 'resume']);
      if (focusChoice === 'unchanged')
        await waitFor(() =>
          expect(screen.getByRole('heading', { name: 'Sources', level: 1 })).toHaveFocus(),
        );
      else {
        await new Promise((resolve) => requestAnimationFrame(resolve));
        expect(chosenFocus).toHaveFocus();
      }
      await user.click(navigation().getByRole('button', { name: 'Now' }));
      expect(screen.getByText('Rotation on')).toBeVisible();
    },
  );
  it('shows an actual desktop service warning and routes to its service controls, including before the first wallpaper', async () => {
    const flow = sourceFirst(),
      user = userEvent.setup();
    const warning =
      'Desktop switching is unavailable. Restart the service to reconnect its macOS Space observer.';
    flow.current = { ...flow.current, status: { ...flow.current.status, warnings: [warning] } };
    flow.mount();
    await ready();
    expect(
      within(screen.getByRole('region', { name: 'Wallpaper' })).getByText(warning),
    ).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Check sources →' })).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Service settings →' }));
    await waitFor(() =>
      expect(document.getElementById('service-options')?.querySelector('summary')).toHaveFocus(),
    );
    expect(document.getElementById('service-options')).toHaveAttribute('open');
    expect(screen.getByRole('heading', { name: 'Settings', level: 1 })).toBeVisible();
    expect(flow.api.Action).not.toHaveBeenCalled();
  });
  it('reads discovery immediately before Start and sends no wallpaper action while it is still checking', async () => {
    const flow = sourceFirst(),
      user = userEvent.setup();
    flow.mount();
    await ready();
    await user.click(screen.getByRole('button', { name: 'Set up Wallhaven in Sources' }));
    await user.type(
      await screen.findByRole('textbox', { name: 'Search terms (optional)' }),
      'forest',
    );
    await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Start wallpapers' })).toBeEnabled(),
    );
    flow.current = { ...flow.current, status: { ...flow.current.status, refreshing: true } };
    const reads = flow.api.Status.mock.calls.length;
    await user.click(screen.getByRole('button', { name: 'Start wallpapers' }));
    expect(await firstWallpaper().findByText(/Panora is checking your sources/)).toBeVisible();
    expect(flow.api.Status.mock.calls.length).toBeGreaterThan(reads);
    expect(flow.api.Action).not.toHaveBeenCalled();
    flow.current = { ...flow.current, status: { ...flow.current.status, refreshing: false } };
    await user.click(screen.getByRole('button', { name: 'Start wallpapers' }));
    await waitFor(() =>
      expect(flow.api.Action.mock.calls.map(([name]) => name)).toEqual(['next', 'resume']),
    );
  });
});
