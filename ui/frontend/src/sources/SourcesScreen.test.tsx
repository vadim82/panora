import { OnboardingProvider } from '../onboarding/Onboarding';
import { useEffect } from 'react';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { ClientProvider, usePanora } from '../client';
import { ImageSettingsProvider } from '../settings/ImageSettings';
import { capabilities, configuration, installBridge, snapshot } from '../test/fixtures';
import type { Configuration, ConfiguredFolder, ConfiguredSearch } from '../types';
import { SourcesScreen } from './SourcesScreen';

function Harness() {
  const ui = usePanora(),
    { navigate } = ui;
  useEffect(() => navigate('providers'), [navigate]);
  return (
    <>
      <nav aria-label="Test navigation">
        <button onClick={() => navigate('providers')}>Sources destination</button>
        <button onClick={() => navigate('settings')}>Settings destination</button>
        <button onClick={() => void ui.reload()}>Reload test configuration</button>
        <output data-testid="settings-revision">{ui.configuration?.revision}</output>
      </nav>
      <section id="page-providers" hidden={ui.page !== 'providers'}>
        <OnboardingProvider>
          <SourcesScreen />
        </OnboardingProvider>
      </section>
    </>
  );
}
async function ready() {
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <Harness />
      </ImageSettingsProvider>
    </ClientProvider>,
    { reactStrictMode: true },
  );
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add source options' })).toBeEnabled(),
  );
}
async function add(user: ReturnType<typeof userEvent.setup>, label: string) {
  await user.click(screen.getByRole('button', { name: 'Add source options' }));
  if (label === 'Add folder…') {
    await user.click(screen.getByRole('button', { name: 'Folder' }));
    await user.click(screen.getByRole('button', { name: 'Browse…' }));
  } else await user.click(screen.getByRole('button', { name: label }));
}
async function detail(user: ReturnType<typeof userEvent.setup>, name: RegExp) {
  await user.click(screen.getByRole('button', { name }));
}
async function edit(user: ReturnType<typeof userEvent.setup>, name: RegExp) {
  await detail(user, name);
}

