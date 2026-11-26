import { useEffect } from 'react';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, it } from 'vitest';
import { ClientProvider, usePanora } from '../client';
import { OnboardingProvider } from '../onboarding/Onboarding';
import { installBridge } from '../test/fixtures';
import { SourcesScreen } from './SourcesScreen';

function Harness() {
  const { navigate } = usePanora();
  useEffect(() => navigate('providers'), [navigate]);
  return (
    <OnboardingProvider>
      <SourcesScreen />
    </OnboardingProvider>
  );
}
async function ready() {
  render(
    <ClientProvider>
      <Harness />
    </ClientProvider>,
  );
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add source options' })).toBeEnabled(),
  );
  return userEvent.setup();
}

it('identifies and focuses an invalid manual folder path while keeping the draft', async () => {
  const api = installBridge(),
    user = await ready();
  await user.click(screen.getByRole('button', { name: 'Add source options' }));
  await user.click(screen.getByRole('button', { name: 'Folder' }));
  const path = screen.getByRole('textbox', { name: 'Folder path' });
  await user.type(path, 'Pictures/keep this ');
  await user.click(screen.getByRole('button', { name: 'Add source' }));
  await waitFor(() => expect(path).toHaveFocus());
  expect(path).toHaveAttribute('aria-invalid', 'true');
  expect(path).toHaveAccessibleDescription('Enter an absolute folder path starting with /.');
  expect(path).toHaveValue('Pictures/keep this ');
  expect(api.AddFolderSettings).not.toHaveBeenCalled();
  await user.clear(path);
  await user.type(path, '/Pictures/keep this ');
  expect(path).not.toHaveAttribute('aria-invalid', 'true');
  expect(api.Action).not.toHaveBeenCalled();
});

it('reveals and focuses invalid folder depth hidden in a collapsed disclosure', async () => {
  const api = installBridge(),
    user = await ready();
  await user.click(screen.getByRole('button', { name: /^Edit Folder Wallpapers/ }));
  await user.click(screen.getByText('Folder options', { selector: 'summary' }));
  const depth = screen.getByRole('spinbutton', { name: 'Subfolder depth' });
  await user.clear(depth);
  await user.type(depth, '-1');
  await user.click(screen.getByText('Folder options', { selector: 'summary' }));
  await user.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(depth).toHaveFocus());
  expect(depth).toBeVisible();
  expect(depth).toHaveAttribute('aria-invalid', 'true');
  expect(depth).toHaveAccessibleDescription('Use a whole subfolder depth from 0 to 2147483647.');
  expect(api.UpdateFolder).not.toHaveBeenCalled();
});

it('accepts empty terms and filters through the original opt-in flow', async () => {
  const api = installBridge(),
    user = await ready();
  await user.click(screen.getByRole('button', { name: 'Add source options' }));
  await user.click(screen.getByRole('button', { name: 'Wallhaven search' }));
  expect(screen.getByRole('textbox', { name: 'Search terms (optional)' })).toHaveValue('');
  await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
  await waitFor(() =>
    expect(api.AddSearch).toHaveBeenCalledWith(
      expect.objectContaining({
        query: '',
        categories: '000',
        tags: [],
        colors: [],
      }),
    ),
  );
  expect(api.Action).not.toHaveBeenCalled();
});

it('allows removing the final category and keeps optional terms', async () => {
  const api = installBridge(),
    user = await ready();
  await user.click(screen.getByRole('button', { name: 'Add source options' }));
  await user.click(screen.getByRole('button', { name: 'Wallhaven search' }));
  await user.type(
    screen.getByRole('textbox', { name: 'Search terms (optional)' }),
    'keep mountains',
  );
  await user.click(screen.getByRole('button', { name: 'Filters' }));
  const general = screen.getByRole('checkbox', { name: 'General' });
  const anime = screen.getByRole('checkbox', { name: 'Anime' });
  await user.click(general);
  await user.click(anime);
  await user.click(anime);
  await user.click(general);
  expect(general).not.toBeChecked();
  await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
  await waitFor(() =>
    expect(api.AddSearch).toHaveBeenCalledWith(
      expect.objectContaining({
        categories: '000',
        query: 'keep mountains',
      }),
    ),
  );
});

