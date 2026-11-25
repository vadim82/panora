import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { expect, test, vi } from 'vitest';
import { App } from './App';
import { ClientProvider } from './client';
import { ImageSettingsProvider } from './settings/ImageSettings';
import { configuration, installBridge, snapshot } from './test/fixtures';

test('About opens from the keyboard while stopped and copies the exact package version', async () => {
  const api = installBridge();
  const user = userEvent.setup();
  const clipboard = vi.fn(async () => true);
  Object.defineProperty(window, 'runtime', {
    configurable: true,
    value: { ClipboardSetText: clipboard },
  });
  api.Status.mockResolvedValue({ connected: false, code: 'service_unavailable' });
  api.BuildIdentity.mockResolvedValue({
    version: '0.6.2',
    arch_package: 'r180.29d3cf859c4b-1',
    revision: '29d3cf859c4b',
    development: false,
  });
  try {
    render(
      <ClientProvider>
        <ImageSettingsProvider>
          <App />
        </ImageSettingsProvider>
      </ClientProvider>,
    );
    await screen.findByRole('alert');
    screen.getByRole('button', { name: 'Settings' }).focus();
    await user.keyboard('{Enter}');
    await user.tab();
    expect(screen.getByRole('button', { name: 'About Panora' })).toHaveFocus();
    await user.keyboard('{Enter}');
    expect(screen.getByRole('heading', { name: 'About Panora' })).toHaveFocus();
    expect(await screen.findByText('r180.29d3cf859c4b-1')).toBeVisible();
    expect(screen.getByText('0.6.2')).toBeVisible();
    expect(screen.queryByText('Development build')).toBeNull();
    await user.click(screen.getByRole('button', { name: 'Copy version details' }));
    expect(clipboard).toHaveBeenCalledWith(
      'Panora\nVersion: 0.6.2\nArch package: r180.29d3cf859c4b-1\nSource revision: 29d3cf859c4b',
    );
    expect(await screen.findByText('Version details copied.')).toBeVisible();
    expect(api.ServiceCommand).not.toHaveBeenCalled();
  } finally {
    Reflect.deleteProperty(window, 'runtime');
  }
});

test('cold and warm native About launches use the same development identity', async () => {
  const api = installBridge();
  const user = userEvent.setup();
  api.TakeAboutRequest.mockResolvedValueOnce(true);
  api.BuildIdentity.mockResolvedValue({ development: true, revision: '29d3cf859c4b (modified)' });
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <App />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
  expect(await screen.findByRole('heading', { name: 'About Panora' })).toHaveFocus();
  expect(await screen.findByText('Development build')).toBeVisible();
  expect(screen.getByText('29d3cf859c4b (modified)')).toBeVisible();
  await user.click(screen.getByRole('button', { name: 'Back to Settings' }));
  api.TakeAboutRequest.mockResolvedValueOnce(true);
  act(() => {
    window.dispatchEvent(new Event('panora:launch-about'));
  });
  expect(await screen.findByRole('heading', { name: 'About Panora' })).toHaveFocus();
  await user.click(screen.getByRole('button', { name: 'Back to Settings' }));
  act(() => {
    window.dispatchEvent(new Event('panora:open-about'));
  });
  expect(screen.getByRole('heading', { name: 'About Panora' })).toHaveFocus();
});

test('the native Settings command opens Settings and focuses its heading', () => {
  const api = installBridge();
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <App />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
  act(() => {
    window.dispatchEvent(new Event('panora:open-settings'));
  });
  expect(screen.getByRole('heading', { name: 'Settings', level: 1 })).toHaveFocus();
  expect(api.Action).not.toHaveBeenCalled();
});

