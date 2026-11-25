import { useEffect } from 'react';
import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, it } from 'vitest';
import { ClientProvider, usePanora } from '../client';
import { OnboardingProvider } from '../onboarding/Onboarding';
import { configuration, installBridge } from '../test/fixtures';
import type { Configuration, ProviderEditorDescription } from '../types';
import { SourcesScreen } from './SourcesScreen';
import first from '../../../../docs/providers/examples/editor/fixture-images.json';
import second from '../../../../docs/providers/examples/editor/fixture-album.json';

function Harness() {
  const { navigate, reload } = usePanora();
  useEffect(() => navigate('providers'), [navigate]);
  return (
    <OnboardingProvider>
      <button onClick={() => void reload()}>Review configuration</button>
      <SourcesScreen />
    </OnboardingProvider>
  );
}

const rule = { min_width: 5120, min_height: 1440, ratio: '32:9', ratio_presets: [] };
const providers = [
  first,
  second,
  {
    id: 'invalid-fixture',
    name: 'Broken fixture',
    editor_error: 'Install a provider with editor version 1.',
  },
] as ProviderEditorDescription[];

it('opens two manifest layouts and isolates an unsupported provider editor', async () => {
  const api = installBridge();
  const user = userEvent.setup();
  let config: Configuration = {
    ...structuredClone(configuration),
    rules: { ...rule, max_depth: 3 },
    provider_editors: providers,
    inputs: [],
  };
  api.Configuration.mockImplementation(async () => ({ configuration: structuredClone(config) }));
  api.AddProviderInput.mockImplementation(async (edit) => {
    config = {
      ...config,
      revision: 'fixture-saved',
      inputs: [
        {
          id: 'fixture-input',
          provider_id: edit.provider_id,
          name: edit.name!,
          input_enabled: true,
          enabled: true,
          values: edit.values!,
          image_rules: edit.image_rules,
          effective_rules: edit.image_rules || rule,
        },
      ],
    };
    return { configuration: structuredClone(config) };
  });
  api.SuggestProviderInput.mockResolvedValueOnce({ values: ['forest'] });

  render(
    <ClientProvider>
      <Harness />
    </ClientProvider>,
  );
  const trigger = screen.getByRole('button', { name: 'Add source options' });
  await waitFor(() => expect(trigger).toBeEnabled());
  const add = async (name: string) => {
    await user.click(trigger);
    await user.click(screen.getByRole('button', { name }));
    return within(screen.getByRole('region', { name: 'Source editor' }));
  };
  let editor = await add('Broken fixture');
  expect(editor.getByRole('alert')).toHaveTextContent('Install a provider with editor version 1.');
  await user.click(editor.getByRole('button', { name: 'Close' }));

  editor = await add('Fixture album');
  expect(editor.getByRole('group', { name: 'Presentation preferences' })).toBeVisible();
  expect(editor.queryByRole('spinbutton', { name: 'Album size' })).toBeNull();
  expect(editor.getByRole('textbox', { name: 'Album directory' })).toBeVisible();
  await user.click(editor.getByRole('checkbox', { name: 'Shuffle order' }));
  expect(editor.getByRole('spinbutton', { name: 'Album size' })).toHaveValue(12);
  await user.click(editor.getByRole('button', { name: 'Cancel' }));
  expect(api.AddProviderInput).not.toHaveBeenCalled();

  editor = await add('Fixture images');
  expect(editor.getByRole('group', { name: 'Location' })).toBeVisible();
  expect(editor.queryByRole('textbox', { name: 'Featured caption' })).toBeNull();
  await user.type(editor.getByRole('textbox', { name: 'Image directory' }), '/photos');
  await user.selectOptions(editor.getByRole('combobox', { name: 'Album' }), 'featured');
  await user.type(editor.getByRole('textbox', { name: 'Featured caption' }), 'Clouds');
  await user.click(editor.getByRole('checkbox', { name: /send matching input prefixes/i }));
  const tag = editor.getByRole('combobox', { name: 'Tag' });
  await user.type(tag, 'fo');
  await editor.findByRole('option', { name: 'forest' });
  await user.keyboard('{ArrowDown}{Enter}');
  expect(editor.getByRole('button', { name: 'Remove forest' })).toBeVisible();
  expect(api.SuggestProviderInput).toHaveBeenCalledWith({
    provider_id: 'fixture-images',
    field: 'tags',
    prefix: 'fo',
    limit: 10,
  });
  let finishStale: (value: { values: string[] }) => void = () => {};
  api.SuggestProviderInput.mockReturnValueOnce(
    new Promise((resolve) => {
      finishStale = resolve;
    }),
  );
  await user.type(tag, 'old');
  await waitFor(() =>
    expect(api.SuggestProviderInput).toHaveBeenLastCalledWith(
      expect.objectContaining({ prefix: 'old' }),
    ),
  );
  await user.clear(tag);
  api.SuggestProviderInput.mockResolvedValueOnce({ values: ['lake'] });
  await user.type(tag, 'la');
  await editor.findByRole('option', { name: 'lake' });
  await act(async () => finishStale({ values: ['old response'] }));
  expect(editor.getByRole('option', { name: 'lake' })).toBeVisible();
  await user.keyboard('{Escape}');
  expect(tag).toHaveFocus();
  expect(editor.queryByRole('listbox')).toBeNull();
  await user.keyboard('{Enter}');
  expect(editor.getByRole('button', { name: 'Remove la' })).toBeVisible();

  const mode = editor.getByRole('combobox', { name: 'Filter settings' });
  expect(mode).toHaveValue('inherit');
  await user.selectOptions(mode, 'custom');
  await user.clear(editor.getByRole('spinbutton', { name: 'Minimum width (pixels)' }));
  await user.type(editor.getByRole('spinbutton', { name: 'Minimum width (pixels)' }), '3840');
  await user.click(editor.getByRole('button', { name: 'Add source' }));
  expect(api.AddProviderInput).toHaveBeenCalledWith(
    expect.objectContaining({
      provider_id: 'fixture-images',
      values: { root: '/photos', album: 'featured', caption: 'Clouds', tags: ['forest', 'la'] },
      image_rules: { ...rule, min_width: 3840 },
    }),
  );
  await user.click(
    await screen.findByRole('button', { name: 'Edit Fixture images Fixture images' }),
  );
  editor = within(screen.getByRole('region', { name: 'Source editor' }));
  expect(editor.getByRole('spinbutton', { name: 'Minimum width (pixels)' })).toHaveValue(3840);
  expect(editor.getByRole('textbox', { name: 'Featured caption' })).toHaveValue('Clouds');
  await user.selectOptions(editor.getByRole('combobox', { name: 'Filter settings' }), 'inherit');
  await user.click(editor.getByRole('button', { name: 'Save' }));
  expect(api.UpdateProviderInput).toHaveBeenCalledWith(
    expect.objectContaining({
      provider_id: 'fixture-images',
      id: 'fixture-input',
      revision: 'fixture-saved',
      inherit_rules: true,
    }),
  );
});
