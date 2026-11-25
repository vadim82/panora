import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { App } from '../App';
import { ClientProvider } from '../client';
import { ImageSettingsProvider } from '../settings/ImageSettings';
import type { SourceImageRules } from '../types';
import { installFixture } from './fixture';

const ownRules: SourceImageRules = {
  min_width: 2560,
  min_height: 1440,
  ratio: 'presets',
  ratio_presets: ['16:9', '21:9'],
};

describe('browser fixture source contracts', () => {
  it('distinguishes first-run enrollment, configured empty history and retained disconnected metadata', async () => {
    const first = installFixture('first-run');
    expect((await first.Configuration()).configuration?.folders).toEqual([]);
    expect((await first.Status()).status).toMatchObject({
      setup_required: true,
      history_length: 0,
    });
    const empty = installFixture('empty');
    expect((await empty.Configuration()).configuration?.folders).toHaveLength(1);
    expect((await empty.Status()).status).toMatchObject({
      setup_required: false,
      history_length: 0,
    });
    const disconnected = await installFixture('disconnected').Status();
    expect(disconnected).toMatchObject({
      connected: false,
      code: 'service_unavailable',
      current: { id: 'preview-2' },
    });
    expect(disconnected.status?.current?.id).toBe('preview-2');
  });

  it('retains source rules, keeps untouched sources inherited, and explicitly resets inheritance', async () => {
    const api = installFixture('populated');
    let config = (await api.Configuration()).configuration!;
    expect(config.folders[0]).toMatchObject({
      image_rules: null,
      effective_rules: { min_width: 1920, ratio: 'any' },
    });
    config = (
      await api.UpdateFolder({
        revision: config.revision,
        id: 'demo-folder',
        image_rules: ownRules,
      })
    ).configuration!;
    expect(config.folders[0]).toMatchObject({ image_rules: ownRules, effective_rules: ownRules });
    config = (await api.UpdateConfiguration({ revision: config.revision, min_width: 3000 }))
      .configuration!;
    expect(config.folders[0].effective_rules).toEqual(ownRules);
    expect(config.searches[0]).toMatchObject({
      image_rules: null,
      effective_rules: { min_width: 3000 },
    });
    config = (
      await api.UpdateSearch({
        revision: config.revision,
        id: 'demo-search',
        image_rules: ownRules,
      })
    ).configuration!;
    config = (
      await api.UpdateSearch({ revision: config.revision, id: 'demo-search', name: 'Renamed' })
    ).configuration!;
    expect(config.searches[0]).toMatchObject({
      name: 'Renamed',
      image_rules: ownRules,
      effective_rules: ownRules,
    });
    config = (
      await api.UpdateFolder({ revision: config.revision, id: 'demo-folder', inherit_rules: true })
    ).configuration!;
    expect(config.folders[0]).toMatchObject({
      image_rules: null,
      effective_rules: { min_width: 3000, ratio: 'any' },
    });
    config = (
      await api.UpdateSearch({ revision: config.revision, id: 'demo-search', inherit_rules: true })
    ).configuration!;
    expect(config.searches[0]).toMatchObject({
      image_rules: null,
      effective_rules: { min_width: 3000 },
    });
    expect((await api.Status()).status?.current?.id).toBe('preview-2');
  });

  it('starts new sources unrestricted, retains inherited folder depth and keeps disabled setup incomplete', async () => {
    const api = installFixture('first-run');
    let config = (await api.Configuration()).configuration!;
    config = (
      await api.UpdateConfiguration({ revision: config.revision, min_width: 5000, max_depth: 4 })
    ).configuration!;
    config = (
      await api.AddFolderSettings({
        revision: config.revision,
        path: '/Demo/New folder',
        enabled: false,
      })
    ).configuration!;
    const folderID = config.folders[0].id;
    expect(config.folders[0]).toMatchObject({
      max_depth: null,
      effective_max_depth: 4,
      image_rules: { min_width: 1, min_height: 1, ratio: 'any' },
    });
    expect((await api.Status()).status).toMatchObject({ setup_required: true, history_length: 0 });
    config = (await api.UpdateFolder({ revision: config.revision, id: folderID, enabled: true }))
      .configuration!;
    expect((await api.Status()).status?.setup_required).toBe(false);
    expect(
      (
        await api.RulePreview({
          revision: config.revision,
          rules: config.rules,
          provider_id: 'local',
          input_id: folderID,
        })
      ).preview,
    ).toMatchObject({
      inspected: 0,
      matched: 0,
      unknown: 0,
      complete: false,
      reason: 'fixture_metadata_only',
    });
    config = (await api.RemoveFolder({ revision: config.revision, id: folderID })).configuration!;
    expect((await api.Status()).status?.setup_required).toBe(true);
    config = (await api.AddSearch({ revision: config.revision, query: 'forests', enabled: false }))
      .configuration!;
    expect(config.searches[0]).toMatchObject({
      input_enabled: false,
      enabled: false,
      purity: '100',
      image_rules: { min_width: 1, min_height: 1, ratio: 'any' },
    });
    expect((await api.Status()).status).toMatchObject({ setup_required: true, history_length: 0 });
  });

  it('scopes metadata-only previews to enrolled sources or inherited inputs without inventing matches', async () => {
    const api = installFixture('populated');
    let config = (await api.Configuration()).configuration!;
    const before = await api.Status();
    const preview = (provider_id?: string, input_id?: string) =>
      api.RulePreview({ revision: config.revision, rules: config.rules, provider_id, input_id });
    expect((await preview('local', 'demo-folder')).preview).toMatchObject({
      provider_id: 'local',
      input_id: 'demo-folder',
      scope: 'source_catalog',
      matched: 0,
      inspected: 1,
      unknown: 1,
      complete: false,
      reason: 'fixture_metadata_only',
    });
    expect((await preview()).preview).toMatchObject({
      scope: 'inherited_catalog',
      inspected: 2,
      unknown: 2,
      matched: 0,
    });
    config = (
      await api.UpdateFolder({
        revision: config.revision,
        id: 'demo-folder',
        image_rules: ownRules,
      })
    ).configuration!;
    expect((await preview()).preview).toMatchObject({
      scope: 'inherited_catalog',
      inspected: 1,
      unknown: 1,
    });
    expect((await preview('local', 'not-saved')).preview).toMatchObject({
      inspected: 0,
      matched: 0,
      complete: false,
      reason: 'source_not_enabled_or_enrolled',
    });
    config = (
      await api.UpdateFolder({ revision: config.revision, id: 'demo-folder', enabled: false })
    ).configuration!;
    expect((await preview('local', 'demo-folder')).preview).toMatchObject({
      inspected: 0,
      unknown: 0,
      reason: 'source_not_enabled_or_enrolled',
    });
    expect((await api.Status()).status?.current).toEqual(before.status?.current);
    expect((await api.Status()).status?.history_length).toEqual(before.status?.history_length);
    expect((await preview('local')).code).toBe('invalid_request');
  });

  it('rejects conflicting edits without changing revision, source rules or snapshots', async () => {
    const api = installFixture('populated');
    const before = (await api.Configuration()).configuration!;
    expect(
      (
        await api.UpdateFolder({
          revision: before.revision,
          id: 'demo-folder',
          image_rules: ownRules,
          inherit_rules: true,
        })
      ).code,
    ).toBe('invalid_settings');
    expect(
      (
        await api.UpdateSearch({
          revision: before.revision,
          id: 'demo-search',
          inherit_rules: false,
        })
      ).code,
    ).toBe('invalid_settings');
    expect(
      (await api.UpdateFolder({ revision: 'old', id: 'demo-folder', image_rules: ownRules })).code,
    ).toBe('config_conflict');
    expect((await api.RulePreview({ revision: 'old', rules: before.rules })).code).toBe(
      'config_conflict',
    );
    expect((await api.Configuration()).configuration).toEqual(before);
    before.folders[0].effective_rules!.ratio_presets.push('1:1');
    expect(
      (await api.Configuration()).configuration?.folders[0].effective_rules?.ratio_presets,
    ).toEqual([]);
  });
});