test('keyboard navigation reaches source setup, cancels its draft and enters settings without changing wallpapers', async () => {
  const api = installBridge(),
    user = userEvent.setup();
  api.PickFolder.mockResolvedValue({ name: 'Keyboard folder', path: '/Pictures/Keyboard folder' });
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <App />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
  await waitFor(() => expect(screen.getByRole('button', { name: 'Next wallpaper' })).toBeEnabled());
  await user.tab();
  expect(screen.getByRole('button', { name: 'Now' })).toHaveFocus();
  await user.tab();
  expect(screen.getByRole('button', { name: 'Sources' })).toHaveFocus();
  await user.keyboard('{Enter}');
  expect(screen.getByRole('heading', { name: 'Sources', level: 1 })).toHaveFocus();
  await user.tab();
  expect(screen.getByRole('button', { name: 'Add source options' })).toHaveFocus();
  await user.keyboard('{Enter}');
  expect(screen.getByRole('button', { name: 'Add source options' })).toHaveAttribute(
    'aria-expanded',
    'true',
  );
  await user.tab();
  expect(screen.getByRole('button', { name: 'Folder' })).toHaveFocus();
  await user.keyboard('{Enter}');
  expect(screen.getByRole('textbox', { name: 'Folder path' })).toHaveFocus();
  await user.tab();
  expect(screen.getByRole('button', { name: 'Browse…' })).toHaveFocus();
  await user.keyboard('{Enter}');
  await waitFor(() =>
    expect(screen.getByRole('textbox', { name: 'Folder path' })).not.toHaveValue(''),
  );
  await user.keyboard('{Escape}');
  expect(screen.getByRole('button', { name: 'Keep editing' })).toHaveFocus();
  const discard = screen.getByRole('button', { name: 'Discard changes' });
  await user.tab();
  expect(discard).toHaveFocus();
  await user.keyboard('{Enter}');
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add source options' })).toHaveFocus(),
  );
  await user.tab({ shift: true });
  expect(screen.getByRole('button', { name: 'Settings' })).toHaveFocus();
  await user.keyboard('{Enter}');
  expect(screen.getByRole('heading', { name: 'Settings', level: 1 })).toHaveFocus();
  await user.tab();
  expect(screen.getByRole('button', { name: 'About Panora' })).toHaveFocus();
  await user.tab();
  expect(screen.getByText('Background service', { selector: 'summary' })).toHaveFocus();
  expect(api.PickFolder).toHaveBeenCalledOnce();
  expect(api.AddFolderSettings).not.toHaveBeenCalled();
  expect(api.UpdateConfiguration).not.toHaveBeenCalled();
  expect(api.Action).not.toHaveBeenCalled();
});

test('opens picture fit from the wallpaper menu and frames the current image without applying it', async () => {
  const api = installBridge();
  const original = await api.Status();
  api.Status.mockResolvedValue({
    ...original,
    status: {
      ...original.status,
      warnings: [
        'Skipping /Pictures/old.png: filter: decoded image does not match current image rules',
        'local: filter: decoded image does not match current image rules',
      ],
    },
  });
  const user = userEvent.setup();
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <App />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
  await screen.findByRole('img', { name: 'Wallpaper: coast.jpg' });
  expect(screen.queryByText(/does not match current image rules/)).not.toBeInTheDocument();
  await user.click(screen.getByLabelText('Image actions'));
  await user.click(screen.getByRole('button', { name: 'Picture settings' }));
  await waitFor(() => expect(screen.getByRole('heading', { name: 'Picture fit' })).toHaveFocus());
  const framed = await screen.findByRole('img', { name: 'Estimated wallpaper placement' });
  const fit = screen.getByRole('combobox', { name: 'Fit' });
  await user.selectOptions(fit, 'auto');
  expect(Number.parseFloat(framed.style.width)).toBeCloseTo((1920 / 3024) * 100);
  await user.selectOptions(fit, 'crop');
  expect(framed).toHaveStyle({ objectFit: 'cover' });
  expect(api.Action).not.toHaveBeenCalled();
  expect(api.UpdateConfiguration).not.toHaveBeenCalled();
});