it('blocks saving an empty source ratio selection and names its invalid group', async () => {
  const api = installBridge(),
    user = await ready();
  await user.click(screen.getByRole('button', { name: /^Edit Folder Wallpapers/ }));
  await user.selectOptions(screen.getByRole('combobox', { name: 'Filter settings' }), 'custom');
  await user.selectOptions(screen.getByRole('combobox', { name: 'Aspect ratio' }), 'presets');
  const group = screen.getByRole('group', { name: 'Choose one or more ratios' });
  await user.click(within(group).getByRole('checkbox', { name: '16:9' }));
  await user.click(screen.getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(group).toHaveFocus());
  expect(group).toHaveAttribute('aria-invalid', 'true');
  expect(group).toHaveAccessibleDescription('Choose at least one aspect ratio.');
  expect(api.UpdateFolder).not.toHaveBeenCalled();
  await user.click(within(group).getByRole('checkbox', { name: '32:9' }));
  expect(group).not.toHaveAttribute('aria-invalid', 'true');
  expect(api.Action).not.toHaveBeenCalled();
});

it('points preview validation at its invalid source dimension instead of leaving focus on Preview', async () => {
  const api = installBridge(),
    user = await ready();
  await user.click(screen.getByRole('button', { name: /^Edit Folder Wallpapers/ }));
  await user.selectOptions(screen.getByRole('combobox', { name: 'Filter settings' }), 'custom');
  const width = screen.getByRole('spinbutton', { name: 'Minimum width (pixels)' });
  await user.clear(width);
  await user.type(width, '0');
  let descriptionAtFocus = '';
  width.addEventListener('focus', () => {
    descriptionAtFocus = (width.getAttribute('aria-describedby') || '')
      .split(' ')
      .map((id) => document.getElementById(id)?.textContent || '')
      .join(' ');
  });
  await user.click(screen.getByRole('button', { name: 'Preview source matches' }));
  await waitFor(() => expect(width).toHaveFocus());
  expect(descriptionAtFocus).toBe('Enter a positive whole pixel dimension.');
  expect(width).toHaveAttribute('aria-invalid', 'true');
  expect(width).toHaveAccessibleDescription('Enter a positive whole pixel dimension.');
  expect(api.RulePreview).not.toHaveBeenCalled();
  expect(api.UpdateFolder).not.toHaveBeenCalled();
});

it('focuses the first invalid search option and keeps native constraint errors associated after correction', async () => {
  const api = installBridge(),
    user = await ready();
  await user.click(screen.getByRole('button', { name: 'Add source options' }));
  await user.click(screen.getByRole('button', { name: 'Wallhaven search' }));
  await user.type(screen.getByRole('textbox', { name: 'Search terms (optional)' }), 'mountains');
  await user.click(screen.getByRole('button', { name: 'More settings' }));
  const pages = screen.getByRole('spinbutton', { name: 'Pages per refresh' });
  const refresh = screen.getByRole('textbox', { name: 'Refresh interval' });
  await user.clear(pages);
  await user.clear(refresh);
  await user.click(screen.getByRole('button', { name: 'More settings' }));
  await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
  await waitFor(() => expect(pages).toHaveFocus());
  expect(pages).toHaveAccessibleDescription('Use a whole number from 1 to 100 pages per refresh.');
  expect(refresh).toHaveAccessibleDescription('Enter a refresh interval such as 5m.');
  await user.type(pages, '3');
  await user.click(screen.getByRole('button', { name: 'Enable Wallhaven and add source' }));
  await waitFor(() => expect(refresh).toHaveFocus());
  expect(pages).not.toHaveAttribute('aria-invalid', 'true');
  expect(api.AddSearch).not.toHaveBeenCalled();
  expect(api.Action).not.toHaveBeenCalled();
});