it('supports keyboard Exclude, Undo and History Restore with only its simulated state', async () => {
  vi.spyOn(globalThis, 'fetch').mockImplementation(
    async () => new Response(new Uint8Array([1, 2, 3])),
  );
  const api = installFixture('populated'),
    user = userEvent.setup();
  const action = vi.spyOn(api, 'Action'),
    exclude = vi.spyOn(api, 'ExcludeCurrent'),
    restore = vi.spyOn(api, 'RestoreExclusion'),
    replay = vi.spyOn(api, 'ReplayHistory');
  const saved = vi.spyOn(api, 'UpdateConfiguration');
  const original = await api.Status();
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <App />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
  await waitFor(() => expect(screen.getByRole('button', { name: 'Exclude image' })).toBeEnabled());
  screen.getByRole('button', { name: 'Exclude image' }).focus();
  await user.keyboard('{Enter}');
  await waitFor(() => expect(screen.getByLabelText('Image actions')).toHaveFocus());
  expect(screen.queryByRole('button', { name: 'Undo last exclusion' })).not.toBeInTheDocument();
  const nav = within(screen.getByRole('navigation', { name: 'Main navigation' }));
  nav.getByRole('button', { name: 'History' }).focus();
  await user.keyboard('{Enter}');
  expect(await screen.findByRole('button', { name: 'Display again: sunset.jpg' })).toBeDisabled();
  screen.getByRole('button', { name: 'Excluded' }).focus();
  await user.keyboard('{Enter}');
  const button = await screen.findByRole('button', { name: 'Restore to rotation: sunset.jpg' });
  await waitFor(() => expect(button).toBeEnabled());
  button.focus();
  await user.keyboard('{Enter}');
  await screen.findByText('No excluded images. Images you exclude from Now will appear here.');
  expect(action).not.toHaveBeenCalled();
  expect(exclude).toHaveBeenCalledExactlyOnceWith(
    original.status?.instance_id,
    original.status?.current?.id,
    original.status?.current?.image_id,
  );
  expect(restore).toHaveBeenCalledOnce();
  expect(replay).not.toHaveBeenCalled();
  expect(saved).not.toHaveBeenCalled();
  const after = await api.Status();
  expect(after.status?.current).toEqual(original.status?.current);
  expect(after.status?.history_length).toEqual(original.status?.history_length);
  expect(after.status?.paused).toEqual(original.status?.paused);
});