test('the complete app preserves image drafts across navigation and a folder save with a new revision', async () => {
  const api = installBridge();
  const saved = structuredClone(configuration);
  saved.revision = 'folder-saved';
  saved.folders.push({ ...saved.folders[0], id: 'picked', name: 'Picked', path: '/Picked' });
  api.PickFolder.mockResolvedValue({ name: 'Picked', path: '/Picked' });
  api.AddFolderSettings.mockImplementation(async () => {
    api.Configuration.mockResolvedValue({ configuration: saved });
    return { configuration: saved };
  });
  const user = userEvent.setup();
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <App />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
  const nav = within(screen.getByRole('navigation', { name: 'Main navigation' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Next wallpaper' })).toBeEnabled());
  await user.click(screen.getByRole('button', { name: 'Settings' }));
  expect(screen.getByRole('heading', { name: 'Settings', level: 1 })).toHaveFocus();
  await user.click(screen.getByText('Image filters', { selector: 'summary' }));
  await user.selectOptions(screen.getByLabelText('Minimum resolution'), 'custom');
  const width = screen.getByRole('spinbutton', { name: 'Minimum width (pixels)' });
  await user.clear(width);
  await user.type(width, '2048');
  await user.click(nav.getByRole('button', { name: 'Sources' }));
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Add source options' })).toBeEnabled(),
  );
  await user.click(screen.getByRole('button', { name: 'Add source options' }));
  await user.click(screen.getByRole('button', { name: 'Folder' }));
  await user.click(screen.getByRole('button', { name: 'Browse…' }));
  await user.click(await screen.findByRole('button', { name: 'Add source' }));
  await waitFor(() => expect(api.AddFolderSettings).toHaveBeenCalledOnce());
  await waitFor(() =>
    expect(screen.queryByRole('button', { name: 'Add source' })).not.toBeInTheDocument(),
  );
  await user.click(screen.getByRole('button', { name: 'Settings' }));
  expect(screen.getByRole('spinbutton', { name: 'Minimum width (pixels)' })).toHaveValue(2048);
  const form = document.getElementById('rules-form')!;
  await user.click(within(form).getByRole('button', { name: 'Save changes' }));
  await waitFor(() =>
    expect(api.UpdateConfiguration).toHaveBeenCalledWith({
      revision: 'folder-saved',
      min_width: 2048,
    }),
  );
  await user.click(nav.getByRole('button', { name: 'History' }));
  expect(screen.getByRole('heading', { name: 'History', level: 1 })).toHaveFocus();
  expect(await screen.findByRole('button', { name: 'Display again: coast.jpg' })).toBeEnabled();
});

test('the global settings recovery action explicitly reloads a failed initial read and unblocks saving', async () => {
  const api = installBridge(),
    user = userEvent.setup();
  api.Configuration.mockResolvedValue({
    code: 'config_invalid',
    error: 'Saved document could not be read.',
  });
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <App />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
  await waitFor(() =>
    expect(screen.getByRole('button', { name: 'Reload settings' })).toBeEnabled(),
  );
  expect(screen.getByRole('alert')).toHaveTextContent('Settings could not be loaded');
  expect(screen.queryByRole('button', { name: 'Refresh' })).not.toBeInTheDocument();
  api.Configuration.mockResolvedValue({ configuration: structuredClone(configuration) });
  await user.click(screen.getByRole('button', { name: 'Reload settings' }));
  await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
  await waitFor(() =>
    expect(screen.getByRole('heading', { name: 'Wallpaper', level: 1 })).toHaveFocus(),
  );
  await user.click(screen.getByRole('button', { name: 'Settings' }));
  await user.click(screen.getByText('Image filters', { selector: 'summary' }));
  await user.selectOptions(screen.getByLabelText('Minimum resolution'), 'custom');
  const width = screen.getByRole('spinbutton', { name: 'Minimum width (pixels)' });
  await user.clear(width);
  await user.type(width, '2048');
  const updated = structuredClone(configuration);
  updated.revision = 'saved-after-recovery';
  updated.rules.min_width = 2048;
  api.UpdateConfiguration.mockResolvedValue({ configuration: updated });
  await user.click(
    within(document.getElementById('rules-form')!).getByRole('button', { name: 'Save changes' }),
  );
  await waitFor(() =>
    expect(api.UpdateConfiguration).toHaveBeenCalledWith({ revision: 'reviewed', min_width: 2048 }),
  );
  expect(api.Action).not.toHaveBeenCalled();
});

test.each(['same page', 'another page'])(
  'a late global settings reload preserves focus moved to %s',
  async (destination) => {
    const api = installBridge(),
      user = userEvent.setup();
    api.Configuration.mockResolvedValue({ error: 'Saved document could not be read.' });
    render(
      <ClientProvider>
        <ImageSettingsProvider>
          <App />
        </ImageSettingsProvider>
      </ClientProvider>,
    );
    const recovery = await screen.findByRole('button', { name: 'Reload settings' });
    await waitFor(() => expect(recovery).toBeEnabled());
    let finish!: (value: { configuration: typeof configuration }) => void;
    api.Configuration.mockResolvedValue({ configuration: structuredClone(configuration) });
    api.Configuration.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    recovery.focus();
    await user.keyboard('{Enter}');
    const target = screen.getByRole('button', { name: 'Settings' });
    if (destination === 'another page') await user.click(target);
    else target.focus();
    const expected = document.activeElement;
    await act(async () => finish({ configuration: structuredClone(configuration) }));
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(expected).toHaveFocus();
    expect(api.UpdateConfiguration).not.toHaveBeenCalled();
    expect(api.Action).not.toHaveBeenCalled();
  },
);

test('keyboard reconnect keeps its retry focus on failure and reaches the page heading after recovery', async () => {
  const api = installBridge(),
    user = userEvent.setup();
  api.Status.mockResolvedValue({ connected: false, code: 'service_unavailable' });
  render(
    <ClientProvider>
      <ImageSettingsProvider>
        <App />
      </ImageSettingsProvider>
    </ClientProvider>,
  );
  const reconnect = await screen.findByRole('button', { name: 'Reconnect' });
  await waitFor(() => expect(reconnect).toBeEnabled());
  reconnect.focus();
  await user.keyboard('{Enter}');
  await waitFor(() => expect(reconnect).toBeEnabled());
  expect(reconnect).toHaveFocus();
  api.Status.mockResolvedValue(structuredClone(snapshot));
  await user.keyboard('{Enter}');
  await waitFor(() =>
    expect(screen.getByRole('heading', { name: 'Wallpaper', level: 1 })).toHaveFocus(),
  );
  expect(screen.queryByRole('button', { name: 'Reconnect' })).not.toBeInTheDocument();
  expect(api.Action).not.toHaveBeenCalled();
  expect(api.UpdateConfiguration).not.toHaveBeenCalled();
});