describe('source editors', () => {
  it('explains Commons discovery and saves the curated collection only after Add source', async () => {
    const api = installBridge();
    const user = userEvent.setup();
    const commons: Configuration = {
      ...structuredClone(configuration),
      provider_editors: [
        {
          id: 'wikimedia_commons',
          name: 'Wikimedia Commons',
          editor: {
            version: 1,
            groups: [
              {
                id: 'collection',
                fields: [
                  {
                    key: 'collection',
                    label: 'Collection',
                    control: 'select',
                    required: true,
                    default: 'featured-landscapes',
                    help: 'Panora contacts Wikimedia Commons to find and download reviewed landscape wallpapers.',
                    options: [{ label: 'Featured landscapes', value: 'featured-landscapes' }],
                  },
                ],
              },
            ],
          },
        },
      ],
    };
    api.Configuration.mockResolvedValue({ configuration: commons });
    await ready();
    await add(user, 'Wikimedia Commons');
    expect(screen.getByLabelText('Collection')).toHaveValue('featured-landscapes');
    expect(screen.getByLabelText('Name')).toHaveValue('Featured landscapes');
    expect(screen.getByText(/contacts Wikimedia Commons/)).toBeVisible();
    expect(screen.getByRole('group', { name: 'Image rules' })).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(api.AddProviderInput).not.toHaveBeenCalled();
    expect(api.SuggestProviderInput).not.toHaveBeenCalled();
    await add(user, 'Wikimedia Commons');
    const editor = within(screen.getByRole('form', { name: 'Add Wikimedia Commons' }));
    await user.click(editor.getByRole('button', { name: 'Add source' }));
    expect(api.AddProviderInput).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        provider_id: 'wikimedia_commons',
        name: 'Featured landscapes',
        enabled: true,
        values: { collection: 'featured-landscapes' },
      }),
    );
  });
  it('shows saved counts and debounced draft matches together in Source status', async () => {
    const api = installBridge();
    const user = userEvent.setup();
    api.Configuration.mockResolvedValue({
      configuration: {
        ...structuredClone(configuration),
        searches: [{ ...savedSearch('space', ''), name: 'Space' }],
      },
    });
    const statusSnapshot = structuredClone(snapshot);
    const inputHealth = {
      id: 'space',
      state: 'ready',
      counts: { scope: 'latest-check', inspected: 8, known_eligible: null },
      last_success: '2026-11-01T12:00:00Z',
    };
    statusSnapshot.status!.providers = [
      { id: 'wallhaven', enabled: true, state: 'ready', inputs: [inputHealth] },
    ];
    api.Status.mockImplementation(async () => structuredClone(statusSnapshot));
    api.CountSearchCandidates.mockResolvedValueOnce({
      counts: { discovered: 221, eligible: null, search_url: '' },
    });
    api.RulePreview.mockResolvedValueOnce({
      preview: {
        matched: 1,
        rejected: 2,
        unknown: 3,
        inspected: 6,
        complete: false,
        limit: 200,
        exclusions_applied: false,
        reasons: {},
      },
    });
    await ready();
    expect(api.CountSearchCandidates).not.toHaveBeenCalled();
    const status = within(document.getElementById('source-troubleshooting')!);
    await user.click(status.getByText('Source troubleshooting'));
    expect(
      await status.findByText(/221 discovered.*Eligible count unavailable.*screen shape/),
    ).toBeVisible();
    expect(status.getByText(/8 inspected in the latest check/)).toBeVisible();
    expect(api.CountSearchCandidates).toHaveBeenCalledExactlyOnceWith(
      'space',
      configuration.revision,
    );
    await user.click(status.getByRole('button', { name: 'Refresh status' }));
    const savedCount = /221 discovered in the catalog.*7 eligible under saved rules and exclusions/;
    expect(await status.findByText(savedCount)).toBeVisible();

    await edit(user, /^Edit Wallhaven search Space/);
    expect(screen.queryByRole('region', { name: 'Catalog results' })).not.toBeInTheDocument();
    expect(api.RulePreview).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Image rules' }));
    await user.selectOptions(screen.getByLabelText('Rules'), 'custom');
    await user.click(screen.getByRole('button', { name: '2560 × 1440' }));
    expect(api.RulePreview).not.toHaveBeenCalled();
    expect(
      await status.findByText(/1 matching.*2 rejected.*3 unknown.*Partial catalog check/),
    ).toBeVisible();
    expect(status.getByText(savedCount)).toBeVisible();
    expect(api.RulePreview).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({
        input_id: 'space',
        rules: expect.objectContaining({ min_width: 2560, min_height: 1440 }),
      }),
    );
    await user.click(status.getByRole('button', { name: 'Refresh status' }));
    expect(await status.findByText(savedCount)).toBeVisible();
    expect(api.RulePreview).toHaveBeenCalledTimes(1);
    const countReads = api.CountSearchCandidates.mock.calls.length;
    const statusReads = api.Status.mock.calls.length;
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'));
    });
    await waitFor(() => expect(api.Status).toHaveBeenCalledTimes(statusReads + 1));
    expect(api.CountSearchCandidates).toHaveBeenCalledTimes(countReads);
    expect(api.RulePreview).toHaveBeenCalledTimes(1);

    inputHealth.last_success = '2026-11-01T12:05:00Z';
    api.CountSearchCandidates.mockResolvedValueOnce({
      counts: { discovered: 222, eligible: 7 },
    });
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'));
    });
    const refreshedCount =
      /222 discovered in the catalog.*7 eligible under saved rules and exclusions/;
    expect(await status.findByText(refreshedCount)).toBeVisible();
    expect(api.CountSearchCandidates).toHaveBeenCalledTimes(countReads + 1);
    expect(api.RulePreview).toHaveBeenCalledTimes(1);

    await user.type(screen.getByRole('textbox', { name: /Search terms/ }), 'stars');
    expect(
      status.getByText(/Save the changed search criteria before estimating matches/),
    ).toBeVisible();
    expect(status.queryByText(/1 matching.*2 rejected/)).not.toBeInTheDocument();
    expect(status.getByText(refreshedCount)).toBeVisible();
    expect(api.RulePreview).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(status.queryByText('With your unsaved rules')).not.toBeInTheDocument();
    expect(status.getByText(refreshedCount)).toBeVisible();
    expect(api.UpdateSearch).not.toHaveBeenCalled();
    expect(api.Action).not.toHaveBeenCalled();
  });

  it('discards a pending draft estimate when its connection goes stale', async () => {
    const api = installBridge();
    const user = userEvent.setup();
    const config = structuredClone(configuration);
    config.searches = [savedSearch('space')];
    api.Configuration.mockImplementation(async () => ({ configuration: structuredClone(config) }));
    const preview = {
      matched: 1,
      rejected: 0,
      unknown: 0,
      inspected: 1,
      complete: true,
      limit: 200,
      exclusions_applied: false,
      reasons: {},
    };
    let finish!: (result: Awaited<ReturnType<typeof api.RulePreview>>) => void;
    api.RulePreview.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );

    await ready();
    await edit(user, /^Edit Wallhaven search/);
    const status = within(document.getElementById('source-troubleshooting')!);
    await user.click(status.getByText('Source troubleshooting'));
    await user.click(screen.getByRole('button', { name: 'Image rules' }));
    await user.selectOptions(screen.getByLabelText('Rules'), 'custom');
    await user.click(screen.getByRole('button', { name: '2560 × 1440' }));
    await waitFor(() => expect(api.RulePreview).toHaveBeenCalledTimes(1));

    api.Status.mockResolvedValue({ connected: false });
    await user.click(status.getByRole('button', { name: 'Refresh status' }));
    expect(
      await status.findByText(/Reconnect or reload settings to estimate matches/),
    ).toBeVisible();
    await act(async () => finish({ preview }));
    expect(status.queryByText(/1 matching/)).not.toBeInTheDocument();
    expect(api.RulePreview).toHaveBeenCalledTimes(1);
  });

  it('explains when a new or disabled search has no estimate and clears an estimate on save', async () => {
    const api = installBridge();
    const user = userEvent.setup();
    let config = structuredClone(configuration);
    config.searches = [
      { ...savedSearch('enabled', 'forest'), name: 'Forest' },
      { ...savedSearch('disabled', 'city'), name: 'City', enabled: false, input_enabled: false },
    ];
    api.Configuration.mockImplementation(async () => ({ configuration: structuredClone(config) }));
    api.UpdateSearch.mockImplementation(async (edit) => {
      config = {
        ...config,
        revision: 'after-save',
        searches: config.searches.map((search) =>
          search.id === edit.id ? { ...search, image_rules: edit.image_rules } : search,
        ),
      };
      return { configuration: structuredClone(config) };
    });

    await ready();
    const status = within(document.getElementById('source-troubleshooting')!);
    await user.click(status.getByText('Source troubleshooting'));
    await add(user, 'Wallhaven search');
    expect(
      await status.findByText(/Save and enable this search to estimate matches/),
    ).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    await edit(user, /^Edit Wallhaven search City/);
    expect(await status.findByText(/Enable this search to estimate matches/)).toBeVisible();
    expect(api.RulePreview).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Cancel' }));

    await edit(user, /^Edit Wallhaven search Forest/);
    await user.click(screen.getByRole('button', { name: 'Image rules' }));
    await user.selectOptions(screen.getByLabelText('Rules'), 'custom');
    await user.click(screen.getByRole('button', { name: '2560 × 1440' }));
    expect(await status.findByText(/1 matching/)).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Search saved.')).toBeVisible();
    expect(status.queryByText('With your unsaved rules')).not.toBeInTheDocument();
    await waitFor(() =>
      expect(api.CountSearchCandidates).toHaveBeenLastCalledWith('enabled', 'after-save'),
    );
    expect(api.RulePreview).toHaveBeenCalledTimes(1);
  });

  it('retains a color draft through the inline picker and enforces the six-color cap', async () => {
    const api = installBridge();
    const user = userEvent.setup();
    api.ThemePalette.mockResolvedValue({
      colors: [
        '#A1A1A1',
        '#a1a1a1',
        '#424153',
        '#eeeeee',
        '#ff9900',
        '#ffffff',
        '#000000',
        '#666666',
      ],
    });
    await ready();
    await add(user, 'Wallhaven search');
    await user.click(screen.getByRole('button', { name: 'Filters' }));
    const addColor = screen.getByRole('button', { name: 'Add color' });
    await user.click(addColor);
    const swatches = () => within(screen.getByRole('group', { name: 'Wallhaven swatches' }));
    expect(swatches().getAllByRole('button')).toHaveLength(9);
    for (const swatch of swatches().getAllByRole('button').slice(0, 7)) await user.click(swatch);
    expect(screen.getByText('6 of 6 selected')).toBeVisible();
    expect(screen.getByText('Choose up to 6 colors.')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Add custom color' }));
    expect(screen.getByText('6 of 6 selected')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Use theme palette' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Remove color #a1a1a1' })).toBeVisible(),
    );
    expect(screen.getByText('6 of 6 selected')).toBeVisible();
    await user.keyboard('{Escape}');
    expect(addColor).toHaveFocus();
    expect(addColor).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByRole('button', { name: 'Filters' })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
    await user.click(screen.getByRole('button', { name: 'Remove color #a1a1a1' }));
    expect(screen.getByText('5 of 6 selected')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
    expect(api.AddSearch).toHaveBeenCalledWith(
      expect.objectContaining({
        colors: ['#424153', '#eeeeee', '#ff9900', '#ffffff', '#000000'],
      }),
    );
  });

  it('shows source types from the header without opening an editor and returns focus on Escape', async () => {
    const api = installBridge();
    const user = userEvent.setup();
    await ready();
    const add = screen.getByRole('button', { name: 'Add source options' });
    await user.click(add);
    expect(add).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('list', { name: 'Saved sources' })).toBeVisible();
    expect(screen.queryByRole('region', { name: 'Source editor' })).not.toBeInTheDocument();
    await user.tab();
    expect(screen.getByRole('button', { name: 'Folder' })).toHaveFocus();
    await user.keyboard('{Escape}');
    expect(add).toHaveFocus();
    expect(add).toHaveAttribute('aria-expanded', 'false');
    await user.click(add);
    await user.click(screen.getByRole('heading', { name: 'Sources' }));
    expect(add).toHaveAttribute('aria-expanded', 'false');
    expect(api.PickFolder).not.toHaveBeenCalled();
    expect(api.AddFolderSettings).not.toHaveBeenCalled();
    expect(api.AddSearch).not.toHaveBeenCalled();
  });

  it.each(['canceled', 'chosen', 'failed'] as const)(
    'preserves focus moved away from a deferred %s folder picker',
    async (outcome) => {
      const client = installBridge(),
        user = userEvent.setup();
      let finishPicker = () => {};
      const pending = new Promise<void>((resolve) => {
        finishPicker = resolve;
      });
      client.PickFolder.mockImplementation(async () => {
        await pending;
        return outcome === 'chosen'
          ? { name: 'Pending folder', path: '/Pictures/Pending folder ' }
          : outcome === 'canceled'
            ? { canceled: true }
            : { error: 'The folder picker is unavailable.' };
      });
      await ready();
      await add(user, 'Add folder…');
      await waitFor(() => expect(client.PickFolder).toHaveBeenCalledTimes(1));
      const navigation = screen.getByRole('button', { name: 'Settings destination' });
      navigation.focus();
      await act(async () => finishPicker());
      await waitFor(() =>
        expect(
          screen.getByRole('button', {
            name: outcome === 'chosen' ? 'Add source' : 'Browse…',
          }),
        ).toBeEnabled(),
      );
      expect(navigation).toHaveFocus();
      if (outcome === 'chosen')
        expect(screen.getByRole('textbox', { name: 'Folder path' })).toHaveValue(
          '/Pictures/Pending folder ',
        );
      if (outcome === 'failed')
        expect(screen.getByText('The folder picker is unavailable.')).toBeVisible();
      expect(client.AddFolderSettings).not.toHaveBeenCalled();
      expect(client.UpdateFolder).not.toHaveBeenCalled();
      expect(client.Action).not.toHaveBeenCalled();
    },
  );

  it('restores Add folder focus when picker cancellation leaves it unclaimed', async () => {
    const client = installBridge(),
      user = userEvent.setup();
    let finishPicker = () => {};
    const pending = new Promise<void>((resolve) => {
      finishPicker = resolve;
    });
    client.PickFolder.mockImplementationOnce(async () => {
      await pending;
      return { canceled: true };
    });
    await ready();
    await user.click(screen.getByRole('button', { name: 'Add source options' }));
    await user.click(screen.getByRole('button', { name: 'Folder' }));
    const choose = screen.getByRole('button', { name: 'Browse…' });
    await user.click(choose);
    choose.blur();
    await act(async () => finishPicker());
    await waitFor(() => expect(choose).toHaveFocus());
    expect(client.AddFolderSettings).not.toHaveBeenCalled();
    expect(client.Action).not.toHaveBeenCalled();
  });

  it.each(['folder', 'search'] as const)(
    'preserves a complete custom %s filter draft when a concurrent edit changes inheritance',
    async (kind) => {
      const client = installBridge(),
        user = userEvent.setup();
      const own = { min_width: 1920, min_height: 1080, ratio: 'any', ratio_presets: [] };
      let config: Configuration = {
        ...structuredClone(configuration),
        folders: [{ ...configuration.folders[0], image_rules: own, effective_rules: own }],
        searches: [
          {
            id: 'search-1',
            name: 'Mountains',
            query: 'mountains',
            enabled: true,
            input_enabled: true,
            categories: '100',
            sorting: 'relevance',
            order: 'desc',
            top_range: '1M',
            max_pages: 20,
            purity: '100',
            refresh_interval: '5m',
            image_rules: own,
            effective_rules: own,
          },
        ],
      };
      client.Configuration.mockImplementation(async () => ({
        configuration: structuredClone(config),
      }));
      await ready();
      await edit(
        user,
        kind === 'folder' ? /^Edit Folder Wallpapers/ : /^Edit Wallhaven search Mountains/,
      );
      if (kind === 'search') {
        await user.click(screen.getByRole('button', { name: 'Image rules' }));
        await user.click(screen.getByText('Custom size'));
      }
      const width = screen.getByLabelText(
        kind === 'folder' ? 'Minimum width (pixels)' : 'Width (px)',
      );
      await user.clear(width);
      await user.type(width, '2560');
      if (kind === 'search')
        await user.click(screen.getByRole('button', { name: 'Apply minimum' }));
      config = {
        ...config,
        revision: 'external-inheritance',
        folders: config.folders.map((source) => ({
          ...source,
          image_rules: null,
          effective_rules: config.rules,
        })),
        searches: config.searches.map((source) => ({
          ...source,
          image_rules: null,
          effective_rules: config.rules,
        })),
      };
      await user.click(screen.getByRole('button', { name: 'Reload test configuration' }));
      await waitFor(() =>
        expect(screen.getByTestId('settings-revision')).toHaveTextContent('external-inheritance'),
      );
      if (kind === 'folder') {
        expect(screen.getByLabelText('Filter settings')).toHaveValue('custom');
        expect(screen.getByLabelText('Minimum width (pixels)')).toHaveValue(2560);
      } else {
        expect(screen.getByRole('button', { name: '2560 × 1080' })).toBeVisible();
      }
      await user.click(screen.getByRole('button', { name: kind === 'folder' ? 'Save' : 'Save' }));
      const mutation = kind === 'folder' ? client.UpdateFolder : client.UpdateSearch;
      await waitFor(() => expect(mutation).toHaveBeenCalledTimes(1));
      expect(mutation.mock.calls[0][0]).toMatchObject({
        revision: 'external-inheritance',
        image_rules: { min_width: 2560, min_height: 1080, ratio: 'any', ratio_presets: [] },
      });
      expect(mutation.mock.calls[0][0]).not.toHaveProperty('inherit_rules');
      expect(client.UpdateConfiguration).not.toHaveBeenCalled();
    },
  );

  it('saves folder filters only on that source and previews its actual scan depth', async () => {
    const client = installBridge(),
      user = userEvent.setup();
    client.Configuration.mockResolvedValue({
      configuration: {
        ...structuredClone(configuration),
        folders: [{ ...configuration.folders[0], max_depth: 0, effective_max_depth: 0 }],
      },
    });
    await ready();
    await edit(user, /^Edit Folder Wallpapers/);
    await user.selectOptions(screen.getByLabelText('Filter settings'), 'custom');
    const width = screen.getByLabelText('Minimum width (pixels)');
    await user.clear(width);
    await user.type(width, '2560');
    await user.click(screen.getByRole('button', { name: 'Preview source matches' }));
    await waitFor(() => expect(client.RulePreview).toHaveBeenCalledTimes(1));
    expect(client.RulePreview.mock.calls[0][0]).toMatchObject({
      provider_id: 'local',
      input_id: 'folder-1',
      rules: { min_width: 2560, max_depth: 0 },
    });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled());
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(client.UpdateFolder).toHaveBeenCalledTimes(1));
    expect(client.UpdateFolder.mock.calls[0][0]).toEqual({
      revision: 'reviewed',
      id: 'folder-1',
      image_rules: { min_width: 2560, min_height: 1080, ratio: 'any', ratio_presets: [] },
    });
    expect(client.UpdateConfiguration).not.toHaveBeenCalled();
    expect(client.Action).not.toHaveBeenCalled();
  });

  it('keeps an unsaved search preview unknown and saves its own ratio rules', async () => {
    const client = installBridge(),
      user = userEvent.setup();
    await ready();
    await add(user, 'Wallhaven search');
    await user.type(screen.getByLabelText('Search terms (optional)'), 'quiet mountains');
    expect(
      screen.queryByRole('button', { name: 'Preview draft image rules' }),
    ).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Image rules' }));
    await user.click(screen.getByRole('button', { name: '3:2' }));
    await user.click(screen.getByText('Custom ratio'));
    await user.type(screen.getByLabelText('Width'), '7');
    await user.type(screen.getByLabelText('Height'), '5');
    await user.click(screen.getByRole('button', { name: 'Add ratio' }));
    await user.selectOptions(screen.getByRole('combobox', { name: 'Resolution match' }), 'exact');
    await user.click(await screen.findByRole('button', { name: 'My screen · 3024 × 1964' }));
    await user.click(screen.getByRole('button', { name: '5120 × 1440' }));
    await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
    await waitFor(() => expect(client.AddSearch).toHaveBeenCalledTimes(1));
    expect(client.AddSearch.mock.calls[0][0]).toMatchObject({
      query: 'quiet mountains',
      image_rules: {
        min_width: 1,
        min_height: 1,
        ratio: 'presets',
        ratio_presets: ['3:2', '7:5'],
        exact_resolutions: ['3024x1964', '5120x1440'],
      },
    });
    expect(client.RulePreview).not.toHaveBeenCalled();
    expect(client.UpdateConfiguration).not.toHaveBeenCalled();
    expect(client.Action).not.toHaveBeenCalled();
  });

  it('saves and reopens Similar to my screen independently of the minimum', async () => {
    const client = installBridge();
    const user = userEvent.setup();
    const config = structuredClone(configuration);
    const oldRule = { min_width: 3024, min_height: 1964, ratio: 'monitor', ratio_presets: [] };
    config.searches = [{ ...savedSearch('nature'), image_rules: oldRule }];
    client.Configuration.mockImplementation(async () => ({
      configuration: structuredClone(config),
    }));
    client.UpdateSearch.mockImplementation(async (request) => {
      config.revision = 'saved';
      config.searches[0].image_rules = request.image_rules;
      return { configuration: structuredClone(config) };
    });
    await ready();
    await edit(user, /^Edit Wallhaven/);
    await user.click(screen.getByRole('button', { name: 'Image rules' }));
    const exact = screen.getByRole('button', { name: 'Same shape as my screen' });
    const similar = screen.getByRole('button', { name: 'Similar to my screen' });
    expect(exact).toHaveAttribute('aria-pressed', 'true');
    await user.click(similar);
    expect(exact).toHaveAttribute('aria-pressed', 'false');
    expect(similar).toHaveAttribute('aria-pressed', 'true');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() =>
      expect(client.UpdateSearch).toHaveBeenCalledWith({
        id: 'nature',
        revision: 'reviewed',
        image_rules: { ...oldRule, ratio: 'monitor-similar' },
      }),
    );
    await waitFor(() => expect(screen.getByTestId('settings-revision')).toHaveTextContent('saved'));
    await edit(user, /^Edit Wallhaven/);
    await user.click(screen.getByRole('button', { name: 'Image rules' }));
    expect(screen.getByRole('button', { name: 'Similar to my screen' })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByRole('button', { name: 'My screen · 3024 × 1964' })).toBeVisible();
    expect(client.Action).not.toHaveBeenCalled();
  });

  it.each([
    {
      kind: 'rotated display in a saved search',
      state: 'partial',
      geometry: {
        state: 'known',
        displays: [{ ...capabilities.geometry.displays[0], transform: 1 }],
      },
      saved: true,
      warning: 'Screen shape is unavailable for this display configuration.',
    },
    {
      kind: 'ambiguous backend in a new draft',
      state: 'ambiguous',
      geometry: { state: 'ambiguous', displays: [] },
      saved: false,
      warning: 'Choose a desktop backend in Settings to determine screen shape.',
    },
    {
      kind: 'multiple displays in a new draft',
      state: 'partial',
      geometry: { state: 'ambiguous', displays: [] },
      saved: false,
      warning: 'Multiple displays make screen shape ambiguous.',
    },
  ])('explains screen shape for $kind', async ({ state, geometry, saved, warning }) => {
    const client = installBridge();
    const user = userEvent.setup();
    client.Capabilities.mockResolvedValue({ capabilities: { ...capabilities, state, geometry } });
    if (saved) {
      const config = structuredClone(configuration);
      config.searches = [
        {
          ...savedSearch('nature'),
          image_rules: { min_width: 1, min_height: 1, ratio: 'monitor-similar', ratio_presets: [] },
          rule_resolution: { state: 'unavailable', reason: 'monitor_geometry_not_inspected' },
        },
      ];
      client.Configuration.mockResolvedValue({ configuration: config });
    }
    await ready();
    if (saved) {
      await edit(user, /^Edit Wallhaven/);
    } else {
      await add(user, 'Wallhaven search');
      await user.click(screen.getByRole('button', { name: 'Image rules' }));
      await user.click(screen.getByRole('button', { name: 'Similar to my screen' }));
    }
    expect(await screen.findByText(warning)).toBeVisible();
    expect(client.UpdateSearch).not.toHaveBeenCalled();
    expect(client.AddSearch).not.toHaveBeenCalled();
  });

  it('uses the detected screen as a minimum without duplicating a fixed preset', async () => {
    const client = installBridge();
    client.Capabilities.mockResolvedValue({
      capabilities: {
        ...capabilities,
        geometry: {
          state: 'known',
          displays: [{ name: 'Display', width: 1920, height: 1080, scale: 1 }],
        },
      },
    });
    const user = userEvent.setup();
    await ready();
    await add(user, 'Wallhaven search');
    await user.click(screen.getByRole('button', { name: 'Image rules' }));
    const preset = await screen.findByRole('button', { name: 'My screen · 1920 × 1080' });
    expect(screen.queryByRole('button', { name: '1920 × 1080' })).not.toBeInTheDocument();
    await user.click(preset);
    expect(preset).toHaveAttribute('aria-pressed', 'true');
    expect(client.AddSearch).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
    expect(client.AddSearch).toHaveBeenCalledWith(
      expect.objectContaining({
        image_rules: expect.objectContaining({ min_width: 1920, min_height: 1080 }),
      }),
    );
  });

  it('keeps exact picker paths through Cancel, then saves, disables and removes the stable folder', async () => {
    const client = installBridge(),
      user = userEvent.setup();
    let config = { ...structuredClone(configuration), folders: [] as ConfiguredFolder[] };
    client.Configuration.mockImplementation(async () => ({
      configuration: structuredClone(config),
    }));
    client.PickFolder.mockResolvedValueOnce({ path: '/Pictures/Staging' })
      .mockResolvedValueOnce({ path: '/Pictures/Wallpapers ' })
      .mockResolvedValueOnce({ canceled: true });
    client.AddFolderSettings.mockImplementation(async (edit) => {
      config = {
        ...config,
        revision: 'added',
        folders: [
          {
            ...configuration.folders[0],
            id: 'generated',
            name: 'Wallpapers ',
            path: edit.path!,
            effective_max_depth: edit.max_depth!,
          },
        ],
      };
      return { configuration: structuredClone(config) };
    });
    let finishToggle = () => {};
    const togglePending = new Promise<void>((resolve) => {
      finishToggle = resolve;
    });
    client.UpdateFolder.mockImplementation(async (edit) => {
      await togglePending;
      config = {
        ...config,
        revision: 'disabled',
        folders: config.folders.map((folder) => ({
          ...folder,
          enabled: edit.enabled!,
          input_enabled: edit.enabled!,
        })),
      };
      return { configuration: structuredClone(config) };
    });
    client.RemoveFolder.mockImplementation(async () => {
      config = { ...config, revision: 'removed', folders: [] };
      return { configuration: structuredClone(config) };
    });
    await ready();
    await add(user, 'Add folder…');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Add source' })).toBeEnabled());
    const path = screen.getByRole('textbox', { name: 'Folder path' });
    const browse = screen.getByRole('button', { name: 'Browse…' });
    expect(path).toHaveValue('/Pictures/Staging');
    await user.click(browse);
    expect(path).toHaveValue('/Pictures/Wallpapers ');
    await user.click(browse);
    expect(path).toHaveValue('/Pictures/Wallpapers ');
    expect(client.AddFolderSettings).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Add source' }));
    await waitFor(() => expect(client.AddFolderSettings).toHaveBeenCalledTimes(1));
    expect(client.AddFolderSettings.mock.calls[0][0]).toMatchObject({
      revision: 'reviewed',
      path: '/Pictures/Wallpapers ',
      max_depth: 2,
      enabled: true,
    });
    const row = await screen.findByRole('button', { name: /Edit Folder Wallpapers/ });
    const actions = screen.getByLabelText(/^Actions for Folder /);
    await user.click(actions);
    await user.click(screen.getByRole('button', { name: 'Take out of rotation' }));
    expect(row).not.toHaveAccessibleDescription(/Out of rotation/);
    (actions as HTMLElement).blur();
    finishToggle();
    await waitFor(() => expect(row).toHaveAccessibleDescription(/Out of rotation/));
    await waitFor(() => expect(actions).toHaveFocus());
    expect(client.UpdateFolder).toHaveBeenCalledWith({
      revision: 'added',
      id: 'generated',
      enabled: false,
    });
    await user.click(screen.getByLabelText(/^Actions for Folder /));
    await user.click(screen.getByRole('button', { name: 'Remove source…' }));
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Keep folder' }),
    );
    expect(client.RemoveFolder).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.getByLabelText(/^Actions for Folder /)).toHaveFocus());
    await user.click(screen.getByLabelText(/^Actions for Folder /));
    await user.click(screen.getByRole('button', { name: 'Remove source…' }));
    await user.click(
      within(screen.getByRole('alertdialog')).getByRole('button', {
        name: 'Remove folder',
      }),
    );
    await waitFor(() =>
      expect(client.RemoveFolder).toHaveBeenCalledWith({ revision: 'disabled', id: 'generated' }),
    );
    expect(
      await screen.findByText('Source removed from rotation. Originals and history are kept.'),
    ).toBeVisible();
  });
  it('preserves edited folder fields across conflicts and reloads untouched fields without retargeting its ID', async () => {
    const client = installBridge(),
      user = userEvent.setup();
    let config = structuredClone(configuration);
    client.Configuration.mockImplementation(async () => ({
      configuration: structuredClone(config),
    }));
    client.UpdateFolder.mockResolvedValueOnce({
      error: 'Another client changed settings.',
      code: 'configuration_conflict',
    }).mockImplementation(async (edit) => ({
      configuration: {
        ...config,
        revision: 'saved',
        folders: [{ ...config.folders[0], name: edit.name! }],
      },
    }));
    await ready();
    await edit(user, /^Edit Folder /);
    await user.click(screen.getByText('Folder options', { selector: 'summary' }));
    const name = within(document.getElementById('folder-form')!).getByLabelText('Name (optional)');
    await user.clear(name);
    await user.type(name, 'My draft');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByText(/Another client changed settings/);
    expect(client.UpdateFolder).toHaveBeenCalledTimes(1);
    expect(name).toHaveValue('My draft');
    config = {
      ...config,
      revision: 'external',
      folders: [
        {
          ...config.folders[0],
          name: 'External label',
          path: '/Pictures/new-root',
          effective_max_depth: 4,
        },
      ],
    };
    await user.click(
      screen.getByRole('button', {
        name: 'Reload settings',
      }),
    );
    await waitFor(() => expect(screen.getByLabelText('Subfolder depth')).toHaveValue(4));
    expect(name).toHaveValue('My draft');
    expect(screen.getByRole('textbox', { name: 'Folder path' })).toHaveValue('/Pictures/new-root');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(client.UpdateFolder).toHaveBeenCalledTimes(2));
    expect(client.UpdateFolder.mock.calls[1][0]).toMatchObject({
      revision: 'external',
      id: 'folder-1',
      name: 'My draft',
    });
    expect(client.UpdateFolder.mock.calls[1][0]).not.toHaveProperty('path');
    expect(client.UpdateFolder.mock.calls[1][0]).not.toHaveProperty('max_depth');
    expect(client.AddFolderSettings).not.toHaveBeenCalled();
  });
  it('keeps inherited folder depth when renaming and preserves keyboard focus moved during the save', async () => {
    const client = installBridge(),
      user = userEvent.setup();
    let config = structuredClone(configuration);
    config.folders[0].max_depth = null;
    client.Configuration.mockImplementation(async () => ({
      configuration: structuredClone(config),
    }));
    let finishSave = () => {};
    const pending = new Promise<void>((resolve) => {
      finishSave = resolve;
    });
    client.UpdateFolder.mockImplementation(async (edit) => {
      await pending;
      config = {
        ...config,
        revision: 'renamed',
        folders: [
          {
            ...config.folders[0],
            name: edit.name!,
            max_depth: edit.max_depth ?? config.folders[0].max_depth,
          },
        ],
      };
      return { configuration: structuredClone(config) };
    });
    await ready();
    await edit(user, /^Edit Folder /);
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    await user.click(screen.getByText('Folder options', { selector: 'summary' }));
    const name = within(document.getElementById('folder-form')!).getByLabelText('Name (optional)');
    await user.type(name, 'Landscapes');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(client.UpdateFolder).toHaveBeenCalledTimes(1));
    expect(client.UpdateFolder).toHaveBeenCalledWith({
      revision: 'reviewed',
      id: 'folder-1',
      name: 'Landscapes',
    });
    screen.getByRole('button', { name: 'Sources destination' }).focus();
    await user.tab();
    const navigation = screen.getByRole('button', { name: 'Settings destination' });
    expect(navigation).toHaveFocus();
    finishSave();
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Add source options' })).toBeEnabled(),
    );
    expect(navigation).toHaveFocus();
    expect(config.folders[0].max_depth).toBeNull();
    config = {
      ...config,
      revision: 'global-depth',
      rules: { ...config.rules, max_depth: 4 },
      folders: [{ ...config.folders[0], effective_max_depth: 4 }],
    };
    await user.click(screen.getByRole('button', { name: 'Reload test configuration' }));
    await edit(user, /^Edit Folder /);
    await user.click(screen.getByText('Folder options', { selector: 'summary' }));
    expect(screen.getByLabelText('Subfolder depth')).toHaveValue(4);
  });
  it('preserves keyboard focus moved during a deferred search removal', async () => {
    const client = installBridge(),
      user = userEvent.setup();
    let config = {
      ...structuredClone(configuration),
      searches: [
        {
          id: 'search-1',
          name: 'Quiet landscapes',
          query: 'nature',
          enabled: true,
          input_enabled: true,
          categories: '100',
          sorting: 'relevance',
          order: 'desc',
          top_range: '1M',
          max_pages: 2,
          purity: 'sfw',
          refresh_interval: '5m',
        },
      ],
    };
    client.Configuration.mockImplementation(async () => ({
      configuration: structuredClone(config),
    }));
    let finishRemove = () => {};
    const pending = new Promise<void>((resolve) => {
      finishRemove = resolve;
    });
    client.RemoveSearch.mockImplementation(async () => {
      await pending;
      config = { ...config, revision: 'removed', searches: [] };
      return { configuration: structuredClone(config) };
    });
    await ready();
    const actions = screen.getByLabelText(/^Actions for Wallhaven search Quiet landscapes/);
    await user.click(actions);
    await user.click(
      within(actions.parentElement!).getByRole('button', { name: 'Remove source…' }),
    );
    await user.click(screen.getByRole('button', { name: 'Remove search' }));
    await waitFor(() => expect(client.RemoveSearch).toHaveBeenCalledTimes(1));
    screen.getByRole('button', { name: 'Sources destination' }).focus();
    await user.tab();
    const navigation = screen.getByRole('button', { name: 'Settings destination' });
    expect(navigation).toHaveFocus();
    finishRemove();
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Add source options' })).toBeEnabled(),
    );
    expect(navigation).toHaveFocus();
  });
  it('recognizes a saved disabled folder without adding or enabling it implicitly', async () => {
    const client = installBridge(),
      user = userEvent.setup();
    const config = structuredClone(configuration);
    config.folders[0] = { ...config.folders[0], enabled: false, input_enabled: false };
    client.Configuration.mockResolvedValue({ configuration: config });
    client.PickFolder.mockResolvedValueOnce({
      name: 'Wallpapers',
      path: '/Pictures/Wallpapers',
    }).mockResolvedValueOnce({ name: 'Wallpapers', path: '/Pictures/Wallpapers/' });
    await ready();
    await add(user, 'Add folder…');
    const alreadySaved = await screen.findByRole('button', { name: 'Already saved' });
    expect(alreadySaved).toBeDisabled();
    expect(screen.getByText(/This folder is already saved as.*It is turned off/)).toBeVisible();
    await user.click(alreadySaved);
    expect(client.AddFolderSettings).not.toHaveBeenCalled();
    expect(client.UpdateFolder).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Review saved folder' }));
    await waitFor(() => expect(document.getElementById('folder-editor-title')).toHaveFocus());
    expect(
      screen.getByRole('button', { name: /Edit Folder Wallpapers/ }),
    ).toHaveAccessibleDescription(/Out of rotation/);
    expect(client.UpdateFolder).not.toHaveBeenCalled();
    // The service may normalize a manually supplied alias to an existing path.
    // A successful no-op response must not claim that a source was added.
    await add(user, 'Add folder…');
    await user.click(await screen.findByRole('button', { name: 'Add source' }));
    await screen.findByText('This folder is already saved. No new source was added.');
    expect(client.AddFolderSettings).toHaveBeenCalledTimes(1);
    expect(client.UpdateFolder).not.toHaveBeenCalled();
  });
  it('protects an open folder draft across selection, add, search and filters', async () => {
    const client = installBridge(),
      user = userEvent.setup();
    const config = structuredClone(configuration);
    config.folders.push({
      ...config.folders[0],
      id: 'folder-2',
      name: 'Wallpapers',
      path: '/Volumes/Archive/Wallpapers',
    });
    client.Configuration.mockResolvedValue({ configuration: config });
    await ready();
    const row = screen.getByRole('button', { name: /Edit Folder Wallpapers.*Pictures/ });
    await user.click(row);
    const editor = screen.getByRole('region', { name: 'Source editor' });
    expect(row).toHaveAttribute('aria-expanded', 'true');
    expect(row).toHaveAttribute('aria-controls', editor.id);
    expect(row.closest('li')).toContainElement(editor);
    expect(editor).toHaveTextContent('Edit folder: Wallpapers');
    await user.click(screen.getByText('Folder options', { selector: 'summary' }));
    const name = within(document.getElementById('folder-form')!).getByLabelText('Name (optional)');
    await user.clear(name);
    await user.type(name, 'Pending folder label');
    const path = screen.getByLabelText('Folder path');
    await user.clear(path);
    await user.type(path, '/Pictures/Unsaved');
    expect(screen.getByText('Unsaved changes')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Add source options' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Wallhaven search' }));
    const keep = screen.getByRole('button', { name: 'Keep editing' });
    expect(keep).toHaveFocus();
    await user.keyboard('{Escape}');
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Add source options' })).toHaveFocus(),
    );
    await user.click(screen.getByRole('button', { name: /Edit Folder Wallpapers.*Volumes/ }));
    await user.click(screen.getByRole('button', { name: 'Keep editing' }));
    await user.type(screen.getByLabelText('Find a source or type'), 'unmatched');
    await user.click(screen.getByRole('button', { name: 'Off' }));
    expect(client.PickFolder).not.toHaveBeenCalled();
    expect(client.UpdateFolder).not.toHaveBeenCalled();
    expect(client.RemoveFolder).not.toHaveBeenCalled();
    expect(name).toHaveValue('Pending folder label');
    expect(path).toHaveValue('/Pictures/Unsaved');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(client.UpdateFolder).toHaveBeenCalledTimes(1));
    expect(client.UpdateFolder.mock.calls[0][0]).toMatchObject({
      id: 'folder-1',
      path: '/Pictures/Unsaved',
      name: 'Pending folder label',
    });
    expect(client.AddFolderSettings).not.toHaveBeenCalled();
  });
  it('fills ten ranked suggestions after case-insensitive exclusion and saves the default Any', async () => {
    const client = installBridge();
    const user = userEvent.setup();
    let config = structuredClone(configuration);
    client.Configuration.mockImplementation(async () => ({
      configuration: structuredClone(config),
    }));
    client.AddSearch.mockImplementation(async ({ tags, tag_match }) => {
      config = {
        ...config,
        revision: 'ranked-tags-added',
        searches: [{ ...savedSearch('ranked-tags', 'ranked tags'), tags, tag_match }],
      };
      return { configuration: structuredClone(config) };
    });
    await ready();
    await add(user, 'Wallhaven search');
    const filters = screen.getByRole('button', { name: 'Filters' });
    await user.click(filters);
    const entry = screen.getByRole('combobox', { name: 'Tags' });
    await user.tab();
    expect(screen.getByRole('combobox', { name: 'Tag matching' })).toHaveFocus();
    await user.tab();
    expect(entry).toHaveFocus();
    const suggestions = screen.getByRole('listbox', { name: 'Tags suggestions' });
    const options = () =>
      within(suggestions)
        .getAllByRole('option')
        .map((option) => option.textContent);
    const initial = options();
    expect(initial).toHaveLength(10);
    expect(initial[0]).toBe('women');
    expect(screen.getByRole('combobox', { name: 'Tag matching' })).toHaveValue('any');

    await user.type(entry, 'WOMEN{Enter}');
    expect(options()).toEqual([...initial.slice(1), 'ass']);
    expect(screen.getByRole('link', { name: 'Browse Wallhaven tags' })).toHaveAttribute(
      'href',
      'https://wallhaven.cc/tags',
    );
    await user.keyboard('{Escape}');
    expect(entry).toHaveFocus();
    expect(suggestions).not.toBeVisible();
    await user.keyboard('{Escape}');
    expect(filters).toHaveFocus();
    expect(filters).toHaveAttribute('aria-expanded', 'false');
    await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
    expect(client.AddSearch).toHaveBeenCalledWith(
      expect.objectContaining({ tags: ['WOMEN'], tag_match: 'any' }),
    );
    expect(client.SuggestProviderInput).not.toHaveBeenCalled();
    await edit(user, /^Edit Wallhaven search .*ranked tags/);
    await user.click(screen.getByRole('button', { name: 'Filters' }));
    await user.click(screen.getByRole('button', { name: 'Remove tag WOMEN' }));
    expect(screen.getByRole('combobox', { name: 'Tags' })).toHaveFocus();
    expect(screen.queryByRole('group', { name: 'Selected tags' })).not.toBeInTheDocument();
  });

  it('keeps legacy All, saves Any by revision and groups manual tags after reopening', async () => {
    const client = installBridge();
    const user = userEvent.setup();
    let config = structuredClone(configuration);
    config.searches = [{ ...savedSearch('wallhaven-tags'), tags: ['mountains', 'coast'] }];
    client.Configuration.mockImplementation(async () => ({
      configuration: structuredClone(config),
    }));
    client.UpdateSearch.mockImplementation(async (edit) => {
      config = {
        ...config,
        revision: 'tags-updated',
        searches: [{ ...config.searches[0], tag_match: edit.tag_match, tags: edit.tags }],
      };
      return { configuration: structuredClone(config) };
    });
    await ready();
    const row = screen.getByRole('button', { name: /^Edit Wallhaven search/ });
    await user.click(row);
    await user.click(screen.getByRole('button', { name: 'Filters' }));
    expect(screen.getByRole('button', { name: 'Remove tag coast' })).toBeVisible();
    expect(screen.getByRole('checkbox', { name: 'General' })).toBeChecked();
    const match = screen.getByRole('combobox', { name: 'Tag matching' });
    expect(match).toHaveValue('all');
    expect(screen.getByRole('link', { name: 'Browse Wallhaven tags' })).toHaveAttribute(
      'href',
      'https://wallhaven.cc/tags',
    );
    const manual = screen.getByRole('combobox', { name: 'Tags' });
    await user.type(manual, 'mini');
    expect(screen.getByRole('option', { name: 'minimalism' })).toBeVisible();
    await user.keyboard('{ArrowDown}{Enter}');
    expect(screen.getByRole('button', { name: 'Remove tag minimalism' })).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Remove tag minimalism' }));
    await user.type(manual, 'night sky');
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    await user.keyboard('{Enter}');
    await user.click(screen.getByRole('button', { name: 'Remove tag coast' }));
    expect(manual).toHaveFocus();
    await user.selectOptions(match, 'any');
    await user.keyboard('{Escape}');
    expect(screen.getByRole('button', { name: 'Filters' })).toHaveAccessibleDescription(
      '2 tags · General',
    );
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(client.UpdateSearch).toHaveBeenCalledWith({
      id: 'wallhaven-tags',
      revision: configuration.revision,
      tags: ['mountains', 'night sky'],
      tag_match: 'any',
    });
    await waitFor(() =>
      expect(screen.getByTestId('settings-revision')).toHaveTextContent('tags-updated'),
    );
    if (row.getAttribute('aria-expanded') === 'true') await user.click(row);
    await user.click(row);
    await user.click(screen.getByRole('button', { name: 'Filters' }));
    expect(screen.getByRole('button', { name: 'Remove tag night sky' })).toBeVisible();
  });

  it('keeps SFW search options and an explicitly removed search draft until Save as new', async () => {
    const client = installBridge(),
      user = userEvent.setup();
    let config = { ...structuredClone(configuration), searches: [] as ConfiguredSearch[] };
    client.Configuration.mockImplementation(async () => ({
      configuration: structuredClone(config),
    }));
    client.AddSearch.mockImplementation(async (edit) => {
      config = {
        ...config,
        revision: 'search-added',
        searches: [
          {
            id: 'search-generated',
            name: edit.name || '',
            query: edit.query!,
            enabled: false,
            input_enabled: true,
            categories: edit.categories!,
            sorting: edit.sorting!,
            order: edit.order!,
            top_range: edit.top_range!,
            max_pages: edit.max_pages!,
            purity: '100',
            refresh_interval: edit.refresh_interval!,
          },
        ],
      };
      return { configuration: structuredClone(config) };
    });
    await ready();
    await add(user, 'Wallhaven search');
    await user.type(screen.getByLabelText('Search terms (optional)'), '<script> mountains');
    await user.click(screen.getByRole('button', { name: 'Rename' }));
    await user.click(screen.getByRole('button', { name: 'More settings' }));
    await user.type(
      within(document.getElementById('search-form')!).getByLabelText('Name (optional)'),
      'Quiet places',
    );
    await user.selectOptions(screen.getByLabelText('Sort'), 'toplist');
    await user.selectOptions(screen.getByLabelText('Top-list period'), '1w');
    const pages = screen.getByLabelText('Pages per refresh');
    await user.clear(pages);
    await user.type(pages, '2');
    await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
    await waitFor(() => expect(client.AddSearch).toHaveBeenCalledTimes(1));
    expect(client.AddSearch.mock.calls[0][0]).toMatchObject({
      query: '<script> mountains',
      categories: '000',
      sorting: 'toplist',
      top_range: '1w',
      max_pages: 2,
    });
    expect(client.AddSearch.mock.calls[0][0]).not.toHaveProperty('purity');
    await screen.findByText(/Wallhaven provider is disabled; this search/);
    expect(document.querySelector('script')).toBeNull();
    await edit(user, /^Edit Wallhaven search Quiet places/);
    await user.clear(screen.getByLabelText('Search terms (optional)'));
    await user.type(screen.getByLabelText('Search terms (optional)'), 'Preserved draft');
    config = { ...config, revision: 'removed-elsewhere', searches: [] };
    await user.click(screen.getByRole('button', { name: 'Reload test configuration' }));
    await screen.findByText(/This search was removed elsewhere/);
    expect(screen.getByLabelText('Search terms (optional)')).toHaveValue('Preserved draft');
    expect(client.AddSearch).toHaveBeenCalledTimes(1);
    expect(client.UpdateSearch).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Save as new search' }));
    await waitFor(() => expect(client.AddSearch).toHaveBeenCalledTimes(2));
    expect(client.AddSearch.mock.calls[1][0]).toMatchObject({
      revision: 'removed-elsewhere',
      query: 'Preserved draft',
    });
    expect(client.AddSearch.mock.calls[1][0]).not.toHaveProperty('id');
  });
});

function savedSearch(id: string, query = 'forest'): ConfiguredSearch {
  return {
    id,
    name: 'Shared source',
    query,
    enabled: true,
    input_enabled: true,
    categories: '100',
    sorting: 'relevance',
    order: 'desc',
    top_range: '1M',
    max_pages: 1,
    purity: '100',
    refresh_interval: '5m',
  };
}

describe('Sources manager', () => {
  it('shows saved image rule modes on collapsed source rows', async () => {
    const api = installBridge();
    const config = structuredClone(configuration);
    const rules = { min_width: 5120, min_height: 1440, ratio: '32:9', ratio_presets: [] };
    config.searches = [
      { ...savedSearch('shared'), name: 'Shared search', effective_rules: rules },
      {
        ...savedSearch('custom'),
        name: 'Custom search',
        image_rules: rules,
        effective_rules: rules,
      },
    ];
    api.Configuration.mockResolvedValue({ configuration: config });
    await ready();

    const shared = screen.getByRole('button', { name: /Edit Wallhaven search Shared search/ });
    const custom = screen.getByRole('button', { name: /Edit Wallhaven search Custom search/ });
    expect(shared).toHaveAttribute('aria-expanded', 'false');
    expect(custom).toHaveAttribute('aria-expanded', 'false');
    expect(within(shared).getByText('Shared image rules')).toBeVisible();
    expect(within(custom).getByText('Source-specific image rules')).toBeVisible();
    expect(shared).toHaveAccessibleDescription('Shared image rules');
    expect(custom).toHaveAccessibleDescription('Source-specific image rules');
  });

  it.each([0, 1, 30])('shows and searches %i configured inputs', async (count) => {
    const api = installBridge();
    const user = userEvent.setup();
    const config = structuredClone(configuration);
    config.folders = Array.from({ length: Math.ceil(count / 2) }, (_, index) => ({
      ...configuration.folders[0],
      id: `input-${index}`,
      name: 'An unusually long duplicate display name '.repeat(4),
      path: `/Pictures/Archive-${index}/Wallpapers`,
    }));
    config.searches = Array.from({ length: Math.floor(count / 2) }, (_, index) =>
      savedSearch(`input-${index}`, index < 2 ? 'same query' : `forest ${index}`),
    );
    api.Configuration.mockResolvedValue({ configuration: config });
    await ready();
    expect(screen.getByText(`${count} ${count === 1 ? 'source' : 'sources'}`)).toBeVisible();
    const controls = screen.queryAllByRole('button', { name: /^Edit (Folder|Wallhaven search)/ });
    expect(controls).toHaveLength(count);
    expect(new Set(controls.map((control) => control.getAttribute('aria-label'))).size).toBe(count);
    if (!count) {
      expect(screen.getByText(/^No saved sources/)).toBeVisible();
      return;
    }
    const search = screen.getByRole('textbox', { name: 'Find a source or type' });
    await user.type(search, 'folder archive-0/');
    expect(screen.getByText(count === 1 ? '1 source' : `1 of ${count} sources`)).toBeVisible();
    expect(screen.getByRole('button', { name: /Edit Folder.*Archive-0/ })).toBeVisible();
    await user.clear(search);
    await user.type(search, 'absent fixture');
    expect(screen.getByText(`0 of ${count} ${count === 1 ? 'source' : 'sources'}`)).toBeVisible();
    expect(screen.getByText(/^No sources match/)).toBeVisible();
  });

  it('separates availability, a disabled provider and current origin from the saved rotation choice', async () => {
    const api = installBridge();
    const user = userEvent.setup();
    const config = structuredClone(configuration);
    config.folders[0] = { ...config.folders[0], enabled: false, input_enabled: false };
    config.folders.push({ ...configuration.folders[0], id: 'bad', path: '/Missing' });
    config.searches = [{ ...savedSearch('folder-1'), enabled: false }];
    api.Configuration.mockResolvedValue({ configuration: config });
    const original = await api.Status();
    original.status!.current!.source_id = 'folder-1';
    original.status!.providers = [
      {
        id: 'local',
        enabled: true,
        state: 'retrying',
        inputs: [
          {
            id: 'bad',
            state: 'retrying',
            retries: [
              {
                scope: 'input',
                attempts: 1,
                next_retry: '2026-11-22T12:00:00Z',
                failure: { code: 'unavailable', message: 'Missing' },
              },
            ],
          },
        ],
      },
      { id: 'wallhaven', enabled: false, state: 'disabled', inputs: [] },
    ];
    api.Status.mockResolvedValue(original);
    await ready();
    const list = within(screen.getByRole('list', { name: 'Saved sources' }));
    expect(list.getAllByText(/Current wallpaper’s source/)).toHaveLength(1);
    expect(list.getByRole('button', { name: /Edit Folder.*Pictures/ })).toHaveAccessibleDescription(
      /Out of rotation/,
    );
    expect(list.getByRole('button', { name: /Edit Wallhaven/ })).toHaveAccessibleDescription(
      /Provider disabled/,
    );
    expect(list.getByRole('button', { name: /Edit Folder.*Missing/ })).toHaveAccessibleDescription(
      /Waiting to retry/,
    );
    expect(screen.getByText('3 sources')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Needs attention' }));
    expect(screen.getByText('2 of 3 sources')).toBeVisible();
    expect(list.getByText('Waiting to retry')).toBeVisible();
    expect(list.getByText('Provider disabled')).toBeVisible();
    await user.click(screen.getByRole('button', { name: 'Off' }));
    expect(list.getByText(/Current wallpaper’s source/)).toBeVisible();
    expect(list.getAllByRole('button', { name: /^Edit Folder/ })).toHaveLength(1);
    expect(api.Action).not.toHaveBeenCalled();
  });

  it('keeps confirmed participation while pending, retries a confirmed rejection, and focuses the active filter', async () => {
    const api = installBridge();
    const user = userEvent.setup();
    let config = structuredClone(configuration);
    config.searches = [savedSearch('s-1')];
    api.Configuration.mockImplementation(async () => ({ configuration: structuredClone(config) }));
    let reject = () => {};
    const pending = new Promise<void>((resolve) => {
      reject = resolve;
    });
    api.UpdateSearch.mockImplementationOnce(async () => {
      await pending;
      return { code: 'invalid_request', error: 'Save rejected.' };
    });
    api.UpdateSearch.mockImplementationOnce(async () => {
      config = {
        ...config,
        revision: 'disabled',
        searches: [{ ...config.searches[0], enabled: false, input_enabled: false }],
      };
      return { configuration: config };
    });
    await ready();
    const filter = screen.getByRole('button', { name: 'In rotation' });
    await user.click(filter);
    const row = screen.getByRole('button', { name: /^Edit Wallhaven/ });
    const actions = screen.getByLabelText(/^Actions for Wallhaven/);
    (actions as HTMLElement).focus();
    await user.keyboard(' ');
    await user.tab();
    expect(
      within(actions.parentElement!).getByRole('button', { name: 'Take out of rotation' }),
    ).toHaveFocus();
    await user.keyboard('{Enter}');
    expect(row).toHaveAccessibleDescription(/Saving…/);
    expect(screen.getByText('Last confirmed · 2 sources')).toBeVisible();
    await act(async () => reject());
    expect(row).not.toHaveAccessibleDescription(/Out of rotation/);
    expect(actions).toHaveFocus();
    await user.click(actions);
    await user.click(screen.getByRole('button', { name: /Retry saving Wallhaven/ }));
    await waitFor(() => expect(row).not.toBeInTheDocument());
    expect(filter).toHaveFocus();
    expect(api.UpdateSearch.mock.calls).toEqual([
      [{ revision: 'reviewed', id: 's-1', enabled: false }],
      [{ revision: 'reviewed', id: 's-1', enabled: false }],
    ]);
    expect(api.Action).not.toHaveBeenCalled();
  });

  it('restores focus immediately on repeated reloads that report the same notice', async () => {
    const api = installBridge();
    const user = userEvent.setup();
    let config = structuredClone(configuration);
    api.Configuration.mockImplementation(async () => ({ configuration: structuredClone(config) }));
    await ready();
    await user.click(screen.getByText('Source troubleshooting'));
    for (const revision of ['external-first', 'external-second']) {
      config = { ...config, revision };
      await user.click(screen.getByRole('button', { name: 'Refresh status' }));
      // Settle the preceding status button's focus return before testing Reload.
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
      const reload = await screen.findByRole('button', { name: 'Reload settings' });
      await user.click(reload);
      await waitFor(() => expect(screen.getByRole('button', { name: 'All' })).toHaveFocus());
    }
    const search = screen.getByRole('textbox', { name: 'Find a source or type' });
    await user.type(search, 'wallpapers');
    expect(search).toHaveFocus();
    expect(search).toHaveValue('wallpapers');
  });

  it.each(['unknown', 'conflict'])(
    'blocks changes after a %s save through disconnect and explicit reconciliation',
    async (outcome) => {
      const api = installBridge();
      const user = userEvent.setup();
      const originalStatus = await api.Status();
      let config = structuredClone(configuration);
      config.searches = [savedSearch('s-1')];
      api.Configuration.mockImplementation(async () => ({
        configuration: structuredClone(config),
      }));
      api.UpdateFolder.mockImplementationOnce(async () => {
        config = {
          ...config,
          revision: 'after-restart',
          folders: [{ ...config.folders[0], enabled: false, input_enabled: false }],
        };
        if (outcome === 'unknown') throw new Error('Reply disconnected');
        return { error: 'Configuration conflict.', code: 'configuration_conflict' };
      });
      await ready();
      await edit(user, /^Edit Wallhaven search/);
      const query = screen.getByRole('textbox', { name: 'Search terms (optional)' });
      await user.type(query, ' draft');
      const folder = screen.getByRole('button', { name: /^Edit Folder/ });
      const actions = screen.getByLabelText(/^Actions for Folder/);
      await user.click(actions);
      await user.click(
        within(actions.parentElement!).getByRole('button', { name: 'Take out of rotation' }),
      );
      const reload = await screen.findByRole('button', { name: 'Reload settings' });
      expect(folder).not.toHaveAccessibleDescription(/Out of rotation/);
      expect(screen.queryByRole('button', { name: /Retry saving/ })).not.toBeInTheDocument();
      expect(screen.getByText(/^Last confirmed · 2 sources/)).toBeVisible();
      await user.click(screen.getByText('Source troubleshooting'));
      api.Status.mockResolvedValue({ connected: false });
      await user.click(screen.getByRole('button', { name: 'Refresh status' }));
      expect(query).toHaveValue('forest draft');
      await user.click(actions);
      expect(
        within(actions.parentElement!).getByRole('button', { name: 'Take out of rotation' }),
      ).toBeDisabled();
      expect(api.UpdateFolder).toHaveBeenCalledTimes(1);
      api.Status.mockResolvedValue({
        ...originalStatus,
        status: { ...originalStatus.status, instance_id: 'restarted' },
      });
      await user.click(screen.getByRole('button', { name: 'Reconnect' }));
      expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
      await user.click(reload);
      await waitFor(() => expect(folder).toHaveAccessibleDescription(/Out of rotation/));
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled();
      expect(query).toHaveValue('forest draft');
      expect(api.UpdateFolder).toHaveBeenCalledTimes(1);
      expect(api.UpdateSearch).not.toHaveBeenCalled();
      expect(api.Action).not.toHaveBeenCalled();
    },
  );
});
